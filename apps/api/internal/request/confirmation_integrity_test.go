package request

import (
	"testing"
	"time"
)

func TestFinalConflictCheckRejectsCorruptReservations(t *testing.T) {
	start := time.Now().UTC().Add(time.Hour)
	end := start.Add(30 * time.Minute)
	for _, scenario := range []string{"unknown-type", "wrong-request", "duplicate-selection", "empty-record-id", "empty-selection", "missing-selection", "invalid-time"} {
		t.Run(scenario, func(t *testing.T) {
			reserved := Option{ID: "option", RequestID: "other", Type: OptionMeeting, StartAt: &end, EndAt: &end}
			later := end.Add(30 * time.Minute)
			reserved.EndAt = &later // Valid baseline is adjacent, not conflicting.
			other := CoordinationRequest{ID: "other", Status: Accepted, AcceptedOptionID: reserved.ID, Options: []Option{reserved}}
			switch scenario {
			case "unknown-type":
				other.Options[0].Type = "unknown"
			case "wrong-request":
				other.Options[0].RequestID = "foreign"
			case "duplicate-selection":
				other.Options = append(other.Options, reserved)
			case "empty-record-id":
				other.ID = ""
			case "empty-selection":
				other.AcceptedOptionID, other.Options[0].ID = "", ""
			case "missing-selection":
				other.Options = nil
			case "invalid-time":
				other.Options[0].EndAt = &end
			}
			if !ConflictsWithMeeting(Option{Type: OptionMeeting, StartAt: &start, EndAt: &end}, other) {
				t.Fatal("corrupt accepted reservation treated as free time")
			}
		})
	}
}

func TestFinalConflictCheckPreservesValidSemantics(t *testing.T) {
	start := time.Now().UTC().Add(time.Hour)
	end := start.Add(30 * time.Minute)
	for _, kind := range []OptionType{OptionMeeting, OptionAsync, OptionDelegate, OptionDecline} {
		other := CoordinationRequest{ID: "other", Status: Accepted, AcceptedOptionID: "option", Options: []Option{{ID: "option", RequestID: "other", Type: kind, StartAt: &start, EndAt: &end}}}
		if got := ConflictsWithMeeting(Option{StartAt: &start, EndAt: &end}, other); got != (kind == OptionMeeting) {
			t.Fatalf("type %s conflict=%v", kind, got)
		}
		adjacentEnd := end.Add(30 * time.Minute)
		if ConflictsWithMeeting(Option{StartAt: &end, EndAt: &adjacentEnd}, other) {
			t.Fatal("adjacent slot blocked")
		}
		other.Status, other.Options = Cancelled, nil
		if ConflictsWithMeeting(Option{StartAt: &start, EndAt: &end}, other) {
			t.Fatal("closed history blocked")
		}
	}
}
