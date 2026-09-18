package calendar

import (
	"context"
	"testing"
	"time"
)

func TestSourceFreshnessAndCoverageBoundaries(t *testing.T) {
	now := time.Date(2026, 9, 18, 0, 0, 0, 0, time.UTC)
	s := SourceSnapshot{Revision: "one", ObservedAt: now, From: now.Add(-time.Hour), To: now.Add(time.Hour)}
	for _, tc := range []struct {
		at   time.Time
		want bool
	}{{now, true}, {now.Add(SourceMaxAge - time.Nanosecond), true}, {now.Add(SourceMaxAge), false}, {now.Add(-time.Nanosecond), false}} {
		if s.Fresh(tc.at) != tc.want {
			t.Fatalf("fresh(%v) != %v", tc.at, tc.want)
		}
	}
	if !s.Covers(s.From, s.To) || s.Covers(s.From.Add(-time.Nanosecond), s.To) || s.Covers(s.From, s.To.Add(time.Nanosecond)) || s.Covers(now, now) {
		t.Fatal("coverage boundary")
	}
	next, err := NextSourceSnapshot(s, false, "two", s.From.Add(-time.Hour), s.To.Add(time.Hour), now.Add(time.Minute))
	if err != nil || !next.From.Equal(s.From) || !next.To.Equal(s.To) || next.Revision == s.Revision || !next.ObservedAt.Equal(now.Add(time.Minute)) {
		t.Fatal("delta expanded coverage", err)
	}
	if _, err := NextSourceSnapshot(SourceSnapshot{}, false, "two", s.From, s.To, now); err == nil {
		t.Fatal("delta accepted without full base")
	}
}

func TestSourceReadAndRebuildStateMachine(t *testing.T) {
	now := time.Date(2026, 9, 18, 0, 0, 0, 0, time.UTC)
	for _, scenario := range []string{"fresh", "stale", "future", "uncommitted", "wrong-publication", "disconnected", "reconnected", "failed", "revoked", "syncing", "expired-lease", "unknown"} {
		t.Run(scenario, func(t *testing.T) {
			at, until := now, now.Add(time.Minute)
			c := &Connection{UserID: "alice", LastSyncedAt: &at}
			s := SourceState{Managed: true, Snapshot: SourceSnapshot{Revision: "one", ObservedAt: now, From: now.Add(-time.Hour), To: now.Add(time.Hour)}, PublishedRevision: "one", Connection: c}
			switch scenario {
			case "stale":
				s.Snapshot.ObservedAt = now.Add(-SourceMaxAge)
			case "future":
				s.Snapshot.ObservedAt = now.Add(time.Second)
			case "uncommitted":
				old := now.Add(-time.Minute)
				c.LastSyncedAt = &old
			case "wrong-publication":
				s.PublishedRevision = "old"
			case "disconnected":
				s.Connection = nil
			case "reconnected":
				c.LastSyncedAt = nil
			case "failed":
				c.LastErrorCode = "timeout"
			case "revoked":
				c.ReconnectRequired = true
			case "syncing":
				c.SyncLeaseID = "lease"
				c.SyncLeaseUntil = &until
			case "expired-lease":
				c.SyncLeaseID = "lease"
				c.SyncLeaseUntil = &now
			case "unknown":
				s.Snapshot = SourceSnapshot{}
			}
			if s.Readable(now) != (scenario == "fresh") {
				t.Fatal("read gate")
			}
			ctx := WithSyncLease(context.Background(), SyncLease{UserID: "alice", ID: "lease"})
			if s.Rebuildable(ctx, "alice", now) != (scenario == "fresh" || scenario == "wrong-publication" || scenario == "syncing") {
				t.Fatal("rebuild gate")
			}
			if scenario == "syncing" && s.Rebuildable(context.Background(), "alice", now) {
				t.Fatal("policy writer bypassed active sync")
			}
		})
	}
	if !(SourceState{}).Readable(now) {
		t.Fatal("policy-only sources should remain distinct from Google")
	}
}
