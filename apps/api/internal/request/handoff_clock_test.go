package request

import (
	"reflect"
	"testing"
	"time"
)

func TestHandoffClockGuardsAndReplay(t *testing.T) {
	now := time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC)
	source := CoordinationRequest{ID: "r", OrganizationID: "org", RequesterUserID: "alice", TargetUserID: "bob", Status: Suggested, CreatedAt: now, UpdatedAt: now.Add(time.Minute), DeadlineAt: now.Add(time.Hour)}
	option := Option{ID: "o", RequestID: "r", Type: OptionAsync, ResponseBy: &source.DeadlineAt, CreatedAt: now}
	for _, clock := range []time.Time{{}, now.Add(-time.Second), now} {
		if _, err := ValidateHandoff(source, "bob", "org", "carol", clock); err != ErrHandoffConflict {
			t.Fatalf("invalid clock accepted: %v", err)
		}
		value := source
		if err := ApplyHandoff(&value, "bob", "carol", []Option{option}, clock); err != ErrHandoffConflict || !reflect.DeepEqual(value, source) {
			t.Fatal("invalid clock mutated handoff")
		}
	}
	value := source
	clock := now.Add(2 * time.Minute).In(time.FixedZone("JST", 9*3600))
	if err := ApplyHandoff(&value, "bob", "carol", []Option{option}, clock); err != nil {
		t.Fatal(err)
	}
	if value.UpdatedAt.Location() != time.UTC {
		t.Fatal("mutation time not normalized")
	}
	if replay, err := ValidateHandoff(value, "bob", "org", "carol", time.Time{}); err != nil || !replay {
		t.Fatal("immutable replay rejected", err)
	}
}
