package main

import (
	"context"
	"database/sql"
	"errors"
	coordinationrequest "github.com/negotiable-calendar/negotiable-calendar/apps/api/internal/request"
	"testing"
	"time"
)

func testPostgresCreation(t *testing.T, ctx context.Context, db *sql.DB, store *coordinationrequest.PostgresStore, fixture func(string, string, string, time.Time) coordinationrequest.CoordinationRequest, now time.Time) {
	t.Helper()
	for _, user := range []string{"alice", "bob"} {
		if _, err := db.ExecContext(ctx, `INSERT INTO memberships(id,organization_id,user_id,role,created_at) VALUES($1,'org',$1,'MANAGER',$2)`, user, now); err != nil {
			t.Fatal(err)
		}
	}
	t.Run("atomic-request-creation", func(t *testing.T) {
		value := fixture("dispatch", "alice", "bob", now.Add(time.Hour))
		type result struct {
			created bool
			err     error
		}
		gate, results := make(chan struct{}), make(chan result, 2)
		for range 2 {
			go func() { <-gate; created, err := store.CreateOnce(ctx, value); results <- result{created, err} }()
		}
		close(gate)
		count := 0
		for range 2 {
			got := <-results
			if got.err != nil {
				t.Fatal(got.err)
			}
			if got.created {
				count++
			}
		}
		if count != 1 {
			t.Fatal("duplicate creation", count)
		}
		got, err := store.LookupCreation(ctx, value)
		if err != nil || got.ID != value.ID || len(got.Options) != 1 {
			t.Fatal("response lost", err)
		}
		note, _ := coordinationrequest.CreationEffects(value)
		for _, table := range []string{"notifications", "audit_logs"} {
			var count int
			if err := db.QueryRowContext(ctx, "SELECT count(*) FROM "+table+" WHERE id=$1", note.ID).Scan(&count); err != nil || count != 1 {
				t.Fatal("missing atomic effect", err, count)
			}
		}
		if _, err := db.ExecContext(ctx, "UPDATE notifications SET read_at=$1 WHERE id=$2", now, note.ID); err != nil {
			t.Fatal(err)
		}
		if err := store.Cancel(ctx, value.ID, "alice"); err != nil {
			t.Fatal(err)
		}
		if created, err := store.CreateOnce(ctx, value); err != nil || created {
			t.Fatal("replay recreated", err)
		}
		got, err = store.LookupCreation(ctx, value)
		if err != nil || got.Status != coordinationrequest.Cancelled {
			t.Fatal("lifecycle reset", err)
		}
		var readAt sql.NullTime
		if err := db.QueryRowContext(ctx, "SELECT read_at FROM notifications WHERE id=$1", note.ID).Scan(&readAt); err != nil || !readAt.Valid {
			t.Fatal("read state reset", err)
		}
		value.Title = "changed"
		if _, err := store.CreateOnce(ctx, value); !errors.Is(err, coordinationrequest.ErrCreationConflict) {
			t.Fatal("conflict accepted", err)
		}
	})
	for _, collision := range []string{"notification", "audit", "member"} {
		t.Run("creation-rollback-"+collision, func(t *testing.T) {
			value := fixture("dispatch-"+collision, "alice", "bob", now.Add(time.Hour))
			note, event := coordinationrequest.CreationEffects(value)
			if collision == "notification" {
				// Seed a valid notification for a different request, sharing only
				// the ID whose collision should roll back the new command.
				seed := fixture(value.ID+"-seed", "alice", "bob", now.Add(time.Hour))
				if err := store.Create(ctx, seed); err != nil {
					t.Fatal(err)
				}
				note.RequestID = seed.ID
				_, err := db.ExecContext(ctx, `INSERT INTO notifications(id,user_id,type,request_id,message,created_at) VALUES($1,$2,$3,$4,$5,$6)`, note.ID, note.UserID, note.Type, note.RequestID, note.Message, now)
				if err != nil {
					t.Fatal(err)
				}
			} else if collision == "audit" {
				_, err := db.ExecContext(ctx, `INSERT INTO audit_logs(id,organization_id,actor_user_id,action,resource_type,resource_id,created_at) VALUES($1,$2,$3,$4,$5,$6,$7)`, event.ID, event.OrganizationID, event.ActorUserID, event.Action, event.ResourceType, event.ResourceID, now)
				if err != nil {
					t.Fatal(err)
				}
			} else {
				value.TargetUserID = "carol"
			}
			if created, err := store.CreateOnce(ctx, value); err == nil || created {
				t.Fatal("invalid creation succeeded")
			}
			for _, query := range []string{"SELECT count(*) FROM coordination_requests WHERE id=$1", "SELECT count(*) FROM coordination_request_options WHERE request_id=$1"} {
				var count int
				if err := db.QueryRowContext(ctx, query, value.ID).Scan(&count); err != nil || count != 0 {
					t.Fatal("partial request/options", err, count)
				}
			}
			if collision == "audit" {
				var count int
				if err := db.QueryRowContext(ctx, "SELECT count(*) FROM notifications WHERE id=$1", note.ID).Scan(&count); err != nil || count != 0 {
					t.Fatal("partial notification", err, count)
				}
			}
		})
	}
}
