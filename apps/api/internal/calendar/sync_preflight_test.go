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

type preflightLeaseStore struct {
	stubStore
	calls int
}

func (s *preflightLeaseStore) AcquireSync(context.Context, string, time.Time, time.Duration) (Connection, error) {
	s.calls++
	return s.connection, nil
}
func TestMissingProjectorRejectsSyncBeforeLeaseOrCredentialUse(t *testing.T) {
	cipher := testCipher(t)
	encrypted, _ := cipher.Encrypt("refresh-secret")
	s := &preflightLeaseStore{stubStore: stubStore{connection: Connection{UserID: "u", RefreshTokenCipher: encrypted}}}
	p := &stubProvider{}
	h := NewHandler(http.NotFoundHandler(), s, p, cipher, nil, HandlerConfig{}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	_, err := h.SyncUser(context.Background(), "u")
	var failure *SyncFailure
	if !errors.As(err, &failure) || failure.Code != "projection_unconfigured" || failure.Status != 503 {
		t.Fatalf("error=%v", err)
	}
	if s.calls != 0 || p.refreshed != "" || len(s.spans) != 0 || s.connection.LastSyncedAt != nil {
		t.Fatal("misconfiguration caused partial sync")
	}
	// Correcting configuration restores the regular path without reconnecting.
	h.projector = &stubProjector{}
	if _, err = h.SyncUser(context.Background(), "u"); err != nil || s.calls != 1 || s.connection.LastSyncedAt == nil {
		t.Fatalf("recovery failed: %v", err)
	}
}
