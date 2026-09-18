package firestorestore

import (
	"errors"
	coordinationrequest "github.com/negotiable-calendar/negotiable-calendar/apps/api/internal/request"
	"testing"
	"time"
)

func TestRescheduleRacesCancellationAndCompetingBooking(t *testing.T) {
	for _, scenario := range []string{"cancel", "competing-booking"} {
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
			command := coordinationrequest.RescheduleCommand{Action: "propose", ProposalID: "proposal-new", ExpectedOptionID: value.AcceptedOptionID, StartAt: now.Add(2 * time.Hour)}
			store := b.Request()
			if err := store.Reschedule(ctx, value.ID, "alice", command); err != nil {
				t.Fatal(err)
			}
			other := confirmationRequest("competitor", "carol", "bob", now, command.StartAt)
			if err := store.Create(ctx, other); err != nil {
				t.Fatal(err)
			}
			command.Action = "accept"
			gate := make(chan struct{})
			results := make(chan error, 2)
			go func() { <-gate; results <- store.Reschedule(ctx, value.ID, "bob", command) }()
			go func() {
				<-gate
				if scenario == "cancel" {
					results <- store.CancelConfirmed(ctx, value.ID, "alice", value.AcceptedOptionID)
				} else {
					results <- store.Respond(ctx, other.ID, "bob", coordinationrequest.Accepted, other.Options[0].ID)
				}
			}()
			close(gate)
			success := 0
			for range 2 {
				err := <-results
				if err == nil {
					success++
				} else if !errors.Is(err, coordinationrequest.ErrBookingConflict) && !errors.Is(err, coordinationrequest.ErrCancellationInvalid) && !errors.Is(err, coordinationrequest.ErrRescheduleInvalid) {
					t.Fatal(err)
				}
			}
			if success != 1 {
				t.Fatalf("success=%d", success)
			}
			got, err := store.GetForUser(ctx, value.ID, "alice")
			if err != nil {
				t.Fatal(err)
			}
			if scenario == "cancel" {
				if !((got.Status == coordinationrequest.Cancelled && got.AcceptedOptionID == value.AcceptedOptionID) || (got.Status == coordinationrequest.Accepted && got.AcceptedOptionID == command.ProposalID)) {
					t.Fatal("mixed cancellation/swap")
				}
			} else {
				competitor, err := store.GetForUser(ctx, other.ID, "bob")
				if err != nil {
					t.Fatal(err)
				}
				if got.Status != coordinationrequest.Accepted || (competitor.Status == coordinationrequest.Accepted) == (got.AcceptedOptionID == command.ProposalID) {
					t.Fatal("double booking or lost original")
				}
			}
		})
	}
}
