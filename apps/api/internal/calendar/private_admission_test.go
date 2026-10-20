package calendar

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/negotiable-calendar/negotiable-calendar/apps/api/internal/auth"
)

type admissionCalendarStore struct {
	stubStore
	reads int
}

func (s *admissionCalendarStore) GetConnection(ctx context.Context, userID string) (Connection, error) {
	s.reads++
	return s.stubStore.GetConnection(ctx, userID)
}

type admissionCalendarProvider struct {
	privateEventStubProvider
	lists int
}

func (p *admissionCalendarProvider) ListPrivateEvents(context.Context, string, time.Time, time.Time) ([]PrivateEventView, error) {
	p.lists++
	return []PrivateEventView{}, nil
}

func TestPrivateReadAdmissionStopsBeforeExternalAccess(t *testing.T) {
	const valid = "from=2026-10-20T00:00:00Z&to=2026-10-21T00:00:00Z"
	for _, tc := range []struct {
		name, query, owner string
		reconnect          bool
		status, reads      int
	}{
		{"valid", valid, "owner", false, 200, 1},
		{"reconnect", valid, "owner", true, 409, 1},
		{"foreign owner", valid, "other", false, 500, 1},
		{"duplicate from", valid + "&from=2026-10-20T00:00:00Z", "owner", false, 400, 0},
		{"duplicate to", valid + "&to=2026-10-22T00:00:00Z", "owner", false, 400, 0},
		{"malformed encoding", valid + "&extra=%zz", "owner", false, 400, 0},
		{"zero from", "from=0001-01-01T00:00:00Z&to=0001-01-02T00:00:00Z", "owner", false, 400, 0},
		{"zero to", "from=0000-12-31T00:00:00Z&to=0001-01-01T00:00:00Z", "owner", false, 400, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cipher := testCipher(t)
			encrypted, err := cipher.Encrypt("synthetic-refresh")
			if err != nil {
				t.Fatal(err)
			}
			store := &admissionCalendarStore{stubStore: stubStore{connection: Connection{UserID: tc.owner, RefreshTokenCipher: encrypted, ReconnectRequired: tc.reconnect}}}
			provider := &admissionCalendarProvider{}
			handler := NewHandler(http.NotFoundHandler(), store, provider, cipher, &stubProjector{}, HandlerConfig{}, slog.New(slog.NewTextHandler(io.Discard, nil)))
			request := httptest.NewRequest(http.MethodGet, "/api/v1/me/private-events?"+tc.query, nil)
			request.Header.Set(auth.AuthenticatedUserHeader, "owner")
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != tc.status || store.reads != tc.reads {
				t.Fatalf("status=%d reads=%d", response.Code, store.reads)
			}
			wantCalls := 0
			if tc.status == 200 {
				wantCalls = 1
			}
			if (provider.refreshed != "") != (wantCalls == 1) || provider.lists != wantCalls {
				t.Fatal("provider called despite failed admission")
			}
			if response.Header().Get("Cache-Control") != "no-store" {
				t.Fatal("private response is cacheable")
			}
			if store.connection.ReconnectRequired != tc.reconnect {
				t.Fatal("read mutated reconnect state")
			}
		})
	}
}
