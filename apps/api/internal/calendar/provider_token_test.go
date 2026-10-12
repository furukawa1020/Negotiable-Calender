package calendar

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/negotiable-calendar/negotiable-calendar/apps/api/internal/auth"
)

func TestGoogleTokenRejectsMalformedSuccessWithoutCredentials(t *testing.T) {
	for _, body := range []string{
		`null`, `[]`, `{}`, `{"access_token":"private-secret"}`,
		`{"access_token":"private-secret","expires_in":0}`,
		`{"access_token":"private-secret","expires_in":-1}`,
		`{"access_token":"private-secret","expires_in":9223372037}`,
		`{"access_token":"private-secret","expires_in":1.5}`,
		`{"access_token":"private-secret","expires_in":"private-secret"}`,
		`{"access_token":"private-secret","expires_in":3600} {}`,
		`{"access_token":"private-secret","access_token":"other","expires_in":3600}`,
		`{"access_token":"private-secret","ACCESS_TOKEN":"other","expires_in":3600}`,
		`{"access_token":"private-secret","expires_in":3600,"scope":{},"private-secret":false}`,
		`{"access_token":"private-secret","expires_in":3600,"token_type":"MAC"}`,
		`{"access_token":"private-secret\r\n","expires_in":3600}`,
		`{"access_token":" ","expires_in":3600}`,
	} {
		for _, refresh := range []bool{false, true} {
			t.Run(fmt.Sprintf("case-%d/refresh-%t", len(body), refresh), func(t *testing.T) {
				reader := &countedCalendarBody{reader: strings.NewReader(body)}
				provider := NewGoogleProvider(GoogleConfig{}, &http.Client{Transport: limitTransport(func(r *http.Request) (*http.Response, error) {
					return &http.Response{StatusCode: 200, Body: reader, ContentLength: -1, Request: r}, nil
				})})
				var tokens TokenSet
				var err error
				if refresh {
					tokens, err = provider.Refresh(context.Background(), "synthetic-refresh")
				} else {
					tokens, err = provider.Exchange(context.Background(), "code", "verifier")
				}
				if !errors.Is(err, errGoogleTokenResponse) || errors.Is(err, ErrReconnectRequired) || !reader.closed || tokens.AccessToken != "" || tokens.RefreshToken != "" || !tokens.ExpiresAt.IsZero() || len(tokens.Scopes) != 0 || strings.Contains(err.Error(), "private-secret") {
					t.Fatal("unsafe token response handling")
				}
			})
		}
	}
}

func TestGoogleTokenByteLimitsAndCancellation(t *testing.T) {
	valid := `{"access_token":"synthetic","expires_in":3600}`
	for _, scenario := range []string{"exact", "overflow", "declared", "cancelled", "read-error"} {
		t.Run(scenario, func(t *testing.T) {
			body := valid + strings.Repeat(" ", googleTokenResponseBytes-len(valid))
			length := int64(-1)
			if scenario == "overflow" {
				body += " "
			}
			if scenario == "declared" {
				length = googleTokenResponseBytes + 1
			}
			var source io.Reader = strings.NewReader(body)
			if scenario == "read-error" {
				source = tokenErrorReader{}
			}
			reader := &countedCalendarBody{reader: source}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			provider := NewGoogleProvider(GoogleConfig{}, &http.Client{Transport: limitTransport(func(r *http.Request) (*http.Response, error) {
				if scenario == "cancelled" {
					cancel()
				}
				return &http.Response{StatusCode: 200, Body: reader, ContentLength: length, Request: r}, nil
			})})
			tokens, err := provider.Refresh(ctx, "refresh")
			if !reader.closed || reader.read > googleTokenResponseBytes+1 {
				t.Fatal("response not bounded/closed")
			}
			if scenario == "exact" {
				if err != nil || tokens.AccessToken != "synthetic" || !tokens.ExpiresAt.After(time.Now()) {
					t.Fatal("valid token rejected")
				}
			} else if err == nil || tokens.AccessToken != "" || strings.Contains(err.Error(), "private-secret") {
				t.Fatal("invalid success accepted/leaked")
			}
			if scenario == "cancelled" && (!errors.Is(err, context.Canceled) || reader.read != 0) {
				t.Fatal("lost cancellation")
			}
			if scenario == "declared" && reader.read != 0 {
				t.Fatal("read declared overflow")
			}
		})
	}
}

