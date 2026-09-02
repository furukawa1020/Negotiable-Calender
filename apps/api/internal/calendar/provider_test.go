package calendar

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestCalendarAuthorizationUsesSeparateReadonlyConsentAndPKCE(t *testing.T) {
	provider := NewGoogleProvider(GoogleConfig{ClientID: "client", RedirectURL: "https://app.example/callback"}, http.DefaultClient)
	parsed, err := url.Parse(provider.AuthorizationURL("state-1", "challenge-1"))
	if err != nil {
		t.Fatal(err)
	}
	query := parsed.Query()
	if query.Get("scope") != CalendarReadonlyScope {
		t.Fatalf("unexpected scope %q", query.Get("scope"))
	}
	if query.Get("access_type") != "offline" || query.Get("prompt") != "consent" {
		t.Fatal("offline consent missing")
	}
	if query.Get("code_challenge") != "challenge-1" || query.Get("code_challenge_method") != "S256" {
		t.Fatal("PKCE missing")
	}
	if strings.Contains(query.Get("scope"), "calendar.events") {
		t.Fatal("write permission requested")
	}
}

func TestCalendarProviderExchangesRefreshTokenAndRedactsEventDetails(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/token":
			_ = request.ParseForm()
			if request.Form.Get("code_verifier") != "verifier-1" {
				t.Errorf("missing verifier")
			}
			_ = json.NewEncoder(response).Encode(map[string]any{"access_token": "access-1", "refresh_token": "refresh-1", "scope": CalendarReadonlyScope, "expires_in": 3600})
		case "/events":
			if request.Header.Get("Authorization") != "Bearer access-1" {
				t.Errorf("missing bearer token")
			}
			if fields := request.URL.Query().Get("fields"); strings.Contains(fields, "summary") || strings.Contains(fields, "description") {
				t.Errorf("private fields requested: %s", fields)
			}
			_ = json.NewEncoder(response).Encode(map[string]any{"items": []map[string]any{{
				"id": "event-1", "summary": "Board secret", "description": "never store",
				"start": map[string]string{"dateTime": "2026-09-03T09:00:00+09:00"},
				"end":   map[string]string{"dateTime": "2026-09-03T10:00:00+09:00"},
			}}})
		}
	}))
	defer server.Close()
	provider := NewGoogleProvider(GoogleConfig{ClientID: "client", RedirectURL: "https://app.example/callback"}, server.Client())
	provider.tokenURL = server.URL + "/token"
	provider.eventsURL = server.URL + "/events"
	tokens, err := provider.Exchange(context.Background(), "code-1", "verifier-1")
	if err != nil {
		t.Fatal(err)
	}
	if tokens.RefreshToken != "refresh-1" || !contains(tokens.Scopes, CalendarReadonlyScope) {
		t.Fatalf("unexpected tokens %#v", tokens)
	}
	spans, err := provider.ListBusy(context.Background(), tokens.AccessToken, time.Now().Add(-time.Hour), time.Now().Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if len(spans) != 1 || spans[0].ProviderEventID != "event-1" || !spans[0].Busy {
		t.Fatalf("unexpected spans %#v", spans)
	}
	if spans[0].StartAt.Location() != time.UTC {
		t.Fatal("busy span not normalized to UTC")
	}
}
