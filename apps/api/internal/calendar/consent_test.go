package calendar

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/negotiable-calendar/negotiable-calendar/apps/api/internal/auth"
)

type consentStore struct {
	stubStore
	flow     Flow
	consumed bool
	saves    int
}

func (store *consentStore) ConsumeFlow(_ context.Context, id, user string, state []byte, now time.Time) (Flow, error) {
	if store.consumed || id != store.flow.ID || user != store.flow.UserID ||
		!bytes.Equal(state, store.flow.StateHash) || !now.Before(store.flow.ExpiresAt) {
		return Flow{}, ErrNotFound
	}
	store.consumed = true
	return store.flow, nil
}

func (store *consentStore) SaveConnection(ctx context.Context, connection Connection) error {
	store.saves++
	return store.stubStore.SaveConnection(ctx, connection)
}

type consentProvider struct {
	stubProvider
	tokens    TokenSet
	err       error
	exchanges int
}

func (provider *consentProvider) Exchange(context.Context, string, string) (TokenSet, error) {
	provider.exchanges++
	return provider.tokens, provider.err
}

func TestConsentFailuresConsumeValidStateAndPreserveConnection(t *testing.T) {
	for _, tc := range []struct {
		name, query, result string
		tokens              TokenSet
		err                 error
		exchanges           int
	}{
		{name: "denied", query: "error=access_denied&error_description=secret-description", result: "denied"},
		{name: "unknown provider error", query: "error=secret-error&error_description=secret-description", result: "provider_failed"},
		{name: "exchange rejected", query: "code=secret-code", result: "exchange_failed", err: errors.New("rejected"), exchanges: 1},
		{name: "missing scope", query: "code=secret-code", result: "permission_required", tokens: TokenSet{RefreshToken: "secret-refresh"}, exchanges: 1},
		{name: "freebusy only", query: "code=secret-code", result: "permission_required", tokens: TokenSet{RefreshToken: "secret-refresh", Scopes: []string{"https://www.googleapis.com/auth/calendar.freebusy"}}, exchanges: 1},
		{name: "write grant only", query: "code=secret-code", result: "permission_required", tokens: TokenSet{RefreshToken: "secret-refresh", Scopes: []string{"https://www.googleapis.com/auth/calendar.events.owned"}}, exchanges: 1},
		{name: "scope prefix spoof", query: "code=secret-code", result: "permission_required", tokens: TokenSet{RefreshToken: "secret-refresh", Scopes: []string{CalendarOwnedEventsReadonlyScope + ".invalid"}}, exchanges: 1},
		{name: "minimal missing offline grant", query: "code=secret-code", result: "permission_required", tokens: TokenSet{Scopes: []string{CalendarOwnedEventsReadonlyScope}}, exchanges: 1},
		{name: "missing offline grant", query: "code=secret-code", result: "permission_required", tokens: TokenSet{Scopes: []string{CalendarReadonlyScope}}, exchanges: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := &consentStore{flow: Flow{ID: "flow-id", UserID: "owner", StateHash: hashToken("secret-state"), ExpiresAt: time.Now().Add(time.Hour)}}
			store.connection = Connection{UserID: "owner", RefreshTokenCipher: []byte("existing-encrypted")}
			provider := &consentProvider{tokens: tc.tokens, err: tc.err}
			handler := NewHandler(http.NotFoundHandler(), store, provider, testCipher(t), &stubProjector{}, HandlerConfig{WebOrigin: "https://app.example", SecureCookies: true}, slog.New(slog.NewTextHandler(io.Discard, nil)))
			makeRequest := func() *http.Request {
				request := httptest.NewRequest(http.MethodGet, "/api/v1/calendar/google/callback?state=secret-state&"+tc.query, nil)
				request.Header.Set(auth.AuthenticatedUserHeader, "owner")
				request.AddCookie(&http.Cookie{Name: flowCookieName, Value: "flow-id"})
				return request
			}
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, makeRequest())
			if response.Code != http.StatusSeeOther || response.Header().Get("Location") != "https://app.example/?calendar="+tc.result {
				t.Fatalf("unexpected recovery response: %d %s", response.Code, response.Header().Get("Location"))
			}
			if !store.consumed || store.saves != 0 || string(store.connection.RefreshTokenCipher) != "existing-encrypted" || provider.exchanges != tc.exchanges {
				t.Fatal("failure mutated connection or skipped single-use validation")
			}
			if response.Header().Get("Cache-Control") != "no-store" || response.Header().Get("Referrer-Policy") != "no-referrer" {
				t.Fatal("callback secrets may be cached or referred")
			}
			cookies := response.Result().Cookies()
			if len(cookies) != 1 || cookies[0].Name != flowCookieName || cookies[0].MaxAge != -1 || !cookies[0].Secure || !cookies[0].HttpOnly {
				t.Fatal("flow cookie not securely expired")
			}
			if strings.Contains(response.Body.String(), "secret-") || strings.Contains(response.Header().Get("Location"), "secret-") {
				t.Fatal("provider values reflected")
			}
			replay := httptest.NewRecorder()
			handler.ServeHTTP(replay, makeRequest())
			if replay.Code != http.StatusBadRequest || provider.exchanges != tc.exchanges {
				t.Fatal("consumed callback accepted again")
			}
		})
	}
}

func TestConsentDenialRequiresBoundUnexpiredFlow(t *testing.T) {
	for _, tc := range []struct {
		name, state, user, cookie, query string
		expired                          bool
	}{
		{name: "wrong state", state: "wrong", user: "owner", cookie: "flow-id", query: "error=access_denied"},
		{name: "missing state", user: "owner", cookie: "flow-id", query: "error=access_denied"},
		{name: "wrong user", state: "valid", user: "other", cookie: "flow-id", query: "error=access_denied"},
		{name: "missing cookie", state: "valid", user: "owner", query: "error=access_denied"},
		{name: "expired", state: "valid", user: "owner", cookie: "flow-id", query: "error=access_denied", expired: true},
		{name: "ambiguous success", state: "valid", user: "owner", cookie: "flow-id", query: "error=access_denied&code=code"},
		{name: "no result", state: "valid", user: "owner", cookie: "flow-id"},
		{name: "no session", state: "valid", cookie: "flow-id", query: "error=access_denied"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			expires := time.Now().Add(time.Hour)
			if tc.expired {
				expires = time.Now().Add(-time.Hour)
			}
			store := &consentStore{flow: Flow{ID: "flow-id", UserID: "owner", StateHash: hashToken("valid"), ExpiresAt: expires}}
			provider := &consentProvider{}
			handler := NewHandler(http.NotFoundHandler(), store, provider, testCipher(t), &stubProjector{}, HandlerConfig{WebOrigin: "https://app.example"}, slog.New(slog.NewTextHandler(io.Discard, nil)))
			request := httptest.NewRequest(http.MethodGet, "/api/v1/calendar/google/callback?state="+tc.state+"&"+tc.query, nil)
			request.Header.Set(auth.AuthenticatedUserHeader, tc.user)
			if tc.cookie != "" {
				request.AddCookie(&http.Cookie{Name: flowCookieName, Value: tc.cookie})
			}
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			expected := http.StatusBadRequest
			if tc.user == "" {
				expected = http.StatusUnauthorized
			}
			if response.Code != expected || response.Header().Get("Location") != "" || store.consumed || store.saves != 0 || provider.exchanges != 0 {
				t.Fatal("untrusted callback accepted")
			}
		})
	}
}
