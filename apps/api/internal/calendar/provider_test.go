package calendar

import (
	"context"
	"encoding/json"
	"errors"
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
			_ = json.NewEncoder(response).Encode(map[string]any{
				"items": []map[string]any{{
					"id": "event-1", "summary": "Board secret", "description": "never store",
					"start": map[string]string{"dateTime": "2026-09-03T09:00:00+09:00"},
					"end":   map[string]string{"dateTime": "2026-09-03T10:00:00+09:00"},
				}},
				"nextSyncToken": "sync-1",
			})
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


func TestCalendarProviderUsesIncrementalCursorAndDeletesCancelledInstances(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.URL.Query().Get("syncToken") != "sync-old" {
			t.Errorf("syncToken = %q", request.URL.Query().Get("syncToken"))
		}
		if request.URL.Query().Get("timeMin") != "" || request.URL.Query().Get("timeMax") != "" {
			t.Error("incremental request included incompatible time bounds")
		}
		_ = json.NewEncoder(response).Encode(map[string]any{
			"items": []map[string]any{
				{"id": "deleted-instance", "status": "cancelled"},
				{"id": "changed-instance", "start": map[string]string{"dateTime": "2026-09-03T09:00:00+09:00"}, "end": map[string]string{"dateTime": "2026-09-03T10:00:00+09:00"}},
			},
			"nextSyncToken": "sync-new",
		})
	}))
	defer server.Close()
	provider := NewGoogleProvider(GoogleConfig{ClientID: "client", RedirectURL: "https://app.example/callback"}, server.Client())
	provider.eventsURL = server.URL
	changes, err := provider.ListChanges(context.Background(), "access", "sync-old", time.Time{}, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	if changes.Full || changes.NextSyncToken != "sync-new" || len(changes.Upserts) != 1 {
		t.Fatalf("unexpected changes %#v", changes)
	}
	if len(changes.DeletedProviderEventIDs) != 1 || changes.DeletedProviderEventIDs[0] != "deleted-instance" {
		t.Fatalf("deleted instances = %#v", changes.DeletedProviderEventIDs)
	}
}

func TestCalendarProviderReportsExpiredSyncCursor(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		response.WriteHeader(http.StatusGone)
	}))
	defer server.Close()
	provider := NewGoogleProvider(GoogleConfig{ClientID: "client", RedirectURL: "https://app.example/callback"}, server.Client())
	provider.eventsURL = server.URL
	_, err := provider.ListChanges(context.Background(), "access", "expired", time.Time{}, time.Time{})
	if !errors.Is(err, ErrSyncTokenExpired) {
		t.Fatalf("error = %v", err)
	}
}

func TestCalendarRefreshClassifiesRevokedGrant(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		response.WriteHeader(http.StatusBadRequest)
	}))
	defer server.Close()
	provider := NewGoogleProvider(GoogleConfig{ClientID: "client", RedirectURL: "https://app.example/callback"}, server.Client())
	provider.tokenURL = server.URL
	_, err := provider.Refresh(context.Background(), "revoked")
	if !errors.Is(err, ErrReconnectRequired) {
		t.Fatalf("error = %v", err)
	}
}


func TestPrivateCalendarProviderReturnsOwnerDetailsWithoutPersistingThem(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		fields := request.URL.Query().Get("fields")
		for _, required := range []string{"summary", "description", "location", "attendees"} {
			if !strings.Contains(fields, required) {
				t.Errorf("owner field %q was not requested", required)
			}
		}
		_ = json.NewEncoder(response).Encode(map[string]any{"items": []map[string]any{
			{
				"id": "timed-1", "summary": "役員会議", "description": "confidential",
				"location": "Tokyo", "hangoutLink": "https://meet.google.com/example",
				"attendees": []map[string]any{{"email": "self@example.com", "self": true}, {"displayName": "Partner", "email": "partner@example.com"}},
				"start": map[string]string{"dateTime": "2026-09-03T09:00:00+09:00"},
				"end": map[string]string{"dateTime": "2026-09-03T10:00:00+09:00"},
			},
			{
				"id": "all-day-1", "summary": "休暇",
				"start": map[string]string{"date": "2026-09-04"},
				"end": map[string]string{"date": "2026-09-05"},
			},
		}})
	}))
	defer server.Close()
	provider := NewGoogleProvider(GoogleConfig{ClientID: "client", RedirectURL: "https://app.example/callback"}, server.Client())
	provider.eventsURL = server.URL
	events, err := provider.ListPrivateEvents(context.Background(), "access", time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC), time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 2 || events[0].Title != "役員会議" || events[0].StartAt == nil || events[0].StartAt.Location() != time.UTC {
		t.Fatalf("timed event = %#v", events)
	}
	if len(events[0].Attendees) != 1 || events[0].Attendees[0] != "Partner" {
		t.Fatalf("attendees = %#v", events[0].Attendees)
	}
	if !events[1].AllDay || events[1].StartDate != "2026-09-04" || events[1].StartAt != nil {
		t.Fatalf("all-day event = %#v", events[1])
	}
}
