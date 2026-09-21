package calendar

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/negotiable-calendar/negotiable-calendar/apps/api/internal/auth"
)

func TestConsentAcceptsMinimalAndLegacyReadGrants(t *testing.T) {
	for _, scopes := range [][]string{
		{CalendarOwnedEventsReadonlyScope},
		{CalendarReadonlyScope},
		{CalendarOwnedEventsReadonlyScope, CalendarReadonlyScope, "openid"},
	} {
		t.Run(strings.Join(scopes, " "), func(t *testing.T) {
			cipher := testCipher(t)
			store := &consentStore{flow: Flow{ID: "flow", UserID: "owner", StateHash: hashToken("state"), ExpiresAt: time.Now().Add(time.Hour)}}
			provider := &consentProvider{tokens: TokenSet{RefreshToken: "refresh-secret", Scopes: scopes}}
			handler := NewHandler(http.NotFoundHandler(), store, provider, cipher, &stubProjector{}, HandlerConfig{WebOrigin: "https://app.example"}, slog.New(slog.NewTextHandler(io.Discard, nil)))
			request := httptest.NewRequest(http.MethodGet, "/api/v1/calendar/google/callback?state=state&code=code", nil)
			request.Header.Set(auth.AuthenticatedUserHeader, "owner")
			request.AddCookie(&http.Cookie{Name: flowCookieName, Value: "flow"})
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != http.StatusFound || response.Header().Get("Location") != "https://app.example/?calendar=connected" || store.saves != 1 {
				t.Fatal("compatible grant rejected")
			}
			if !reflect.DeepEqual(store.connection.GrantedScopes, scopes) {
				t.Fatal("recorded grant must match actual grant, not pretend legacy access was revoked")
			}
			refresh, err := cipher.Decrypt(store.connection.RefreshTokenCipher)
			if err != nil || refresh != "refresh-secret" || strings.Contains(string(store.connection.RefreshTokenCipher), "refresh-secret") {
				t.Fatal("refresh token not encrypted correctly")
			}
		})
	}
}

type scopeTransport func(*http.Request) (*http.Response, error)

func (transport scopeTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	return transport(request)
}

func TestReadFeaturesUseOnlyPrimaryEventsEndpoint(t *testing.T) {
	// This verifies our API use, not Google's enforcement of a real OAuth grant.
	// Exact requested scope is tested separately; live consent remains #76/#121.
	for _, grant := range []string{CalendarOwnedEventsReadonlyScope, CalendarReadonlyScope} {
		t.Run(grant, func(t *testing.T) {
			calls := 0
			provider := NewGoogleProvider(GoogleConfig{ClientID: "client", ClientSecret: "synthetic", RedirectURL: "https://app.example/callback"}, &http.Client{Transport: scopeTransport(func(request *http.Request) (*http.Response, error) {
				var body any
				switch request.URL.Host + request.URL.Path {
				case "oauth2.googleapis.com/token":
					if request.Method != http.MethodPost {
						t.Fatal("unexpected token method")
					}
					if err := request.ParseForm(); err != nil {
						t.Fatal(err)
					}
					if request.Form.Get("grant_type") != "refresh_token" || request.Form.Get("refresh_token") != "existing-refresh" || request.Form.Get("scope") != "" {
						t.Fatal("refresh must preserve existing grant without requesting a different scope")
					}
					body = map[string]any{"access_token": "access", "scope": grant, "expires_in": 3600}
				case "www.googleapis.com/calendar/v3/calendars/primary/events":
					if request.Method != http.MethodGet || request.Header.Get("Authorization") != "Bearer access" {
						t.Fatal("event use must be authenticated read-only")
					}
					calls++
					if calls == 2 && request.URL.Query().Get("syncToken") != "cursor" {
						t.Fatal("incremental sync lost")
					}
					body = map[string]any{"items": []any{}, "nextSyncToken": "cursor"}
				default:
					t.Fatal("read feature requested an endpoint outside primary events")
				}
				data, err := json.Marshal(body)
				if err != nil {
					t.Fatal(err)
				}
				return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(string(data))), Request: request}, nil
			})})
			tokens, err := provider.Refresh(context.Background(), "existing-refresh")
			if err != nil || !reflect.DeepEqual(tokens.Scopes, []string{grant}) {
				t.Fatal("existing grant refresh failed")
			}
			from := time.Now().UTC()
			to := from.Add(time.Hour)
			full, err := provider.ListChanges(context.Background(), tokens.AccessToken, "", from, to)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := provider.ListChanges(context.Background(), tokens.AccessToken, full.NextSyncToken, from, to); err != nil {
				t.Fatal(err)
			}
			if _, err := provider.ListPrivateEvents(context.Background(), tokens.AccessToken, from, to); err != nil {
				t.Fatal(err)
			}
			if calls != 3 {
				t.Fatal("not all read features exercised")
			}
		})
	}
}
