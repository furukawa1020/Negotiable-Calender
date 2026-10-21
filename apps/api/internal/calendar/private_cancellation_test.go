package calendar

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/negotiable-calendar/negotiable-calendar/apps/api/internal/auth"
)

type cancelledPrivateStore struct {
	stubStore
	stage        string
	cancel       context.CancelFunc
	loads, zones int
}

func (s *cancelledPrivateStore) GetConnection(ctx context.Context, user string) (Connection, error) {
	s.loads++
	if s.stage == "connection" {
		s.cancel()
	}
	return s.stubStore.GetConnection(ctx, user)
}
func (s *cancelledPrivateStore) UserTimezone(context.Context, string) (string, error) {
	s.zones++
	if s.stage == "timezone" {
		s.cancel()
	}
	return "Asia/Tokyo", nil
}

type cancelledPrivateProvider struct {
	stubProvider
	stage            string
	cancel           context.CancelFunc
	refreshes, reads int
	err              error
}

func (p *cancelledPrivateProvider) Refresh(context.Context, string) (TokenSet, error) {
	p.refreshes++
	if p.stage == "refresh" {
		p.cancel()
		return TokenSet{AccessToken: "secret-access"}, p.err
	}
	return TokenSet{AccessToken: "secret-access"}, nil
}
func (p *cancelledPrivateProvider) ListPrivateEvents(context.Context, string, time.Time, time.Time) ([]PrivateEventView, error) {
	p.reads++
	if p.stage == "events" {
		p.cancel()
		return []PrivateEventView{{Title: "secret-event"}}, p.err
	}
	return []PrivateEventView{{Title: "secret-event"}}, nil
}
func TestPrivateReadDiscardsLateDependencyResults(t *testing.T) {
	for _, stage := range []string{"before", "connection", "refresh", "events", "timezone", "healthy"} {
		for _, providerErr := range []error{nil, ErrReconnectRequired} {
			t.Run(stage+"/"+map[bool]string{true: "error", false: "success"}[providerErr != nil], func(t *testing.T) {
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				cipher := testCipher(t)
				encrypted, err := cipher.Encrypt("secret-refresh")
				if err != nil {
					t.Fatal(err)
				}
				s := &cancelledPrivateStore{stubStore: stubStore{connection: Connection{UserID: "owner", RefreshTokenCipher: encrypted}}, stage: stage, cancel: cancel}
				p := &cancelledPrivateProvider{stage: stage, cancel: cancel, err: providerErr}
				h := NewHandler(http.NotFoundHandler(), s, p, cipher, &stubProjector{}, HandlerConfig{}, slog.New(slog.NewTextHandler(io.Discard, nil)))
				r := httptest.NewRequest("GET", "/api/v1/me/private-events?from=2026-10-21T00:00:00Z&to=2026-10-22T00:00:00Z", nil).WithContext(ctx)
				r.Header.Set(auth.AuthenticatedUserHeader, "owner")
				if stage == "before" {
					cancel()
				}
				w := httptest.NewRecorder()
				h.ServeHTTP(w, r)
				want := 504
				if stage == "healthy" {
					want = 200
				}
				if w.Code != want || w.Header().Get("Cache-Control") != "no-store" || s.connection.ReconnectRequired {
					t.Fatal("unsafe cancellation", w.Code, w.Body)
				}
				if stage != "healthy" && strings.Contains(w.Body.String(), "secret-") {
					t.Fatal("late private content disclosed")
				}
				if stage == "before" && s.loads != 0 {
					t.Fatal("loaded cancelled request")
				}
				if (stage == "before" || stage == "connection") && p.refreshes != 0 {
					t.Fatal("refreshed cancelled request")
				}
				if (stage == "before" || stage == "connection" || stage == "refresh") && p.reads != 0 {
					t.Fatal("read after cancellation")
				}
				if stage != "timezone" && stage != "healthy" && s.zones != 0 {
					t.Fatal("timezone read after cancellation")
				}
			})
		}
	}
}
