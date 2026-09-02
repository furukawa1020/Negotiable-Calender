package calendar

import (
	"context"
	"encoding/base64"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/negotiable-calendar/negotiable-calendar/apps/api/internal/auth"
)

type stubStore struct {
	connection Connection
	spans      []BusySpan
}

func (*stubStore) CreateFlow(context.Context, Flow) error { return nil }
func (*stubStore) ConsumeFlow(context.Context, string, string, []byte, time.Time) (Flow, error) {
	return Flow{}, ErrNotFound
}
func (store *stubStore) SaveConnection(_ context.Context, value Connection) error {
	store.connection = value
	return nil
}
func (store *stubStore) GetConnection(context.Context, string) (Connection, error) {
	if store.connection.UserID == "" {
		return Connection{}, ErrNotFound
	}
	return store.connection, nil
}
func (store *stubStore) ReplaceBusySpans(_ context.Context, _ string, values []BusySpan, _, _, _ time.Time) error {
	store.spans = values
	return nil
}
func (store *stubStore) MarkReconnectRequired(context.Context, string) error {
	store.connection.ReconnectRequired = true
	return nil
}
func (store *stubStore) DeleteConnection(context.Context, string) error {
	store.connection = Connection{}
	store.spans = nil
	return nil
}

type stubProvider struct{ refreshed string }

func (*stubProvider) Configured() bool                       { return true }
func (*stubProvider) AuthorizationURL(string, string) string { return "https://google.example/consent" }
func (*stubProvider) Exchange(context.Context, string, string) (TokenSet, error) {
	return TokenSet{}, nil
}
func (provider *stubProvider) Refresh(_ context.Context, value string) (TokenSet, error) {
	provider.refreshed = value
	return TokenSet{AccessToken: "access"}, nil
}
func (*stubProvider) ListBusy(context.Context, string, time.Time, time.Time) ([]BusySpan, error) {
	return []BusySpan{{ProviderEventID: "event-1", CalendarID: "primary", StartAt: time.Date(2026, 9, 3, 0, 0, 0, 0, time.UTC), EndAt: time.Date(2026, 9, 3, 1, 0, 0, 0, time.UTC), Busy: true}}, nil
}

func TestCalendarEndpointsRequireRealAuthenticatedSession(t *testing.T) {
	handler := NewHandler(http.NotFoundHandler(), &stubStore{}, &stubProvider{}, testCipher(t), HandlerConfig{}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	request := httptest.NewRequest(http.MethodGet, "/api/v1/calendar/connection", nil)
	request.Header.Set("X-Demo-User-ID", "demo-manager")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("demo identity connected calendar: %d", response.Code)
	}
}

func TestCalendarSyncDecryptsGrantAndStoresOnlyBusySpanShape(t *testing.T) {
	cipher := testCipher(t)
	encrypted, err := cipher.Encrypt("refresh-secret")
	if err != nil {
		t.Fatal(err)
	}
	store := &stubStore{connection: Connection{UserID: "user-1", RefreshTokenCipher: encrypted}}
	provider := &stubProvider{}
	handler := NewHandler(http.NotFoundHandler(), store, provider, cipher, HandlerConfig{}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	request := httptest.NewRequest(http.MethodPost, "/api/v1/calendar/sync", nil)
	request.Header.Set(auth.AuthenticatedUserHeader, "user-1")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("sync failed: %d %s", response.Code, response.Body.String())
	}
	if provider.refreshed != "refresh-secret" {
		t.Fatal("encrypted grant was not decrypted for refresh")
	}
	if len(store.spans) != 1 || store.spans[0].ProviderEventID != "event-1" {
		t.Fatalf("busy span missing: %#v", store.spans)
	}
}

func testCipher(t *testing.T) *TokenCipher {
	t.Helper()
	value, err := NewTokenCipher(base64.StdEncoding.EncodeToString(make([]byte, 32)))
	if err != nil {
		t.Fatal(err)
	}
	return value
}
