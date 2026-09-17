package request

import (
	"errors"
	"github.com/negotiable-calendar/negotiable-calendar/apps/api/internal/policy"
	"github.com/negotiable-calendar/negotiable-calendar/apps/api/internal/projection"
	"testing"
	"time"
)

func TestConfirmableMeetingGuards(t *testing.T) {
	now := time.Now().UTC()
	start := now.Add(time.Hour)
	end := start.Add(time.Hour)
	base := CoordinationRequest{ID: "r", DeadlineAt: end, Options: []Option{{ID: "o", RequestID: "r", Type: OptionMeeting, StartAt: &start, EndAt: &end, CreatedAt: now}}}
	if _, err := ConfirmableMeeting(base, "o", now); err != nil {
		t.Fatal(err)
	}
	if _, err := ConfirmableMeeting(base, "o", start); !errors.Is(err, ErrCandidateExpired) {
		t.Fatal(err)
	}
	if _, err := ConfirmableMeeting(base, "missing", now); !errors.Is(err, ErrCandidateInvalid) {
		t.Fatal(err)
	}
	base.DeadlineAt = start
	if _, err := ConfirmableMeeting(base, "o", now); !errors.Is(err, ErrCandidateExpired) {
		t.Fatal(err)
	}
	base.Options[0].Type = OptionAsync
	if _, err := ConfirmableMeeting(base, "o", now); !errors.Is(err, ErrCandidateInvalid) {
		t.Fatal(err)
	}
}

func TestMeetingAvailabilityCoverage(t *testing.T) {
	now := time.Now().UTC()
	start := now.Add(time.Hour)
	middle := start.Add(15 * time.Minute)
	end := middle.Add(15 * time.Minute)
	option := Option{StartAt: &start, EndAt: &end}
	base := projection.ScheduleProjection{ID: "p", UserID: "u", StartAt: start, EndAt: middle, State: policy.InteractionState{Availability: policy.Available, Requestability: policy.RequestOpen, Interruptibility: "normal", Reschedulability: policy.RescheduleMedium}, ExpectedResponseBucket: "soon", GeneratedAt: now, ExpiresAt: now.Add(time.Hour)}
	second := base
	second.ID = "second"
	second.StartAt = middle
	second.EndAt = end
	if err := ValidateMeetingAvailability("u", option, []projection.ScheduleProjection{second, base}, now); err != nil {
		t.Fatal(err)
	}
	for _, mutate := range []func(*projection.ScheduleProjection){
		func(p *projection.ScheduleProjection) { p.StartAt = p.StartAt.Add(time.Second) },
		func(p *projection.ScheduleProjection) { p.State.Availability = policy.Unknown },
		func(p *projection.ScheduleProjection) { p.State.Requestability = policy.RequestClosed },
		func(p *projection.ScheduleProjection) { p.ExpiresAt = now },
		func(p *projection.ScheduleProjection) { p.UserID = "other" },
	} {
		changed := second
		mutate(&changed)
		if err := ValidateMeetingAvailability("u", option, []projection.ScheduleProjection{base, changed}, now); !errors.Is(err, ErrAvailabilityChanged) {
			t.Fatal(err)
		}
	}
	if err := ValidateMeetingAvailability("u", option, nil, now); !errors.Is(err, ErrAvailabilityChanged) {
		t.Fatal("gap accepted")
	}
}
