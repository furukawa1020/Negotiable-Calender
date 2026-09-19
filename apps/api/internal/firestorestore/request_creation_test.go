package firestorestore

import (
	"cloud.google.com/go/firestore"
	"errors"
	"github.com/negotiable-calendar/negotiable-calendar/apps/api/internal/notification"
	coordinationrequest "github.com/negotiable-calendar/negotiable-calendar/apps/api/internal/request"
	"testing"
	"time"
)

func TestAtomicRequestCreation(t *testing.T) {
	for _, scenario := range []string{"concurrent", "notification-collision", "audit-collision", "missing-member", "deleting"} {
		t.Run(scenario, func(t *testing.T) {
			b, ctx := emulatorBackend(t)
			now := time.Now().UTC().Truncate(time.Microsecond)
			value := confirmationRequest("create-once", "alice", "bob", now, now.Add(time.Hour))
			for _, user := range []string{"alice", "bob"} {
				if scenario == "missing-member" && user == "bob" {
					continue
				}
				putDocument(t, ctx, b.Client.Collection("organizations").Doc(value.OrganizationID).Collection("members").Doc(user), map[string]any{"Role": "MANAGER"})
			}
			note, event := coordinationrequest.CreationEffects(value)
			requestRef := b.Client.Collection("coordinationRequests").Doc(value.ID)
			noteRef := b.Client.Collection("users").Doc(note.UserID).Collection("notifications").Doc(note.ID)
			eventRef := b.Client.Collection("organizations").Doc(event.OrganizationID).Collection("auditLogs").Doc(event.ID)
			if scenario == "notification-collision" {
				note.ReadAt = &now
				putDocument(t, ctx, noteRef, note)
			}
			if scenario == "audit-collision" {
				putDocument(t, ctx, eventRef, event)
			}
			if scenario == "deleting" {
				putDocument(t, ctx, b.accountDeletionRef("alice"), accountDeletion{Phase: "deleting", StartedAt: now})
			}
			store := b.Request()
			if scenario != "concurrent" {
				created, err := store.CreateOnce(ctx, value)
				if err == nil || created {
					t.Fatal("invalid creation succeeded")
				}
				if _, err := requestRef.Get(ctx); !firestoreNotFound(err) {
					t.Fatal("partial request exists")
				}
				for _, ref := range []*firestore.DocumentRef{noteRef, eventRef} {
					seeded := (scenario == "notification-collision" && ref == noteRef) || (scenario == "audit-collision" && ref == eventRef)
					_, err := ref.Get(ctx)
					if !seeded && !firestoreNotFound(err) {
						t.Fatal("partial effect exists")
					}
				}
				if scenario == "missing-member" || scenario == "deleting" {
					if _, err := store.LookupCreation(ctx, value); err == nil {
						t.Fatal("lookup bypassed access fence")
					}
				}
				return
			}
			type result struct {
				created bool
				err     error
			}
			gate, results := make(chan struct{}), make(chan result, 2)
			for range 2 {
				go func() { <-gate; created, err := store.CreateOnce(ctx, value); results <- result{created, err} }()
			}
			close(gate)
			count := 0
			for range 2 {
				got := <-results
				if got.err != nil {
					t.Fatal(got.err)
				}
				if got.created {
					count++
				}
			}
			if count != 1 {
				t.Fatalf("creation count %d", count)
			}
			saved, err := store.LookupCreation(ctx, value)
			if err != nil || saved.ID != value.ID || len(saved.Options) != len(value.Options) {
				t.Fatal("cannot recover response", err)
			}
			note.ReadAt = &now
			putDocument(t, ctx, noteRef, note)
			if err := store.Cancel(ctx, value.ID, "alice"); err != nil {
				t.Fatal(err)
			}
			if created, err := store.CreateOnce(ctx, value); err != nil || created {
				t.Fatal("replay recreated request", err)
			}
			replay, err := store.LookupCreation(ctx, value)
			if err != nil || replay.Status != coordinationrequest.Cancelled {
				t.Fatal("replay lost lifecycle", err)
			}
			doc, err := noteRef.Get(ctx)
			if err != nil {
				t.Fatal(err)
			}
			var existing notification.Notification
			if err := doc.DataTo(&existing); err != nil || existing.ReadAt == nil {
				t.Fatal("notification read state overwritten")
			}
			value.Title = "changed"
			if _, err := store.CreateOnce(ctx, value); !errors.Is(err, coordinationrequest.ErrCreationConflict) {
				t.Fatal("changed payload accepted", err)
			}
			if _, err := store.LookupCreation(ctx, value); !errors.Is(err, coordinationrequest.ErrCreationConflict) {
				t.Fatal("changed lookup accepted", err)
			}
		})
	}
}