type tokenErrorReader struct{}

func (tokenErrorReader) Read([]byte) (int, error) { return 0, errors.New("private-secret") }

func TestGoogleTokenOptionalRefreshFieldsAndLifetime(t *testing.T) {
	body := `{"access_token":"synthetic","refresh_token":"synthetic-refresh","expires_in":3600,"scope":"` + CalendarOwnedEventsReadonlyScope + `","token_type":"Bearer","future_metadata":{"valid":true}}`
	provider := NewGoogleProvider(GoogleConfig{}, &http.Client{Transport: limitTransport(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
	})})
	before := time.Now()
	tokens, err := provider.Refresh(context.Background(), "refresh")
	if err != nil || tokens.RefreshToken != "synthetic-refresh" || !hasCalendarReadScope(tokens.Scopes) || tokens.ExpiresAt.Before(before.Add(time.Hour)) || tokens.ExpiresAt.After(time.Now().Add(time.Hour)) {
		t.Fatal("valid token fields changed")
	}
}

func TestMalformedGoogleTokenPreservesSyncAndPrivateBoundaryThenRecovers(t *testing.T) {
	for _, private := range []bool{false, true} {
		t.Run(fmt.Sprint(private), func(t *testing.T) {
			healthy, eventCalls := false, 0
			provider := NewGoogleProvider(GoogleConfig{ClientID: "client", RedirectURL: "https://example.test/callback"}, &http.Client{Transport: limitTransport(func(r *http.Request) (*http.Response, error) {
				body := `{"access_token":"private-secret","expires_in":"private-secret"}`
				if healthy {
					body = `{"access_token":"synthetic","expires_in":3600}`
				}
				if r.URL.Host == "www.googleapis.com" {
					eventCalls++
					body = `{"timeZone":"UTC","items":[],"nextSyncToken":"new"}`
				}
				return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
			})})
			cipher := testCipher(t)
			encrypted, err := cipher.Encrypt("refresh")
			if err != nil {
				t.Fatal(err)
			}
			store := &backgroundStubStore{stubStore: stubStore{connection: Connection{UserID: "u", RefreshTokenCipher: encrypted, SyncToken: "old"}}}
			projector := &stubProjector{}
			var logs strings.Builder
			handler := NewHandler(http.NotFoundHandler(), store, provider, cipher, projector, HandlerConfig{}, slog.New(slog.NewTextHandler(&logs, nil)))
			readPrivate := func() *httptest.ResponseRecorder {
				r := httptest.NewRequest("GET", "/api/v1/me/private-events?from=2026-10-09T00:00:00Z&to=2026-10-10T00:00:00Z", nil)
				r.Header.Set(auth.AuthenticatedUserHeader, "u")
				w := httptest.NewRecorder()
				handler.ServeHTTP(w, r)
				return w
			}
			if private {
				w := readPrivate()
				if w.Code != 502 || w.Header().Get("Cache-Control") != "no-store" || strings.Contains(w.Body.String(), "private-secret") {
					t.Fatal("unsafe private failure")
				}
			} else {
				_, err := handler.SyncUser(context.Background(), "u")
				var failure *SyncFailure
				if !errors.As(err, &failure) || failure.Code != "token_refresh_failed" || store.failureCode != "temporary_failure" || !store.nextAttempt.After(time.Now()) {
					t.Fatal("lost sync retry")
				}
			}
			if eventCalls != 0 || projector.rebuilt || store.successToken != "" || store.connection.SyncToken != "old" || !bytes.Equal(store.connection.RefreshTokenCipher, encrypted) || store.reconnect || strings.Contains(logs.String(), "private-secret") {
				t.Fatal("malformed token changed evidence or leaked")
			}
			healthy = true
			if private {
				if w := readPrivate(); w.Code != 200 {
					t.Fatal("private retry failed")
				}
			} else {
				if _, err := handler.SyncUser(context.Background(), "u"); err != nil || !projector.rebuilt || store.successToken == "" {
					t.Fatal("sync retry failed")
				}
			}
			if eventCalls != 1 || store.reconnect {
				t.Fatal("healthy retry required new consent")
			}
		})
	}
}
