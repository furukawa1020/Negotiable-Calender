package calendar

import (
	"context"
	"testing"
	"time"
)

func TestPolicyOnlyRequiresAbsenceOfAllProviderMetadata(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Microsecond)
	to := now.Add(time.Hour)
	states := map[string]SourceState{
		"never-connected":               {},
		"connection":                    {Connection: &Connection{}},
		"revision":                      {Snapshot: SourceSnapshot{Revision: "r"}},
		"observed":                      {Snapshot: SourceSnapshot{ObservedAt: now}},
		"from":                          {Snapshot: SourceSnapshot{From: now}},
		"to":                            {Snapshot: SourceSnapshot{To: to}},
		"publication":                   {PublishedRevision: "r"},
		"managed-empty":                 {Managed: true},
		"complete-receipt-without-flag": {Snapshot: SourceSnapshot{Revision: "r", ObservedAt: now, From: now, To: to}, PublishedRevision: "r", Connection: &Connection{LastSyncedAt: &now}},
	}
	for name, s := range states {
		t.Run(name, func(t *testing.T) {
			want := name == "never-connected"
			if s.PolicyOnly() != want || s.Readable(now) != want || s.PrivateReadable(now, to, now) != want || s.Rebuildable(context.Background(), "owner", now) != want {
				t.Fatal("contradictory metadata bypassed source gate")
			}
			if s.Committed(now) {
				t.Fatal("unmanaged/incomplete receipt committed")
			}
			if err := ValidateBusyIntervals(s, nil, now, to, now); (err == nil) != want {
				t.Fatal("reservation gate mismatch", err)
			}
			candidate, err := NewCandidateAvailability(s, nil, now)
			if (err == nil) != want || candidate.Allows(now, to, now) != want {
				t.Fatal("candidate gate mismatch", err)
			}
		})
	}
}
