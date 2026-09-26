package firestorestore

import (
	"encoding/json"
	"errors"
	coord "github.com/negotiable-calendar/negotiable-calendar/apps/api/internal/request"
	"testing"
	"time"
)

func TestRescheduleScopedAuthorization(t *testing.T) {

	for _, action := range []string{"propose", "accept", "decline", "withdraw"} {
		t.Run("reschedule-scope-"+action, func(t *testing.T) {

			b, ctx := emulatorBackend(t)
			now := time.Now().UTC().Truncate(time.Second)
			store := b.Request()
			value := confirmationRequest("scoped-"+action, "alice", "bob", now, now.Add(23*time.Hour))
			value.Status, value.AcceptedOptionID = coord.Accepted, value.Options[0].ID
			putDocument(t, ctx, b.Client.Collection("coordinationRequests").Doc(value.ID), value)
			members := b.Client.Collection("organizations").Doc(value.OrganizationID).Collection("members")
			for _, user := range []string{"alice", "bob"} {
				putDocument(t, ctx, members.Doc(user), map[string]any{"UserID": user})
			}
			p := publicationFixtures(now, "available", 1)[0]
			p.UserID, p.EndAt = "bob", now.Add(25*time.Hour)
			putDocument(t, ctx, b.Client.Collection("users").Doc("bob").Collection("scheduleProjections").Doc(p.ID), p)

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
				audits, err := b.Client.Collection("organizations").Doc(value.OrganizationID).Collection("auditLogs").Documents(ctx).GetAll()
				if err != nil {
					t.Fatal(err)
				}
				n := len(audits)
				for _, user := range []string{"alice", "bob"} {
					notes, err := b.Client.Collection("users").Doc(user).Collection("notifications").Documents(ctx).GetAll()
					if err != nil {
						t.Fatal(err)
					}
					n += len(notes)
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
					if _, err := members.Doc(user).Delete(ctx); err != nil {
						t.Fatal(err)
					}
					err := store.RescheduleInOrganization(ctx, value.ID, actor, value.OrganizationID, command)
					putDocument(t, ctx, members.Doc(user), map[string]any{"UserID": user})
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
