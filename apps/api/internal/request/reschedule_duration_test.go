package request

import (
	"reflect"
	"testing"
	"time"
)

func TestRescheduleRejectsDurationChangedEvidence(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Microsecond)
	for _, terminal := range []bool{false, true} {
		for _, action := range []string{"propose", "accept", "decline", "withdraw"} {
			value := rescheduleFixture(now)
			cmd := RescheduleCommand{Action: "propose", ProposalID: "proposal-duration", ExpectedOptionID: "original", StartAt: now.Add(2 * time.Hour)}
			if err := ApplyReschedule(&value, "alice", cmd, now); err != nil {
				t.Fatal(err)
			}
			if terminal {
				value.RescheduleProposal.Status = "accepted"
				value.AcceptedOptionID = cmd.ProposalID
			}
			end := value.Options[1].EndAt.Add(time.Minute)
			value.Options[1].EndAt = &end
			before := value
			cmd.Action = action
			actor := "bob"
			if action == "propose" || action == "withdraw" {
				actor = "alice"
			}
			if err := ApplyReschedule(&value, actor, cmd, now); err != ErrRescheduleInvalid {
				t.Fatalf("%s accepted changed duration: %v", action, err)
			}
			if !reflect.DeepEqual(value, before) {
				t.Fatal("invalid evidence mutated state")
			}
		}
	}
}
