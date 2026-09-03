package policy

import (
	"encoding/json"
	"strings"
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

func TestSharingPolicyRejectsOverlappingAndExcessiveConfiguration(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 9, 3, 0, 0, 0, 0, time.UTC)
	state := InteractionState{Availability: Limited, Interruptibility: UrgentOnly, Requestability: AsyncOnly, Reschedulability: RescheduleLow}
	value := SharingPolicy{
		ID: "policy-1", UserID: "user-1", Default: state, CreatedAt: now, UpdatedAt: now,
		WorkingHours: []WorkingWindow{
			{Weekday: time.Monday, StartMinute: 540, EndMinute: 720},
			{Weekday: time.Monday, StartMinute: 660, EndMinute: 1080},
		},
	}
	if err := value.Validate(); err == nil {
		t.Fatal("overlapping working windows accepted")
	}
	value.WorkingHours = make([]WorkingWindow, MaxWorkingWindows+1)
	for index := range value.WorkingHours {
		value.WorkingHours[index] = WorkingWindow{Weekday: time.Weekday(index % 7), StartMinute: index * 5, EndMinute: index*5 + 1}
	}
	if err := value.Validate(); err == nil {
		t.Fatal("excessive working windows accepted")
	}
	value.WorkingHours = nil
	value.Rules = make([]Rule, MaxPolicyRules+1)
	if err := value.Validate(); err == nil {
		t.Fatal("excessive rules accepted")
	}
}

func TestRuleConditionSchemaIsStrictAndBounded(t *testing.T) {
	t.Parallel()
	state := InteractionState{Availability: Limited, Interruptibility: UrgentOnly, Requestability: AsyncOnly, Reschedulability: RescheduleLow}
	valid := []Rule{
		{ID: "r1", PolicyID: "p1", ConditionType: "organization", Condition: json.RawMessage(`{}`), State: state},
		{ID: "r2", PolicyID: "p1", ConditionType: "calendar", Condition: json.RawMessage(`{"calendarId":"primary"}`), State: state, Priority: MaxRulePriority},
		{ID: "r3", PolicyID: "p1", ConditionType: "event", Condition: json.RawMessage(`{"busyStatus":"busy"}`), State: state},
	}
	for _, rule := range valid {
		if err := rule.Validate(); err != nil {
			t.Fatalf("valid rule rejected: %v", err)
		}
	}
	invalid := []Rule{
		{ID: "r", PolicyID: "p", ConditionType: "organization", Condition: json.RawMessage(`null`), State: state},
		{ID: "r", PolicyID: "p", ConditionType: "organization", Condition: json.RawMessage(`{"secret":"title"}`), State: state},
		{ID: "r", PolicyID: "p", ConditionType: "calendar", Condition: json.RawMessage(`{"calendarId":""}`), State: state},
		{ID: "r", PolicyID: "p", ConditionType: "calendar", Condition: json.RawMessage(`{"calendarId":"primary","name":"Secret"}`), State: state},
		{ID: "r", PolicyID: "p", ConditionType: "calendar", Condition: json.RawMessage("{\"calendarId\":\"" + strings.Repeat("a", MaxCalendarIDRunes+1) + "\"}"), State: state},
		{ID: "r", PolicyID: "p", ConditionType: "event", Condition: json.RawMessage(`{"busyStatus":"tentative"}`), State: state},
		{ID: "r", PolicyID: "p", ConditionType: "event", Condition: json.RawMessage(`{"busyStatus":"busy","title":"Secret"}`), State: state},
		{ID: "r", PolicyID: "p", ConditionType: "event", Condition: json.RawMessage(`{"busyStatus":"busy"}`), State: state, Priority: MaxRulePriority + 1},
	}
	for _, rule := range invalid {
		if err := rule.Validate(); err == nil {
			t.Fatalf("invalid rule accepted: type=%s condition=%s priority=%d", rule.ConditionType, rule.Condition, rule.Priority)
		}
	}
}
