package main

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func validAuthBundle(t *testing.T) string {
	t.Helper()
	config := authTestConfig()
	data, err := json.Marshal(authSecretBundle{1, config["GOOGLE_CLIENT_ID"], config["GOOGLE_CLIENT_SECRET"], config["CALENDAR_TOKEN_ENCRYPTION_KEY"]})
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestAuthSecretBundleLoadsBeforeAuthValidation(t *testing.T) {
	for _, mixed := range []bool{false, true} {
		config := authTestConfig()
		if !mixed {
			delete(config, "GOOGLE_CLIENT_ID")
			delete(config, "GOOGLE_CLIENT_SECRET")
			delete(config, "CALENDAR_TOKEN_ENCRYPTION_KEY")
		}
		config["AUTH_SECRETS_JSON"] = validAuthBundle(t)
		get := func(key string) string { return config[key] }
		if err := loadAuthSecretBundle(get, func(k, v string) error { config[k] = v; return nil }); err != nil {
			t.Fatal(err)
		}
		if err := validateAuthConfig(get); err != nil {
			t.Fatal(err)
		}
	}
}

func TestAuthSecretBundleRejectsInvalidWithoutWritesOrLeaks(t *testing.T) {
	valid := validAuthBundle(t)
	for name, raw := range map[string]string{
		"syntax":    "secret-do-not-log",
		"null":      "null",
		"array":     "[]",
		"version":   strings.Replace(valid, `"version":1`, `"version":2`, 1),
		"unknown":   strings.Replace(valid, `"version":1`, `"secret-do-not-log":1,"version":1`, 1),
		"missing":   `{"version":1}`,
		"blank":     strings.Replace(valid, "secret-do-not-log", "   ", 1),
		"nul":       strings.Replace(valid, "secret-do-not-log", `secret-do-not-log\u0000`, 1),
		"bad-key":   strings.Replace(valid, authTestConfig()["CALENDAR_TOKEN_ENCRYPTION_KEY"], "secret-do-not-log", 1),
		"trailing":  valid + " null",
		"oversized": strings.Repeat("secret-do-not-log", 2000),
	} {
		t.Run(name, func(t *testing.T) {
			writes := 0
			err := loadAuthSecretBundle(func(k string) string {
				if k == "AUTH_SECRETS_JSON" {
					return raw
				}
				return ""
			}, func(k, v string) error { writes++; return nil })
			if err == nil || writes != 0 {
				t.Fatalf("invalid bundle accepted or written: %d", writes)
			}
			if strings.Contains(err.Error(), "secret-do-not-log") {
				t.Fatal("secret leaked")
			}
		})
	}
}

func TestAuthSecretBundleCompatibilityAndConflict(t *testing.T) {
	if err := loadAuthSecretBundle(func(string) string { return "" }, func(string, string) error { t.Fatal("unexpected write"); return nil }); err != nil {
		t.Fatal(err)
	}
	config := authTestConfig()
	config["AUTH_SECRETS_JSON"] = validAuthBundle(t)
	for _, key := range []string{"GOOGLE_CLIENT_ID", "GOOGLE_CLIENT_SECRET", "CALENDAR_TOKEN_ENCRYPTION_KEY"} {
		old := config[key]
		config[key] = "conflicting-secret"
		err := loadAuthSecretBundle(func(k string) string { return config[k] }, func(string, string) error { t.Fatal("conflict wrote config"); return nil })
		if err == nil || !strings.Contains(err.Error(), key) || strings.Contains(err.Error(), "conflicting-secret") {
			t.Fatal("unsafe conflict handling")
		}
		config[key] = old
	}
	err := loadAuthSecretBundle(func(k string) string { return config[k] }, func(string, string) error { return errors.New("secret-do-not-log") })
	if err == nil || strings.Contains(err.Error(), "secret-do-not-log") {
		t.Fatal("unsafe setter error")
	}
}
