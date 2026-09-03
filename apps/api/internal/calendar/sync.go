package calendar

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"time"
)

type SyncResult struct {
	BusySpanCount int       `json:"busySpanCount"`
	LastSyncedAt  time.Time `json:"lastSyncedAt"`
}

type SyncFailure struct {
	Code       string
	Status     int
	PublicText string
	Cause      error
}

func (value *SyncFailure) Error() string { return value.Code }
func (value *SyncFailure) Unwrap() error { return value.Cause }

func (handler *Handler) SyncUser(ctx context.Context, userID string) (SyncResult, error) {
	if !handler.provider.Configured() || handler.cipher == nil {
		return SyncResult{}, syncFailure("not_configured", 503, "google calendar is not configured", nil)
	}
	connection, err := handler.store.GetConnection(ctx, userID)
	if errors.Is(err, ErrNotFound) {
		return SyncResult{}, syncFailure("connection_required", 409, "calendar connection required", err)
	}
	if err != nil {
		return SyncResult{}, syncFailure("connection_load_failed", 500, "unable to sync calendar", err)
	}
	refresh, err := handler.cipher.Decrypt(connection.RefreshTokenCipher)
	if err != nil {
		handler.markFailure(ctx, connection, "grant_decrypt_failed", true)
		return SyncResult{}, syncFailure("grant_decrypt_failed", 500, "calendar reconnect required", err)
	}
	tokens, err := handler.provider.Refresh(ctx, refresh)
	if err != nil {
		reconnect := errors.Is(err, ErrReconnectRequired)
		handler.markFailure(ctx, connection, failureCode(err), reconnect)
		if reconnect {
			_ = handler.store.MarkReconnectRequired(ctx, userID)
			return SyncResult{}, syncFailure("reconnect_required", 409, "calendar reconnect required", err)
		}
		return SyncResult{}, syncFailure("token_refresh_failed", 502, "unable to refresh calendar permission", err)
	}

	now := handler.now().UTC()
	from, to := now.Add(-handler.config.SyncPast), now.Add(handler.config.SyncFuture)
	count := 0
	nextToken := connection.SyncToken
	backgroundStore, backgroundOK := handler.store.(BackgroundStore)
	incrementalProvider, incrementalOK := handler.provider.(IncrementalProvider)
	if backgroundOK && incrementalOK {
		changes, changeErr := incrementalProvider.ListChanges(ctx, tokens.AccessToken, connection.SyncToken, from, to)
		if errors.Is(changeErr, ErrSyncTokenExpired) {
			changes, changeErr = incrementalProvider.ListChanges(ctx, tokens.AccessToken, "", from, to)
		}
		if changeErr != nil {
			reconnect := errors.Is(changeErr, ErrReconnectRequired)
			handler.markFailure(ctx, connection, failureCode(changeErr), reconnect)
			if reconnect {
				_ = handler.store.MarkReconnectRequired(ctx, userID)
				return SyncResult{}, syncFailure("reconnect_required", 409, "calendar reconnect required", changeErr)
			}
			return SyncResult{}, syncFailure("calendar_read_failed", 502, "unable to read calendar", changeErr)
		}
		if err := backgroundStore.ApplyChanges(ctx, userID, changes, from, to, now); err != nil {
			handler.markFailure(ctx, connection, "event_store_failed", false)
			return SyncResult{}, syncFailure("event_store_failed", 500, "unable to store calendar sync", err)
		}
		count = len(changes.Upserts)
		nextToken = changes.NextSyncToken
	} else {
		spans, listErr := handler.provider.ListBusy(ctx, tokens.AccessToken, from, to)
		if listErr != nil {
			return SyncResult{}, syncFailure("calendar_read_failed", 502, "unable to read calendar", listErr)
		}
		if err := handler.store.ReplaceBusySpans(ctx, userID, spans, from, to, now); err != nil {
			return SyncResult{}, syncFailure("event_store_failed", 500, "unable to store calendar sync", err)
		}
		count = len(spans)
	}

	if handler.projector == nil {
		handler.markFailure(ctx, connection, "projection_unconfigured", false)
		return SyncResult{}, syncFailure("projection_unconfigured", 500, "unable to rebuild public availability", nil)
	}
	if err := handler.projector.Rebuild(ctx, userID, from, to, now); err != nil {
		handler.markFailure(ctx, connection, "projection_rebuild_failed", false)
		return SyncResult{}, syncFailure("projection_rebuild_failed", 500, "unable to rebuild public availability", err)
	}
	if backgroundOK {
		if err := backgroundStore.MarkSyncSuccess(ctx, userID, nextToken, now, now.Add(defaultSyncInterval)); err != nil {
			return SyncResult{}, syncFailure("completion_store_failed", 500, "unable to complete calendar sync", err)
		}
	} else if err := handler.store.MarkSynced(ctx, userID, now); err != nil {
		return SyncResult{}, syncFailure("completion_store_failed", 500, "unable to complete calendar sync", err)
	}
	return SyncResult{BusySpanCount: count, LastSyncedAt: now}, nil
}

