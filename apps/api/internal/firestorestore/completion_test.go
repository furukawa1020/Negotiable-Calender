package firestorestore

import (
	coord "github.com/negotiable-calendar/negotiable-calendar/apps/api/internal/request"
	"testing"
	"time"
)

func TestCompletionRollbackAndEvidence(t *testing.T) {
	for _, scenario := range []string{"notification", "audit", "membership", "deletion", "wrong-document", "early", "stale", "proposal", "duplicate", "success"} {
		t.Run(scenario, func(t *testing.T) {
			b, ctx := emulatorBackend(t)
			now := time.Now().UTC()
			v := confirmationRequest("completion", "alice", "bob", now, now.Add(-2*time.Hour))
			v.Status, v.AcceptedOptionID = coord.Accepted, v.Options[0].ID
			for _, user := range []string{"alice", "bob"} {
				if scenario == "membership" && user == "bob" {
					continue
				}
				putDocument(t, ctx, b.Client.Collection("organizations").Doc(v.OrganizationID).Collection("members").Doc(user), map[string]any{"Role": "MANAGER"})
			}
			cmd := coord.ResolutionCommand{Status: coord.Completed, ExpectedOptionID: v.AcceptedOptionID}
			switch scenario {
			case "deletion":
				putDocument(t, ctx, b.accountDeletionRef("bob"), accountDeletion{Phase: "deleting"})
			case "wrong-document":
				v.ID = "other"
			case "early":
				end := now.Add(time.Hour)
				v.Options[0].EndAt = &end
			case "stale":
				cmd.ExpectedOptionID = "old"
			case "proposal":
				v.RescheduleProposal = &coord.RescheduleProposal{Status: "proposed"}
			case "duplicate":
				v.Options = append(v.Options, v.Options[0])
			}
			ref := b.Client.Collection("coordinationRequests").Doc("completion")
			putDocument(t, ctx, ref, v)
			completed := v
			completed.Status = coord.Completed
			completed.UpdatedAt = now
			note, event := coord.ResolutionEffects(completed, "alice")
			noteRef := b.Client.Collection("users").Doc(note.UserID).Collection("notifications").Doc(note.ID)
			eventRef := b.Client.Collection("organizations").Doc(event.OrganizationID).Collection("auditLogs").Doc(event.ID)
			if scenario == "notification" {
				putDocument(t, ctx, noteRef, note)
			}
			if scenario == "audit" {
				putDocument(t, ctx, eventRef, event)
			}
			before, err := ref.Get(ctx)
			if err != nil {
				t.Fatal(err)
			}
			_, err = b.Request().ResolveRequest(ctx, "completion", "alice", v.OrganizationID, cmd)
			after, readErr := ref.Get(ctx)
			if readErr != nil {
				t.Fatal(readErr)
			}
			if scenario == "success" {
				if err != nil || after.Data()["Status"] != "completed" {
					t.Fatal("completion not committed", err)
				}
				return
			}
			if err == nil || !before.UpdateTime.Equal(after.UpdateTime) {
				t.Fatal("unsafe completion changed state", err)
			}
			if scenario != "notification" {
				if _, e := noteRef.Get(ctx); !firestoreNotFound(e) {
					t.Fatal("partial notification", e)
				}
			}
			if scenario != "audit" {
				if _, e := eventRef.Get(ctx); !firestoreNotFound(e) {
					t.Fatal("partial audit", e)
				}
			}
		})
	}
}
