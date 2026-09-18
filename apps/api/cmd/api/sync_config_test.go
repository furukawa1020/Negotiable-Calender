package main

import "testing"

func TestSyncModeConfiguration(t *testing.T) {
	for _, mode := range []string{"off", "background", "external", "invalid"} {
		t.Run(mode, func(t *testing.T) {
			env := map[string]string{"CALENDAR_SYNC_MODE": mode, "CALENDAR_SYNC_AUDIENCE": "https://example.test/internal/calendar/sync-due", "CALENDAR_SYNC_SUBJECT": "123456789012345678901", "CALENDAR_SYNC_EMAIL": "sync@project.iam.gserviceaccount.com"}
			get := func(k string) string { return env[k] }
			if err := validateSyncConfig(get); (err != nil) != (mode == "invalid") {
				t.Fatal(err)
			}
			if mode == "external" {
				for _, key := range []string{"CALENDAR_SYNC_AUDIENCE", "CALENDAR_SYNC_SUBJECT", "CALENDAR_SYNC_EMAIL"} {
					old := env[key]
					env[key] = ""
					if validateSyncConfig(get) == nil {
						t.Fatalf("missing %s accepted", key)
					}
					env[key] = old
				}
			}
		})
	}
	if syncMode(func(string) string { return "" }) != "background" {
		t.Fatal("local default")
	}
	if syncMode(func(k string) string {
		if k == "K_SERVICE" {
			return "cloud-run"
		}
		return ""
	}) != "off" {
		t.Fatal("cloud run silently enabled background worker")
	}
}
