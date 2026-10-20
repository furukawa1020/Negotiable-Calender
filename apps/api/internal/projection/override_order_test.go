package projection

import (
	"reflect"
	"slices"
	"testing"
	"time"

	"github.com/negotiable-calendar/negotiable-calendar/apps/api/internal/policy"
)

func TestOverridePrecedenceIsIndependentOfStorageOrder(t *testing.T) {
	now := time.Date(2026, 8, 24, 0, 0, 0, 0, time.UTC)
	for _, tie := range []bool{false, true} {
		input := validInput(now, now.Add(BucketSize))
		older := policy.ManualOverride{
			ID: "a", UserID: input.UserID, StartAt: now, EndAt: input.To,
			CreatedAt: now.Add(-time.Hour), ExpiresAt: now.Add(time.Hour), State: input.Policy.Default,
		}
		newer := older
		newer.ID, newer.State = "z", closedState()
		if !tie {
			newer.CreatedAt = now
		}
		for _, values := range [][]policy.ManualOverride{{older, newer}, {newer, older}} {
			input.ManualOverrides = values
			before := slices.Clone(values)
			got, err := NewEngine().Generate(input)
			if err != nil || got[0].State != closedState() {
				t.Fatalf("tie=%v: order-dependent result: %v %v", tie, got, err)
			}
			if !reflect.DeepEqual(before, values) {
				t.Fatal("caller snapshot reordered")
			}
		}
		input.ManualOverrides = []policy.ManualOverride{older, older}
		if got, err := NewEngine().Generate(input); err == nil || got != nil {
			t.Fatal("duplicate identity accepted")
		}
	}
}
