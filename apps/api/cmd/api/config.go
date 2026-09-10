package main

import (
	"fmt"
	"net/url"
	"strings"

	calendarintegration "github.com/negotiable-calendar/negotiable-calendar/apps/api/internal/calendar"
)

// Validate before storage initialization so a misconfigured release never becomes ready.
func validateAuthConfig(getenv func(string) string) error {
	mode := getenv("DEMO_MODE")
	if mode == "true" {
		return nil
	}
	if mode != "" && mode != "false" {
		return fmt.Errorf("DEMO_MODE must be true or false")
	}
	for _, key := range []string{"GOOGLE_CLIENT_ID", "GOOGLE_CLIENT_SECRET", "WEB_ORIGIN", "GOOGLE_REDIRECT_URL", "GOOGLE_CALENDAR_REDIRECT_URL", "CALENDAR_TOKEN_ENCRYPTION_KEY"} {
		if strings.TrimSpace(getenv(key)) == "" {
			return fmt.Errorf("%s is required outside demo mode", key)
		}
	}
	origin, err := url.Parse(getenv("WEB_ORIGIN"))
	if err != nil || !validAuthURL(origin) || origin.Path != "" {
		return fmt.Errorf("WEB_ORIGIN must be an HTTPS origin (HTTP loopback is allowed locally)")
	}
	if origin.Scheme == "https" && getenv("COOKIE_SECURE") == "false" {
		return fmt.Errorf("COOKIE_SECURE must be enabled for HTTPS")
	}
	for _, callback := range []struct{ key, path string }{
		{"GOOGLE_REDIRECT_URL", "/api/v1/auth/google/callback"},
		{"GOOGLE_CALENDAR_REDIRECT_URL", "/api/v1/calendar/google/callback"},
	} {
		u, err := url.Parse(getenv(callback.key))
		if err != nil || !validAuthURL(u) || u.EscapedPath() != callback.path || (origin.Scheme == "https" && u.Scheme != "https") {
			return fmt.Errorf("%s must be a secure URL with the expected callback path", callback.key)
		}
	}
	if _, err := calendarintegration.NewTokenCipher(getenv("CALENDAR_TOKEN_ENCRYPTION_KEY")); err != nil {
		return fmt.Errorf("CALENDAR_TOKEN_ENCRYPTION_KEY must encode a 32-byte key in base64")
	}
	return nil
}

func validAuthURL(u *url.URL) bool {
	if u == nil || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || u.Opaque != "" {
		return false
	}
	if u.Scheme == "https" {
		return true
	}
	return u.Scheme == "http" && (u.Hostname() == "localhost" || u.Hostname() == "127.0.0.1" || u.Hostname() == "::1")
}
