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

type admissionLeaseStore struct{ *backgroundStubStore }

func (s *admissionLeaseStore) AcquireSync(context.Context, string, time.Time, time.Duration) (Connection, error) {
	return s.connection, nil
}

func TestSyncRejectsUntrustedConnectionBeforeCredentialUse(t *testing.T) {
	for _, leased := range []bool{false, true} {
		for _, tc := range []struct {
			owner, actor, code string
			reconnect          bool
		}{
			{"other", "user-1", "connection_load_failed", false},
			{"", "", "connection_load_failed", false},
			{"user-1", "user-1", "reconnect_required", true},
		} {
			// An invalid cipher also proves admission happens before decryption.
			base := &backgroundStubStore{stubStore: stubStore{connection: Connection{UserID: tc.owner, ReconnectRequired: tc.reconnect}}}
			var s Store = base
			if leased {
				s = &admissionLeaseStore{base}
			} else if tc.owner == "" {
				continue
			}
			p, projector := &stubProvider{}, &stubProjector{}
			h := NewHandler(http.NotFoundHandler(), s, p, testCipher(t), projector, HandlerConfig{}, slog.New(slog.NewTextHandler(io.Discard, nil)))
			_, err := h.SyncUser(context.Background(), tc.actor)
			var failure *SyncFailure
			if !errors.As(err, &failure) || failure.Code != tc.code {
				t.Fatalf("leased=%v: %v", leased, err)
			}
			if p.refreshed != "" || projector.rebuilt || base.failureCode != "" || !base.successAt.IsZero() || len(base.spans) != 0 {
				t.Fatal("untrusted connection caused side effects")
			}
		}
	}
}
