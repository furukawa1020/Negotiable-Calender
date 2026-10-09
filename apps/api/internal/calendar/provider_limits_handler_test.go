package calendar

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/negotiable-calendar/negotiable-calendar/apps/api/internal/auth"
)

func TestCalendarReadLimitPreservesSyncStateAndPrivateHTTPBoundary(t *testing.T) {
	for _, private := range []bool{false, true} {
		name := "sync"
		if private {
			name = "private"
		}
		t.Run(name, func(t *testing.T) {
			calls := 0
			provider := NewGoogleProvider(GoogleConfig{ClientID: "client", RedirectURL: "https://example.test/callback"}, &http.Client{Transport: limitTransport(func(r *http.Request) (*http.Response, error) {
				body := `{"access_token":"access","expires_in":3600}`
				if r.URL.Host == "www.googleapis.com" {
					calls++
					body = `{"timeZone":"UTC","items":[{"id":"private-id","summary":"private-title","start":{"dateTime":"2026-10-09T00:00:00Z"},"end":{"dateTime":"2026-10-09T01:00:00Z"}}],"nextPageToken":"private-cursor"}`
				}
				return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
			})})
			cipher := testCipher(t)
			encrypted, err := cipher.Encrypt("refresh")
			if err != nil {
				t.Fatal(err)
			}
			store := &backgroundStubStore{stubStore: stubStore{connection: Connection{UserID: "u", RefreshTokenCipher: encrypted, SyncToken: "legacy"}}}
			projector := &stubProjector{}
			var logs strings.Builder
			handler := NewHandler(http.NotFoundHandler(), store, provider, cipher, projector, HandlerConfig{}, slog.New(slog.NewTextHandler(&logs, nil)))
			if private {
				r := httptest.NewRequest(http.MethodGet, "/api/v1/me/private-events?from=2026-10-09T00:00:00Z&to=2026-10-10T00:00:00Z", nil)
				r.Header.Set(auth.AuthenticatedUserHeader, "u")
				w := httptest.NewRecorder()
				handler.ServeHTTP(w, r)
				if w.Code != http.StatusBadGateway || w.Header().Get("Cache-Control") != "no-store" || strings.Contains(w.Body.String(), "private-title") || strings.Contains(w.Body.String(), "private-id") || strings.Contains(w.Body.String(), "private-cursor") {
					t.Fatal("unsafe private response", w.Code, w.Body.String())
				}
			} else {
				_, err := handler.SyncUser(context.Background(), "u")
				var failure *SyncFailure
				if !errors.As(err, &failure) || failure.Code != "calendar_read_failed" || store.failureCode == "" || store.reconnect {
					t.Fatal("incorrect failure handling", err)
				}
			}
			if calls != 2 || store.successToken != "" || store.changes.NextSyncToken != "" || len(store.changes.Upserts) != 0 || projector.rebuilt || store.connection.SyncToken != "legacy" {
				t.Fatal("partial evidence persisted or unbounded requests")
			}
			for _, secret := range []string{"private-id", "private-title", "private-cursor"} {
				if strings.Contains(logs.String(), secret) {
					t.Fatal("private provider data logged")
				}
			}
		})
	}
}
