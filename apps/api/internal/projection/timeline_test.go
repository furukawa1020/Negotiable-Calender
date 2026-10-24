package projection

import (
	"reflect"
	"testing"
	"time"

	"github.com/negotiable-calendar/negotiable-calendar/apps/api/internal/policy"
)

func TestViewOrdersWithoutMutatingSource(t *testing.T) {
	now := time.Date(2026, 10, 25, 0, 0, 0, 0, time.UTC)
	state := policy.InteractionState{Availability: policy.Available, Interruptibility: policy.InterruptOpen, Requestability: policy.RequestOpen, Reschedulability: policy.RescheduleHigh}
	row := func(id string, start, end time.Duration) ScheduleProjection {
		return validProjection(id, now.Add(start), now.Add(end), state, "within_window")
	}
	rows := []ScheduleProjection{row("c", 3*time.Hour, 4*time.Hour), row("b", time.Hour, 2*time.Hour), row("a", 0, time.Hour)}
	before := append([]ScheduleProjection(nil), rows...)
	view, err := NewView("u1", "UTC", rows)
	if err != nil || len(view.Segments) != 2 {
		t.Fatalf("view=%+v err=%v", view, err)
	}
	if !view.Segments[0].StartAt.Equal(now) || !view.Segments[0].EndAt.Equal(now.Add(2*time.Hour)) || !view.Segments[1].StartAt.Equal(now.Add(3*time.Hour)) {
		t.Fatal("chronology, merge or gap changed")
	}
	if !reflect.DeepEqual(rows, before) {
		t.Fatal("source mutated")
	}
	for name, invalid := range map[string][]ScheduleProjection{
		"duplicate":  {row("a", 0, time.Hour), row("a", 2*time.Hour, 3*time.Hour)},
		"overlap":    {row("b", time.Hour, 3*time.Hour), row("a", 0, 2*time.Hour)},
		"same-start": {row("b", 0, time.Hour), row("a", 0, 2*time.Hour)},
		"nested":     {row("a", 0, 4*time.Hour), row("b", time.Hour, 2*time.Hour)},
	} {
		t.Run(name, func(t *testing.T) {
			view, err := NewView("u1", "UTC", invalid)
			if err == nil || len(view.Segments) != 0 {
				t.Fatalf("ambiguous view returned: %+v %v", view, err)
			}
		})
	}
}
