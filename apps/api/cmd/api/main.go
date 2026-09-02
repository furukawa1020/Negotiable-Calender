package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/negotiable-calendar/negotiable-calendar/apps/api/internal/audit"
	"github.com/negotiable-calendar/negotiable-calendar/apps/api/internal/auth"
	calendarintegration "github.com/negotiable-calendar/negotiable-calendar/apps/api/internal/calendar"
	"github.com/negotiable-calendar/negotiable-calendar/apps/api/internal/httpapi"
	"github.com/negotiable-calendar/negotiable-calendar/apps/api/internal/notification"
	"github.com/negotiable-calendar/negotiable-calendar/apps/api/internal/organization"
	"github.com/negotiable-calendar/negotiable-calendar/apps/api/internal/policy"
	"github.com/negotiable-calendar/negotiable-calendar/apps/api/internal/projection"
	coordinationrequest "github.com/negotiable-calendar/negotiable-calendar/apps/api/internal/request"
)

const defaultPort = "8080"

func main() {
	if len(os.Args) > 1 && os.Args[1] == "healthcheck" {
		if err := checkHealth(); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}

	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		logger.Error("DATABASE_URL is required")
		os.Exit(1)
	}

	db, err := sql.Open("pgx", databaseURL)
	if err != nil {
		logger.Error("open database", "error", err)
		os.Exit(1)
	}
	defer db.Close()
	migrationContext, cancelMigration := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancelMigration()
	if err := policy.EnsureSchema(migrationContext, db); err != nil {
		logger.Error("migrate database", "error", err)
		os.Exit(1)
	}
	if err := organization.EnsureSchema(migrationContext, db); err != nil {
		logger.Error("migrate organization database", "error", err)
		os.Exit(1)
	}
	if err := auth.EnsureSchema(migrationContext, db); err != nil {
		logger.Error("migrate authentication database", "error", err)
		os.Exit(1)
	}
	if err := calendarintegration.EnsureSchema(migrationContext, db); err != nil {
		logger.Error("migrate calendar integration database", "error", err)
		os.Exit(1)
	}
	if err := projection.EnsureSchema(migrationContext, db); err != nil {
		logger.Error("migrate projection database", "error", err)
		os.Exit(1)
	}
	if err := coordinationrequest.EnsureSchema(migrationContext, db); err != nil {
		logger.Error("migrate coordination request database", "error", err)
		os.Exit(1)
	}
	if err := notification.EnsureSchema(migrationContext, db); err != nil {
		logger.Error("migrate notification database", "error", err)
		os.Exit(1)
	}
	if err := audit.EnsureSchema(migrationContext, db); err != nil {
		logger.Error("migrate audit database", "error", err)
		os.Exit(1)
	}
	if os.Getenv("DEMO_MODE") == "true" {
		if err := organization.SeedDemo(migrationContext, db, time.Now().UTC()); err != nil {
			logger.Error("seed demo organization", "error", err)
			os.Exit(1)
		}
		if err := projection.SeedDemo(migrationContext, db, time.Now()); err != nil {
			logger.Error("seed demo projections", "error", err)
			os.Exit(1)
		}
	}

	port := os.Getenv("PORT")
	if port == "" {
		port = defaultPort
	}

	demoMode := os.Getenv("DEMO_MODE") == "true"
	secureCookies := os.Getenv("COOKIE_SECURE") != "false"
	googleProvider := auth.NewGoogleProvider(auth.GoogleConfig{
		ClientID: os.Getenv("GOOGLE_CLIENT_ID"), ClientSecret: os.Getenv("GOOGLE_CLIENT_SECRET"),
		RedirectURL: os.Getenv("GOOGLE_REDIRECT_URL"),
	}, &http.Client{Timeout: 10 * time.Second})
	calendarProvider := calendarintegration.NewGoogleProvider(calendarintegration.GoogleConfig{
		ClientID: os.Getenv("GOOGLE_CLIENT_ID"), ClientSecret: os.Getenv("GOOGLE_CLIENT_SECRET"),
		RedirectURL: os.Getenv("GOOGLE_CALENDAR_REDIRECT_URL"),
	}, &http.Client{Timeout: 10 * time.Second})
	var calendarCipher *calendarintegration.TokenCipher
	if encodedKey := os.Getenv("CALENDAR_TOKEN_ENCRYPTION_KEY"); encodedKey != "" {
		calendarCipher, err = calendarintegration.NewTokenCipher(encodedKey)
		if err != nil {
			logger.Error("configure calendar token encryption", "error", err)
			os.Exit(1)
		}
	}
	apiHandler := httpapi.NewWithStores(db, policy.NewPostgresStore(db), projection.NewPostgresStore(db), organization.NewPostgresStore(db), coordinationrequest.NewPostgresStore(db), notification.NewPostgresStore(db), audit.NewPostgresStore(db), os.Getenv("WEB_ORIGIN"), logger)
	calendarHandler := calendarintegration.NewHandler(apiHandler, calendarintegration.NewPostgresStore(db), calendarProvider, calendarCipher, calendarintegration.HandlerConfig{
		WebOrigin: os.Getenv("WEB_ORIGIN"), SecureCookies: secureCookies,
	}, logger)
	handler := auth.NewHandler(calendarHandler, auth.NewPostgresStore(db), googleProvider, auth.HandlerConfig{
		WebOrigin: os.Getenv("WEB_ORIGIN"), DemoMode: demoMode, SecureCookies: secureCookies,
	}, logger)

	server := &http.Server{
		Addr:              ":" + port,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	shutdownSignal, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	go func() {
		logger.Info("api listening", "address", server.Addr)
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("serve api", "error", err)
			os.Exit(1)
		}
	}()

	<-shutdownSignal.Done()
	shutdownContext, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := server.Shutdown(shutdownContext); err != nil {
		logger.Error("shutdown api", "error", err)
		os.Exit(1)
	}
	logger.Info("api stopped")
}

func checkHealth() error {
	client := &http.Client{Timeout: 2 * time.Second}
	response, err := client.Get("http://127.0.0.1:" + envOrDefault("PORT", defaultPort) + "/healthz")
	if err != nil {
		return fmt.Errorf("health request: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("health status: %s", response.Status)
	}
	return nil
}

func envOrDefault(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
