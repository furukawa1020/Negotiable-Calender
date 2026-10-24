package request

import (
	"encoding/json"
	"errors"
	"testing"
	"time"
)

func TestNegotiationMutationClock(t *testing.T) {
	now := time.Date(2026, 10, 25, 0, 0, 0, 0, time.UTC)
	type operation struct {
		prepare func() CoordinationRequest
		apply   func(*CoordinationRequest, time.Time) error
	}
	suggested := func() CoordinationRequest {
		value := rescheduleFixture(now)
		value.OrganizationID, value.CreatedAt = "org", now.Add(-time.Hour)
		value.Status, value.AcceptedOptionID, value.Options = Suggested, "", nil
		return value
	}
	command := RescheduleCommand{Action: "propose", ProposalID: "proposal-clock", ExpectedOptionID: "original", StartAt: now.Add(2 * time.Hour)}
	operations := map[string]operation{
		"resolve": {suggested, func(v *CoordinationRequest, at time.Time) error {
			_, err := PrepareResolution(v, ResolutionCommand{Status: Declined}, at)
			return err
		}},
		"counterproposal": {suggested, func(v *CoordinationRequest, at time.Time) error {
			_, _, err := PrepareProposal(v, "bob", "org", now.Add(2*time.Hour), now.Add(150*time.Minute), at)
			return err
		}},
		"reschedule-propose": {func() CoordinationRequest { return rescheduleFixture(now) }, func(v *CoordinationRequest, at time.Time) error { return ApplyReschedule(v, "alice", command, at) }},
	}
	for _, action := range []string{"accept", "decline", "withdraw"} {
		operations["reschedule-"+action] = operation{func() CoordinationRequest {
			v := rescheduleFixture(now)
			if err := ApplyReschedule(&v, "alice", command, now); err != nil {
				t.Fatal(err)
			}
			return v
		}, func(v *CoordinationRequest, at time.Time) error {
			c := command
			c.Action = action
			actor := "bob"
			if action == "withdraw" {
				actor = "alice"
			}
			return ApplyReschedule(v, actor, c, at)
		}}
	}
	for name, op := range operations {
		t.Run(name, func(t *testing.T) {
			for _, invalid := range []string{"zero", "updated", "created"} {
				v := op.prepare()
				at := now
				switch invalid {
				case "zero":
					at = time.Time{}
				case "updated":
					v.UpdatedAt = now.Add(time.Second)
				case "created":
					v.CreatedAt = now.Add(time.Second)
				}
				before, _ := json.Marshal(v)
				if err := op.apply(&v, at); err == nil {
					t.Fatalf("accepted %s clock", invalid)
				}
				after, _ := json.Marshal(v)
				if string(before) != string(after) {
					t.Fatal("failed command mutated record")
				}
			}
			v := op.prepare()
			if err := op.apply(&v, now.In(time.FixedZone("local", 9*3600))); err != nil {
				t.Fatal(err)
			}
			if v.UpdatedAt.Location() != time.UTC {
				t.Fatal("non-UTC update")
			}
			for _, option := range v.Options {
				if option.CreatedAt.Location() != time.UTC {
					t.Fatal("non-UTC option")
				}
			}
			before, _ := json.Marshal(v)
			err := op.apply(&v, now.Add(-time.Hour))
			if err != nil && !errors.Is(err, ErrRescheduleRepeated) {
				t.Fatalf("recovery blocked by clock: %v", err)
			}
			after, _ := json.Marshal(v)
			if string(before) != string(after) {
				t.Fatal("replay mutated record")
			}
		})
	}
}
