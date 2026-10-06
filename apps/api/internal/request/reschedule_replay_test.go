package request

import (
	"errors"
	"reflect"
	"testing"
	"time"
)

func TestRescheduleReplaySurvivesResolutionAndCancellation(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Microsecond)
	for _, action := range []string{"accept", "decline", "withdraw"} {
		for _, closed := range []bool{false, true} {
			t.Run(action+"/"+map[bool]string{false: "accepted", true: "cancelled"}[closed], func(t *testing.T) {
				value := rescheduleFixture(now)
				proposal := RescheduleCommand{Action: "propose", ProposalID: "proposal-replay", ExpectedOptionID: "original", StartAt: now.Add(2 * time.Hour)}
				if err := ApplyReschedule(&value, "alice", proposal, now); err != nil {
					t.Fatal(err)
				}
				resolution, actor := proposal, "bob"
				resolution.Action = action
				if action == "withdraw" {
					actor = "alice"
				}
				if err := ApplyReschedule(&value, actor, resolution, now); err != nil {
					t.Fatal(err)
				}
				if closed {
					value.Status = Cancelled
				}
				before := value
				before.Options = append([]Option{}, value.Options...)
				beforeProposal := *value.RescheduleProposal
				before.RescheduleProposal = &beforeProposal
				for _, replay := range []struct {
					actor   string
					command RescheduleCommand
				}{{"alice", proposal}, {actor, resolution}} {
					if err := ApplyReschedule(&value, replay.actor, replay.command, now.Add(48*time.Hour)); !errors.Is(err, ErrRescheduleRepeated) {
						t.Fatalf("saved command cannot be recovered: %v", err)
					}
					if !reflect.DeepEqual(before, value) {
						t.Fatal("replay changed latest state")
					}
				}
			})
		}
	}
}

func TestRescheduleReplayRejectsAlteredOrDamagedCommands(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Microsecond)
	for _, scenario := range []string{"actor", "time", "expected", "superseded", "duplicate", "foreign-option", "missing-option", "unknown-state", "unknown-proposer", "selection-mismatch", "new-action-after-cancel"} {
		t.Run(scenario, func(t *testing.T) {
			value := rescheduleFixture(now)
			command := RescheduleCommand{Action: "propose", ProposalID: "proposal-replay", ExpectedOptionID: "original", StartAt: now.Add(2 * time.Hour)}
			if err := ApplyReschedule(&value, "alice", command, now); err != nil {
				t.Fatal(err)
			}
			value.Status = Cancelled
			actor := "alice"
			switch scenario {
			case "actor":
				actor = "bob"
			case "time":
				command.StartAt = command.StartAt.Add(time.Minute)
			case "expected":
				command.ExpectedOptionID = "other"
			case "superseded":
				command.ProposalID = "older-proposal"
			case "duplicate":
				value.Options = append(value.Options, value.Options[1])
			case "foreign-option":
				value.Options[1].RequestID = "another-request"
			case "missing-option":
				value.Options = value.Options[:1]
			case "unknown-state":
				value.RescheduleProposal.Status = "unknown"
			case "unknown-proposer":
				value.RescheduleProposal.ProposerUserID = "outsider"
			case "selection-mismatch":
				value.AcceptedOptionID = command.ProposalID
			case "new-action-after-cancel":
				command.Action, actor = "accept", "bob"
			}
			if err := ApplyReschedule(&value, actor, command, now); !errors.Is(err, ErrRescheduleInvalid) {
				t.Fatalf("invalid replay accepted: %v", err)
			}
		})
	}
}
