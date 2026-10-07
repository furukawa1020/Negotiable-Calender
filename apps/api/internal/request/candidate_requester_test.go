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

func TestCandidatesRespectRequesterCoverageAndUnavailableSnapshots(t *testing.T) {
	now := time.Date(2026, 10, 7, 0, 0, 0, 0, time.UTC)
	for _, scenario := range []string{"partial", "all-busy", "unavailable", "stale", "async"} {
		t.Run(scenario, func(t *testing.T) {
			from, to := now.Add(time.Hour), now.Add(2*time.Hour)
			source := cal.SourceState{Managed: true, Snapshot: cal.SourceSnapshot{Revision: "r", ObservedAt: now, From: from, To: to}, Connection: &cal.Connection{LastSyncedAt: &now}}
			intervals := []cal.BusyInterval{}
			if scenario == "all-busy" {
				intervals = append(intervals, cal.BusyInterval{StartAt: from, EndAt: to, Status: "busy"})
			}
			snapshot, err := cal.NewCandidateAvailability(source, intervals, now)
			if err != nil {
				t.Fatal(err)
			}
			value := validCandidateRequest(now)
			at := now
			if scenario == "unavailable" || scenario == "async" {
				snapshot = cal.CandidateAvailability{}
			}
			if scenario == "stale" {
				at = now.Add(cal.SourceMaxAge)
			}
			if scenario == "async" {
				value.SyncPreference = AsyncPreferred
			}
			options, err := GenerateCandidates(CandidateInput{Request: value, Projections: []projection.ScheduleProjection{validCandidateProjection(now, now.Add(3*time.Hour))}, RequesterCalendar: &snapshot, Now: at})
			if scenario == "unavailable" || scenario == "stale" {
				if err == nil || len(options) != 0 {
					t.Fatal("unsafe async fallback")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if scenario == "partial" {
				if len(options) != 3 {
					t.Fatal("coverage lost all candidates")
				}
				for _, option := range options {
					if option.Type != OptionMeeting || option.StartAt.Before(from) || option.EndAt.After(to) {
						t.Fatal("outside source coverage")
					}
				}
			} else if len(options) != 1 || options[0].Type != OptionAsync {
				t.Fatal("expected honest non-meeting alternative")
			}
		})
	}
}
