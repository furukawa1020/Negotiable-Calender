package main

import (
	"context"
	"database/sql"
	"errors"
	"reflect"
	"testing"
	"time"

	coord "github.com/negotiable-calendar/negotiable-calendar/apps/api/internal/request"
)

func testPostgresCreationFreshness(t *testing.T, ctx context.Context, db *sql.DB, store *coord.PostgresStore, fixture func(string, string, string, time.Time) coord.CoordinationRequest, now time.Time) {
	for _, scenario := range []string{"deadline", "meeting-started", "async-expired", "expired-replay"} {
		t.Run("creation-freshness/"+scenario, func(t *testing.T) {
			value := fixture("freshness-"+scenario, "alice", "bob", now.Add(-time.Hour))
			value.CreatedAt, value.UpdatedAt, value.Options[0].CreatedAt = now.Add(-3*time.Hour), now.Add(-3*time.Hour), now.Add(-3*time.Hour)
			if scenario == "deadline" || scenario == "expired-replay" {
				value.DeadlineAt = now.Add(-time.Minute)
			}
			if scenario == "async-expired" {
				at := now.Add(-time.Minute)
				value.Options = []coord.Option{{ID: value.ID + "-async", RequestID: value.ID, Type: coord.OptionAsync, ResponseBy: &at, CreatedAt: value.CreatedAt}}
			}
			if scenario == "expired-replay" {
				saved := value
				saved.Status = coord.Cancelled
				if err := store.Create(ctx, saved); err != nil {
					t.Fatal(err)
				}
				before, err := store.LookupCreation(ctx, value)
				if err != nil {
					t.Fatal(err)
				}
				if created, err := store.CreateOnce(ctx, value); err != nil || created {
					t.Fatal("expired replay rejected or recreated", err)
				}
				after, err := store.LookupCreation(ctx, value)
				if err != nil || !reflect.DeepEqual(before, after) {
					t.Fatal("replay changed saved state", err)
				}
				return
			}
			if created, err := store.CreateOnce(ctx, value); !errors.Is(err, coord.ErrCreationExpired) || created {
				t.Fatal("new expired request persisted", err)
			}
			for _, query := range []string{`SELECT count(*) FROM coordination_requests WHERE id=$1`, `SELECT count(*) FROM coordination_request_options WHERE request_id=$1`, `SELECT count(*) FROM notifications WHERE request_id=$1`, `SELECT count(*) FROM audit_logs WHERE resource_id=$1`} {
				var count int
				if err := db.QueryRowContext(ctx, query, value.ID).Scan(&count); err != nil || count != 0 {
					t.Fatal("partial request/effect", count, err)
				}
			}
		})
	}
}
