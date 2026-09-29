package main

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	coord "github.com/negotiable-calendar/negotiable-calendar/apps/api/internal/request"
)

func testPostgresWorkspaceLists(t *testing.T, ctx context.Context, db *sql.DB, store *coord.PostgresStore, fixture func(string, string, string, time.Time) coord.CoordinationRequest, now time.Time) {
	t.Run("workspace-lists", func(t *testing.T) {
		exec := func(query string, args ...any) {
			t.Helper()
			if _, err := db.ExecContext(ctx, query, args...); err != nil {
				t.Fatal(err)
			}
		}
		for _, org := range []string{"list-org", "list-other"} {
			exec("INSERT INTO organizations VALUES($1,$1,$2,$2)", org, now)
			for _, user := range []string{"alice", "carol"} {
				exec("INSERT INTO memberships(id,organization_id,user_id,role,created_at) VALUES($1,$2,$3,'MANAGER',$4)", org+"-"+user, org, user, now)
			}
			// No Bob membership: remaining participants retain their own history.
			for _, pair := range []struct{ id, requester, target string }{{"a", "bob", "alice"}, {"z", "bob", "alice"}, {"sent", "alice", "bob"}} {
				value := fixture(org+"-"+pair.id, pair.requester, pair.target, now.Add(time.Hour))
				value.OrganizationID = org
				if pair.id == "a" {
					value.Options = nil
				}
				if err := store.Create(ctx, value); err != nil {
					t.Fatal(err)
				}
			}
		}
		defer func() {
			exec("DELETE FROM coordination_requests WHERE organization_id IN ('list-org','list-other')")
			exec("DELETE FROM memberships WHERE organization_id IN ('list-org','list-other')")
			exec("DELETE FROM organizations WHERE id IN ('list-org','list-other')")
		}()
		for _, org := range []string{"list-org", "list-other"} {
			for _, sent := range []bool{false, true} {
				values, err := store.ListInOrganization(ctx, "alice", org, sent)
				want := []string{org + "-z", org + "-a"}
				if sent {
					want = []string{org + "-sent"}
				}
				if err != nil || len(values) != len(want) {
					t.Fatalf("org=%s sent=%v values=%+v err=%v", org, sent, values, err)
				}
				for i, id := range want {
					if values[i].ID != id || values[i].OrganizationID != org || values[i].Options == nil {
						t.Fatalf("unexpected request: %+v", values[i])
					}
					if id != org+"-a" && (len(values[i].Options) != 1 || values[i].Options[0].ID != id+"-option") {
						t.Fatalf("options missing: %+v", values[i])
					}
				}
				values, err = store.ListInOrganization(ctx, "carol", org, sent)
				if err != nil || len(values) != 0 {
					t.Fatalf("nonparticipant values=%+v err=%v", values, err)
				}
			}
		}
		global, err := store.ListForUser(ctx, "alice")
		if err != nil || len(global) != 6 {
			t.Fatalf("global conflict/export read changed: values=%d err=%v", len(global), err)
		}
		exec("DELETE FROM memberships WHERE organization_id='list-org' AND user_id='alice'")
		for _, sent := range []bool{false, true} {
			for _, pair := range [][2]string{{"alice", "list-org"}, {"bob", "list-other"}, {"alice", ""}, {"", "list-other"}} {
				values, err := store.ListInOrganization(ctx, pair[0], pair[1], sent)
				if !errors.Is(err, coord.ErrCreationForbidden) || values != nil {
					t.Fatalf("revoked/missing context leaked: %+v %v", values, err)
				}
			}
		}
	})
}
