package request

import (
	"errors"
	"testing"
	"time"
)

func TestCreationFreshnessBoundaries(t *testing.T) {
	now := time.Now().UTC()
	for _, kind := range []string{"deadline", "meeting", "async"} {
		for _, offset := range []time.Duration{-time.Nanosecond, 0, time.Nanosecond} {
			at := now.Add(offset)
			value := CoordinationRequest{DeadlineAt: now.Add(time.Hour)}
			switch kind {
			case "deadline":
				value.DeadlineAt = at
			case "meeting":
				value.Options = []Option{{Type: OptionMeeting, StartAt: &at}}
			case "async":
				value.Options = []Option{{Type: OptionAsync, ResponseBy: &at}}
			}
			err := ValidateCreationFreshness(value, now)
			if (offset <= 0 && !errors.Is(err, ErrCreationExpired)) || (offset > 0 && err != nil) {
				t.Fatalf("%s offset=%v err=%v", kind, offset, err)
			}
		}
	}
	if err := ValidateCreationFreshness(CoordinationRequest{DeadlineAt: now.Add(time.Hour)}, time.Time{}); !errors.Is(err, ErrCreationExpired) {
		t.Fatal("zero clock accepted")
	}
}
