package main

import (
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	calendarintegration "github.com/negotiable-calendar/negotiable-calendar/apps/api/internal/calendar"
)

func syncMode(getenv func(string) string) string {
	if mode := getenv("CALENDAR_SYNC_MODE"); mode != "" {
		return mode
	}
	if getenv("K_SERVICE") != "" {
		return "off"
	}
	return "background"
}

func validateSyncConfig(getenv func(string) string) error {
	switch syncMode(getenv) {
	case "off", "background":
		return nil
	case "external":
		audience, err := url.Parse(getenv("CALENDAR_SYNC_AUDIENCE"))
		if err != nil || audience.Scheme != "https" || audience.Host == "" || audience.User != nil || audience.RawQuery != "" || audience.Fragment != "" || audience.Path != calendarintegration.ScheduledSyncPath {
			return fmt.Errorf("CALENDAR_SYNC_AUDIENCE must be the HTTPS sync endpoint")
		}
		if !regexp.MustCompile(`^[0-9]{10,30}$`).MatchString(getenv("CALENDAR_SYNC_SUBJECT")) {
			return fmt.Errorf("CALENDAR_SYNC_SUBJECT must identify the dedicated service account")
		}
		email := getenv("CALENDAR_SYNC_EMAIL")
		if !strings.HasSuffix(email, ".iam.gserviceaccount.com") || !strings.Contains(email, "@") || strings.ContainsAny(email, " \n\r\t") {
			return fmt.Errorf("CALENDAR_SYNC_EMAIL must identify the dedicated service account")
		}
		return nil
	default:
		return fmt.Errorf("CALENDAR_SYNC_MODE must be off, background or external")
	}
}

func scheduledSyncHandler(next http.Handler, store calendarintegration.BackgroundStore, handler *calendarintegration.Handler, getenv func(string) string, logger *slog.Logger) http.Handler {
	config := calendarintegration.ScheduledConfig{}
	if syncMode(getenv) == "external" {
		config = calendarintegration.ScheduledConfig{Audience: getenv("CALENDAR_SYNC_AUDIENCE"), Subject: getenv("CALENDAR_SYNC_SUBJECT"), Email: getenv("CALENDAR_SYNC_EMAIL")}
	}
	worker := calendarintegration.NewWorker(store, handler, calendarintegration.WorkerConfig{ClaimLimit: 5, SyncTimeout: 35 * time.Second, RunTimeout: 40 * time.Second}, logger)
	return calendarintegration.NewScheduledHandler(next, worker, handler.Configured, config, logger)
}
