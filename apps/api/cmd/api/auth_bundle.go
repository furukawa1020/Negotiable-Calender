package main

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"

	calendarintegration "github.com/negotiable-calendar/negotiable-calendar/apps/api/internal/calendar"
)

// authSecretBundle is supplied by the runtime secret store, never a Web build.
type authSecretBundle struct {
	Version                    int    `json:"version"`
	GoogleClientID             string `json:"googleClientId"`
	GoogleClientSecret         string `json:"googleClientSecret"`
	CalendarTokenEncryptionKey string `json:"calendarTokenEncryptionKey"`
}

// Expand once before validation/storage initialization. Direct env configuration
// remains supported, but ambiguous mixed configurations fail closed.
func loadAuthSecretBundle(getenv func(string) string, setenv func(string, string) error) error {
	raw := getenv("AUTH_SECRETS_JSON")
	if raw == "" {
		return nil
	}
	if len(raw) > 16384 {
		return fmt.Errorf("AUTH_SECRETS_JSON exceeds size limit")
	}
	var bundle authSecretBundle
	decoder := json.NewDecoder(strings.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&bundle); err != nil {
		return fmt.Errorf("AUTH_SECRETS_JSON must be a valid secret bundle")
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return fmt.Errorf("AUTH_SECRETS_JSON must contain exactly one object")
	}
	if bundle.Version != 1 {
		return fmt.Errorf("AUTH_SECRETS_JSON version must be 1")
	}
	values := []struct{ key, value string }{
		{"GOOGLE_CLIENT_ID", bundle.GoogleClientID},
		{"GOOGLE_CLIENT_SECRET", bundle.GoogleClientSecret},
		{"CALENDAR_TOKEN_ENCRYPTION_KEY", bundle.CalendarTokenEncryptionKey},
	}
	for _, entry := range values {
		if strings.TrimSpace(entry.value) == "" || strings.ContainsRune(entry.value, '\x00') {
			return fmt.Errorf("AUTH_SECRETS_JSON requires valid %s", entry.key)
		}
		if existing := getenv(entry.key); existing != "" && existing != entry.value {
			return fmt.Errorf("AUTH_SECRETS_JSON conflicts with %s", entry.key)
		}
	}
	if _, err := calendarintegration.NewTokenCipher(bundle.CalendarTokenEncryptionKey); err != nil {
		return fmt.Errorf("AUTH_SECRETS_JSON requires a base64 32-byte encryption key")
	}
	for _, entry := range values {
		if err := setenv(entry.key, entry.value); err != nil {
			return fmt.Errorf("AUTH_SECRETS_JSON could not configure %s", entry.key)
		}
	}
	return nil
}
