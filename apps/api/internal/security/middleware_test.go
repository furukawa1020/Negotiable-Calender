package security

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestCookieAuthenticatedMutationRequiresConfiguredOrigin(t *testing.T) {
	t.Parallel()
	calls := 0
	handler := New(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		calls++
		response.WriteHeader(http.StatusNoContent)
	}), Config{WebOrigin: "https://calendar.example"})

	for _, test := range []struct {
		name   string
		origin string
		want   int
	}{
		{name: "missing", want: http.StatusForbidden},
		{name: "different", origin: "https://evil.example", want: http.StatusForbidden},
		{name: "configured", origin: "https://calendar.example/", want: http.StatusNoContent},
	} {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodPost, "/api/v1/requests/request-1/accept", nil)
			request.AddCookie(&http.Cookie{Name: sessionCookieName, Value: "secret-session"})
			if test.origin != "" {
				request.Header.Set("Origin", test.origin)
			}
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != test.want {
				t.Fatalf("status = %d, want %d", response.Code, test.want)
			}
		})
	}
	if calls != 1 {
		t.Fatalf("downstream calls = %d, want 1", calls)
	}
}

func TestReadRequestDoesNotRequireOrigin(t *testing.T) {
	t.Parallel()
	handler := New(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		response.WriteHeader(http.StatusNoContent)
	}), Config{WebOrigin: "https://calendar.example"})
	request := httptest.NewRequest(http.MethodGet, "/api/v1/requests", nil)
	request.AddCookie(&http.Cookie{Name: sessionCookieName, Value: "secret-session"})
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusNoContent {
		t.Fatalf("status = %d", response.Code)
	}
}

func TestPreflightRejectsUnconfiguredOrigin(t *testing.T) {
	t.Parallel()
	handler := New(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		response.WriteHeader(http.StatusNoContent)
	}), Config{WebOrigin: "https://calendar.example"})
	request := httptest.NewRequest(http.MethodOptions, "/api/v1/requests", nil)
	request.Header.Set("Origin", "https://evil.example")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusForbidden {
		t.Fatalf("status = %d", response.Code)
	}
}

func TestRequestLimitUsesHashedSessionAndResets(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 9, 3, 0, 0, 0, 0, time.UTC)
	middleware := New(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		response.WriteHeader(http.StatusCreated)
	}), Config{WebOrigin: "https://calendar.example", RequestLimit: 2, Window: time.Minute}).(*Middleware)
	middleware.now = func() time.Time { return now }

	request := func() *http.Request {
		value := httptest.NewRequest(http.MethodPost, "/api/v1/requests", nil)
		value.Header.Set("Origin", "https://calendar.example")
		value.AddCookie(&http.Cookie{Name: sessionCookieName, Value: "raw-session-must-not-be-a-key"})
		return value
	}
	for index, want := range []int{http.StatusCreated, http.StatusCreated, http.StatusTooManyRequests} {
		response := httptest.NewRecorder()
		middleware.ServeHTTP(response, request())
		if response.Code != want {
			t.Fatalf("attempt %d status = %d, want %d", index+1, response.Code, want)
		}
		if want == http.StatusTooManyRequests {
			if response.Header().Get("Retry-After") == "" {
				t.Fatal("Retry-After is missing")
			}
			body := response.Body.String()
			for _, forbidden := range []string{"raw-session", "candidate", "event", "calendar"} {
				if strings.Contains(body, forbidden) {
					t.Fatalf("rate-limit response leaks %q: %s", forbidden, body)
				}
			}
		}
	}
	for key := range middleware.buckets {
		if strings.Contains(key, "raw-session-must-not-be-a-key") {
			t.Fatal("raw session was retained in limiter key")
		}
	}
	now = now.Add(time.Minute)
	response := httptest.NewRecorder()
	middleware.ServeHTTP(response, request())
	if response.Code != http.StatusCreated {
		t.Fatalf("status after reset = %d", response.Code)
	}
}

func TestOAuthLimitIgnoresForwardedHeaders(t *testing.T) {
	t.Parallel()
	handler := New(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		response.WriteHeader(http.StatusFound)
	}), Config{AuthLimit: 1, Window: time.Minute})

	for index, forwarded := range []string{"198.51.100.1", "203.0.113.10"} {
		request := httptest.NewRequest(http.MethodGet, "/api/v1/auth/google/login", nil)
		request.RemoteAddr = "192.0.2.5:4321"
		request.Header.Set("X-Forwarded-For", forwarded)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		want := http.StatusFound
		if index == 1 {
			want = http.StatusTooManyRequests
		}
		if response.Code != want {
			t.Fatalf("attempt %d status = %d, want %d", index+1, response.Code, want)
		}
	}
}

func TestSecurityHeadersAreAlwaysSet(t *testing.T) {
	t.Parallel()
	handler := New(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		response.WriteHeader(http.StatusNoContent)
	}), Config{})
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	for _, name := range []string{
		"X-Content-Type-Options", "X-Frame-Options", "Referrer-Policy",
		"Content-Security-Policy", "Permissions-Policy", "Cache-Control",
	} {
		if response.Header().Get(name) == "" {
			t.Fatalf("%s is missing", name)
		}
	}
}

func TestLimiterBoundsDistinctKeys(t *testing.T) {
	t.Parallel()
	middleware := New(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		response.WriteHeader(http.StatusFound)
	}), Config{AuthLimit: 2, MaxKeys: 1, Window: time.Minute}).(*Middleware)

	first := httptest.NewRequest(http.MethodGet, "/api/v1/auth/google/login", nil)
	first.RemoteAddr = "192.0.2.1:1000"
	firstResponse := httptest.NewRecorder()
	middleware.ServeHTTP(firstResponse, first)
	if firstResponse.Code != http.StatusFound {
		t.Fatalf("first status = %d", firstResponse.Code)
	}

	second := httptest.NewRequest(http.MethodGet, "/api/v1/auth/google/login", nil)
	second.RemoteAddr = "192.0.2.2:1000"
	secondResponse := httptest.NewRecorder()
	middleware.ServeHTTP(secondResponse, second)
	if secondResponse.Code != http.StatusTooManyRequests {
		t.Fatalf("new key beyond bound status = %d", secondResponse.Code)
	}
}
