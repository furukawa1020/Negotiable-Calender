package request

import (
	"errors"
	"testing"
	"time"
)

func rescheduleFixture(now time.Time) CoordinationRequest {
	start, end := now.Add(time.Hour), now.Add(90*time.Minute)
	return CoordinationRequest{ID: "r", RequesterUserID: "alice", TargetUserID: "bob", Status: Accepted, AcceptedOptionID: "original", DurationMinutes: 30, DeadlineAt: now.Add(24 * time.Hour), UpdatedAt: now, Options: []Option{{ID: "original", RequestID: "r", Type: OptionMeeting, StartAt: &start, EndAt: &end, CreatedAt: now}}}
}

func TestRescheduleStateMachine(t *testing.T) {
	now := time.Now().UTC()
	for _, actor := range []string{"alice", "bob"} {
		t.Run(actor, func(t *testing.T) {
			value := rescheduleFixture(now)
			// A manually suggested confirmed option can differ from the requested duration.
			value.DurationMinutes = 90
			command := RescheduleCommand{Action: "propose", ProposalID: "proposal-new", ExpectedOptionID: "original", StartAt: now.Add(2 * time.Hour)}
			if err := ApplyReschedule(&value, actor, command, now); err != nil {
				t.Fatal(err)
			}
			if value.AcceptedOptionID != "original" || value.Status != Accepted {
				t.Fatal("old reservation lost")
			}
			proposed := value.Options[len(value.Options)-1]
			if proposed.EndAt.Sub(*proposed.StartAt) != 30*time.Minute {
				t.Fatal("confirmed duration changed")
			}
			if err := ApplyReschedule(&value, actor, command, now); !errors.Is(err, ErrRescheduleRepeated) {
				t.Fatal(err)
			}
			command.Action = "accept"
			if err := ApplyReschedule(&value, actor, command, now); !errors.Is(err, ErrRescheduleInvalid) {
				t.Fatal("self approval", err)
			}
			other := "alice"
			if actor == other {
				other = "bob"
			}
			if err := ApplyReschedule(&value, other, command, now); err != nil {
				t.Fatal(err)
			}
			if value.AcceptedOptionID != command.ProposalID || value.RescheduleProposal.Status != "accepted" {
				t.Fatal("not swapped")
			}
			if err := ApplyReschedule(&value, other, command, now.Add(4*time.Hour)); !errors.Is(err, ErrRescheduleRepeated) {
				t.Fatal("replay", err)
			}
		})
	}
}

func TestRescheduleRejectsInvalidTransitions(t *testing.T) {
	now := time.Now().UTC()
	for _, scenario := range []string{"outsider", "stale", "cancelled", "past-original", "past-new", "deadline", "same-time", "active-proposal", "reused-id", "changed-replay", "self-decline", "other-withdraw", "started-accept"} {
		t.Run(scenario, func(t *testing.T) {
			value := rescheduleFixture(now)
			actor := "alice"
			command := RescheduleCommand{Action: "propose", ProposalID: "proposal-new", ExpectedOptionID: "original", StartAt: now.Add(2 * time.Hour)}
			at := now
			switch scenario {
			case "outsider":
				actor = "mallory"
			case "stale":
				command.ExpectedOptionID = "stale"
			case "cancelled":
				value.Status = Cancelled
			case "past-original":
				at = now.Add(time.Hour)
			case "past-new":
				command.StartAt = now
			case "deadline":
				command.StartAt = value.DeadlineAt
			case "same-time":
				command.StartAt = *value.Options[0].StartAt
			default:
				if err := ApplyReschedule(&value, actor, command, now); err != nil {
					t.Fatal(err)
				}
				switch scenario {
				case "active-proposal":
					command.ProposalID = "another-proposal"
				case "reused-id":
					value.RescheduleProposal.Status = "withdrawn"
				case "changed-replay":
					command.StartAt = now.Add(3 * time.Hour)
				case "self-decline":
					command.Action = "decline"
				case "other-withdraw":
					command.Action = "withdraw"
					actor = "bob"
				case "started-accept":
					command.Action = "accept"
					actor = "bob"
					at = now.Add(time.Hour)
				}
			}
			if err := ApplyReschedule(&value, actor, command, at); err == nil {
				t.Fatal("invalid transition accepted")
			}
			if value.AcceptedOptionID != "original" {
				t.Fatal("original lost")
			}
		})
	}
	for _, action := range []string{"decline", "withdraw"} {
		value := rescheduleFixture(now)
		command := RescheduleCommand{Action: "propose", ProposalID: "proposal-new", ExpectedOptionID: "original", StartAt: now.Add(2 * time.Hour)}
		if err := ApplyReschedule(&value, "alice", command, now); err != nil {
			t.Fatal(err)
		}
		command.Action = action
		actor := "bob"
		if action == "withdraw" {
			actor = "alice"
		}
		if err := ApplyReschedule(&value, actor, command, now); err != nil {
			t.Fatal(err)
		}
		if err := ApplyReschedule(&value, actor, command, now); !errors.Is(err, ErrRescheduleRepeated) {
			t.Fatal(err)
		}
		if value.AcceptedOptionID != "original" {
			t.Fatal("rejection lost original")
		}
	}
}
