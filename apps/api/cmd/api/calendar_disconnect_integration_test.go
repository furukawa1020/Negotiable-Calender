package main

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/negotiable-calendar/negotiable-calendar/apps/api/internal/auth"
	calendarintegration "github.com/negotiable-calendar/negotiable-calendar/apps/api/internal/calendar"
	"github.com/negotiable-calendar/negotiable-calendar/apps/api/internal/organization"
	"github.com/negotiable-calendar/negotiable-calendar/apps/api/internal/policy"
	"github.com/negotiable-calendar/negotiable-calendar/apps/api/internal/projection"
)

func TestCalendarDisconnectPostgresAtomicity(t *testing.T) {
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is not configured")
	}
	db, err := sql.Open("pgx", databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	schema := fmt.Sprintf("calendar_disconnect_%d", time.Now().UnixNano())
	exec := func(query string, args ...any) {
		t.Helper()
		if _, err := db.ExecContext(ctx, query, args...); err != nil {
			t.Fatal(err)
		}
	}
	exec("CREATE SCHEMA " + schema)
	defer db.ExecContext(context.Background(), "DROP SCHEMA "+schema+" CASCADE")
	exec("SET search_path TO " + schema)
	for _, migrate := range []func(context.Context, *sql.DB) error{policy.EnsureSchema, organization.EnsureSchema, auth.EnsureSchema, calendarintegration.EnsureSchema, calendarintegration.EnsureBackgroundSchema, projection.EnsureSchema} {
		if err := migrate(ctx, db); err != nil {
			t.Fatal(err)
		}
	}
	now := time.Now().UTC().Truncate(time.Second)
	store := calendarintegration.NewPostgresStore(db)
	for _, id := range []string{"alice", "bob"} {
		exec("INSERT INTO users(id,email,display_name,timezone,created_at,updated_at) VALUES($1,$2,$1,'Asia/Tokyo',$3,$3)", id, id+"@example.test", now)
		if err := store.SaveConnection(ctx, calendarintegration.Connection{UserID: id, RefreshTokenCipher: []byte("synthetic"), GrantedScopes: []string{calendarintegration.CalendarReadonlyScope}, ConnectedAt: now}); err != nil {
			t.Fatal(err)
		}
		if err := store.ReplaceBusySpans(ctx, id, []calendarintegration.BusySpan{{ProviderEventID: "event", CalendarID: "primary", StartAt: now, EndAt: now.Add(time.Hour), Busy: true}}, now, now.Add(time.Hour), now); err != nil {
			t.Fatal(err)
		}
		exec("INSERT INTO schedule_projections(id,user_id,start_at,end_at,availability,interruptibility,requestability,reschedulability,expected_response_bucket,generated_at,expires_at) VALUES($1,$1,$2,$3,'BUSY','NORMAL','OPEN','MEDIUM','later',$2,$3)", id, now, now.Add(time.Hour))
	}
	handler := calendarintegration.NewHandler(http.NotFoundHandler(), store, nil, nil, nil, calendarintegration.HandlerConfig{}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	disconnect := func(want int) {
		t.Helper()
		r := httptest.NewRequest(http.MethodDelete, "/api/v1/calendar/connection", nil).WithContext(ctx)
		r.Header.Set(auth.AuthenticatedUserHeader, "alice")
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		if w.Code != want {
			t.Fatalf("disconnect status=%d want=%d", w.Code, want)
		}
	}
	count := func(table, user string, want int) {
		t.Helper()
		var got int
		if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+table+" WHERE user_id=$1", user).Scan(&got); err != nil {
			t.Fatal(err)
		}
		if got != want {
			t.Fatalf("%s/%s=%d want=%d", table, user, got, want)
		}
	}
	// A failure after deleting projections/spans must roll the entire operation back.
	exec("CREATE FUNCTION reject_disconnect() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'synthetic disconnect failure'; END $$")
	exec("CREATE TRIGGER reject_disconnect BEFORE DELETE ON calendar_connections FOR EACH ROW EXECUTE FUNCTION reject_disconnect()")
	disconnect(http.StatusInternalServerError)
	for _, table := range []string{"schedule_projections", "private_events", "calendar_connections"} {
		count(table, "alice", 1)
		count(table, "bob", 1)
	}
	exec("DROP TRIGGER reject_disconnect ON calendar_connections")
	disconnect(http.StatusNoContent)
	disconnect(http.StatusNoContent)
	for _, table := range []string{"schedule_projections", "private_events", "calendar_connections"} {
		count(table, "alice", 0)
		count(table, "bob", 1)
	}
}
