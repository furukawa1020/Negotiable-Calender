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

func testPostgresRescheduleReplay(t *testing.T, ctx context.Context, db *sql.DB, store *coord.PostgresStore, fixture func(string, string, string, time.Time) coord.CoordinationRequest, now time.Time) {
	for _, action := range []string{"accept", "decline", "withdraw"} {
		for _, closed := range []bool{false, true} {
			t.Run("resolved-replay/"+action+"/"+map[bool]string{false: "active", true: "cancelled"}[closed], func(t *testing.T) {
				exec := func(query string, args ...any) {
					t.Helper()
					if _, err := db.ExecContext(ctx, query, args...); err != nil {
						t.Fatal(err)
					}
				}
				// Only this fixture in the parent-owned isolated test schema.
				defer exec(`DELETE FROM coordination_requests WHERE id='resolved-replay'`)
				defer exec(`DELETE FROM notifications WHERE request_id='resolved-replay'`)
				defer exec(`DELETE FROM audit_logs WHERE resource_id='resolved-replay'`)
				value := fixture("resolved-replay", "alice", "bob", now.Add(7*time.Hour))
				value.Status, value.AcceptedOptionID = coord.Accepted, value.Options[0].ID
				if err := store.Create(ctx, value); err != nil {
					t.Fatal(err)
				}
				exec(`UPDATE coordination_requests SET accepted_option_id=$1 WHERE id=$2`, value.AcceptedOptionID, value.ID)
				proposal := coord.RescheduleCommand{Action: "propose", ProposalID: "replay-proposal", ExpectedOptionID: value.AcceptedOptionID, StartAt: now.Add(8 * time.Hour)}
				if err := store.RescheduleInOrganization(ctx, value.ID, "alice", "org", proposal); err != nil {
					t.Fatal(err)
				}
				resolution, actor := proposal, "bob"
				resolution.Action = action
				if action == "withdraw" {
					actor = "alice"
				}
				if err := store.RescheduleInOrganization(ctx, value.ID, actor, "org", resolution); err != nil {
					t.Fatal(err)
				}
				before, err := store.GetForUser(ctx, value.ID, "alice")
				if err != nil {
					t.Fatal(err)
				}
				if closed {
					if err := store.CancelConfirmedInOrganization(ctx, value.ID, "alice", "org", before.AcceptedOptionID); err != nil {
						t.Fatal(err)
					}
					before, err = store.GetForUser(ctx, value.ID, "alice")
					if err != nil {
						t.Fatal(err)
					}
				}
				for _, retry := range []struct {
					actor   string
					command coord.RescheduleCommand
				}{{"alice", proposal}, {actor, resolution}} {
					if err := store.RescheduleInOrganization(ctx, value.ID, retry.actor, "org", retry.command); !errors.Is(err, coord.ErrRescheduleRepeated) {
						t.Fatal("saved command not recovered", err)
					}
				}
				if err := store.RescheduleInOrganization(ctx, value.ID, "carol", "org", proposal); !errors.Is(err, coord.ErrNotFound) {
					t.Fatal("outsider replay", err)
				}
				if err := store.RescheduleInOrganization(ctx, value.ID, "alice", "other", proposal); !errors.Is(err, coord.ErrNotFound) {
					t.Fatal("cross-workspace replay", err)
				}
				exec(`UPDATE memberships SET organization_id='other' WHERE organization_id='org' AND user_id='alice'`)
				defer exec(`UPDATE memberships SET organization_id='org' WHERE organization_id='other' AND user_id='alice'`)
				if err := store.RescheduleInOrganization(ctx, value.ID, "alice", "org", proposal); !errors.Is(err, coord.ErrCreationForbidden) {
					t.Fatal("revoked membership replay", err)
				}
				after, err := store.GetForUser(ctx, value.ID, "alice")
				if err != nil || !reflect.DeepEqual(before, after) {
					t.Fatal("replay changed reservation", err)
				}
				want := 2
				if closed {
					want++
				}
				for _, query := range []string{`SELECT count(*) FROM notifications WHERE request_id=$1`, `SELECT count(*) FROM audit_logs WHERE resource_id=$1`} {
					var count int
					if err := db.QueryRowContext(ctx, query, value.ID).Scan(&count); err != nil || count != want {
						t.Fatal("replayed effects", count, err)
					}
				}
			})
		}
	}
}
