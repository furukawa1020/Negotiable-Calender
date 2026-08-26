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

	"github.com/negotiable-calendar/negotiable-calendar/apps/api/internal/httpapi"
	"github.com/negotiable-calendar/negotiable-calendar/apps/api/internal/organization"
	"github.com/negotiable-calendar/negotiable-calendar/apps/api/internal/policy"
	"github.com/negotiable-calendar/negotiable-calendar/apps/api/internal/projection"
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
	if err := projection.EnsureSchema(migrationContext, db); err != nil {
		logger.Error("migrate projection database", "error", err)
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

	server := &http.Server{
		Addr:              ":" + port,
		Handler:           httpapi.New(db, policy.NewPostgresStore(db), projection.NewPostgresStore(db), organization.NewPostgresStore(db), os.Getenv("WEB_ORIGIN"), logger),
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
