package firestorestore

import (
	"errors"
	"reflect"
	"testing"
	"time"

	coord "github.com/negotiable-calendar/negotiable-calendar/apps/api/internal/request"
)

func TestResolvedRescheduleReplayDoesNotReviveReservations(t *testing.T) {
	for _, action := range []string{"accept", "decline", "withdraw"} {
		for _, closed := range []bool{false, true} {
			t.Run(action+"/"+map[bool]string{false: "active", true: "cancelled"}[closed], func(t *testing.T) {
				b, ctx := emulatorBackend(t)
				now := time.Now().UTC().Truncate(time.Microsecond)
				value := confirmationRequest("replay-booking", "alice", "bob", now, now.Add(time.Hour))
				value.Status, value.AcceptedOptionID = coord.Accepted, value.Options[0].ID
				putDocument(t, ctx, b.Client.Collection("coordinationRequests").Doc(value.ID), value)
				for _, user := range []string{"alice", "bob"} {
					putDocument(t, ctx, b.Client.Collection("organizations").Doc(value.OrganizationID).Collection("members").Doc(user), map[string]any{"Role": "MANAGER"})
				}
				p := publicationFixtures(now, "available", 1)[0]
				p.UserID, p.EndAt = "bob", now.Add(4*time.Hour)
				putDocument(t, ctx, b.Client.Collection("users").Doc("bob").Collection("scheduleProjections").Doc(p.ID), p)
				proposal := coord.RescheduleCommand{Action: "propose", ProposalID: "replay-proposal", ExpectedOptionID: value.AcceptedOptionID, StartAt: now.Add(2 * time.Hour)}
				store := b.Request()
				if err := store.RescheduleInOrganization(ctx, value.ID, "alice", value.OrganizationID, proposal); err != nil {
					t.Fatal(err)
				}
				resolution, actor := proposal, "bob"
				resolution.Action = action
				if action == "withdraw" {
					actor = "alice"
				}
				if err := store.RescheduleInOrganization(ctx, value.ID, actor, value.OrganizationID, resolution); err != nil {
					t.Fatal(err)
				}
				before, err := store.GetForUser(ctx, value.ID, "alice")
				if err != nil {
					t.Fatal(err)
				}
				if closed {
					if err := store.CancelConfirmedInOrganization(ctx, value.ID, "alice", value.OrganizationID, before.AcceptedOptionID); err != nil {
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
					if err := store.RescheduleInOrganization(ctx, value.ID, retry.actor, value.OrganizationID, retry.command); !errors.Is(err, coord.ErrRescheduleRepeated) {
						t.Fatal("saved command not recovered", err)
					}
				}
				if err := store.RescheduleInOrganization(ctx, value.ID, "carol", value.OrganizationID, proposal); !errors.Is(err, coord.ErrNotFound) {
					t.Fatal("outsider replay", err)
				}
				if err := store.RescheduleInOrganization(ctx, value.ID, "alice", "other", proposal); !errors.Is(err, coord.ErrNotFound) {
					t.Fatal("cross-workspace replay", err)
				}
				member := b.Client.Collection("organizations").Doc(value.OrganizationID).Collection("members").Doc("alice")
				if _, err := member.Delete(ctx); err != nil {
					t.Fatal(err)
				}
				if err := store.RescheduleInOrganization(ctx, value.ID, "alice", value.OrganizationID, proposal); !errors.Is(err, coord.ErrCreationForbidden) {
					t.Fatal("revoked membership replay", err)
				}
				putDocument(t, ctx, member, map[string]any{"Role": "MANAGER"})
				putDocument(t, ctx, b.accountDeletionRef("alice"), accountDeletion{Phase: "deleting", StartedAt: now})
				if err := store.RescheduleInOrganization(ctx, value.ID, "alice", value.OrganizationID, proposal); !errors.Is(err, errAccountDeleting) {
					t.Fatal("deleting account replay", err)
				}
				after, err := store.GetForUser(ctx, value.ID, "alice")
				if err != nil || !reflect.DeepEqual(before, after) {
					t.Fatal("replay changed reservation", err)
				}
				want := 2
				if closed {
					want++
				}
				notes := 0
				for _, user := range []string{"alice", "bob"} {
					docs, err := b.Client.Collection("users").Doc(user).Collection("notifications").Documents(ctx).GetAll()
					if err != nil {
						t.Fatal(err)
					}
					notes += len(docs)
				}
				audits, err := b.Client.Collection("organizations").Doc(value.OrganizationID).Collection("auditLogs").Documents(ctx).GetAll()
				if err != nil || notes != want || len(audits) != want {
					t.Fatal("replayed effects", notes, len(audits), err)
				}
			})
		}
	}
}
