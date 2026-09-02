package calendar

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"testing"
	"time"
)

type backgroundStubStore struct {
	stubStore
	changes       ChangeSet
	successToken  string
	successAt     time.Time
	nextAttempt   time.Time
	failureCode   string
	reconnect     bool
	claimed       []Connection
	claimCount    int
}

func (store *backgroundStubStore) ClaimDueConnections(context.Context, time.Time, int, time.Duration) ([]Connection, error) {
	store.claimCount++
	return store.claimed, nil
}
func (store *backgroundStubStore) ApplyChanges(_ context.Context, _ string, changes ChangeSet, _, _, _ time.Time) error {
	store.changes = changes
	return nil
}
func (store *backgroundStubStore) MarkSyncSuccess(_ context.Context, _ string, token string, now, next time.Time) error {
	store.successToken, store.successAt, store.nextAttempt = token, now, next
	store.connection.SyncToken = token
	store.connection.LastSyncedAt = &now
	return nil
}
func (store *backgroundStubStore) MarkSyncFailure(_ context.Context, _ string, code string, next time.Time, reconnect bool) error {
	store.failureCode, store.nextAttempt, store.reconnect = code, next, reconnect
	return nil
}

type incrementalStubProvider struct {
	stubProvider
	cursors []string
	results []ChangeSet
	errs    []error
}

func (provider *incrementalStubProvider) ListChanges(_ context.Context, _ string, cursor string, _, _ time.Time) (ChangeSet, error) {
	provider.cursors = append(provider.cursors, cursor)
	index := len(provider.cursors) - 1
	var result ChangeSet
	if index < len(provider.results) {
		result = provider.results[index]
	}
	if index < len(provider.errs) {
		return result, provider.errs[index]
	}
	return result, nil
}

func TestSyncUserAppliesIncrementalChangesBeforeAdvancingCursor(t *testing.T) {
	t.Parallel()
	cipher := testCipher(t)
	encrypted, err := cipher.Encrypt("refresh-secret")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 9, 3, 1, 0, 0, 0, time.UTC)
	store := &backgroundStubStore{stubStore: stubStore{connection: Connection{
		UserID: "user-1", RefreshTokenCipher: encrypted, SyncToken: "sync-old",
	}}}
	provider := &incrementalStubProvider{results: []ChangeSet{{
		Upserts: []BusySpan{{ProviderEventID: "changed", StartAt: now, EndAt: now.Add(time.Hour), Busy: true}},
		DeletedProviderEventIDs: []string{"deleted"}, NextSyncToken: "sync-new",
	}}}
	projector := &stubProjector{}
	handler := NewHandler(http.NotFoundHandler(), store, provider, cipher, projector, HandlerConfig{}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	handler.now = func() time.Time { return now }

	result, err := handler.SyncUser(context.Background(), "user-1")
	if err != nil {
		t.Fatal(err)
	}
	if result.BusySpanCount != 1 || len(store.changes.Upserts) != 1 || len(store.changes.DeletedProviderEventIDs) != 1 {
		t.Fatalf("changes were not applied: %#v", store.changes)
	}
	if !projector.rebuilt || store.successToken != "sync-new" || store.successAt != now {
		t.Fatal("cursor advanced before a successful projection rebuild")
	}
	if len(provider.cursors) != 1 || provider.cursors[0] != "sync-old" {
		t.Fatalf("cursors = %#v", provider.cursors)
	}
}

func TestSyncUserFallsBackToFullSyncWhenCursorExpired(t *testing.T) {
	t.Parallel()
	cipher := testCipher(t)
	encrypted, _ := cipher.Encrypt("refresh-secret")
	store := &backgroundStubStore{stubStore: stubStore{connection: Connection{
		UserID: "user-1", RefreshTokenCipher: encrypted, SyncToken: "expired",
	}}}
	provider := &incrementalStubProvider{
		errs: []error{ErrSyncTokenExpired, nil},
		results: []ChangeSet{{}, {Full: true, NextSyncToken: "fresh"}},
	}
	handler := NewHandler(http.NotFoundHandler(), store, provider, cipher, &stubProjector{}, HandlerConfig{}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if _, err := handler.SyncUser(context.Background(), "user-1"); err != nil {
		t.Fatal(err)
	}
	if len(provider.cursors) != 2 || provider.cursors[0] != "expired" || provider.cursors[1] != "" {
		t.Fatalf("cursors = %#v", provider.cursors)
	}
	if !store.changes.Full || store.successToken != "fresh" {
		t.Fatalf("full recovery not persisted: %#v", store.changes)
	}
}

type revokedProvider struct{}

func (*revokedProvider) Configured() bool { return true }
func (*revokedProvider) AuthorizationURL(string, string) string { return "" }
func (*revokedProvider) Exchange(context.Context, string, string) (TokenSet, error) {
	return TokenSet{}, nil
}
func (*revokedProvider) Refresh(context.Context, string) (TokenSet, error) {
	return TokenSet{}, ErrReconnectRequired
}
func (*revokedProvider) ListBusy(context.Context, string, time.Time, time.Time) ([]BusySpan, error) {
	return nil, nil
}

func TestSyncUserMarksRevokedGrantForReconnect(t *testing.T) {
	t.Parallel()
	cipher := testCipher(t)
	encrypted, _ := cipher.Encrypt("refresh-secret")
	store := &backgroundStubStore{stubStore: stubStore{connection: Connection{
		UserID: "user-1", RefreshTokenCipher: encrypted,
	}}}
	handler := NewHandler(http.NotFoundHandler(), store, &revokedProvider{}, cipher, &stubProjector{}, HandlerConfig{}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	_, err := handler.SyncUser(context.Background(), "user-1")
	var failure *SyncFailure
	if !errors.As(err, &failure) || failure.Code != "reconnect_required" {
		t.Fatalf("error = %v", err)
	}
	if !store.reconnect || store.failureCode != "reconnect_required" || !store.connection.ReconnectRequired {
		t.Fatal("revoked grant was not stopped")
	}
}

type workerSyncer struct{ users []string }

func (syncer *workerSyncer) SyncUser(_ context.Context, userID string) (SyncResult, error) {
	syncer.users = append(syncer.users, userID)
	return SyncResult{}, nil
}

func TestWorkerProcessesOnlyClaimedConnections(t *testing.T) {
	t.Parallel()
	store := &backgroundStubStore{claimed: []Connection{{UserID: "user-1"}, {UserID: "user-2"}}}
	syncer := &workerSyncer{}
	worker := NewWorker(store, syncer, WorkerConfig{SyncTimeout: time.Second}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	worker.runDue(context.Background())
	if store.claimCount != 1 || len(syncer.users) != 2 || syncer.users[0] != "user-1" || syncer.users[1] != "user-2" {
		t.Fatalf("processed users = %#v", syncer.users)
	}
}

func TestSyncBackoffIsBoundedAndDeterministic(t *testing.T) {
	t.Parallel()
	first := syncBackoff("user-1", 2)
	if first != syncBackoff("user-1", 2) || first < 4*time.Minute || first > 5*time.Minute {
		t.Fatalf("unexpected deterministic backoff %s", first)
	}
	if value := syncBackoff("user-1", 100); value > 6*time.Hour {
		t.Fatalf("backoff exceeded cap: %s", value)
	}
}
