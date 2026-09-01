package auth

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestGoogleAuthorizationURLUsesLoginScopesAndPKCE(t *testing.T) {
	t.Parallel()
	provider := NewGoogleProvider(GoogleConfig{ClientID: "client-1", RedirectURL: "https://app.example/callback"}, nil)
	value, err := url.Parse(provider.AuthorizationURL("state-1", "challenge-1"))
	if err != nil {
		t.Fatal(err)
	}
	query := value.Query()
	if query.Get("state") != "state-1" || query.Get("code_challenge") != "challenge-1" || query.Get("code_challenge_method") != "S256" {
		t.Fatalf("missing oauth protections: %s", value.RawQuery)
	}
	if query.Get("scope") != "openid profile email" || strings.Contains(query.Get("scope"), "calendar") {
		t.Fatalf("login requested incorrect scopes: %q", query.Get("scope"))
	}
}

func TestGoogleExchangeUsesVerifierAndVerifiedUserInfo(t *testing.T) {
	t.Parallel()
	mux := http.NewServeMux()
	mux.HandleFunc("POST /token", func(response http.ResponseWriter, request *http.Request) {
		if err := request.ParseForm(); err != nil {
			t.Fatal(err)
		}
		if request.Form.Get("code") != "code-1" || request.Form.Get("code_verifier") != "verifier-1" {
			t.Fatalf("unexpected exchange form: %#v", request.Form)
		}
		_ = json.NewEncoder(response).Encode(map[string]string{"access_token": "access-1"})
	})
	mux.HandleFunc("GET /userinfo", func(response http.ResponseWriter, request *http.Request) {
		if request.Header.Get("Authorization") != "Bearer access-1" {
			t.Fatalf("missing bearer token: %q", request.Header.Get("Authorization"))
		}
		_ = json.NewEncoder(response).Encode(map[string]any{
			"sub": "google-1", "email": "person@example.com", "email_verified": true, "name": "Person",
		})
	})
	server := httptest.NewServer(mux)
	defer server.Close()
	provider := NewGoogleProvider(GoogleConfig{
		ClientID: "client-1", RedirectURL: "https://app.example/callback",
		TokenURL: server.URL + "/token", UserInfoURL: server.URL + "/userinfo",
	}, server.Client())

	profile, err := provider.Exchange(context.Background(), "code-1", "verifier-1")
	if err != nil {
		t.Fatal(err)
	}
	if profile.Subject != "google-1" || profile.Email != "person@example.com" || !profile.EmailVerified {
		t.Fatalf("unexpected profile: %#v", profile)
	}
}

func TestGoogleExchangeRejectsUnverifiedEmail(t *testing.T) {
	t.Parallel()
	mux := http.NewServeMux()
	mux.HandleFunc("POST /token", func(response http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(response).Encode(map[string]string{"access_token": "access-1"})
	})
	mux.HandleFunc("GET /userinfo", func(response http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(response).Encode(map[string]any{"sub": "google-1", "email": "person@example.com", "email_verified": false})
	})
	server := httptest.NewServer(mux)
	defer server.Close()
	provider := NewGoogleProvider(GoogleConfig{ClientID: "client", RedirectURL: "https://app/callback", TokenURL: server.URL + "/token", UserInfoURL: server.URL + "/userinfo"}, server.Client())
	if _, err := provider.Exchange(context.Background(), "code", "verifier"); err == nil {
		t.Fatal("unverified email was accepted")
	}
}
