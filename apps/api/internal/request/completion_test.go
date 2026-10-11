package request

import (
	"errors"
	"reflect"
	"testing"
	"time"
)

func TestCompletionLifecycle(t *testing.T) {
	now := time.Now().UTC()
	for _, actor := range []string{"alice", "bob"} {
		v := rescheduleFixture(now.Add(-3 * time.Hour))
		v.OrganizationID = "org"
		cmd := ResolutionCommand{Status: Completed, ExpectedOptionID: v.AcceptedOptionID}
		if err := AuthorizeResolution(v, actor, "org", cmd); err != nil {
			t.Fatal(err)
		}
		selected := v.AcceptedOptionID
		if replay, err := PrepareResolution(&v, cmd, now); err != nil || replay || v.Status != Completed || v.AcceptedOptionID != selected {
			t.Fatal("completion failed", err)
		}
		before := v
		if replay, err := PrepareResolution(&v, cmd, now.Add(time.Hour)); err != nil || !replay || !reflect.DeepEqual(v, before) {
			t.Fatal("unstable retry", err)
		}
		if _, err := CalendarExport(v); err != nil {
			t.Fatal("completed history cannot export", err)
		}
		note, event := ResolutionEffects(v, actor)
		if string(note.Type) != "request_completed" || string(event.Action) != "request_completed" || note.UserID == actor {
			t.Fatal("incorrect completion effects")
		}
		if ranges, err := ConfirmedRanges([]CoordinationRequest{v}); err != nil || len(ranges) != 0 {
			t.Fatal("completed meeting reserves time", err)
		}
	}
}

func TestCompletionRejectsUnsafeCommands(t *testing.T) {
	now := time.Now().UTC()
	for _, scenario := range []string{"early", "wrong-option", "duplicate", "foreign-option", "invalid-time", "pending-proposal", "cancelled", "suggested", "message", "missing-option", "invalid-clock"} {
		t.Run(scenario, func(t *testing.T) {
			v := rescheduleFixture(now.Add(-3 * time.Hour))
			v.OrganizationID = "org"
			cmd := ResolutionCommand{Status: Completed, ExpectedOptionID: v.AcceptedOptionID}
			at := now
			switch scenario {
			case "early":
				at = *v.Options[0].StartAt
			case "wrong-option":
				cmd.ExpectedOptionID = "other"
			case "duplicate":
				v.Options = append(v.Options, v.Options[0])
			case "foreign-option":
				v.Options[0].RequestID = "other"
			case "invalid-time":
				v.Options[0].EndAt = v.Options[0].StartAt
			case "pending-proposal":
				v.RescheduleProposal = &RescheduleProposal{Status: "proposed"}
			case "cancelled":
				v.Status = Cancelled
			case "suggested":
				v.Status = Suggested
			case "message":
				cmd.Message = "unexpected"
			case "missing-option":
				cmd.ExpectedOptionID = ""
			case "invalid-clock":
				at = time.Time{}
			}
			before := v
			if _, err := PrepareResolution(&v, cmd, at); !errors.Is(err, ErrResolutionConflict) || !reflect.DeepEqual(before, v) {
				t.Fatal("unsafe completion", err)
			}
		})
	}
}

func TestCompletionAtEndBoundaryAndCorruptReplay(t *testing.T) {
	now := time.Now().UTC()
	v := rescheduleFixture(now.Add(-3 * time.Hour))
	cmd := ResolutionCommand{Status: Completed, ExpectedOptionID: v.AcceptedOptionID}
	if _, err := PrepareResolution(&v, cmd, *v.Options[0].EndAt); err != nil {
		t.Fatal("end boundary rejected", err)
	}
	v.Options = append(v.Options, v.Options[0])
	if _, err := PrepareResolution(&v, cmd, now); !errors.Is(err, ErrResolutionConflict) {
		t.Fatal("corrupt completion replay accepted", err)
	}
}
