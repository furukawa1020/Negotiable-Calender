package firestorestore

import (
	"errors"
	"reflect"
	"testing"
	"time"

	coord "github.com/negotiable-calendar/negotiable-calendar/apps/api/internal/request"
)

func TestFinalApprovalRejectsCorruptReservationEvidence(t *testing.T) {
	for _, flow := range []string{"confirmation", "reschedule"} {
		for _, role := range []string{"requester", "target"} {
			for _, scenario := range []string{"unknown-type", "wrong-request", "duplicate-selection", "wrong-id", "missing-selection", "invalid-time"} {
				t.Run(flow+"/"+role+"/"+scenario, func(t *testing.T) {
					b, ctx := emulatorBackend(t)
					now := time.Now().UTC().Truncate(time.Microsecond)
					value := confirmationRequest("original", "alice", "bob", now, now.Add(time.Hour))
					value.DurationMinutes = 30
					if flow == "reschedule" {
						value.Status, value.AcceptedOptionID = coord.Accepted, value.Options[0].ID
					}
					putDocument(t, ctx, b.Client.Collection("coordinationRequests").Doc(value.ID), value)
					p := publicationFixtures(now, "available", 1)[0]
					p.UserID, p.EndAt = "bob", now.Add(6*time.Hour)
					putDocument(t, ctx, b.Client.Collection("users").Doc("bob").Collection("scheduleProjections").Doc(p.ID), p)
					command := coord.RescheduleCommand{Action: "propose", ProposalID: "new-proposal", ExpectedOptionID: value.AcceptedOptionID, StartAt: now.Add(2 * time.Hour)}
					if flow == "reschedule" {
						if err := b.Request().Reschedule(ctx, value.ID, "alice", command); err != nil {
							t.Fatal(err)
						}
					}
					before, err := b.Request().GetForUser(ctx, value.ID, "alice")
					if err != nil {
						t.Fatal(err)
					}
					// The other workspace's record is non-overlapping: corruption alone must block.
					other := confirmationRequest("damaged", "alice", "carol", now, now.Add(4*time.Hour))
					if role == "target" {
						other.RequesterUserID, other.TargetUserID = "carol", "bob"
					}
					other.OrganizationID, other.Status, other.AcceptedOptionID = "another-workspace", coord.Accepted, other.Options[0].ID
					switch scenario {
					case "unknown-type":
						other.Options[0].Type = "unknown"
					case "wrong-request":
						other.Options[0].RequestID = "foreign"
					case "duplicate-selection":
						other.Options = append(other.Options, other.Options[0])
					case "wrong-id":
						other.ID = "foreign-document"
					case "missing-selection":
						other.Options = nil
					case "invalid-time":
						other.Options[0].EndAt = other.Options[0].StartAt
					}
					putDocument(t, ctx, b.Client.Collection("coordinationRequests").Doc("damaged"), other)
					note, event := coord.ConfirmationEffects(value, now)
					if flow == "reschedule" {
						command.Action = "accept"
						note, event = coord.RescheduleEffects(before, "bob", "accept", now)
						err = b.Request().Reschedule(ctx, value.ID, "bob", command)
					} else {
						err = b.Request().Respond(ctx, value.ID, "bob", coord.Accepted, value.Options[0].ID)
					}
					if !errors.Is(err, coord.ErrBookingConflict) {
						t.Fatalf("corrupt reservation admitted: %v", err)
					}
					after, err := b.Request().GetForUser(ctx, value.ID, "alice")
					if err != nil || !reflect.DeepEqual(before, after) {
						t.Fatal("failed approval mutated request", err)
					}
					if _, err := b.Client.Collection("users").Doc(note.UserID).Collection("notifications").Doc(note.ID).Get(ctx); !firestoreNotFound(err) {
						t.Fatal("partial notification", err)
					}
					if _, err := b.Client.Collection("organizations").Doc(event.OrganizationID).Collection("auditLogs").Doc(event.ID).Get(ctx); !firestoreNotFound(err) {
						t.Fatal("partial audit", err)
					}
					for _, participant := range []string{"alice", "bob"} {
						if _, err := b.Client.Collection("users").Doc(participant).Collection("projectionControls").Doc("coordinationConfirmation").Get(ctx); !firestoreNotFound(err) {
							t.Fatal("failed approval wrote a reservation lock", err)
						}
					}
				})
			}
		}
	}
}
