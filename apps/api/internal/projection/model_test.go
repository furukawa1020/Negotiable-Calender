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
