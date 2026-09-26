package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	coord "github.com/negotiable-calendar/negotiable-calendar/apps/api/internal/request"
	"testing"
	"time"
)

func testPostgresCancellationAuthorization(t *testing.T, ctx context.Context, db *sql.DB, store *coord.PostgresStore, fixture func(string, string, string, time.Time) coord.CoordinationRequest, now time.Time) {

	for _, actor := range []string{"alice", "bob"} {
		for _, peerPresent := range []bool{true, false} {
			t.Run(fmt.Sprintf("cancel-scope-%s-peer-%t", actor, peerPresent), func(t *testing.T) {

				value := fixture(fmt.Sprintf("scoped-cancel-%s-%t", actor, peerPresent), "alice", "bob", now.Add(23*time.Hour))
				if err := store.Create(ctx, value); err != nil {
					t.Fatal(err)
				}
				defer db.ExecContext(ctx, "DELETE FROM coordination_requests WHERE id=$1", value.ID)
				value.AcceptedOptionID = value.Options[0].ID
				if _, err := db.ExecContext(ctx, "UPDATE coordination_requests SET status='accepted',accepted_option_id=$1 WHERE id=$2", value.AcceptedOptionID, value.ID); err != nil {
					t.Fatal(err)
				}

				peer := "alice"
				if actor == peer {
					peer = "bob"
				}

				restoreMember := func(user string) {
					t.Helper()
					if _, err := db.ExecContext(ctx, "INSERT INTO memberships(id,organization_id,user_id,role,created_at) VALUES($1,$2,$1,'MANAGER',$3)", user, value.OrganizationID, now); err != nil {
						t.Fatal(err)
					}
				}
				removeMember := func(user string) {
					t.Helper()
					if _, err := db.ExecContext(ctx, "DELETE FROM memberships WHERE organization_id=$1 AND user_id=$2", value.OrganizationID, user); err != nil {
						t.Fatal(err)
					}
				}

				if !peerPresent {
					removeMember(peer)
					defer restoreMember(peer)
				}
				read := func() string {
					t.Helper()
					got, err := store.GetForUser(ctx, value.ID, actor)
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
						if err := store.CancelConfirmedInOrganization(ctx, value.ID, scope.actor, scope.org, value.AcceptedOptionID); !errors.Is(err, coord.ErrNotFound) {
							t.Fatalf("scope accepted: %v", err)
						}
					}
					removeMember(actor)
					err := store.CancelConfirmedInOrganization(ctx, value.ID, actor, value.OrganizationID, value.AcceptedOptionID)
					wrongOption := store.CancelConfirmedInOrganization(ctx, value.ID, actor, value.OrganizationID, "wrong-option")
					restoreMember(actor)
					if !errors.Is(err, coord.ErrCreationForbidden) || !errors.Is(wrongOption, coord.ErrCreationForbidden) {
						t.Fatalf("membership must precede replay/selection: %v, %v", err, wrongOption)
					}
					if before != read() || effects != countEffects() {
						t.Fatal("denial changed reservation/options/effects")
					}
				}
				checkDenied()
				effects := countEffects()
				if err := store.CancelConfirmedInOrganization(ctx, value.ID, actor, value.OrganizationID, value.AcceptedOptionID); err != nil {
					t.Fatal("authorized cancellation", err)
				}
				got, err := store.GetForUser(ctx, value.ID, actor)
				if err != nil || got.Status != coord.Cancelled || got.AcceptedOptionID != value.AcceptedOptionID {
					t.Fatal("wrong cancelled selection", err)
				}
				ranges, err := coord.ConfirmedRanges([]coord.CoordinationRequest{got})
				if err != nil || len(ranges) != 0 {
					t.Fatal("reservation retained", err)
				}
				if countEffects() != effects+2 {
					t.Fatal("missing atomic effects")
				}
				before := read()
				if err := store.CancelConfirmedInOrganization(ctx, value.ID, actor, value.OrganizationID, value.AcceptedOptionID); !errors.Is(err, coord.ErrAlreadyCancelled) {
					t.Fatal("authorized replay", err)
				}
				if before != read() || countEffects() != effects+2 {
					t.Fatal("replay changed state/effects")
				}
				checkDenied()
			})
		}
	}

}
