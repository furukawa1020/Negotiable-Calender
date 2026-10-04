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

func testPostgresConfirmationIntegrity(t *testing.T, ctx context.Context, db *sql.DB, store *coord.PostgresStore, fixture func(string, string, string, time.Time) coord.CoordinationRequest, now time.Time) {
	for _, flow := range []string{"confirmation", "reschedule"} {
		for _, role := range []string{"requester", "target"} {
			for _, scenario := range []string{"unknown-type", "missing-selection", "wrong-request", "invalid-time"} {
				t.Run("integrity/"+flow+"/"+role+"/"+scenario, func(t *testing.T) {
					exec := func(query string, args ...any) {
						t.Helper()
						if _, err := db.ExecContext(ctx, query, args...); err != nil {
							t.Fatal(err)
						}
					}
					// Isolated parent test schema; remove only this test's fixed fixture IDs.
					defer exec(`DELETE FROM coordination_requests WHERE id IN ('integrity-original','integrity-damaged')`)
					defer exec(`DELETE FROM notifications WHERE request_id='integrity-original'`)
					defer exec(`DELETE FROM audit_logs WHERE resource_id='integrity-original'`)
					value := fixture("integrity-original", "alice", "bob", now.Add(time.Hour))
					if err := store.Create(ctx, value); err != nil {
						t.Fatal(err)
					}
					command := coord.RescheduleCommand{Action: "propose", ProposalID: "integrity-proposal", ExpectedOptionID: value.Options[0].ID, StartAt: now.Add(2 * time.Hour)}
					if flow == "reschedule" {
						exec(`UPDATE coordination_requests SET status='accepted',accepted_option_id=$1 WHERE id=$2`, value.Options[0].ID, value.ID)
						if err := store.Reschedule(ctx, value.ID, "alice", command); err != nil {
							t.Fatal(err)
						}
					}
					before, err := store.GetForUser(ctx, value.ID, "alice")
					if err != nil {
						t.Fatal(err)
					}
					other := fixture("integrity-damaged", "alice", "carol", now.Add(4*time.Hour))
					if role == "target" {
						other.RequesterUserID, other.TargetUserID = "carol", "bob"
					}
					other.OrganizationID = "other"
					if err := store.Create(ctx, other); err != nil {
						t.Fatal(err)
					}
					exec(`UPDATE coordination_requests SET status='accepted',accepted_option_id=$1 WHERE id=$2`, other.Options[0].ID, other.ID)
					switch scenario {
					case "unknown-type":
						exec(`UPDATE coordination_request_options SET type='unknown' WHERE id=$1`, other.Options[0].ID)
					case "missing-selection":
						exec(`UPDATE coordination_requests SET accepted_option_id='missing' WHERE id=$1`, other.ID)
					case "wrong-request":
						exec(`UPDATE coordination_requests SET accepted_option_id=$1 WHERE id=$2`, value.Options[0].ID, other.ID)
					case "invalid-time":
						exec(`UPDATE coordination_request_options SET end_at=start_at WHERE id=$1`, other.Options[0].ID)
					}
					if flow == "reschedule" {
						command.Action = "accept"
						err = store.Reschedule(ctx, value.ID, "bob", command)
					} else {
						err = store.Respond(ctx, value.ID, "bob", coord.Accepted, value.Options[0].ID)
					}
					if !errors.Is(err, coord.ErrBookingConflict) {
						t.Fatalf("corrupt reservation admitted: %v", err)
					}
					after, err := store.GetForUser(ctx, value.ID, "alice")
					if err != nil || !reflect.DeepEqual(before, after) {
						t.Fatal("failed approval mutated request", err)
					}
					expectedEffects := 0
					if flow == "reschedule" {
						expectedEffects = 1
					} // Only the already-saved proposal.
					for _, query := range []string{`SELECT count(*) FROM notifications WHERE request_id=$1`, `SELECT count(*) FROM audit_logs WHERE resource_id=$1`} {
						var count int
						if err := db.QueryRowContext(ctx, query, value.ID).Scan(&count); err != nil || count != expectedEffects {
							t.Fatalf("partial effects: count=%d err=%v", count, err)
						}
					}
				})
			}
		}
	}
}
