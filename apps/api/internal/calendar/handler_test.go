package calendar

import (
	"context"
	"encoding/base64"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"strings"
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
func (store *stubStore) MarkSynced(_ context.Context, _ string, value time.Time) error {
	store.connection.LastSyncedAt = &value
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

type stubProvider struct {
	refreshed string
	revoked   string
}

type stubProjector struct {
	rebuilt bool
	err     error
}

func (value *stubProjector) Rebuild(context.Context, string, time.Time, time.Time, time.Time) error {
	value.rebuilt = true
	return value.err
}

func (*stubProvider) Configured() bool                       { return true }
func (*stubProvider) AuthorizationURL(string, string) string { return "https://google.example/consent" }
func (*stubProvider) Exchange(context.Context, string, string) (TokenSet, error) {
	return TokenSet{}, nil
}
func (provider *stubProvider) Refresh(_ context.Context, value string) (TokenSet, error) {
	provider.refreshed = value
	return TokenSet{AccessToken: "access"}, nil
}
func (provider *stubProvider) Revoke(_ context.Context, value string) error {
	provider.revoked = value
	return nil
}
func (*stubProvider) ListBusy(context.Context, string, time.Time, time.Time) ([]BusySpan, error) {
	return []BusySpan{{ProviderEventID: "event-1", CalendarID: "primary", StartAt: time.Date(2026, 9, 3, 0, 0, 0, 0, time.UTC), EndAt: time.Date(2026, 9, 3, 1, 0, 0, 0, time.UTC), Busy: true}}, nil
}

func TestCalendarEndpointsRequireRealAuthenticatedSession(t *testing.T) {
	handler := NewHandler(http.NotFoundHandler(), &stubStore{}, &stubProvider{}, testCipher(t), &stubProjector{}, HandlerConfig{}, slog.New(slog.NewTextHandler(io.Discard, nil)))
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
	projector := &stubProjector{}
	handler := NewHandler(http.NotFoundHandler(), store, provider, cipher, projector, HandlerConfig{}, slog.New(slog.NewTextHandler(io.Discard, nil)))
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
	if !projector.rebuilt || store.connection.LastSyncedAt == nil {
		t.Fatal("public availability was not rebuilt before sync completion")
	}
}

func TestCalendarSyncDoesNotMarkCompleteWhenProjectionRebuildFails(t *testing.T) {
	cipher := testCipher(t)
	encrypted, err := cipher.Encrypt("refresh-secret")
	if err != nil {
		t.Fatal(err)
	}
	store := &stubStore{connection: Connection{UserID: "user-1", RefreshTokenCipher: encrypted}}
	projector := &stubProjector{err: errors.New("projection database unavailable")}
	handler := NewHandler(http.NotFoundHandler(), store, &stubProvider{}, cipher, projector, HandlerConfig{}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	request := httptest.NewRequest(http.MethodPost, "/api/v1/calendar/sync", nil)
	request.Header.Set(auth.AuthenticatedUserHeader, "user-1")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusInternalServerError {
		t.Fatalf("expected failed sync, got %d", response.Code)
	}
	if store.connection.LastSyncedAt != nil {
		t.Fatal("failed projection rebuild was marked as a completed sync")
	}
}

func TestAccountDeletionGrantIsPreparedWithoutEarlyRevocation(t *testing.T) {
	t.Parallel()
	cipher := testCipher(t)
	encrypted, err := cipher.Encrypt("refresh-secret")
	if err != nil {
		t.Fatal(err)
	}
	provider := &stubProvider{}
	handler := NewHandler(http.NotFoundHandler(), &stubStore{connection: Connection{
		UserID: "user-1", RefreshTokenCipher: encrypted,
	}}, provider, cipher, &stubProjector{}, HandlerConfig{}, slog.New(slog.NewTextHandler(io.Discard, nil)))

	revoke, err := handler.PrepareForAccountDeletion(context.Background(), "user-1")
	if err != nil || revoke == nil {
		t.Fatalf("prepare revocation failed: %v", err)
	}
	if provider.revoked != "" {
		t.Fatal("grant was revoked before database deletion committed")
	}
	if err := revoke(context.Background()); err != nil {
		t.Fatal(err)
	}
	if provider.revoked != "refresh-secret" {
		t.Fatalf("wrong grant revoked: %q", provider.revoked)
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


type privateEventStubProvider struct {
	stubProvider
	events []PrivateEventView
}

func (provider *privateEventStubProvider) ListPrivateEvents(context.Context, string, time.Time, time.Time) ([]PrivateEventView, error) {
	return provider.events, nil
}

func TestPrivateEventsEndpointReturnsOnlyAuthenticatedOwnersCalendar(t *testing.T) {
	t.Parallel()
	cipher := testCipher(t)
	encrypted, err := cipher.Encrypt("refresh-secret")
	if err != nil {
		t.Fatal(err)
	}
	store := &stubStore{connection: Connection{UserID: "owner-1", RefreshTokenCipher: encrypted}}
	provider := &privateEventStubProvider{events: []PrivateEventView{{
		ID: "event-1", Title: "Confidential board meeting", Location: "Secret room",
	}}}
	handler := NewHandler(http.NotFoundHandler(), store, provider, cipher, &stubProjector{}, HandlerConfig{}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	url := "/api/v1/me/private-events?from=2026-09-01T00:00:00Z&to=2026-09-02T00:00:00Z"

	unauthorized := httptest.NewRecorder()
	handler.ServeHTTP(unauthorized, httptest.NewRequest(http.MethodGet, url, nil))
	if unauthorized.Code != http.StatusUnauthorized {
		t.Fatalf("unauthorized status = %d", unauthorized.Code)
	}

	request := httptest.NewRequest(http.MethodGet, url, nil)
	request.Header.Set(auth.AuthenticatedUserHeader, "owner-1")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", response.Code, response.Body.String())
	}
	body := response.Body.String()
	for _, expected := range []string{"owner-1", "Confidential board meeting", "Secret room"} {
		if !strings.Contains(body, expected) {
			t.Fatalf("missing %q: %s", expected, body)
		}
	}
	for _, forbidden := range []string{"refresh-secret", "RefreshTokenCipher", "syncToken"} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("response leaked %q", forbidden)
		}
	}
	if response.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("private response may be cached")
	}
}

func TestPrivateEventsEndpointBoundsRangesBeforeProviderAccess(t *testing.T) {
	t.Parallel()
	handler := NewHandler(http.NotFoundHandler(), &stubStore{}, &privateEventStubProvider{}, testCipher(t), &stubProjector{}, HandlerConfig{}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	for _, url := range []string{
		"/api/v1/me/private-events",
		"/api/v1/me/private-events?from=invalid&to=2026-09-02T00:00:00Z",
		"/api/v1/me/private-events?from=2026-09-02T00:00:00Z&to=2026-09-01T00:00:00Z",
		"/api/v1/me/private-events?from=2026-01-01T00:00:00Z&to=2026-03-01T00:00:00Z",
	} {
		request := httptest.NewRequest(http.MethodGet, url, nil)
		request.Header.Set(auth.AuthenticatedUserHeader, "owner-1")
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != http.StatusBadRequest && response.Code != http.StatusUnprocessableEntity {
			t.Fatalf("%s status = %d", url, response.Code)
		}
	}
}
