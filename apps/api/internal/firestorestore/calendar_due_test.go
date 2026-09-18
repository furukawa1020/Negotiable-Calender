package firestorestore

import (
	calendarintegration "github.com/negotiable-calendar/negotiable-calendar/apps/api/internal/calendar"
	"testing"
	"time"
)

func TestDueCalendarClaimsAreBoundedAndFenced(t *testing.T) {
	b, ctx := emulatorBackend(t)
	now := time.Now().UTC().Truncate(time.Second)
	past, future := now.Add(-time.Hour), now.Add(time.Hour)
	for _, id := range []string{"alice", "bob", "carol", "deleting", "revoked", "leased", "legacy"} {
		v := calendarintegration.Connection{UserID: id, ConnectedAt: past, NextAttemptAt: &past}
		switch id {
		case "deleting":
			putDocument(t, ctx, b.accountDeletionRef(id), accountDeletion{Phase: "deleting", StartedAt: now})
		case "revoked":
			v.ReconnectRequired = true
		case "leased":
			v.SyncLeaseID = "active"
			v.SyncLeaseUntil = &future
		case "legacy":
			v.NextAttemptAt = nil
		}
		putDocument(t, ctx, b.Client.Collection("calendarConnections").Doc(id), v)
	}
	// An unrelated future row must not be decoded by the due query.
	putDocument(t, ctx, b.Client.Collection("calendarConnections").Doc("future"), map[string]any{"ReconnectRequired": false, "NextAttemptAt": future, "UserID": 42})
	gate := make(chan struct{})
	results := make(chan []calendarintegration.Connection, 2)
	failures := make(chan error, 2)
	for range 2 {
		go func() {
			<-gate
			v, err := b.Calendar().ClaimDueConnections(ctx, now, 2, 2*time.Minute)
			results <- v
			failures <- err
		}()
	}
	close(gate)
	seen := map[string]bool{}
	for range 2 {
		values := <-results
		if err := <-failures; err != nil {
			t.Fatal(err)
		}
		if len(values) > 2 {
			t.Fatal("unbounded claim")
		}
		for _, v := range values {
			if seen[v.UserID] || v.UserID == "deleting" || v.UserID == "revoked" || v.UserID == "leased" {
				t.Fatal("duplicate or fenced claim", v.UserID)
			}
			seen[v.UserID] = true
		}
	}
	values, err := b.Calendar().ClaimDueConnections(ctx, now, 5, 2*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	for _, v := range values {
		if seen[v.UserID] {
			t.Fatal("claimed twice")
		}
		seen[v.UserID] = true
	}
	if len(seen) != 4 {
		t.Fatalf("eligible claims=%v", seen)
	}
	doc, err := b.Client.Collection("calendarConnections").Doc("deleting").Get(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var untouched calendarintegration.Connection
	if err := doc.DataTo(&untouched); err != nil || untouched.LastAttemptAt != nil {
		t.Fatal("deleting account was updated")
	}
}
