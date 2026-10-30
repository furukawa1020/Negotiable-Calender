package request

import (
	"errors"
	"testing"
	"time"
)

func TestCancellationRejectsClockRegressionButPreservesReplay(t *testing.T) {
	now := time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC)
	start, end := now.Add(time.Hour), now.Add(2*time.Hour)
	value := CoordinationRequest{ID: "r", RequesterUserID: "alice", TargetUserID: "bob", Status: Accepted, AcceptedOptionID: "o", CreatedAt: now.Add(-time.Hour), UpdatedAt: now,
		Options: []Option{{ID: "o", RequestID: "r", Type: OptionMeeting, StartAt: &start, EndAt: &end, CreatedAt: now}}}
	for _, at := range []time.Time{{}, now.Add(-time.Second), now.Add(-2 * time.Hour)} {
		if err := ValidateConfirmedCancellation(value, "alice", "o", at); !errors.Is(err, ErrCancellationInvalid) {
			t.Fatalf("invalid clock accepted: %v", err)
		}
	}
	if err := ValidateConfirmedCancellation(value, "bob", "o", now); err != nil {
		t.Fatal(err)
	}
	value.Status = Cancelled
	if err := ValidateConfirmedCancellation(value, "alice", "o", time.Time{}); !errors.Is(err, ErrAlreadyCancelled) {
		t.Fatal(err)
	}
	if err := ValidateConfirmedCancellation(value, "outsider", "o", time.Time{}); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
}
