package request

import (
	"testing"
	"time"
)

func TestConfirmedRangesUseOnlySelectedAcceptedMeetings(t *testing.T) {
	now := time.Now().UTC()
	start := now.Add(time.Hour)
	end := start.Add(time.Hour)
	value := CoordinationRequest{Status: Accepted, AcceptedOptionID: "chosen", Options: []Option{{ID: "ignored", Type: OptionAsync}, {ID: "chosen", Type: OptionMeeting, StartAt: &start, EndAt: &end}}}
	ranges, err := ConfirmedRanges([]CoordinationRequest{value, {Status: Suggested}})
	if err != nil || len(ranges) != 1 || !ranges[0].StartAt.Equal(start) {
		t.Fatalf("ranges: %v %v", ranges, err)
	}
	value.AcceptedOptionID = "missing"
	if _, err := ConfirmedRanges([]CoordinationRequest{value}); err == nil {
		t.Fatal("corrupt reservation silently ignored")
	}
}
