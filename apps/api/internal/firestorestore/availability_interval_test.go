package firestorestore

import (
	"errors"
	"testing"
	"time"

	"github.com/negotiable-calendar/negotiable-calendar/apps/api/internal/projection"
	coord "github.com/negotiable-calendar/negotiable-calendar/apps/api/internal/request"
)

func TestAvailabilityAfterFullSyncSizedPublication(t *testing.T) {
	b, ctx := emulatorBackend(t)
	now := time.Now().UTC().Truncate(time.Second)
	for _, user := range []string{"alice", "bob"} {
		putDocument(t, ctx, b.Client.Collection("users").Doc(user), userRecord{ID: user})
	}
	// Match the production default: 30 past + 90 future days, every 15 minutes.
	from := now.Add(-30 * 24 * time.Hour).Truncate(projection.BucketSize)
	rows := publicationFixtures(now, "full-sync", 120*24*4)
	for i := range rows {
		rows[i].UserID = "bob"
		rows[i].StartAt = from.Add(time.Duration(i) * projection.BucketSize)
		rows[i].EndAt = rows[i].StartAt.Add(projection.BucketSize)
	}
	if err := b.Projection().Replace(ctx, "bob", from, from.Add(120*24*time.Hour), rows); err != nil {
		t.Fatal(err)
	}
	start := now.Add(time.Hour).Truncate(projection.BucketSize)
	end := start.Add(30 * time.Minute)
	trace := &planningReadTrace{}
	reader := planningReader(t, ctx, trace)
	t.Run("planning", func(t *testing.T) {
		// Use the same project/client as the parent fixture.
		segments, bookings, err := reader.LoadPlanningSources(ctx, "bob", "alice", start, end)
		if err != nil || len(segments) != 2 || len(bookings) != 0 || trace.documents != 2 {
			t.Fatalf("full-sync planning: segments=%d bookings=%d err=%v", len(segments), len(bookings), err)
		}
	})
	t.Run("confirmation-and-reschedule", func(t *testing.T) {
		value := confirmationRequest("full-sync-request", "alice", "bob", now, start)
		value.DurationMinutes = 30
		if err := b.Request().Create(ctx, value); err != nil {
			t.Fatal(err)
		}
		*trace = planningReadTrace{}
		if err := reader.Respond(ctx, value.ID, "bob", coord.Accepted, value.Options[0].ID); err != nil {
			t.Fatalf("full-sync confirmation: %v", err)
		}
		// Two role-query appearances of this request plus two overlapping buckets.
		if trace.documents != 4 {
			t.Fatalf("confirmation fetched unrelated projections: %d documents", trace.documents)
		}
		command := coord.RescheduleCommand{Action: "propose", ProposalID: "full-sync-move", ExpectedOptionID: value.Options[0].ID, StartAt: start.Add(time.Hour)}
		if err := b.Request().Reschedule(ctx, value.ID, "alice", command); err != nil {
			t.Fatal(err)
		}
		command.Action = "accept"
		if err := b.Request().Reschedule(ctx, value.ID, "bob", command); err != nil {
			t.Fatalf("full-sync reschedule: %v", err)
		}
	})
}

func TestAvailabilityIntervalPreservesOverlapsAndExcludesAdjacentRows(t *testing.T) {
	for _, scenario := range []string{"adjacent", "spanning-closed", "inside-closed", "expired", "gap"} {
		t.Run(scenario, func(t *testing.T) {
			b, ctx := emulatorBackend(t)
			now := time.Now().UTC().Truncate(time.Second)
			start := now.Add(time.Hour)
			value := confirmationRequest("interval", "alice", "bob", now, start)
			end := *value.Options[0].EndAt
			rows := publicationFixtures(now, "interval", 3)
			for i := range rows {
				rows[i].UserID = "bob"
			}
			rows[0].StartAt, rows[0].EndAt = start, end
			rows[1].StartAt, rows[1].EndAt = start.Add(-time.Hour), start
			rows[2].StartAt, rows[2].EndAt = end, end.Add(time.Hour)
			rows[1].State.Requestability, rows[2].State.Requestability = "closed", "closed"
			wantCount := 1
			switch scenario {
			case "spanning-closed":
				rows[1].EndAt = end.Add(time.Hour)
				wantCount = 2
			case "inside-closed":
				rows[1].StartAt, rows[1].EndAt = start.Add(time.Minute), end.Add(-time.Minute)
				wantCount = 2
			case "expired":
				rows[0].ExpiresAt = now.Add(-time.Second)
			case "gap":
				rows[0].EndAt = end.Add(-time.Minute)
			}
			for _, user := range []string{"alice", "bob"} {
				putDocument(t, ctx, b.Client.Collection("users").Doc(user), userRecord{ID: user})
			}
			for _, row := range rows {
				putDocument(t, ctx, b.Client.Collection("users").Doc("bob").Collection("scheduleProjections").Doc(row.ID), row)
			}
			trace := &planningReadTrace{}
			reader := planningReader(t, ctx, trace)
			segments, _, err := reader.LoadPlanningSources(ctx, "bob", "alice", start, end)
			if err != nil || len(segments) != wantCount || trace.documents != wantCount {
				t.Fatalf("overlap query: rows=%d fetched=%d want=%d err=%v", len(segments), trace.documents, wantCount, err)
			}
			if err := reader.Create(ctx, value); err != nil {
				t.Fatal(err)
			}
			err = reader.Respond(ctx, value.ID, "bob", coord.Accepted, value.Options[0].ID)
			if scenario == "adjacent" {
				if err != nil {
					t.Fatal(err)
				}
			} else if !errors.Is(err, coord.ErrAvailabilityChanged) {
				t.Fatalf("unsafe confirmation: %v", err)
			}
		})
	}
}

func TestConfirmationOverlapOverflowStillFailsClosed(t *testing.T) {
	b, ctx := emulatorBackend(t)
	now := time.Now().UTC().Truncate(time.Second)
	start, end := now.Add(time.Hour), now.Add(90*time.Minute)
	rows := publicationFixtures(now, "overflow", 10001)
	for i := range rows {
		rows[i].UserID = "bob"
		rows[i].StartAt, rows[i].EndAt = start, end
	}
	if err := b.Projection().Replace(ctx, "bob", start, end, rows); err != nil {
		t.Fatal(err)
	}
	value := confirmationRequest("overflow", "alice", "bob", now, start)
	if err := b.Request().Create(ctx, value); err != nil {
		t.Fatal(err)
	}
	if err := b.Request().Respond(ctx, value.ID, "bob", coord.Accepted, value.Options[0].ID); !errors.Is(err, coord.ErrAvailabilityChanged) {
		t.Fatalf("overflow must not become partial availability: %v", err)
	}
	got, err := b.Request().GetForUser(ctx, value.ID, "bob")
	if err != nil || got.Status != coord.Suggested || got.AcceptedOptionID != "" {
		t.Fatal("overflow changed the request", err)
	}
	notes, err := b.Client.Collection("users").Doc("alice").Collection("notifications").Documents(ctx).GetAll()
	if err != nil || len(notes) != 0 {
		t.Fatal("overflow created confirmation effects", err)
	}
}
