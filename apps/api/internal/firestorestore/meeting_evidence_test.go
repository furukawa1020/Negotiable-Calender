package firestorestore

import (
	"errors"
	"reflect"
	"testing"
	"time"

	coord "github.com/negotiable-calendar/negotiable-calendar/apps/api/internal/request"
)

func TestMeetingLifecycleCorruptEvidenceHasNoEffects(t *testing.T) {
	for _, action := range []string{"confirm", "confirm-replay", "offer-replay", "cancel", "cancel-replay", "reschedule"} {
		for _, corruption := range []string{"duplicate", "foreign-option", "document-identity", "invalid-created"} {
			t.Run(action+"/"+corruption, func(t *testing.T) {
				b, ctx := emulatorBackend(t)
				now := time.Now().UTC().Truncate(time.Microsecond)
				value := confirmationRequest("evidence", "alice", "bob", now, now.Add(time.Hour))
				value.DurationMinutes = 30
				value.Status = coord.Suggested
				ref := b.Client.Collection("coordinationRequests").Doc(value.ID)
				start, end := *value.Options[0].StartAt, *value.Options[0].EndAt
				if action == "offer-replay" {
					value.Options = nil
					if _, _, err := coord.PrepareProposal(&value, "bob", "org", start, end, now); err != nil {
						t.Fatal(err)
					}
				}
				optionID := value.Options[0].ID
				if action == "confirm-replay" || action == "cancel" || action == "reschedule" {
					value.Status, value.AcceptedOptionID = coord.Accepted, optionID
				}
				if action == "cancel-replay" {
					value.Status, value.AcceptedOptionID = coord.Cancelled, optionID
				}
				switch corruption {
				case "duplicate":
					value.Options = append(value.Options, value.Options[0])
				case "foreign-option":
					value.Options[0].RequestID = "another-request"
				case "document-identity":
					value.ID = "another-request"
					value.Options[0].RequestID = value.ID
				case "invalid-created":
					value.Options[0].CreatedAt = time.Time{}
				}
				putDocument(t, ctx, ref, value)
				before, err := ref.Get(ctx)
				if err != nil {
					t.Fatal(err)
				}
				for _, user := range []string{"alice", "bob"} {
					putDocument(t, ctx, b.Client.Collection("organizations").Doc("org").Collection("members").Doc(user), map[string]any{"Role": "MANAGER"})
				}
				p := publicationFixtures(now, "available", 1)[0]
				p.UserID, p.EndAt = "bob", now.Add(4*time.Hour)
				putDocument(t, ctx, b.Client.Collection("users").Doc("bob").Collection("scheduleProjections").Doc(p.ID), p)
				want := coord.ErrCandidateInvalid
				switch action {
				case "confirm", "confirm-replay":
					err = b.Request().ConfirmMeeting(ctx, ref.ID, "bob", "org", optionID)
				case "offer-replay":
					want = coord.ErrProposalConflict
					var replay bool
					var option coord.Option
					option, replay, err = b.Request().ProposeMeeting(ctx, ref.ID, "bob", "org", start, end)
					if replay || option.ID != "" {
						t.Error("returned corrupt offer as success")
					}
				case "cancel", "cancel-replay":
					want = coord.ErrCancellationInvalid
					err = b.Request().CancelConfirmedInOrganization(ctx, ref.ID, "alice", "org", optionID)
				case "reschedule":
					want = coord.ErrRescheduleInvalid
					err = b.Request().RescheduleInOrganization(ctx, ref.ID, "alice", "org", coord.RescheduleCommand{Action: "propose", ProposalID: "proposal-evidence", ExpectedOptionID: optionID, StartAt: start.Add(time.Hour)})
				}
				if corruption == "document-identity" {
					want = coord.ErrNotFound
				}
				if !errors.Is(err, want) {
					t.Errorf("got %v want %v", err, want)
				}
				after, err := ref.Get(ctx)
				if err != nil || !reflect.DeepEqual(before.Data(), after.Data()) || !before.UpdateTime.Equal(after.UpdateTime) {
					t.Fatal("request changed", err)
				}
				for _, user := range []string{"alice", "bob"} {
					docs, err := b.Client.Collection("users").Doc(user).Collection("notifications").Documents(ctx).GetAll()
					if err != nil || len(docs) != 0 {
						t.Fatal("notification written", err)
					}
					if _, err := b.Client.Collection("users").Doc(user).Collection("projectionControls").Doc("coordinationConfirmation").Get(ctx); !firestoreNotFound(err) {
						t.Fatal("lock written", err)
					}
				}
				docs, err := b.Client.Collection("organizations").Doc("org").Collection("auditLogs").Documents(ctx).GetAll()
				if err != nil || len(docs) != 0 {
					t.Fatal("audit written", err)
				}
			})
		}
	}
}
