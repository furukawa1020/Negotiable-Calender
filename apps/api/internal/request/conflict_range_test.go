package request

import (
	"testing"
	"time"
)

func TestConflictCheckRejectsInvalidCandidateRange(t *testing.T) {
	now := time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC)
	start, end := now.Add(time.Hour), now.Add(2*time.Hour)
	other := CoordinationRequest{ID: "r", RequesterUserID: "alice", TargetUserID: "bob", Status: Accepted, AcceptedOptionID: "o", Options: []Option{{ID: "o", RequestID: "r", Type: OptionMeeting, StartAt: &start, EndAt: &end, CreatedAt: now}}}
	zero := time.Time{}
	later := end.Add(time.Hour)
	local := end.In(time.FixedZone("JST", 9*3600))
	for _, option := range []Option{{}, {StartAt: &zero, EndAt: &now}, {StartAt: &local, EndAt: &later}, {StartAt: &end, EndAt: &end}, {StartAt: &later, EndAt: &end}} {
		if !ConflictsWithMeeting(option, other) {
			t.Fatal("malformed candidate treated as free")
		}
	}
	if ConflictsWithMeeting(Option{StartAt: &end, EndAt: &later}, other) {
		t.Fatal("adjacent valid interval rejected")
	}
	other.Status = Cancelled
	if ConflictsWithMeeting(Option{}, other) {
		t.Fatal("cancelled reservation blocks availability")
	}
}
