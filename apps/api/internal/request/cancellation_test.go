package request

import (
	"errors"
	"testing"
	"time"
)

func TestConfirmedCancellationValidation(t *testing.T) {
	now := time.Now().UTC()
	start, end := now.Add(time.Hour), now.Add(2*time.Hour)
	fixture := CoordinationRequest{ID: "r", RequesterUserID: "alice", TargetUserID: "bob", Status: Accepted, AcceptedOptionID: "o", Options: []Option{{ID: "o", RequestID: "r", Type: OptionMeeting, StartAt: &start, EndAt: &end, CreatedAt: now}}}
	for _, actor := range []string{"alice", "bob"} {
		if err := ValidateConfirmedCancellation(fixture, actor, "o", now); err != nil {
			t.Fatal(err)
		}
		note, event := ConfirmedCancellationEffects(fixture, actor, now)
		if note.UserID == actor || event.ActorUserID != actor || event.ResourceID != fixture.ID {
			t.Fatal("wrong effect recipient")
		}
	}
	for _, tc := range []struct {
		name, actor, option string
		state               Status
		at                  time.Time
		want                error
	}{
		{"outsider", "mallory", "o", Accepted, now, ErrNotFound},
		{"anonymous", "", "o", Accepted, now, ErrNotFound},
		{"stale", "alice", "old", Accepted, now, ErrCancellationInvalid},
		{"missing", "alice", "", Accepted, now, ErrCancellationInvalid},
		{"pending", "alice", "o", Suggested, now, ErrCancellationInvalid},
		{"started", "alice", "o", Accepted, start, ErrCancellationInvalid},
		{"replay-after-start", "bob", "o", Cancelled, end, ErrAlreadyCancelled},
		{"stale-replay", "bob", "old", Cancelled, now, ErrCancellationInvalid},
	} {
		t.Run(tc.name, func(t *testing.T) {
			value := fixture
			value.Status = tc.state
			if err := ValidateConfirmedCancellation(value, tc.actor, tc.option, tc.at); !errors.Is(err, tc.want) {
				t.Fatalf("got %v want %v", err, tc.want)
			}
		})
	}
	fixture.Options[0].Type = OptionAsync
	if err := ValidateConfirmedCancellation(fixture, "alice", "o", now); !errors.Is(err, ErrCancellationInvalid) {
		t.Fatal(err)
	}
}
