package projection

import (
	"github.com/negotiable-calendar/negotiable-calendar/apps/api/internal/policy"
	"testing"
	"time"
)

func TestViewRejectsForeignProjectionWithoutPartialResult(t *testing.T) {
	now := time.Now().UTC()
	state := policy.InteractionState{Availability: policy.Available, Interruptibility: policy.InterruptOpen, Requestability: policy.RequestOpen, Reschedulability: policy.RescheduleHigh}
	for _, scenario := range []string{"self", "foreign", "mixed", "empty-owner", "empty"} {
		t.Run(scenario, func(t *testing.T) {
			values := []ScheduleProjection{validProjection("p1", now, now.Add(time.Hour), state, "within_window")}
			valid := false
			switch scenario {
			case "self":
				valid = true
			case "foreign":
				values[0].UserID = "u2"
			case "mixed":
				other := values[0]
				other.UserID = "u2"
				values = append(values, other)
			case "empty-owner":
				values[0].UserID = ""
			case "empty":
				values = nil
				valid = true
			}
			view, err := NewView("u1", "Asia/Tokyo", values)
			if valid {
				if err != nil || view.UserID != "u1" || view.Segments == nil {
					t.Fatal(view, err)
				}
				return
			}
			if err == nil || view.UserID != "" || len(view.Segments) != 0 {
				t.Fatal("foreign or partial view returned", view, err)
			}
		})
	}
}