func (handler *Handler) markFailure(ctx context.Context, connection Connection, code string, reconnect bool) {
	store, ok := handler.store.(BackgroundStore)
	if !ok {
		return
	}
	next := handler.now().UTC().Add(syncBackoff(connection.UserID, connection.FailureCount))
	if reconnect {
		next = handler.now().UTC().Add(24 * time.Hour)
	}
	if err := store.MarkSyncFailure(ctx, connection.UserID, code, next, reconnect); err != nil {
		handler.logger.Error("record calendar sync failure", "failure_code", "state_store_failed")
	}
}

func failureCode(err error) string {
	switch {
	case errors.Is(err, ErrReconnectRequired):
		return "reconnect_required"
	case errors.Is(err, context.DeadlineExceeded):
		return "timeout"
	case errors.Is(err, context.Canceled):
		return "cancelled"
	default:
		return "temporary_failure"
	}
}

func syncFailure(code string, status int, publicText string, cause error) *SyncFailure {
	return &SyncFailure{Code: code, Status: status, PublicText: publicText, Cause: cause}
}

func syncBackoff(userID string, failures int) time.Duration {
	if failures < 0 {
		failures = 0
	}
	exponent := min(failures, 6)
	base := time.Minute * time.Duration(math.Pow(2, float64(exponent)))
	sum := sha256.Sum256([]byte(fmt.Sprintf("%s:%d", userID, failures)))
	jitter := time.Duration(sum[0]%21) * base / 100
	value := base + jitter
	if value > 6*time.Hour {
		return 6 * time.Hour
	}
	return value
}

type WorkerConfig struct {
	PollInterval time.Duration
	ClaimLimit   int
	ClaimLease   time.Duration
	SyncTimeout  time.Duration
}

type Worker struct {
	store  BackgroundStore
	syncer interface {
		SyncUser(context.Context, string) (SyncResult, error)
	}
	config WorkerConfig
	logger *slog.Logger
}

func NewWorker(store BackgroundStore, syncer interface {
	SyncUser(context.Context, string) (SyncResult, error)
}, config WorkerConfig, logger *slog.Logger) *Worker {
	if config.PollInterval <= 0 {
		config.PollInterval = time.Minute
	}
	if config.ClaimLimit <= 0 {
		config.ClaimLimit = 10
	}
	if config.ClaimLease <= 0 {
		config.ClaimLease = defaultClaimLease
	}
	if config.SyncTimeout <= 0 {
		config.SyncTimeout = 45 * time.Second
	}
	return &Worker{store: store, syncer: syncer, config: config, logger: logger}
}

func (worker *Worker) Run(ctx context.Context) {
	worker.runDue(ctx)
	ticker := time.NewTicker(worker.config.PollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			worker.runDue(ctx)
		}
	}
}

func (worker *Worker) runDue(ctx context.Context) {
	connections, err := worker.store.ClaimDueConnections(ctx, time.Now().UTC(), worker.config.ClaimLimit, worker.config.ClaimLease)
	if err != nil {
		worker.logger.Error("claim calendar sync work", "failure_code", "claim_failed")
		return
	}
	for _, connection := range connections {
		syncContext, cancel := context.WithTimeout(ctx, worker.config.SyncTimeout)
		_, err := worker.syncer.SyncUser(syncContext, connection.UserID)
		cancel()
		if err != nil {
			var failure *SyncFailure
			code := "temporary_failure"
			if errors.As(err, &failure) {
				code = failure.Code
			}
			worker.logger.Warn("background calendar sync incomplete", "failure_code", code)
		}
	}
}
