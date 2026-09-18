package firestorestore

import (
	"errors"
	coordinationrequest "github.com/negotiable-calendar/negotiable-calendar/apps/api/internal/request"
	"testing"
	"time"
)

func TestRescheduleAtomicSwapAndReplay(t *testing.T) {
	for _, actor := range []string{"alice", "bob"} {
		t.Run(actor, func(t *testing.T) {
			b, ctx := emulatorBackend(t)
			now := time.Now().UTC()
			start := now.Add(time.Hour)
			value := confirmationRequest("original", "alice", "bob", now, start)
			value.DurationMinutes = 30
			store := b.Request()
			p := publicationFixtures(now, "available", 1)[0]
			p.UserID = "bob"
			p.EndAt = now.Add(6 * time.Hour)
			putDocument(t, ctx, b.Client.Collection("users").Doc("bob").Collection("scheduleProjections").Doc(p.ID), p)
			if err := store.Create(ctx, value); err != nil {
				t.Fatal(err)
			}
			if err := store.Respond(ctx, value.ID, "bob", coordinationrequest.Accepted, value.Options[0].ID); err != nil {
				t.Fatal(err)
			}
			command := coordinationrequest.RescheduleCommand{Action: "propose", ProposalID: "new-proposal", ExpectedOptionID: value.Options[0].ID, StartAt: now.Add(2 * time.Hour)}
			if err := store.Reschedule(ctx, value.ID, actor, command); err != nil {
				t.Fatal(err)
			}
			if err := store.Reschedule(ctx, value.ID, actor, command); !errors.Is(err, coordinationrequest.ErrRescheduleRepeated) {
				t.Fatal(err)
			}
			got, err := store.GetForUser(ctx, value.ID, actor)
			if err != nil || got.AcceptedOptionID != value.Options[0].ID {
				t.Fatal("original not retained")
			}
			if err := store.CancelConfirmed(ctx, value.ID, actor, command.ProposalID); !errors.Is(err, coordinationrequest.ErrCancellationInvalid) {
				t.Fatal("new option cancelled old booking")
			}
			command.Action = "accept"
			other := "alice"
			if actor == other {
				other = "bob"
			}
			gate := make(chan struct{})
			results := make(chan error, 2)
			for range 2 {
				go func() { <-gate; results <- store.Reschedule(ctx, value.ID, other, command) }()
			}
			close(gate)
			success, replay := 0, 0
			for range 2 {
				err := <-results
				if err == nil {
					success++
				} else if errors.Is(err, coordinationrequest.ErrRescheduleRepeated) {
					replay++
				} else {
					t.Fatal(err)
				}
			}
			if success != 1 || replay != 1 {
				t.Fatalf("success=%d replay=%d", success, replay)
			}
			got, err = store.GetForUser(ctx, value.ID, actor)
			if err != nil || got.AcceptedOptionID != command.ProposalID {
				t.Fatal("not swapped")
			}
			ranges, err := coordinationrequest.ConfirmedRanges([]coordinationrequest.CoordinationRequest{got})
			if err != nil || len(ranges) != 1 || !ranges[0].StartAt.Equal(command.StartAt.Truncate(time.Microsecond)) {
				t.Fatal("wrong reservation", err)
			}
			audits, err := b.Client.Collection("organizations").Doc(value.OrganizationID).Collection("auditLogs").Documents(ctx).GetAll()
			if err != nil || len(audits) != 3 {
				t.Fatal("duplicate/missing effects")
			}
			reuse := confirmationRequest("reuse", "carol", "bob", now, start)
			if err := store.Create(ctx, reuse); err != nil {
				t.Fatal(err)
			}
			if err := store.Respond(ctx, reuse.ID, "bob", coordinationrequest.Accepted, reuse.Options[0].ID); err != nil {
				t.Fatal("original slot not released", err)
			}
		})
	}
}

func TestRescheduleFailurePreservesReservation(t *testing.T) {
	for _, scenario := range []string{"busy", "dirty", "audit-failure", "deleting", "cancelled"} {
		t.Run(scenario, func(t *testing.T) {
			b, ctx := emulatorBackend(t)
			now := time.Now().UTC()
			value := confirmationRequest("original", "alice", "bob", now, now.Add(time.Hour))
			value.DurationMinutes = 30
			value.Status = coordinationrequest.Accepted
			value.AcceptedOptionID = value.Options[0].ID
			putDocument(t, ctx, b.Client.Collection("coordinationRequests").Doc(value.ID), value)
			p := publicationFixtures(now, "available", 1)[0]
			p.UserID = "bob"
			p.EndAt = now.Add(6 * time.Hour)
			putDocument(t, ctx, b.Client.Collection("users").Doc("bob").Collection("scheduleProjections").Doc(p.ID), p)
			command := coordinationrequest.RescheduleCommand{Action: "propose", ProposalID: "new-proposal", ExpectedOptionID: value.AcceptedOptionID, StartAt: now.Add(2 * time.Hour)}
			store := b.Request()
			if err := store.Reschedule(ctx, value.ID, "alice", command); err != nil {
				t.Fatal(err)
			}
			got, err := store.GetForUser(ctx, value.ID, "alice")
			if err != nil {
				t.Fatal(err)
			}
			command.Action = "accept"
			switch scenario {
			case "busy":
				other := confirmationRequest("busy", "carol", "bob", now, command.StartAt)
				other.Status = coordinationrequest.Accepted
				other.AcceptedOptionID = other.Options[0].ID
				putDocument(t, ctx, b.Client.Collection("coordinationRequests").Doc(other.ID), other)
			case "dirty":
				putDocument(t, ctx, b.projectionPublicationRef("bob"), projectionPublication{Dirty: true})
			case "audit-failure":
				_, event := coordinationrequest.RescheduleEffects(got, "bob", "accept", now)
				putDocument(t, ctx, b.Client.Collection("organizations").Doc(event.OrganizationID).Collection("auditLogs").Doc(event.ID), event)
			case "deleting":
				putDocument(t, ctx, b.accountDeletionRef("alice"), accountDeletion{Phase: "deleting", StartedAt: now})
			case "cancelled":
				if err := store.CancelConfirmed(ctx, value.ID, "alice", value.AcceptedOptionID); err != nil {
					t.Fatal(err)
				}
			}
			if err := store.Reschedule(ctx, value.ID, "bob", command); err == nil {
				t.Fatal("failed operation accepted")
			}
			doc, err := b.Client.Collection("coordinationRequests").Doc(value.ID).Get(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if err := doc.DataTo(&got); err != nil {
				t.Fatal(err)
			}
			if got.AcceptedOptionID != value.AcceptedOptionID || got.RescheduleProposal.Status != "proposed" {
				t.Fatal("partial swap")
			}
			note, _ := coordinationrequest.RescheduleEffects(got, "bob", "accept", now)
			if _, err := b.Client.Collection("users").Doc(note.UserID).Collection("notifications").Doc(note.ID).Get(ctx); !firestoreNotFound(err) {
				t.Fatal("partial notification")
			}
		})
	}
}
