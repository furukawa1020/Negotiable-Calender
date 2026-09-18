package calendar

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"testing"
	"time"
)

type failureContextStore struct {
	backgroundStubStore
	lease          SyncLease
	alive, bounded bool
}

func (s *failureContextStore) MarkSyncFailure(ctx context.Context, userID, code string, next time.Time, reconnect bool) error {
	s.lease, _ = SyncLeaseFromContext(ctx)
	s.alive = ctx.Err() == nil
	deadline, ok := ctx.Deadline()
	s.bounded = ok && time.Until(deadline) > 0 && time.Until(deadline) <= 2*time.Second
	return s.backgroundStubStore.MarkSyncFailure(ctx, userID, code, next, reconnect)
}

func TestTimeoutFailureRecordPreservesFencingWithBoundedCleanup(t *testing.T) {
	store := &failureContextStore{}
	handler := NewHandler(http.NotFoundHandler(), store, &stubProvider{}, testCipher(t), &stubProjector{}, HandlerConfig{}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	lease := SyncLease{UserID: "alice", ID: "current-lease"}
	ctx, cancel := context.WithCancel(WithSyncLease(context.Background(), lease))
	cancel()
	handler.markFailure(ctx, Connection{UserID: "alice"}, "timeout", false)
	if !store.alive || !store.bounded || store.lease != lease || store.failureCode != "timeout" || !store.nextAttempt.After(time.Now()) {
		t.Fatalf("failure record lost its bounded context or fencing: %+v", store)
	}
}
