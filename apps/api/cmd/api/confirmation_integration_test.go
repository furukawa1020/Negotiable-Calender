package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/negotiable-calendar/negotiable-calendar/apps/api/internal/audit"
	calendarintegration "github.com/negotiable-calendar/negotiable-calendar/apps/api/internal/calendar"
	"github.com/negotiable-calendar/negotiable-calendar/apps/api/internal/notification"
	"github.com/negotiable-calendar/negotiable-calendar/apps/api/internal/organization"
	"github.com/negotiable-calendar/negotiable-calendar/apps/api/internal/projection"
	coordinationrequest "github.com/negotiable-calendar/negotiable-calendar/apps/api/internal/request"
)

func TestPostgresAtomicConfirmation(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL is not configured")
	}
	admin, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	schema := fmt.Sprintf("confirmation_%d", time.Now().UnixNano())
	if _, err := admin.ExecContext(ctx, "CREATE SCHEMA "+schema); err != nil {
		t.Fatal(err)
	}
	defer admin.ExecContext(context.Background(), "DROP SCHEMA "+schema+" CASCADE")
	config, err := pgx.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	config.RuntimeParams["search_path"] = schema
	db := stdlib.OpenDB(*config)
	defer db.Close()
	db.SetMaxOpenConns(4)
	for _, migrate := range []func(context.Context, *sql.DB) error{organization.EnsureSchema, calendarintegration.EnsureSchema, calendarintegration.EnsureBackgroundSchema, projection.EnsureSchema, coordinationrequest.EnsureSchema, notification.EnsureSchema, audit.EnsureSchema} {
		if err := migrate(ctx, db); err != nil {
			t.Fatal(err)
		}
	}
	exec := func(query string, args ...any) {
		t.Helper()
		if _, err := db.ExecContext(ctx, query, args...); err != nil {
			t.Fatal(err)
		}
	}
	now := time.Now().UTC().Truncate(time.Second)
	start := now.Add(time.Hour)
	for _, id := range []string{"alice", "bob", "carol"} {
		exec("INSERT INTO users(id,email,display_name,timezone,created_at,updated_at) VALUES($1,$2,$1,'UTC',$3,$3)", id, id+"@example.test", now)
		exec("INSERT INTO schedule_projections VALUES($1,$1,$2,$3,'available','normal','open','medium','soon',$2,$3)", id, now, now.Add(48*time.Hour))
	}
	for _, id := range []string{"org", "other"} {
		exec("INSERT INTO organizations VALUES($1,$1,$2,$2)", id, now)
	}
	store := coordinationrequest.NewPostgresStore(db)
	fixture := func(id, requester, target string, at time.Time) coordinationrequest.CoordinationRequest {
		end := at.Add(30 * time.Minute)
		return coordinationrequest.CoordinationRequest{ID: id, OrganizationID: "org", RequesterUserID: requester, TargetUserID: target, Type: coordinationrequest.Meeting, Title: "Synthetic", DurationMinutes: 30, DeadlineAt: now.Add(24 * time.Hour), SyncPreference: coordinationrequest.Either, Priority: coordinationrequest.PriorityNormal, Status: coordinationrequest.Suggested, CreatedAt: now, UpdatedAt: now, Options: []coordinationrequest.Option{{ID: id + "-option", RequestID: id, Type: coordinationrequest.OptionMeeting, StartAt: &at, EndAt: &end, CreatedAt: now}}}
	}
	testPostgresConfirmationEffects(t, ctx, db, store, fixture, now)
	testPostgresReschedule(t, ctx, db, store, fixture, now)
	t.Run("confirmed-cancellation", func(t *testing.T) {
		for i, actor := range []string{"alice", "bob"} {
			value := fixture("cancel-"+actor, "alice", "bob", start.Add(time.Duration(8+i)*time.Hour))
			if err := store.Create(ctx, value); err != nil {
				t.Fatal(err)
			}
			if err := store.Respond(ctx, value.ID, "bob", coordinationrequest.Accepted, value.Options[0].ID); err != nil {
				t.Fatal(err)
			}
			if err := store.CancelConfirmed(ctx, value.ID, "carol", value.Options[0].ID); !errors.Is(err, coordinationrequest.ErrNotFound) {
				t.Fatal(err)
			}
			if err := store.CancelConfirmed(ctx, value.ID, actor, "old"); !errors.Is(err, coordinationrequest.ErrCancellationInvalid) {
				t.Fatal(err)
			}
			gate := make(chan struct{})
			results := make(chan error, 2)
			for range 2 {
				go func() { <-gate; results <- store.CancelConfirmed(ctx, value.ID, actor, value.Options[0].ID) }()
			}
			close(gate)
			success := 0
			for range 2 {
				err := <-results
				if err == nil {
					success++
				} else if !errors.Is(err, coordinationrequest.ErrAlreadyCancelled) && !errors.Is(err, coordinationrequest.ErrBookingConflict) {
					t.Fatal(err)
				}
			}
			if success != 1 {
				t.Fatalf("success=%d", success)
			}
			if err := store.CancelConfirmed(ctx, value.ID, actor, value.Options[0].ID); !errors.Is(err, coordinationrequest.ErrAlreadyCancelled) {
				t.Fatal(err)
			}
			got, err := store.GetForUser(ctx, value.ID, actor)
			if err != nil || got.Status != coordinationrequest.Cancelled || got.AcceptedOptionID != value.Options[0].ID {
				t.Fatal("wrong cancelled state")
			}
			for _, query := range []string{"SELECT count(*) FROM notifications WHERE request_id=$1 AND type='request_cancelled'", "SELECT count(*) FROM audit_logs WHERE resource_id=$1 AND action='request_cancelled'"} {
				var count int
				if err := db.QueryRowContext(ctx, query, value.ID).Scan(&count); err != nil || count != 1 {
					t.Fatalf("effects=%d err=%v", count, err)
				}
			}
			note, _ := coordinationrequest.ConfirmedCancellationEffects(value, actor, now)
			var recipient string
			if err := db.QueryRowContext(ctx, "SELECT user_id FROM notifications WHERE request_id=$1 AND type='request_cancelled'", value.ID).Scan(&recipient); err != nil || recipient != note.UserID {
				t.Fatal("wrong recipient")
			}
			replacement := fixture("replacement-"+actor, "carol", "bob", *value.Options[0].StartAt)
			if err := store.Create(ctx, replacement); err != nil {
				t.Fatal(err)
			}
			if err := store.Respond(ctx, replacement.ID, "bob", coordinationrequest.Accepted, replacement.Options[0].ID); err != nil {
				t.Fatal("slot not released", err)
			}
		}
	})
	t.Run("cancellation-rollback", func(t *testing.T) {
		value := fixture("cancel-rollback", "alice", "bob", start.Add(11*time.Hour))
		if err := store.Create(ctx, value); err != nil {
			t.Fatal(err)
		}
		if err := store.Respond(ctx, value.ID, "bob", coordinationrequest.Accepted, value.Options[0].ID); err != nil {
			t.Fatal(err)
		}
		_, event := coordinationrequest.ConfirmedCancellationEffects(value, "alice", now)
		if err := audit.NewPostgresStore(db).Create(ctx, event); err != nil {
			t.Fatal(err)
		}
		if err := store.CancelConfirmed(ctx, value.ID, "alice", value.Options[0].ID); err == nil {
			t.Fatal("collision accepted")
		}
		got, err := store.GetForUser(ctx, value.ID, "alice")
		if err != nil || got.Status != coordinationrequest.Accepted {
			t.Fatal("partial cancellation")
		}
		var count int
		if err := db.QueryRowContext(ctx, "SELECT count(*) FROM notifications WHERE request_id=$1 AND type='request_cancelled'", value.ID).Scan(&count); err != nil || count != 0 {
			t.Fatal("partial notification")
		}
	})
	for _, reversed := range []bool{false, true} {
		name := "target"
		if reversed {
			name = "roles"
		}
		t.Run(name, func(t *testing.T) {
			slot := start
			if reversed {
				slot = slot.Add(2 * time.Hour)
			}
			first := fixture(name+"-first", "alice", "bob", slot)
			second := fixture(name+"-second", "carol", "bob", slot)
			if reversed {
				second.RequesterUserID = "bob"
				second.TargetUserID = "carol"
			}
			second.OrganizationID = "other"
			for _, v := range []coordinationrequest.CoordinationRequest{first, second} {
				if err := store.Create(ctx, v); err != nil {
					t.Fatal(err)
				}
			}
			gate := make(chan struct{})
			results := make(chan error, 2)
			for _, v := range []coordinationrequest.CoordinationRequest{first, second} {
				go func(v coordinationrequest.CoordinationRequest) {
					<-gate
					results <- store.Respond(ctx, v.ID, v.TargetUserID, coordinationrequest.Accepted, v.Options[0].ID)
				}(v)
			}
			close(gate)
			success, conflict := 0, 0
			for i := 0; i < 2; i++ {
				err := <-results
				if err == nil {
					success++
				} else if errors.Is(err, coordinationrequest.ErrBookingConflict) {
					conflict++
				} else {
					t.Fatal(err)
				}
			}
			if success != 1 || conflict != 1 {
				t.Fatalf("success=%d conflict=%d", success, conflict)
			}
			for _, v := range []coordinationrequest.CoordinationRequest{first, second} {
				got, err := store.GetForUser(ctx, v.ID, v.TargetUserID)
				if err != nil {
					t.Fatal(err)
				}
				if got.Status == coordinationrequest.Accepted {
					if err := store.Respond(ctx, v.ID, v.TargetUserID, coordinationrequest.Accepted, v.Options[0].ID); !errors.Is(err, coordinationrequest.ErrAlreadyAccepted) {
						t.Fatal(err)
					}
				}
			}
			adjacent := fixture(name+"-adjacent", "alice", "bob", slot.Add(30*time.Minute))
			if err := store.Create(ctx, adjacent); err != nil {
				t.Fatal(err)
			}
			if err := store.Respond(ctx, adjacent.ID, "bob", coordinationrequest.Accepted, adjacent.Options[0].ID); err != nil {
				t.Fatalf("adjacent: %v", err)
			}
		})
	}
	expired := fixture("expired", "alice", "bob", now.Add(-time.Minute))
	if err := store.Create(ctx, expired); err != nil {
		t.Fatal(err)
	}
	if err := store.Respond(ctx, expired.ID, "bob", coordinationrequest.Accepted, expired.Options[0].ID); !errors.Is(err, coordinationrequest.ErrCandidateExpired) {
		t.Fatal(err)
	}
	closed := fixture("closed", "alice", "bob", start.Add(6*time.Hour))
	if err := store.Create(ctx, closed); err != nil {
		t.Fatal(err)
	}
	exec("UPDATE schedule_projections SET requestability='closed' WHERE user_id='bob'")
	if err := store.Respond(ctx, closed.ID, "bob", coordinationrequest.Accepted, closed.Options[0].ID); !errors.Is(err, coordinationrequest.ErrAvailabilityChanged) {
		t.Fatal(err)
	}
	got, err := store.GetForUser(ctx, closed.ID, "bob")
	if err != nil || got.Status != coordinationrequest.Suggested {
		t.Fatal("failed confirmation mutated request", err)
	}
	testPostgresSourceFreshness(t, ctx, db, store, fixture, now)
}
