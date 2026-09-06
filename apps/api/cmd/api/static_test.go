package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestStaticFilesServeAssetsAndSPAFallbackWithoutInterceptingAPI(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "assets"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "index.html"), []byte("<main>Negotiable Calendar</main>"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "assets", "app.js"), []byte("export {}"), 0o600); err != nil {
		t.Fatal(err)
	}
	next := http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		response.Header().Set("X-Next-Path", request.URL.Path)
		response.WriteHeader(http.StatusTeapot)
	})
	handler := withStaticFiles(next, root)

	for _, test := range []struct {
		method, target, body string
		status               int
		next                 bool
	}{
		{method: http.MethodGet, target: "/", body: "Negotiable Calendar", status: http.StatusOK},
		{method: http.MethodGet, target: "/settings/privacy", body: "Negotiable Calendar", status: http.StatusOK},
		{method: http.MethodGet, target: "/assets/app.js", body: "export {}", status: http.StatusOK},
		{method: http.MethodGet, target: "/assets/missing.js", status: http.StatusNotFound},
		{method: http.MethodGet, target: "/api/v1/status", status: http.StatusTeapot, next: true},
		{method: http.MethodGet, target: "/healthz", status: http.StatusTeapot, next: true},
		{method: http.MethodPost, target: "/settings/privacy", status: http.StatusTeapot, next: true},
	} {
		t.Run(test.method+" "+test.target, func(t *testing.T) {
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, httptest.NewRequest(test.method, test.target, nil))
			if response.Code != test.status {
				t.Fatalf("status = %d, want %d", response.Code, test.status)
			}
			if test.body != "" && !strings.Contains(response.Body.String(), test.body) {
				t.Fatalf("body = %q", response.Body.String())
			}
			if got := response.Header().Get("X-Next-Path"); (got != "") != test.next {
				t.Fatalf("next path = %q, next = %v", got, test.next)
			}
		})
	}
}

func TestStaticFilesDisabledDelegatesEveryRequest(t *testing.T) {
	t.Parallel()
	called := false
	handler := withStaticFiles(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		called = true
		response.WriteHeader(http.StatusNoContent)
	}), "")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/", nil))
	if !called || response.Code != http.StatusNoContent {
		t.Fatalf("called = %v, status = %d", called, response.Code)
	}
}
