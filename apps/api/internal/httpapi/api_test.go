package httpapi

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type stubDatabase struct {
	err error
}

func (database stubDatabase) PingContext(context.Context) error {
	return database.err
}

func TestHealth(t *testing.T) {
	t.Parallel()

	request := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	response := httptest.NewRecorder()
	New(stubDatabase{}, "", testLogger()).ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("expected %d, got %d", http.StatusOK, response.Code)
	}
	if !strings.Contains(response.Body.String(), `"status":"ok"`) {
		t.Fatalf("unexpected body: %s", response.Body.String())
	}
}

func TestReadinessFailsClosedWhenDatabaseIsUnavailable(t *testing.T) {
	t.Parallel()

	request := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	response := httptest.NewRecorder()
	New(stubDatabase{err: errors.New("database unavailable")}, "", testLogger()).ServeHTTP(response, request)

	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected %d, got %d", http.StatusServiceUnavailable, response.Code)
	}
	if strings.Contains(response.Body.String(), "database unavailable") {
		t.Fatal("internal database error leaked to response")
	}
	if !strings.Contains(response.Body.String(), `"status":"unavailable"`) {
		t.Fatalf("unexpected body: %s", response.Body.String())
	}
}

func TestCORSAllowsOnlyConfiguredWebOrigin(t *testing.T) {
	t.Parallel()

	handler := New(stubDatabase{}, "https://calendar.example", testLogger())
	for _, test := range []struct {
		name           string
		origin         string
		expectedHeader string
	}{
		{name: "configured origin", origin: "https://calendar.example", expectedHeader: "https://calendar.example"},
		{name: "other origin", origin: "https://attacker.example", expectedHeader: ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, "/healthz", nil)
			request.Header.Set("Origin", test.origin)
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if actual := response.Header().Get("Access-Control-Allow-Origin"); actual != test.expectedHeader {
				t.Fatalf("expected origin %q, got %q", test.expectedHeader, actual)
			}
		})
	}
}

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}
