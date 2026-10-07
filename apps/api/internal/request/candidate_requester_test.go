package request

import (
	"testing"
	"time"

	cal "github.com/negotiable-calendar/negotiable-calendar/apps/api/internal/calendar"
	"github.com/negotiable-calendar/negotiable-calendar/apps/api/internal/projection"
)

func TestCandidatesExcludeRequesterBusyBeforeRanking(t *testing.T) {
	now := time.Date(2026, 10, 7, 0, 0, 0, 0, time.UTC)
	end := now.Add(3 * time.Hour)
	source := cal.SourceState{Managed: true, Snapshot: cal.SourceSnapshot{Revision: "source", ObservedAt: now, From: now, To: end}, Connection: &cal.Connection{LastSyncedAt: &now}}
	snapshot, err := cal.NewCandidateAvailability(source, []cal.BusyInterval{{StartAt: now, EndAt: now.Add(time.Hour), Status: "busy"}}, now)
	if err != nil {
		t.Fatal(err)
	}
	options, err := GenerateCandidates(CandidateInput{Request: validCandidateRequest(now), Projections: []projection.ScheduleProjection{validCandidateProjection(now, end)}, RequesterCalendar: &snapshot, Now: now})
	if err != nil || len(options) != 3 {
		t.Fatal("generation failed", err)
	}
	for _, option := range options {
		if option.Type != OptionMeeting || option.StartAt.Before(now.Add(time.Hour)) {
			t.Fatal("requester busy time ranked as a meeting")
		}
	}
}
