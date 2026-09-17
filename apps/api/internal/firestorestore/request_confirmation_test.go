package firestorestore

import (
	"errors"
	coordinationrequest "github.com/negotiable-calendar/negotiable-calendar/apps/api/internal/request"
	"testing"
	"time"
)

func confirmationRequest(id, requester, target string, now, start time.Time) coordinationrequest.CoordinationRequest {
	value := deletionRequest(now)
	value.ID = id
	value.RequesterUserID = requester
	value.TargetUserID = target
	value.DeadlineAt = start.Add(24 * time.Hour)
	end := start.Add(30 * time.Minute)
	value.Options = []coordinationrequest.Option{{ID: id + "-option", RequestID: id, Type: coordinationrequest.OptionMeeting, StartAt: &start, EndAt: &end, CreatedAt: now}}
	return value
}

func TestConcurrentConfirmationsSerializeBothRoles(t *testing.T) {
	for _, reversed := range []bool{false, true} {
		name := "same-target"
		if reversed {
			name = "opposite-roles"
		}
		t.Run(name, func(t *testing.T) {
			b, ctx := emulatorBackend(t)
			now := time.Now().UTC()
			start := now.Add(time.Hour)
			first := confirmationRequest("first", "alice", "bob", now, start)
			second := confirmationRequest("second", "carol", "bob", now, start)
			if reversed {
				second.RequesterUserID = "bob"
				second.TargetUserID = "carol"
			}
			second.OrganizationID = "another-org"
			for _, value := range []coordinationrequest.CoordinationRequest{first, second} {
				if err := b.Request().Create(ctx, value); err != nil {
					t.Fatal(err)
				}
				p := publicationFixtures(now, "available", 1)[0]
				p.UserID = value.TargetUserID
				p.EndAt = start.Add(4 * time.Hour)
				putDocument(t, ctx, b.Client.Collection("users").Doc(value.TargetUserID).Collection("scheduleProjections").Doc(p.ID), p)
			}
			ready := make(chan struct{})
			results := make(chan error, 2)
			for _, v := range []coordinationrequest.CoordinationRequest{first, second} {
				go func(v coordinationrequest.CoordinationRequest) {
					<-ready
					results <- b.Request().Respond(ctx, v.ID, v.TargetUserID, coordinationrequest.Accepted, v.Options[0].ID)
				}(v)
			}
			close(ready)
			accepted, conflicts := 0, 0
			for i := 0; i < 2; i++ {
				err := <-results
				if err == nil {
					accepted++
				} else if errors.Is(err, coordinationrequest.ErrBookingConflict) {
					conflicts++
				} else {
					t.Fatal(err)
				}
			}
			if accepted != 1 || conflicts != 1 {
				t.Fatalf("accepted=%d conflicts=%d", accepted, conflicts)
			}
			for _, v := range []coordinationrequest.CoordinationRequest{first, second} {
				got, err := b.Request().GetForUser(ctx, v.ID, v.TargetUserID)
				if err != nil {
					t.Fatal(err)
				}
				if got.Status == coordinationrequest.Accepted {
					if err := b.Request().Respond(ctx, v.ID, v.TargetUserID, coordinationrequest.Accepted, v.Options[0].ID); !errors.Is(err, coordinationrequest.ErrAlreadyAccepted) {
						t.Fatalf("non-idempotent retry: %v", err)
					}
				}
			}
			adjacent := confirmationRequest("adjacent", "alice", "bob", now, start.Add(30*time.Minute))
			if err := b.Request().Create(ctx, adjacent); err != nil {
				t.Fatal(err)
			}
			if err := b.Request().Respond(ctx, adjacent.ID, "bob", coordinationrequest.Accepted, adjacent.Options[0].ID); err != nil {
				t.Fatalf("adjacent rejected: %v", err)
			}
		})
	}
}

func TestConfirmationFailsClosedOnStaleInputs(t *testing.T) {
	for _, scenario := range []string{"missing", "expired", "unknown", "dirty", "policy", "private", "disconnected", "started", "deadline", "async"} {
		t.Run(scenario, func(t *testing.T) {
			b, ctx := emulatorBackend(t)
			now := time.Now().UTC()
			start := now.Add(time.Hour)
			value := confirmationRequest("request", "alice", "bob", now, start)
			p := publicationFixtures(now, "available", 1)[0]
			p.UserID = "bob"
			p.EndAt = start.Add(time.Hour)
			want := coordinationrequest.ErrAvailabilityChanged
			switch scenario {
			case "expired":
				p.ExpiresAt = now.Add(-time.Second)
			case "unknown":
				p.State.Availability = "unknown"
			case "dirty":
				putDocument(t, ctx, b.projectionPublicationRef("bob"), projectionPublication{ID: "partial", Ready: false, Dirty: true})
			case "policy":
				putDocument(t, ctx, b.policyRevisionRef("bob"), map[string]any{"ID": "new-policy"})
			case "private":
				putDocument(t, ctx, b.privateInputsRef("bob"), privateInputsControl{ID: "partial", Ready: false})
			case "disconnected":
				putDocument(t, ctx, b.projectionBlock("bob"), map[string]any{"InProgress": true})
			case "started":
				past := now.Add(-time.Minute)
				value.Options[0].StartAt = &past
				want = coordinationrequest.ErrCandidateExpired
			case "deadline":
				value.DeadlineAt = start
				want = coordinationrequest.ErrCandidateExpired
			case "async":
				value.Options[0].Type = coordinationrequest.OptionAsync
				want = coordinationrequest.ErrCandidateInvalid
			}
			if scenario != "missing" {
				putDocument(t, ctx, b.Client.Collection("users").Doc("bob").Collection("scheduleProjections").Doc(p.ID), p)
			}
			if err := b.Request().Create(ctx, value); err != nil {
				t.Fatal(err)
			}
			if err := b.Request().Respond(ctx, value.ID, "bob", coordinationrequest.Accepted, value.Options[0].ID); !errors.Is(err, want) {
				t.Fatalf("got %v want %v", err, want)
			}
			got, err := b.Request().GetForUser(ctx, value.ID, "bob")
			if err != nil || got.Status != coordinationrequest.Suggested || got.AcceptedOptionID != "" {
				t.Fatalf("failed confirmation changed request: %+v %v", got, err)
			}
		})
	}
}
