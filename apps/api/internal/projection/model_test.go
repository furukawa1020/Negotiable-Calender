package projection

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/negotiable-calendar/negotiable-calendar/apps/api/internal/policy"
)

func TestViewContainsOnlyProjectionFields(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 8, 23, 5, 0, 0, 0, time.UTC)
	item := ScheduleProjection{
		ID: "p1", UserID: "u1", StartAt: now, EndAt: now.Add(time.Hour),
		State:                  policy.InteractionState{Availability: policy.Limited, Interruptibility: policy.UrgentOnly, Requestability: policy.RequestLater, Reschedulability: policy.RescheduleLow},
		ExpectedResponseBucket: "after_15_30", GeneratedAt: now, ExpiresAt: now.Add(time.Hour),
	}
	view, err := NewView("u1", "Asia/Tokyo", []ScheduleProjection{item})
	if err != nil {
		t.Fatalf("view generation failed: %v", err)
	}
	encoded, err := json.Marshal(view)
	if err != nil {
		t.Fatalf("view serialization failed: %v", err)
	}
	for _, forbidden := range []string{"title", "description", "location", "attendees", "organizer", "providerEventId", "calendarName"} {
		if strings.Contains(string(encoded), forbidden) {
			t.Fatalf("private key %q leaked in projection DTO", forbidden)
		}
	}
}

func TestNewViewMergesOnlyAdjacentEqualPublicSegments(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 8, 23, 5, 0, 0, 0, time.UTC)
	open := policy.InteractionState{Availability: policy.Available, Interruptibility: policy.InterruptOpen, Requestability: policy.RequestOpen, Reschedulability: policy.RescheduleHigh}
	closed := policy.InteractionState{Availability: policy.Unavailable, Interruptibility: policy.DoNotInterrupt, Requestability: policy.RequestClosed, Reschedulability: policy.RescheduleFixed}
	projections := []ScheduleProjection{
		validProjection("p1", now, now.Add(15*time.Minute), open, "within_window"),
		validProjection("p2", now.Add(15*time.Minute), now.Add(30*time.Minute), open, "within_window"),
		validProjection("p3", now.Add(30*time.Minute), now.Add(45*time.Minute), closed, "unknown"),
		validProjection("p4", now.Add(time.Hour), now.Add(75*time.Minute), closed, "unknown"),
	}

	view, err := NewView("u1", "Asia/Tokyo", projections)
	if err != nil {
		t.Fatalf("view generation failed: %v", err)
	}
	if len(view.Segments) != 3 {
		t.Fatalf("expected 3 public segments, got %d", len(view.Segments))
	}
	if !view.Segments[0].StartAt.Equal(now) {
		t.Fatalf("merged segment start changed: %s", view.Segments[0].StartAt)
	}
	if !view.Segments[0].EndAt.Equal(now.Add(30 * time.Minute)) {
		t.Fatalf("adjacent equal buckets were not merged: %s", view.Segments[0].EndAt)
	}
	if !view.Segments[2].StartAt.Equal(now.Add(time.Hour)) {
		t.Fatalf("segments separated by a gap were merged")
	}
}

func TestNewViewKeepsDifferentResponseBucketsSeparate(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 8, 23, 5, 0, 0, 0, time.UTC)
	state := policy.InteractionState{Availability: policy.Limited, Interruptibility: policy.UrgentOnly, Requestability: policy.RequestLater, Reschedulability: policy.RescheduleLow}
	projections := []ScheduleProjection{
		validProjection("p1", now, now.Add(15*time.Minute), state, "after_15_30"),
		validProjection("p2", now.Add(15*time.Minute), now.Add(30*time.Minute), state, "after_30_60"),
	}

	view, err := NewView("u1", "Asia/Tokyo", projections)
	if err != nil {
		t.Fatalf("view generation failed: %v", err)
	}
	if len(view.Segments) != 2 {
		t.Fatalf("different response buckets were merged")
	}
}

func validProjection(id string, startAt, endAt time.Time, state policy.InteractionState, responseBucket string) ScheduleProjection {
	return ScheduleProjection{
		ID: id, UserID: "u1", StartAt: startAt, EndAt: endAt, State: state,
		ExpectedResponseBucket: responseBucket, GeneratedAt: startAt, ExpiresAt: endAt.Add(time.Hour),
	}
}
