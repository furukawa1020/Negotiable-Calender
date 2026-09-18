package projection

import (
	calendarintegration "github.com/negotiable-calendar/negotiable-calendar/apps/api/internal/calendar"
	"testing"
	"time"
)

func TestPolicyRebuildCannotExtendSourceFreshnessOrCoverage(t *testing.T) {
	now := time.Date(2026, 9, 18, 0, 0, 0, 0, time.UTC)
	s := calendarintegration.SourceState{Managed: true, Snapshot: calendarintegration.SourceSnapshot{Revision: "one", ObservedAt: now.Add(-20 * time.Minute), From: now, To: now.Add(time.Hour)}}
	values := []ScheduleProjection{{ID: "covered", StartAt: now, EndAt: now.Add(time.Hour), GeneratedAt: now, ExpiresAt: now.Add(time.Hour)}, {ID: "outside", StartAt: now.Add(time.Hour), EndAt: now.Add(2 * time.Hour), ExpiresAt: now.Add(time.Hour)}}
	got := BoundToSource(values, s)
	if len(got) != 1 || got[0].ID != "covered" || !got[0].ExpiresAt.Equal(now.Add(10*time.Minute)) {
		t.Fatal("source bounds lost", got)
	}
	if !values[0].ExpiresAt.Equal(now.Add(time.Hour)) {
		t.Fatal("mutated input")
	}
	if len(BoundToSource(values, calendarintegration.SourceState{})) != 2 {
		t.Fatal("policy-only mode changed")
	}
}
