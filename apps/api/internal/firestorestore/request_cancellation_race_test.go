package firestorestore

import (
	"errors"
	"testing"
	"time"

	coordinationrequest "github.com/negotiable-calendar/negotiable-calendar/apps/api/internal/request"
)

func TestCancellationRacesReplacementAcceptance(t *testing.T) {
	b, ctx := emulatorBackend(t)
	now := time.Now().UTC()
	start := now.Add(time.Hour)
	old := confirmationRequest("old", "alice", "bob", now, start)
	old.Status = coordinationrequest.Accepted
	old.AcceptedOptionID = old.Options[0].ID
	next := confirmationRequest("next", "carol", "bob", now, start)
	for _, value := range []coordinationrequest.CoordinationRequest{old, next} {
		putDocument(t, ctx, b.Client.Collection("coordinationRequests").Doc(value.ID), value)
	}
	p := publicationFixtures(now, "available", 1)[0]
	p.UserID = "bob"
	p.EndAt = start.Add(time.Hour)
	putDocument(t, ctx, b.Client.Collection("users").Doc("bob").Collection("scheduleProjections").Doc(p.ID), p)
	gate := make(chan struct{})
	cancellations := make(chan error, 1)
	acceptances := make(chan error, 1)
	go func() {
		<-gate
		cancellations <- b.Request().CancelConfirmed(ctx, old.ID, "alice", old.AcceptedOptionID)
	}()
	go func() {
		<-gate
		acceptances <- b.Request().Respond(ctx, next.ID, "bob", coordinationrequest.Accepted, next.Options[0].ID)
	}()
	close(gate)
	if err := <-cancellations; err != nil {
		t.Fatal(err)
	}
	if err := <-acceptances; err != nil && !errors.Is(err, coordinationrequest.ErrBookingConflict) {
		t.Fatal(err)
	}
	if err := b.Request().Respond(ctx, next.ID, "bob", coordinationrequest.Accepted, next.Options[0].ID); err != nil && !errors.Is(err, coordinationrequest.ErrAlreadyAccepted) {
		t.Fatal(err)
	}
	got, err := b.Request().GetForUser(ctx, old.ID, "alice")
	if err != nil || got.Status != coordinationrequest.Cancelled {
		t.Fatal("old reservation retained")
	}
}

func TestConfirmedCancellationRespectsDeletionFences(t *testing.T) {
	for _, person := range []string{"alice", "bob", "delegate"} {
		t.Run(person, func(t *testing.T) {
			b, ctx := emulatorBackend(t)
			now := time.Now().UTC()
			value := confirmationRequest("meeting", "alice", "bob", now, now.Add(time.Hour))
			value.Status = coordinationrequest.Accepted
			value.AcceptedOptionID = value.Options[0].ID
			value.Options = append(value.Options, coordinationrequest.Option{ID: "delegate-option", RequestID: value.ID, Type: coordinationrequest.OptionDelegate, DelegateUserID: "delegate", CreatedAt: now})
			putDocument(t, ctx, b.Client.Collection("coordinationRequests").Doc(value.ID), value)
			putDocument(t, ctx, b.accountDeletionRef(person), accountDeletion{Phase: "deleting", StartedAt: now})
			if err := b.Request().CancelConfirmed(ctx, value.ID, "alice", value.AcceptedOptionID); !errors.Is(err, errAccountDeleting) {
				t.Fatalf("unfenced: %v", err)
			}
			doc, err := b.Client.Collection("coordinationRequests").Doc(value.ID).Get(ctx)
			if err != nil {
				t.Fatal(err)
			}
			var got coordinationrequest.CoordinationRequest
			if err := doc.DataTo(&got); err != nil || got.Status != coordinationrequest.Accepted {
				t.Fatal("changed under deletion")
			}
			note, event := coordinationrequest.ConfirmedCancellationEffects(value, "alice", now)
			if _, err := b.Client.Collection("users").Doc(note.UserID).Collection("notifications").Doc(note.ID).Get(ctx); !firestoreNotFound(err) {
				t.Fatal("late notification")
			}
			if _, err := b.Client.Collection("organizations").Doc(event.OrganizationID).Collection("auditLogs").Doc(event.ID).Get(ctx); !firestoreNotFound(err) {
				t.Fatal("late audit")
			}
		})
	}
}
