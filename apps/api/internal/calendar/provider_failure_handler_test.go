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
	"time"

	"github.com/negotiable-calendar/negotiable-calendar/apps/api/internal/auth"
)

func TestGoogleFailureRetainsSyncRetryAndPrivateBoundary(t *testing.T) {
	for _, token := range []bool{false, true} {
		for _, private := range []bool{false, true} {
			t.Run(map[bool]string{true: "token", false: "events"}[token]+map[bool]string{true: "-private", false: "-sync"}[private], func(t *testing.T) {
				calls := 0
				provider := NewGoogleProvider(GoogleConfig{ClientID: "client", RedirectURL: "https://example.test/callback"}, &http.Client{Transport: limitTransport(func(r *http.Request) (*http.Response, error) {
					calls++
					status, body := 200, `{"access_token":"access","expires_in":3600}`
					if token {
						status, body = 401, `{"error":"invalid_client","error_description":"private-secret"}`
					} else if r.URL.Host == "www.googleapis.com" {
						status, body = 403, `{"error":{"errors":[{"reason":"rateLimitExceeded"}],"message":"private-secret"}}`
					}
					return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
				})})
				cipher := testCipher(t)
				encrypted, err := cipher.Encrypt("refresh")
				if err != nil {
					t.Fatal(err)
				}
				store := &backgroundStubStore{stubStore: stubStore{connection: Connection{UserID: "u", RefreshTokenCipher: encrypted, SyncToken: "old-cursor"}}}
				projector := &stubProjector{}
				var logs strings.Builder
				handler := NewHandler(http.NotFoundHandler(), store, provider, cipher, projector, HandlerConfig{}, slog.New(slog.NewTextHandler(&logs, nil)))
				if private {
					r := httptest.NewRequest(http.MethodGet, "/api/v1/me/private-events?from=2026-10-09T00:00:00Z&to=2026-10-10T00:00:00Z", nil)
					r.Header.Set(auth.AuthenticatedUserHeader, "u")
					w := httptest.NewRecorder()
					handler.ServeHTTP(w, r)
					if w.Code != 502 || w.Header().Get("Cache-Control") != "no-store" || strings.Contains(w.Body.String(), "private-secret") {
						t.Fatal("unsafe private failure", w.Code, w.Body.String())
					}
				} else {
					_, err := handler.SyncUser(context.Background(), "u")
					var failure *SyncFailure
					want := "rate_limited"
					if token {
						want = "provider_configuration"
					}
					if !errors.As(err, &failure) || failure.Status != 502 || store.failureCode != want || !store.nextAttempt.After(time.Now()) {
						t.Fatal("lost retry", err, store.failureCode)
					}
				}
				wantCalls := 2
				if token {
					wantCalls = 1
				}
				if calls != wantCalls || store.connection.ReconnectRequired || store.reconnect || store.successToken != "" || len(store.changes.Upserts) != 0 || projector.rebuilt || store.connection.SyncToken != "old-cursor" || strings.Contains(logs.String(), "private-secret") {
					t.Fatal("failure changed evidence or consent, or leaked provider data")
				}
			})
		}
	}
}

func TestGoogleErrorReadBudgetAndCancellation(t *testing.T) {
	for _, token := range []bool{false, true} {
		for _, cancelled := range []bool{false, true} {
			body := &countedCalendarBody{reader: strings.NewReader(strings.Repeat("x", 32*1024))}
			ctx, cancel := context.WithCancel(context.Background())
			provider := NewGoogleProvider(GoogleConfig{}, &http.Client{Transport: limitTransport(func(r *http.Request) (*http.Response, error) {
				if cancelled {
					cancel()
				}
				status := 403
				if token {
					status = 400
				}
				return &http.Response{StatusCode: status, Body: body, ContentLength: -1, Request: r}, nil
			})})
			var err error
			if token {
				_, err = provider.Refresh(ctx, "refresh")
			} else {
				_, err = provider.ListPrivateEvents(ctx, "access", time.Now(), time.Now().Add(time.Hour))
			}
			cancel()
			if err == nil || !body.closed || body.read > 16*1024+1 || errors.Is(err, ErrReconnectRequired) {
				t.Fatal("unbounded/unsafe provider failure", err, body.read, body.closed)
			}
			if cancelled && (!errors.Is(err, context.Canceled) || body.read != 0) {
				t.Fatal("cancellation lost", err)
			}
		}
	}
}
