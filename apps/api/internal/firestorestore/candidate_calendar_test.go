package firestorestore

import (
	"context"
	cal "github.com/negotiable-calendar/negotiable-calendar/apps/api/internal/calendar"
	"testing"
	"time"
)

func TestCandidateCalendarMissingDeletedCancelledAndChangedSource(t *testing.T) {
	b, ctx := emulatorBackend(t)
	store := b.Request()
	now := time.Now().UTC().Truncate(time.Microsecond)
	if got, err := store.LoadRequesterCalendar(ctx, "missing"); err == nil || got.Validate(now) == nil {
		t.Fatal("missing user treated as free")
	}
	putDocument(t, ctx, b.Client.Collection("users").Doc("alice"), userRecord{ID: "alice"})
	if got, err := store.LoadRequesterCalendar(ctx, "alice"); err != nil || !got.Allows(now, now.Add(time.Hour), now) {
		t.Fatal("never-connected user blocked", err)
	}
	source := cal.SourceSnapshot{Revision: "first", ObservedAt: now, From: now, To: now.Add(time.Hour)}
	inputs := privateInputsControl{ID: source.Revision, Ready: true, Source: &source}
	putDocument(t, ctx, b.privateInputsRef("alice"), inputs)
	putDocument(t, ctx, b.Client.Collection("calendarConnections").Doc("alice"), cal.Connection{UserID: "alice", LastSyncedAt: &now})
	first, err := store.LoadRequesterCalendar(ctx, "alice")
	if err != nil {
		t.Fatal(err)
	}
	inputs.Ready = false
	putDocument(t, ctx, b.privateInputsRef("alice"), inputs)
	if got, err := store.LoadRequesterCalendar(ctx, "alice"); err == nil || got.Validate(now) == nil {
		t.Fatal("changed source reused old successful read")
	}
	if !first.Allows(now, now.Add(time.Hour), now) {
		t.Fatal("returned snapshot is not immutable")
	}
	inputs.Ready = true
	putDocument(t, ctx, b.privateInputsRef("alice"), inputs)
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if got, err := store.LoadRequesterCalendar(cancelled, "alice"); err == nil || got.Validate(now) == nil {
		t.Fatal("cancelled read returned free time")
	}
	putDocument(t, ctx, b.accountDeletionRef("alice"), accountDeletion{Phase: "deleting", StartedAt: now})
	if got, err := store.LoadRequesterCalendar(ctx, "alice"); err == nil || got.Validate(now) == nil {
		t.Fatal("deleting account calendar read")
	}
}
