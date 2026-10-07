package calendar

import (
	"encoding/json"
	"testing"
	"time"
)

func TestCandidateAvailabilityCoveragePrivacyAndFreshness(t *testing.T) {
	now := time.Date(2026, 10, 7, 0, 0, 0, 0, time.UTC)
	from, to := now.Add(time.Hour), now.Add(4*time.Hour)
	source := SourceState{Managed: true, Snapshot: SourceSnapshot{Revision: "r", ObservedAt: now, From: from, To: to}, Connection: &Connection{LastSyncedAt: &now}}
	values := []BusyInterval{
		{StartAt: from.Add(time.Hour), EndAt: from.Add(90 * time.Minute), Status: "tentative"},
		{StartAt: from.Add(30 * time.Minute), EndAt: from.Add(75 * time.Minute), Status: "busy"},
		{StartAt: from.Add(45 * time.Minute), EndAt: from.Add(60 * time.Minute), Status: "unknown"},
		{StartAt: from, EndAt: to, Status: "free"},
	}
	s, err := NewCandidateAvailability(source, values, now)
	if err != nil {
		t.Fatal(err)
	}
	values[0].EndAt = to // Caller mutation must not modify the snapshot.
	for _, tc := range []struct {
		start, end time.Time
		allowed    bool
	}{
		{from, from.Add(30 * time.Minute), true},
		{from.Add(30 * time.Minute), from.Add(45 * time.Minute), false},
		{from.Add(90 * time.Minute), to, true},
		{from.Add(-time.Minute), from, false},
		{to, to.Add(time.Minute), false},
		{from, from, false},
	} {
		if s.Allows(tc.start, tc.end, now) != tc.allowed {
			t.Fatal("wrong interval classification", tc)
		}
	}
	if s.Validate(now.Add(SourceMaxAge-time.Nanosecond)) != nil || s.Validate(now.Add(SourceMaxAge)) == nil || s.Validate(now.Add(-time.Nanosecond)) == nil {
		t.Fatal("freshness boundary lost")
	}
	if _, err := json.Marshal(s); err == nil {
		t.Fatal("snapshot leaked through JSON")
	}
	if (CandidateAvailability{}).Validate(now) == nil {
		t.Fatal("zero snapshot treated as free")
	}
	for _, scenario := range []string{"overflow", "invalid", "zero", "unknown", "stale", "failed", "uncommitted", "lease", "reconnect", "disconnected"} {
		t.Run(scenario, func(t *testing.T) {
			c := *source.Connection
			v := source
			v.Connection = &c
			intervals := []BusyInterval{{StartAt: from, EndAt: to, Status: "free"}}
			switch scenario {
			case "overflow":
				intervals = make([]BusyInterval, BusyEvidenceLimit+1)
			case "invalid":
				intervals[0].Status = "bad"
			case "zero":
				intervals[0].StartAt = time.Time{}
			case "unknown":
				v.Snapshot = SourceSnapshot{}
			case "stale":
				v.Snapshot.ObservedAt = now.Add(-SourceMaxAge)
				c.LastSyncedAt = &v.Snapshot.ObservedAt
			case "failed":
				c.LastErrorCode = "timeout"
			case "uncommitted":
				c.LastSyncedAt = nil
			case "lease":
				c.SyncLeaseID = "lease"
			case "reconnect":
				c.ReconnectRequired = true
			case "disconnected":
				v.Connection = nil
			}
			if got, err := NewCandidateAvailability(v, intervals, now); err == nil || got.Validate(now) == nil {
				t.Fatal("unsafe snapshot accepted")
			}
		})
	}
}
