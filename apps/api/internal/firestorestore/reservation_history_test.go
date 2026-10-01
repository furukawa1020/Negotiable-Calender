package firestorestore

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	coord "github.com/negotiable-calendar/negotiable-calendar/apps/api/internal/request"
)

func seedReservationHistory(t *testing.T, b *Backend, ctx context.Context, count int, accepted bool, now, start time.Time) {
	t.Helper()
	statuses := []coord.Status{coord.Pending, coord.Suggested, coord.Declined, coord.Delegated, coord.Cancelled, coord.Expired, coord.Completed, coord.Async}
	batch := b.Client.Batch()
	for i := 0; i < count; i++ {
		value := confirmationRequest(fmt.Sprintf("history-%05d", i), "alice", "bob", now, start)
		value.Status = statuses[i%len(statuses)]
		if accepted {
			value.Status, value.AcceptedOptionID = coord.Accepted, value.Options[0].ID
		}
		batch.Set(b.Client.Collection("coordinationRequests").Doc(value.ID), value)
		if (i+1)%400 == 0 || i == count-1 {
			if _, err := batch.Commit(ctx); err != nil {
				t.Fatal(err)
			}
			batch = b.Client.Batch()
		}
	}
}

func TestConfirmationAndRescheduleIgnoreNonAcceptedHistory(t *testing.T) {
	b, ctx := emulatorBackend(t)
	now := time.Now().UTC().Truncate(time.Second)
	start := now.Add(time.Hour)
	seedReservationHistory(t, b, ctx, 5001, false, now, now.Add(-24*time.Hour))
	p := publicationFixtures(now, "available", 1)[0]
	p.UserID, p.EndAt = "bob", start.Add(4*time.Hour)
	putDocument(t, ctx, b.Client.Collection("users").Doc("bob").Collection("scheduleProjections").Doc(p.ID), p)
	value := confirmationRequest("new-booking", "alice", "bob", now, start)
	if err := b.Request().Create(ctx, value); err != nil {
		t.Fatal(err)
	}
	trace := &planningReadTrace{}
	reader := planningReader(t, ctx, trace)
	if err := reader.Respond(ctx, value.ID, "bob", coord.Accepted, value.Options[0].ID); err != nil {
		t.Fatalf("closed history prevented confirmation: %v", err)
	}
	if trace.documents != 1 {
		t.Fatalf("confirmation fetched history: %d documents, want only the projection", trace.documents)
	}
	command := coord.RescheduleCommand{Action: "propose", ProposalID: "history-move", ExpectedOptionID: value.Options[0].ID, StartAt: start.Add(time.Hour)}
	if err := reader.Reschedule(ctx, value.ID, "alice", command); err != nil {
		t.Fatal(err)
	}
	*trace = planningReadTrace{}
	command.Action = "accept"
	if err := reader.Reschedule(ctx, value.ID, "bob", command); err != nil {
		t.Fatalf("closed history prevented reschedule: %v", err)
	}
	if trace.documents != 3 {
		t.Fatalf("reschedule fetched history: %d documents, want own booking twice and projection", trace.documents)
	}
	history, err := reader.GetForUser(ctx, "history-00006", "alice")
	if err != nil || history.Status != coord.Completed {
		t.Fatal("historical request was removed or changed", err)
	}
}

func TestAcceptedReservationsStillBlockAcrossWorkspacesAndRoles(t *testing.T) {
	for _, participant := range []string{"alice", "bob"} {
		for _, role := range []string{"requester", "target"} {
			for _, corrupt := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/%s/corrupt=%t", participant, role, corrupt), func(t *testing.T) {
					b, ctx := emulatorBackend(t)
					now := time.Now().UTC().Truncate(time.Second)
					start := now.Add(time.Hour)
					other := confirmationRequest("other-org-booking", "carol", participant, now, start)
					if role == "requester" {
						other.RequesterUserID, other.TargetUserID = participant, "carol"
					}
					other.OrganizationID = "another-workspace"
					other.Status, other.AcceptedOptionID = coord.Accepted, other.Options[0].ID
					if corrupt {
						other.Options = nil
					}
					putDocument(t, ctx, b.Client.Collection("coordinationRequests").Doc(other.ID), other)
					value := confirmationRequest("new-booking", "alice", "bob", now, start)
					if err := b.Request().Create(ctx, value); err != nil {
						t.Fatal(err)
					}
					if err := b.Request().Respond(ctx, value.ID, "bob", coord.Accepted, value.Options[0].ID); !errors.Is(err, coord.ErrBookingConflict) {
						t.Fatalf("accepted conflict omitted: %v", err)
					}
				})
			}
		}
	}
}

func TestAcceptedReservationBudgetBoundary(t *testing.T) {
	for _, count := range []int{5000, 5001} {
		t.Run(fmt.Sprint(count), func(t *testing.T) {
			b, ctx := emulatorBackend(t)
			now := time.Now().UTC().Truncate(time.Second)
			// These accepted meetings do not overlap; the budget must still apply.
			seedReservationHistory(t, b, ctx, count, true, now, now.Add(24*time.Hour))
			value := confirmationRequest("boundary", "alice", "bob", now, now.Add(time.Hour))
			p := publicationFixtures(now, "available", 1)[0]
			p.UserID, p.EndAt = "bob", now.Add(3*time.Hour)
			putDocument(t, ctx, b.Client.Collection("users").Doc("bob").Collection("scheduleProjections").Doc(p.ID), p)
			if err := b.Request().Create(ctx, value); err != nil {
				t.Fatal(err)
			}
			err := b.Request().Respond(ctx, value.ID, "bob", coord.Accepted, value.Options[0].ID)
			if count == 5000 {
				if err != nil {
					t.Fatalf("exact reservation limit rejected: %v", err)
				}
				return
			}
			if !errors.Is(err, coord.ErrAvailabilityChanged) {
				t.Fatalf("accepted overflow became partial success: %v", err)
			}
			got, err := b.Request().GetForUser(ctx, value.ID, "bob")
			if err != nil || got.Status != coord.Suggested || got.AcceptedOptionID != "" {
				t.Fatal("overflow changed request", err)
			}
			notes, err := b.Client.Collection("users").Doc("alice").Collection("notifications").Documents(ctx).GetAll()
			if err != nil || len(notes) != 0 {
				t.Fatal("overflow emitted confirmation effects", err)
			}
		})
	}
}
