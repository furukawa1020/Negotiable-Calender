package calendar

import (
	"encoding/json"
	"testing"
	"time"
)

func TestBusyAvailabilityBoundariesAndSourceEvidence(t *testing.T) {
	now := time.Date(2026, 10, 6, 0, 0, 0, 0, time.UTC)
	from, to := now.Add(time.Hour), now.Add(2*time.Hour)
	for _, scenario := range []string{"busy", "tentative", "unknown-status", "free", "empty", "before", "after", "contains", "contained", "invalid-status", "invalid-time", "zero", "stale", "future", "unknown-source", "outside", "uncommitted", "lease", "reconnect", "failed", "disconnected", "limit", "overflow", "unmanaged"} {
		t.Run(scenario, func(t *testing.T) {
			observed := now.Add(-time.Minute)
			c := Connection{LastSyncedAt: &observed}
			s := SourceState{Managed: true, Connection: &c, Snapshot: SourceSnapshot{Revision: "source", ObservedAt: observed, From: now, To: to}}
			intervals := []BusyInterval{{StartAt: from, EndAt: to, Status: "free"}}
			allowed := false
			switch scenario {
			case "busy":
				intervals[0].Status = "busy"
			case "tentative":
				intervals[0].Status = "tentative"
			case "unknown-status":
				intervals[0].Status = "unknown"
			case "free":
				intervals[0].Status, allowed = "free", true
			case "empty":
				intervals, allowed = nil, true
			case "before":
				intervals[0].StartAt, intervals[0].EndAt, allowed = now, from, true
			case "after":
				intervals[0].StartAt, intervals[0].EndAt, allowed = to, to.Add(time.Hour), true
			case "contains":
				intervals[0].StartAt, intervals[0].EndAt, intervals[0].Status = now, to.Add(time.Hour), "busy"
			case "contained":
				intervals[0].StartAt, intervals[0].EndAt, intervals[0].Status = from.Add(time.Minute), to.Add(-time.Minute), "busy"
			case "invalid-status":
				intervals[0].Status = "bad"
			case "invalid-time":
				intervals[0].EndAt = from
			case "zero":
				intervals[0].StartAt = time.Time{}
			case "stale":
				s.Snapshot.ObservedAt = now.Add(-SourceMaxAge)
				c.LastSyncedAt = &s.Snapshot.ObservedAt
			case "future":
				s.Snapshot.ObservedAt = now.Add(time.Second)
				c.LastSyncedAt = &s.Snapshot.ObservedAt
			case "unknown-source":
				s.Snapshot = SourceSnapshot{}
			case "outside":
				s.Snapshot.To = to.Add(-time.Nanosecond)
			case "uncommitted":
				s.Snapshot.ObservedAt = now
			case "lease":
				c.SyncLeaseID = "syncing"
			case "reconnect":
				c.ReconnectRequired = true
			case "failed":
				c.LastErrorCode = "timeout"
			case "disconnected":
				s.Connection = nil
			case "limit", "overflow":
				count := BusyEvidenceLimit
				if scenario == "overflow" {
					count++
				}
				intervals = make([]BusyInterval, count)
				for i := range intervals {
					intervals[i] = BusyInterval{StartAt: from, EndAt: to, Status: "free"}
				}
				allowed = scenario == "limit"
			case "unmanaged":
				s, intervals, allowed = SourceState{}, nil, true
			}
			if got := ValidateBusyIntervals(s, intervals, from, to, now); (got == nil) != allowed {
				t.Fatalf("allowed=%v err=%v", allowed, got)
			}
		})
	}
}

func TestBusyIntervalsNeverSerialize(t *testing.T) {
	if _, err := json.Marshal([]BusyInterval{{Status: "busy"}}); err == nil {
		t.Fatal("private busy evidence serialized")
	}
}
