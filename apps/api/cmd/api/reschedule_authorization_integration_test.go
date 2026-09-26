package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	coord "github.com/negotiable-calendar/negotiable-calendar/apps/api/internal/request"
	"testing"
	"time"
)

func testPostgresRescheduleAuthorization(t *testing.T, ctx context.Context, db *sql.DB, store *coord.PostgresStore, fixture func(string, string, string, time.Time) coord.CoordinationRequest, now time.Time) {

	for _, action := range []string{"propose", "accept", "decline", "withdraw"} {
		t.Run("reschedule-scope-"+action, func(t *testing.T) {

			value := fixture("scoped-"+action, "alice", "bob", now.Add(23*time.Hour))
			if err := store.Create(ctx, value); err != nil {
				t.Fatal(err)
			}
			defer db.ExecContext(ctx, "DELETE FROM coordination_requests WHERE id=$1", value.ID)
			value.AcceptedOptionID = value.Options[0].ID
			if _, err := db.ExecContext(ctx, "UPDATE coordination_requests SET status='accepted',accepted_option_id=$1 WHERE id=$2", value.AcceptedOptionID, value.ID); err != nil {
				t.Fatal(err)
			}

			command := coord.RescheduleCommand{Action: "propose", ProposalID: "scoped-proposal-" + action, ExpectedOptionID: value.AcceptedOptionID, StartAt: now.Add(23*time.Hour + 30*time.Minute)}
			if action != "propose" {
				if err := store.RescheduleInOrganization(ctx, value.ID, "alice", value.OrganizationID, command); err != nil {
					t.Fatal(err)
				}
			}
			command.Action = action
			actor := "alice"
			if action == "accept" || action == "decline" {
				actor = "bob"
			}
			read := func() string {
				got, err := store.GetForUser(ctx, value.ID, "alice")
				if err != nil {
					t.Fatal(err)
				}
				data, err := json.Marshal(got)
				if err != nil {
					t.Fatal(err)
				}
				return string(data)
			}

			countEffects := func() int {
				t.Helper()
				var n int
				if err := db.QueryRowContext(ctx, "SELECT (SELECT count(*) FROM notifications WHERE request_id=$1)+(SELECT count(*) FROM audit_logs WHERE resource_id=$1)", value.ID).Scan(&n); err != nil {
					t.Fatal(err)
				}
				return n
			}

			checkDenied := func() {
				t.Helper()
				before, effects := read(), countEffects()
				for _, scope := range []struct{ actor, org string }{{actor, "other"}, {actor, ""}, {"carol", value.OrganizationID}, {"", value.OrganizationID}} {
					if err := store.RescheduleInOrganization(ctx, value.ID, scope.actor, scope.org, command); !errors.Is(err, coord.ErrNotFound) {
						t.Fatalf("scope accepted: %v", err)
					}
				}
				for _, user := range []string{"alice", "bob"} {
					if _, err := db.ExecContext(ctx, "DELETE FROM memberships WHERE organization_id=$1 AND user_id=$2", value.OrganizationID, user); err != nil {
						t.Fatal(err)
					}
					err := store.RescheduleInOrganization(ctx, value.ID, actor, value.OrganizationID, command)
					if _, e := db.ExecContext(ctx, "INSERT INTO memberships(id,organization_id,user_id,role,created_at) VALUES($1,$2,$1,'MANAGER',$3)", user, value.OrganizationID, now); e != nil {
						t.Fatal(e)
					}
					if !errors.Is(err, coord.ErrCreationForbidden) {
						t.Fatalf("revoked %s membership accepted: %v", user, err)
					}
				}
				if read() != before || countEffects() != effects {
					t.Fatal("denial changed reservation/options/effects")
				}
			}
			checkDenied()
			beforeEffects := countEffects()
			if err := store.RescheduleInOrganization(ctx, value.ID, actor, value.OrganizationID, command); err != nil {
				t.Fatal("authorized operation", err)
			}
			if countEffects() != beforeEffects+2 {
				t.Fatal("missing atomic notification/audit")
			}
			before := read()
			if err := store.RescheduleInOrganization(ctx, value.ID, actor, value.OrganizationID, command); !errors.Is(err, coord.ErrRescheduleRepeated) {
				t.Fatal("authorized replay", err)
			}
			if read() != before || countEffects() != beforeEffects+2 {
				t.Fatal("replay changed effects/state")
			}
			checkDenied() // Authorization must run BEFORE replay detection, for all actions.
			got, err := store.GetForUser(ctx, value.ID, "alice")
			if err != nil {
				t.Fatal(err)
			}
			expected := value.AcceptedOptionID
			if action == "accept" {
				expected = command.ProposalID
			}
			if got.AcceptedOptionID != expected {
				t.Fatal("wrong reservation retained")
			}
		})
	}

}
