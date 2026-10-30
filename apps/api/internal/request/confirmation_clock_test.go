package request

import (
	"errors"
	"testing"
	"time"
)

func TestConfirmableMeetingRejectsInvalidClock(t *testing.T) {
	now := time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC)
	start, end := now.Add(time.Hour), now.Add(2*time.Hour)
	value := CoordinationRequest{ID: "r", CreatedAt: now.Add(-time.Hour), UpdatedAt: now, DeadlineAt: end, Options: []Option{{ID: "o", RequestID: "r", Type: OptionMeeting, StartAt: &start, EndAt: &end, CreatedAt: now}}}
	for _, at := range []time.Time{{}, now.Add(-time.Second), now.Add(-2 * time.Hour)} {
		if _, err := ConfirmableMeeting(value, "o", at); !errors.Is(err, ErrCandidateInvalid) {
			t.Fatalf("invalid clock accepted: %v", err)
		}
	}
	if _, err := ConfirmableMeeting(value, "o", now); err != nil {
		t.Fatal(err)
	}
	if _, err := ConfirmableMeeting(value, "o", start); !errors.Is(err, ErrCandidateExpired) {
		t.Fatal(err)
	}
}
