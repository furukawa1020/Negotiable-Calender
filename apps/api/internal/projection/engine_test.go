package projection

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/negotiable-calendar/negotiable-calendar/apps/api/internal/policy"
	"github.com/negotiable-calendar/negotiable-calendar/apps/api/internal/privateevent"
)

func TestEngineGeneratesPrivateBucketsAndAppliesManualOverrideLast(t *testing.T) {
	t.Parallel()
	start := time.Date(2026, 8, 24, 0, 0, 0, 0, time.UTC)
	input := validInput(start, start.Add(time.Hour))
	input.Events = []privateevent.PrivateEvent{validEvent(start.Add(30*time.Minute), start.Add(time.Hour), privateevent.Busy)}
	input.Policy.Rules = []policy.Rule{{
		ID: "rule-1", PolicyID: input.Policy.ID, ConditionType: "event",
		Condition: json.RawMessage(`{"busyStatus":"busy"}`), Enabled: true, Priority: 10,
		State: policy.InteractionState{Availability: policy.Limited, Interruptibility: policy.UrgentOnly, Requestability: policy.RequestLater, Reschedulability: policy.RescheduleLow},
	}}
	input.ManualOverrides = []policy.ManualOverride{{
		ID: "override-1", UserID: input.UserID, StartAt: start.Add(45 * time.Minute), EndAt: start.Add(time.Hour),
		ExpiresAt: start.Add(2 * time.Hour), CreatedAt: start,
		State: policy.InteractionState{Availability: policy.Available, Interruptibility: policy.InterruptOpen, Requestability: policy.RequestOpen, Reschedulability: policy.RescheduleHigh},
	}}

	segments, err := NewEngine().Generate(input)
	if err != nil {
		t.Fatalf("generation failed: %v", err)
	}
	if len(segments) != 4 {
		t.Fatalf("expected 4 buckets, got %d", len(segments))
	}
	if segments[2].State.Availability != policy.Limited {
		t.Fatalf("event rule was not applied: %s", segments[2].State.Availability)
	}
	if segments[3].State.Availability != policy.Available {
		t.Fatalf("manual override was not applied last: %s", segments[3].State.Availability)
	}
}

func TestEngineFailsClosedOutsideWorkingHours(t *testing.T) {
	t.Parallel()
	start := time.Date(2026, 8, 24, 8, 45, 0, 0, time.UTC)
	input := validInput(start, start.Add(30*time.Minute))
	segments, err := NewEngine().Generate(input)
	if err != nil {
		t.Fatalf("generation failed: %v", err)
	}
	if segments[0].State.Availability != policy.Available {
		t.Fatalf("working-hours bucket is not available: %s", segments[0].State.Availability)
	}
	if segments[1].State.Availability != policy.Unavailable {
		t.Fatalf("outside-hours bucket did not fail closed: %s", segments[1].State.Availability)
	}
}

func TestUnknownEventFailsClosed(t *testing.T) {
	t.Parallel()
	start := time.Date(2026, 8, 24, 0, 0, 0, 0, time.UTC)
	input := validInput(start, start.Add(15*time.Minute))
	input.Events = []privateevent.PrivateEvent{validEvent(start, start.Add(15*time.Minute), privateevent.BusyUnknown)}
	segments, err := NewEngine().Generate(input)
	if err != nil {
		t.Fatalf("generation failed: %v", err)
	}
	if segments[0].State.Availability != policy.Unavailable {
		t.Fatalf("unknown event became visible availability: %s", segments[0].State.Availability)
	}
}

func validInput(from, to time.Time) GenerateInput {
	defaultState := policy.InteractionState{Availability: policy.Available, Interruptibility: policy.InterruptOpen, Requestability: policy.RequestOpen, Reschedulability: policy.RescheduleHigh}
	return GenerateInput{
		UserID: "manager-1", Timezone: "Asia/Tokyo", From: from, To: to, Now: from,
		Policy: policy.SharingPolicy{
			ID: "policy-1", UserID: "manager-1", Default: defaultState,
			WorkingHours: []policy.WorkingWindow{{Weekday: time.Monday, StartMinute: 9 * 60, EndMinute: 18 * 60}},
			CreatedAt:    from, UpdatedAt: from,
		},
	}
}

func validEvent(startAt, endAt time.Time, status privateevent.BusyStatus) privateevent.PrivateEvent {
	return privateevent.PrivateEvent{
		ID: "event-1", UserID: "manager-1", ProviderEventID: "provider-1", CalendarID: "calendar-1",
		StartAt: startAt, EndAt: endAt, BusyStatus: status, Visibility: privateevent.VisibilityPrivate,
		TitleEncrypted: []byte("encrypted"), CreatedAt: startAt, UpdatedAt: startAt,
	}
}
