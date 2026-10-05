package firestorestore

import (
	"reflect"
	"testing"
	"time"

	"cloud.google.com/go/firestore"
	coord "github.com/negotiable-calendar/negotiable-calendar/apps/api/internal/request"
)

func TestCreationFreshnessAndExpiredReplay(t *testing.T) {
	for _, scenario := range []string{"deadline", "meeting-started", "async-expired", "expired-replay"} {
		t.Run(scenario, func(t *testing.T) {
			b, ctx := emulatorBackend(t)
			now := time.Now().UTC().Truncate(time.Microsecond)
			value := confirmationRequest("freshness", "alice", "bob", now.Add(-3*time.Hour), now.Add(-time.Hour))
			if scenario == "deadline" || scenario == "expired-replay" {
				value.DeadlineAt = now.Add(-time.Minute)
			}
			if scenario == "async-expired" {
				at := now.Add(-time.Minute)
				value.Options = []coord.Option{{ID: "async", RequestID: value.ID, Type: coord.OptionAsync, ResponseBy: &at, CreatedAt: value.CreatedAt}}
			}
			for _, user := range []string{"alice", "bob"} {
				putDocument(t, ctx, b.Client.Collection("organizations").Doc(value.OrganizationID).Collection("members").Doc(user), map[string]any{"Role": "MANAGER"})
			}
			ref := b.Client.Collection("coordinationRequests").Doc(value.ID)
			note, event := coord.CreationEffects(value)
			noteRef := b.Client.Collection("users").Doc(note.UserID).Collection("notifications").Doc(note.ID)
			eventRef := b.Client.Collection("organizations").Doc(event.OrganizationID).Collection("auditLogs").Doc(event.ID)
			if scenario == "expired-replay" {
				// A pre-existing winner may have expired and changed lifecycle while the response was lost.
				saved := value
				saved.Status = coord.Cancelled
				putDocument(t, ctx, ref, saved)
				putDocument(t, ctx, noteRef, note)
				putDocument(t, ctx, eventRef, event)
				before, err := b.Request().LookupCreation(ctx, value)
				if err != nil {
					t.Fatal(err)
				}
				created, err := b.Request().CreateOnce(ctx, value)
				if err != nil || created {
					t.Fatal("expired replay rejected or recreated", err)
				}
				after, err := b.Request().LookupCreation(ctx, value)
				if err != nil || !reflect.DeepEqual(before, after) {
					t.Fatal("replay changed saved state", err)
				}
				return
			}
			if created, err := b.Request().CreateOnce(ctx, value); err == nil || created {
				t.Fatal("new expired request persisted", err)
			}
			for _, ref := range []*firestore.DocumentRef{ref, noteRef, eventRef} {
				if _, err := ref.Get(ctx); !firestoreNotFound(err) {
					t.Fatal("partial request/effect", err)
				}
			}
		})
	}
}
