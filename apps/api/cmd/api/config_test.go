package main

import (
	"encoding/base64"
	"strings"
	"testing"
)

func authTestConfig() map[string]string {
	return map[string]string{
		"DEMO_MODE": "false", "WEB_ORIGIN": "https://calendar.example.com",
		"GOOGLE_CLIENT_ID": "test-client", "GOOGLE_CLIENT_SECRET": "secret-do-not-log",
		"GOOGLE_REDIRECT_URL":           "https://api.example.com/api/v1/auth/google/callback",
		"GOOGLE_CALENDAR_REDIRECT_URL":  "https://api.example.com/api/v1/calendar/google/callback",
		"CALENDAR_TOKEN_ENCRYPTION_KEY": base64.StdEncoding.EncodeToString(make([]byte, 32)),
	}
}

func TestValidateAuthConfig(t *testing.T) {
	tests := []struct{ name, key, value string }{
		{"bad mode", "DEMO_MODE", "FALSE"},
		{"missing client", "GOOGLE_CLIENT_ID", ""},
		{"blank secret", "GOOGLE_CLIENT_SECRET", "  "},
		{"insecure cookie", "COOKIE_SECURE", "false"},
		{"public http", "WEB_ORIGIN", "http://calendar.example.com"},
		{"origin path", "WEB_ORIGIN", "https://calendar.example.com/path"},
		{"origin query", "WEB_ORIGIN", "https://calendar.example.com?token=secret-do-not-log"},
		{"bad URL", "GOOGLE_REDIRECT_URL", "https://%secret-do-not-log"},
		{"wrong callback", "GOOGLE_REDIRECT_URL", "https://api.example.com/wrong"},
		{"callback query", "GOOGLE_REDIRECT_URL", "https://api.example.com/api/v1/auth/google/callback?x=1"},
		{"callback fragment", "GOOGLE_REDIRECT_URL", "https://api.example.com/api/v1/auth/google/callback#x"},
		{"callback userinfo", "GOOGLE_REDIRECT_URL", "https://secret-do-not-log@api.example.com/api/v1/auth/google/callback"},
		{"http callback", "GOOGLE_REDIRECT_URL", "http://api.example.com/api/v1/auth/google/callback"},
		{"mixed local callback", "GOOGLE_REDIRECT_URL", "http://localhost:8080/api/v1/auth/google/callback"},
		{"missing calendar callback", "GOOGLE_CALENDAR_REDIRECT_URL", ""},
		{"wrong calendar callback", "GOOGLE_CALENDAR_REDIRECT_URL", "https://api.example.com/api/v1/auth/google/callback"},
		{"missing key", "CALENDAR_TOKEN_ENCRYPTION_KEY", ""},
		{"bad key", "CALENDAR_TOKEN_ENCRYPTION_KEY", "secret-do-not-log"},
		{"short key", "CALENDAR_TOKEN_ENCRYPTION_KEY", base64.StdEncoding.EncodeToString(make([]byte, 16))},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := authTestConfig()
			config[tt.key] = tt.value
			err := validateAuthConfig(func(key string) string { return config[key] })
			if err == nil {
				t.Fatal("invalid configuration accepted")
			}
			if !strings.Contains(err.Error(), tt.key) {
				t.Fatalf("error does not identify setting: %v", err)
			}
			if strings.Contains(err.Error(), "secret-do-not-log") {
				t.Fatal("configuration value leaked")
			}
		})
	}
}

func TestValidAuthConfig(t *testing.T) {
	for _, mode := range []string{"false", ""} {
		config := authTestConfig()
		config["DEMO_MODE"] = mode
		if err := validateAuthConfig(func(key string) string { return config[key] }); err != nil {
			t.Fatal(err)
		}
	}
}

func TestExplicitDemoNeedsNoOAuthCredentials(t *testing.T) {
	err := validateAuthConfig(func(key string) string {
		if key == "DEMO_MODE" {
			return "true"
		}
		return ""
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestLocalOAuthConfig(t *testing.T) {
	config := authTestConfig()
	config["WEB_ORIGIN"] = "http://localhost:3000"
	config["GOOGLE_REDIRECT_URL"] = "http://localhost:8080/api/v1/auth/google/callback"
	config["GOOGLE_CALENDAR_REDIRECT_URL"] = "http://localhost:8080/api/v1/calendar/google/callback"
	config["COOKIE_SECURE"] = "false"
	if err := validateAuthConfig(func(key string) string { return config[key] }); err != nil {
		t.Fatal(err)
	}
}
