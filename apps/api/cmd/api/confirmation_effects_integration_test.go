package main

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/negotiable-calendar/negotiable-calendar/apps/api/internal/audit"
	"github.com/negotiable-calendar/negotiable-calendar/apps/api/internal/notification"
	coordinationrequest "github.com/negotiable-calendar/negotiable-calendar/apps/api/internal/request"
)

func testPostgresConfirmationEffects(t *testing.T, ctx context.Context, db *sql.DB, store *coordinationrequest.PostgresStore, fixture func(string, string, string, time.Time) coordinationrequest.CoordinationRequest, now time.Time) {
	for i, scenario := range []string{"concurrent", "notification-collision", "audit-collision"} {
		t.Run("confirmation-effects-"+scenario, func(t *testing.T) {
			value := fixture("effects-"+scenario, "alice", "bob", now.Add(time.Duration(4+i)*time.Hour))
			if err := store.Create(ctx, value); err != nil {
				t.Fatal(err)
			}
			note, event := coordinationrequest.ConfirmationEffects(value, now)
			if scenario == "notification-collision" {
				note.ReadAt = &now
				if err := notification.NewPostgresStore(db).Create(ctx, note); err != nil {
					t.Fatal(err)
				}
			}
			if scenario == "audit-collision" {
				if err := audit.NewPostgresStore(db).Create(ctx, event); err != nil {
					t.Fatal(err)
				}
			}
			if scenario != "concurrent" {
				if err := store.Respond(ctx, value.ID, "bob", coordinationrequest.Accepted, value.Options[0].ID); err == nil {
					t.Fatal("partial commit accepted")
				}
				got, err := store.GetForUser(ctx, value.ID, "alice")
				if err != nil || got.Status != coordinationrequest.Suggested || got.AcceptedOptionID != "" {
					t.Fatal("partial acceptance")
				}
				for kind, query := range map[string]string{"notification-collision": "SELECT count(*) FROM notifications WHERE request_id=$1", "audit-collision": "SELECT count(*) FROM audit_logs WHERE resource_id=$1"} {
					var count int
					want := 0
					if kind == scenario {
						want = 1
					}
					if err := db.QueryRowContext(ctx, query, value.ID).Scan(&count); err != nil || count != want {
						t.Fatal("partial/overwritten effect", count, err)
					}
				}
				if scenario == "notification-collision" {
					var readAt time.Time
					if err := db.QueryRowContext(ctx, "SELECT read_at FROM notifications WHERE id=$1", note.ID).Scan(&readAt); err != nil || !readAt.Equal(now) {
						t.Fatal("read state overwritten", err)
					}
				}
				return
			}
			gate, results := make(chan struct{}), make(chan error, 2)
			for range 2 {
				go func() {
					<-gate
					results <- store.Respond(ctx, value.ID, "bob", coordinationrequest.Accepted, value.Options[0].ID)
				}()
			}
			close(gate)
			success := 0
			for range 2 {
				err := <-results
				if err == nil {
					success++
				} else if !errors.Is(err, coordinationrequest.ErrAlreadyAccepted) && !errors.Is(err, coordinationrequest.ErrBookingConflict) {
					t.Fatal(err)
				}
			}
			if success != 1 {
				t.Fatalf("successful commits=%d", success)
			}
			readAt := now.Add(time.Second)
			if ok, err := notification.NewPostgresStore(db).MarkRead(ctx, note.ID, "alice", readAt); err != nil || !ok {
				t.Fatal(err)
			}
			if err := store.Respond(ctx, value.ID, "bob", coordinationrequest.Accepted, value.Options[0].ID); !errors.Is(err, coordinationrequest.ErrAlreadyAccepted) {
				t.Fatal(err)
			}
			for _, query := range []string{"SELECT count(*) FROM notifications WHERE request_id=$1", "SELECT count(*) FROM audit_logs WHERE resource_id=$1"} {
				var count int
				if err := db.QueryRowContext(ctx, query, value.ID).Scan(&count); err != nil || count != 1 {
					t.Fatal("effects not exactly once", count, err)
				}
			}
			got, err := store.GetForUser(ctx, value.ID, "alice")
			if err != nil || got.Status != coordinationrequest.Accepted {
				t.Fatal("acceptance missing", err)
			}
			var recipient, kind, actor, action string
			var created, read, auditAt time.Time
			if err := db.QueryRowContext(ctx, "SELECT user_id,type,created_at,read_at FROM notifications WHERE id=$1", note.ID).Scan(&recipient, &kind, &created, &read); err != nil || recipient != "alice" || kind != string(notification.RequestAccepted) || !created.Equal(got.UpdatedAt) || !read.Equal(readAt) {
				t.Fatal("notification mismatch", err)
			}
			if err := db.QueryRowContext(ctx, "SELECT actor_user_id,action,created_at FROM audit_logs WHERE id=$1", event.ID).Scan(&actor, &action, &auditAt); err != nil || actor != "bob" || action != string(audit.RequestAccepted) || !auditAt.Equal(got.UpdatedAt) {
				t.Fatal("audit mismatch", err)
			}
		})
	}
}
