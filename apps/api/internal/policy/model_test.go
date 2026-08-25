package policy

import (
	"testing"
	"time"
)

func TestInteractionStateAndPolicyValidation(t *testing.T) {
	t.Parallel()
	state := InteractionState{Availability: Limited, Interruptibility: UrgentOnly, Requestability: RequestLater, Reschedulability: RescheduleLow}
	if err := state.Validate(); err != nil {
		t.Fatalf("valid state rejected: %v", err)
	}
	state.Availability = Availability("customer_meeting")
	if err := state.Validate(); err == nil {
		t.Fatal("activity-revealing state accepted")
	}
	now := time.Date(2026, 8, 23, 0, 0, 0, 0, time.UTC)
	policy := SharingPolicy{
		ID: "p1", UserID: "u1", CreatedAt: now, UpdatedAt: now,
		Default:      InteractionState{Availability: Unknown, Interruptibility: UrgentOnly, Requestability: RequestClosed, Reschedulability: RescheduleFixed},
		WorkingHours: []WorkingWindow{{Weekday: time.Monday, StartMinute: 540, EndMinute: 1080}},
	}
	if err := policy.Validate(); err != nil {
		t.Fatalf("valid policy rejected: %v", err)
	}
}
