package firestorestore

import (
	"cloud.google.com/go/firestore"
	"errors"
	"testing"
	"time"

	"github.com/negotiable-calendar/negotiable-calendar/apps/api/internal/audit"
	"github.com/negotiable-calendar/negotiable-calendar/apps/api/internal/notification"
	coordinationrequest "github.com/negotiable-calendar/negotiable-calendar/apps/api/internal/request"
)

func TestConfirmationEffectsCommitOrRollback(t *testing.T) {
	for _, scenario := range []string{"concurrent", "notification-collision", "audit-collision", "outsider", "deleting"} {
		t.Run(scenario, func(t *testing.T) {
			b, ctx := emulatorBackend(t)
			now := time.Now().UTC().Truncate(time.Microsecond)
			value := confirmationRequest("r", "alice", "bob", now, now.Add(time.Hour))
			p := publicationFixtures(now, "available", 1)[0]
			p.UserID, p.EndAt = "bob", now.Add(3*time.Hour)
			putDocument(t, ctx, b.Client.Collection("users").Doc("bob").Collection("scheduleProjections").Doc(p.ID), p)
			store := b.Request()
			if err := store.Create(ctx, value); err != nil {
				t.Fatal(err)
			}
			note, event := coordinationrequest.ConfirmationEffects(value, now)
			noteRef := b.Client.Collection("users").Doc(note.UserID).Collection("notifications").Doc(note.ID)
			eventRef := b.Client.Collection("organizations").Doc(event.OrganizationID).Collection("auditLogs").Doc(event.ID)
			switch scenario {
			case "notification-collision":
				note.ReadAt = &now
				putDocument(t, ctx, noteRef, note)
			case "audit-collision":
				putDocument(t, ctx, eventRef, event)
			case "deleting":
				putDocument(t, ctx, b.accountDeletionRef("alice"), accountDeletion{Phase: "deleting", StartedAt: now})
			}
			actor := "bob"
			if scenario == "outsider" {
				actor = "mallory"
			}
			if scenario != "concurrent" {
				if err := store.Respond(ctx, value.ID, actor, coordinationrequest.Accepted, value.Options[0].ID); err == nil {
					t.Fatal("invalid acceptance committed")
				}
				doc, err := b.Client.Collection("coordinationRequests").Doc(value.ID).Get(ctx)
				if err != nil {
					t.Fatal(err)
				}
				var got coordinationrequest.CoordinationRequest
				if err := doc.DataTo(&got); err != nil || got.Status != coordinationrequest.Suggested || got.AcceptedOptionID != "" {
					t.Fatal("partial acceptance")
				}
				for _, ref := range []*firestore.DocumentRef{noteRef, eventRef} {
					doc, err := ref.Get(ctx)
					seeded := (scenario == "notification-collision" && ref == noteRef) || (scenario == "audit-collision" && ref == eventRef)
					if !seeded && !firestoreNotFound(err) {
						t.Fatal("partial effect")
					}
					if seeded && (err != nil || doc == nil) {
						t.Fatal("existing record lost")
					}
					if seeded && ref == noteRef {
						var saved notification.Notification
						if err := doc.DataTo(&saved); err != nil || saved.ReadAt == nil || !saved.ReadAt.Equal(now) {
							t.Fatal("existing read state overwritten")
						}
					}
				}
				return
			}
			gate, results := make(chan struct{}), make(chan error, 2)
			for range 2 {
				go func() {
					<-gate
					results <- store.Respond(ctx, value.ID, actor, coordinationrequest.Accepted, value.Options[0].ID)
				}()
			}
			close(gate)
			success := 0
			for range 2 {
				err := <-results
				if err == nil {
					success++
				} else if !errors.Is(err, coordinationrequest.ErrAlreadyAccepted) && !errors.Is(err, coordinationrequest.ErrBookingConflict) {
					t.Fatal(err)
				}
			}
			if success != 1 {
				t.Fatalf("successful commits=%d", success)
			}
			readAt := now.Add(time.Second)
			if ok, err := b.Notification().MarkRead(ctx, note.ID, "alice", readAt); err != nil || !ok {
				t.Fatal("mark read failed", err)
			}
			if err := store.Respond(ctx, value.ID, actor, coordinationrequest.Accepted, value.Options[0].ID); !errors.Is(err, coordinationrequest.ErrAlreadyAccepted) {
				t.Fatal(err)
			}
			got, err := store.GetForUser(ctx, value.ID, "alice")
			if err != nil || got.Status != coordinationrequest.Accepted {
				t.Fatal("acceptance missing", err)
			}
			notes, err := noteRef.Parent.Documents(ctx).GetAll()
			if err != nil || len(notes) != 1 {
				t.Fatal("notification count", len(notes), err)
			}
			var saved notification.Notification
			if err := notes[0].DataTo(&saved); err != nil || saved.ID != note.ID || saved.UserID != "alice" || saved.Type != notification.RequestAccepted || saved.ReadAt == nil || !saved.ReadAt.Equal(readAt) || !saved.CreatedAt.Equal(got.UpdatedAt) {
				t.Fatal("notification mismatch", err)
			}
			events, err := eventRef.Parent.Documents(ctx).GetAll()
			if err != nil || len(events) != 1 {
				t.Fatal("audit count", len(events), err)
			}
			var savedEvent audit.Event
			if err := events[0].DataTo(&savedEvent); err != nil || savedEvent.ID != event.ID || savedEvent.ActorUserID != "bob" || savedEvent.Action != audit.RequestAccepted || !savedEvent.CreatedAt.Equal(got.UpdatedAt) {
				t.Fatal("audit mismatch", err)
			}
		})
	}
}
