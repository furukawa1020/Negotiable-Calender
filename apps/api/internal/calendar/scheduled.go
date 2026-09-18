package calendar

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync/atomic"
	"time"

	"google.golang.org/api/idtoken"
)

const ScheduledSyncPath = "/internal/calendar/sync-due"

type ScheduledConfig struct{ Audience, Subject, Email string }
type BatchResult struct {
	Claimed         int  `json:"claimed"`
	Attempted       int  `json:"attempted"`
	Succeeded       int  `json:"succeeded"`
	Failed          int  `json:"failed"`
	Unprocessed     int  `json:"unprocessed"`
	CapacityReached bool `json:"capacityReached"`
}
type batchRunner interface {
	RunDue(context.Context) (BatchResult, error)
}
type tokenValidator func(context.Context, string, string) (*idtoken.Payload, error)

type ScheduledHandler struct {
	next       http.Handler
	runner     batchRunner
	config     ScheduledConfig
	configured func() bool
	validate   tokenValidator
	logger     *slog.Logger
	running    atomic.Bool
}

func NewScheduledHandler(next http.Handler, runner batchRunner, configured func() bool, config ScheduledConfig, logger *slog.Logger) *ScheduledHandler {
	return &ScheduledHandler{next: next, runner: runner, configured: configured, config: config, validate: idtoken.Validate, logger: logger}
}

func (handler *ScheduledHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != ScheduledSyncPath {
		handler.next.ServeHTTP(w, r)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	if handler.config.Audience == "" || handler.config.Subject == "" || handler.config.Email == "" {
		http.NotFound(w, r)
		return
	}
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		writeJSON(w, 405, map[string]string{"error": "POST required"})
		return
	}
	// No cookies, demo headers, provider tokens or user ID inputs confer scheduler authority.
	parts := strings.Fields(r.Header.Get("Authorization"))
	if len(r.Header.Values("Authorization")) != 1 || len(parts) != 2 || parts[0] != "Bearer" || len(parts[1]) > 8192 {
		writeJSON(w, 401, map[string]string{"error": "scheduler identity required"})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	payload, err := handler.validate(ctx, parts[1], handler.config.Audience)
	cancel()
	now := time.Now().Unix()
	if err != nil || payload == nil || payload.Audience != handler.config.Audience || payload.Subject != handler.config.Subject || payload.Issuer != "https://accounts.google.com" || payload.Expires <= now || payload.IssuedAt <= 0 || payload.IssuedAt > now+30 || payload.Claims["email"] != handler.config.Email || payload.Claims["email_verified"] != true {
		writeJSON(w, 401, map[string]string{"error": "scheduler identity required"})
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 1))
	if err != nil || len(body) != 0 || r.URL.RawQuery != "" {
		writeJSON(w, 400, map[string]string{"error": "scheduler accepts no parameters"})
		return
	}
	if !handler.running.CompareAndSwap(false, true) {
		writeJSON(w, 409, map[string]string{"error": "sync batch already running"})
		return
	}
	defer handler.running.Store(false)
	if !handler.configured() {
		handler.logger.Info("calendar sync batch", "status", "not_configured")
		writeJSON(w, 200, map[string]any{"status": "not_configured", "configured": false, "result": BatchResult{}})
		return
	}
	// All work completes before the response; nothing depends on idle Cloud Run CPU.
	ctx, cancel = context.WithTimeout(r.Context(), 40*time.Second)
	defer cancel()
	result, err := handler.runner.RunDue(ctx)
	state := "completed"
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		state = "budget_exhausted"
	} else if err != nil {
		state = "claim_failed"
	}
	handler.logger.Info("calendar sync batch", "status", state, "claimed", result.Claimed, "attempted", result.Attempted, "succeeded", result.Succeeded, "failed", result.Failed, "unprocessed", result.Unprocessed)
	code := 200
	if state == "claim_failed" {
		code = 503
	}
	writeJSON(w, code, map[string]any{"status": state, "configured": true, "result": result})
}

func (handler *Handler) Configured() bool {
	return handler.provider.Configured() && handler.cipher != nil && handler.projector != nil
}
