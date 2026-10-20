package policy

import (
	"encoding/json"
	"testing"
	"time"
)

func TestPolicyValidatesRuleOwnershipAndIdentity(t *testing.T) {
	now := time.Date(2026, 10, 20, 0, 0, 0, 0, time.UTC)
	state := InteractionState{Availability: Limited, Interruptibility: UrgentOnly, Requestability: AsyncOnly, Reschedulability: RescheduleLow}
	for _, tc := range []struct {
		name, parent, secondID string
		wantValid              bool
	}{
		{"owned distinct rules", "policy", "second", true},
		{"foreign rule", "foreign", "second", false},
		{"duplicate identity", "policy", "first", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			value := SharingPolicy{ID: "policy", UserID: "owner", Default: state, CreatedAt: now, UpdatedAt: now}
			value.Rules = []Rule{
				{ID: "first", PolicyID: value.ID, ConditionType: "organization", Condition: json.RawMessage(`{}`), State: state, Enabled: true},
				{ID: tc.secondID, PolicyID: tc.parent, ConditionType: "organization", Condition: json.RawMessage(`{}`), State: state},
			}
			if got := value.Validate() == nil; got != tc.wantValid {
				t.Fatalf("valid=%v want %v (disabled rules must also be checked)", got, tc.wantValid)
			}
		})
	}
}
