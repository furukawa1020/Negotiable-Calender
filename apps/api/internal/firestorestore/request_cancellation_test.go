package firestorestore

import (
	"errors"
	"testing"
	"time"

	coordinationrequest "github.com/negotiable-calendar/negotiable-calendar/apps/api/internal/request"
)

func TestConfirmedCancellationAtomicLifecycle(t *testing.T) {
	for _, actor := range []string{"alice", "bob"} {
		t.Run(actor, func(t *testing.T) {
			b, ctx := emulatorBackend(t)
			now := time.Now().UTC()
			start := now.Add(time.Hour)
			value := confirmationRequest("meeting", "alice", "bob", now, start)
			p := publicationFixtures(now, "available", 1)[0]
			p.UserID = "bob"
			p.EndAt = start.Add(time.Hour)
			putDocument(t, ctx, b.Client.Collection("users").Doc("bob").Collection("scheduleProjections").Doc(p.ID), p)
			store := b.Request()
			if err := store.Create(ctx, value); err != nil {
				t.Fatal(err)
			}
			if err := store.Respond(ctx, value.ID, "bob", coordinationrequest.Accepted, value.Options[0].ID); err != nil {
				t.Fatal(err)
			}
			if err := store.CancelConfirmed(ctx, value.ID, "mallory", value.Options[0].ID); !errors.Is(err, coordinationrequest.ErrNotFound) {
				t.Fatal(err)
			}
			if err := store.CancelConfirmed(ctx, value.ID, actor, "old-option"); !errors.Is(err, coordinationrequest.ErrCancellationInvalid) {
				t.Fatal(err)
			}
			gate := make(chan struct{})
			results := make(chan error, 2)
			for range 2 {
				go func() { <-gate; results <- store.CancelConfirmed(ctx, value.ID, actor, value.Options[0].ID) }()
			}
			close(gate)
			success, replay := 0, 0
			for range 2 {
				err := <-results
				if err == nil {
					success++
				} else if errors.Is(err, coordinationrequest.ErrAlreadyCancelled) {
					replay++
				} else {
					t.Fatal(err)
				}
			}
			if success != 1 || replay != 1 {
				t.Fatalf("success=%d replay=%d", success, replay)
			}
			got, err := store.GetForUser(ctx, value.ID, actor)
			if err != nil || got.Status != coordinationrequest.Cancelled || got.AcceptedOptionID != value.Options[0].ID {
				t.Fatalf("state=%+v err=%v", got, err)
			}
			ranges, err := coordinationrequest.ConfirmedRanges([]coordinationrequest.CoordinationRequest{got})
			if err != nil || len(ranges) != 0 {
				t.Fatal("reservation retained")
			}
			note, event := coordinationrequest.ConfirmedCancellationEffects(value, actor, now)
			notes, err := b.Client.Collection("users").Doc(note.UserID).Collection("notifications").Where("Type", "==", note.Type).Documents(ctx).GetAll()
			if err != nil || len(notes) != 1 || notes[0].Ref.ID != note.ID {
				t.Fatalf("notes=%d err=%v", len(notes), err)
			}
			audits, err := b.Client.Collection("organizations").Doc(event.OrganizationID).Collection("auditLogs").Where("Action", "==", event.Action).Documents(ctx).GetAll()
			if err != nil || len(audits) != 1 || audits[0].Ref.ID != event.ID {
				t.Fatal("audit missing or duplicated")
			}
			replacement := confirmationRequest("replacement", "carol", "bob", now, start)
			if err := store.Create(ctx, replacement); err != nil {
				t.Fatal(err)
			}
			if err := store.Respond(ctx, replacement.ID, "bob", coordinationrequest.Accepted, replacement.Options[0].ID); err != nil {
				t.Fatalf("released slot unavailable: %v", err)
			}
		})
	}
}

func TestConfirmedCancellationRollsBackEffects(t *testing.T) {
	for _, failure := range []string{"notification", "audit"} {
		t.Run(failure, func(t *testing.T) {
			b, ctx := emulatorBackend(t)
			now := time.Now().UTC()
			value := confirmationRequest("meeting", "alice", "bob", now, now.Add(time.Hour))
			value.Status = coordinationrequest.Accepted
			value.AcceptedOptionID = value.Options[0].ID
			ref := b.Client.Collection("coordinationRequests").Doc(value.ID)
			putDocument(t, ctx, ref, value)
			note, event := coordinationrequest.ConfirmedCancellationEffects(value, "alice", now)
			noteRef := b.Client.Collection("users").Doc(note.UserID).Collection("notifications").Doc(note.ID)
			auditRef := b.Client.Collection("organizations").Doc(event.OrganizationID).Collection("auditLogs").Doc(event.ID)
			if failure == "notification" {
				putDocument(t, ctx, noteRef, note)
			} else {
				putDocument(t, ctx, auditRef, event)
			}
			if err := b.Request().CancelConfirmed(ctx, value.ID, "alice", value.AcceptedOptionID); err == nil {
				t.Fatal("effect failure accepted")
			}
			got, err := b.Request().GetForUser(ctx, value.ID, "alice")
			if err != nil || got.Status != coordinationrequest.Accepted {
				t.Fatal("partial cancellation committed")
			}
			missing := noteRef
			if failure == "notification" {
				missing = auditRef
			}
			if _, err := missing.Get(ctx); !firestoreNotFound(err) {
				t.Fatal("partial side effect committed")
			}
		})
	}
}
