package request

import (
	"fmt"
	"testing"
	"time"

	"github.com/negotiable-calendar/negotiable-calendar/apps/api/internal/policy"
	"github.com/negotiable-calendar/negotiable-calendar/apps/api/internal/projection"
)

func continuousCandidateInput(minutes int) CandidateInput {
	now := time.Date(2026, 10, 1, 8, 0, 0, 0, time.UTC)
	value := validCandidateRequest(now)
	value.DurationMinutes = minutes
	input := CandidateInput{Request: value, Now: now}
	for i := range 8 {
		start := now.Add(time.Hour + time.Duration(i)*projection.BucketSize)
		p := validCandidateProjection(start, start.Add(projection.BucketSize))
		p.ID, p.GeneratedAt, p.ExpiresAt = fmt.Sprintf("bucket-%d", i), now, now.Add(time.Hour)
		input.Projections = append(input.Projections, p)
	}
	return input
}

func TestCandidatesSpanContinuousBuckets(t *testing.T) {
	for _, minutes := range []int{30, 60} {
		t.Run(fmt.Sprint(minutes), func(t *testing.T) {
			input := continuousCandidateInput(minutes)
			options, err := GenerateCandidates(input)
			if err != nil || len(options) != 3 {
				t.Fatalf("options=%+v err=%v", options, err)
			}
			for i, option := range options {
				start := input.Now.Add(time.Hour + time.Duration(i)*candidateStep)
				if option.Type != OptionMeeting || option.StartAt == nil || !option.StartAt.Equal(start) || option.EndAt.Sub(*option.StartAt) != time.Duration(minutes)*time.Minute {
					t.Fatalf("invalid long candidate: %+v", option)
				}
				if err := ValidateMeetingAvailability(input.Request.TargetUserID, option, input.Projections, input.Now); err != nil {
					t.Fatalf("candidate cannot pass confirmation availability: %v", err)
				}
			}
		})
	}
}

func TestCandidatesDoNotMaskContradictoryOverlap(t *testing.T) {
	input := continuousCandidateInput(30)
	open := input.Projections[0]
	open.EndAt = input.Now.Add(3 * time.Hour)
	closed := input.Projections[2]
	closed.State.Requestability = policy.RequestClosed
	input.Projections = []projection.ScheduleProjection{open, closed}
	options, err := GenerateCandidates(input)
	if err != nil {
		t.Fatal(err)
	}
	for _, option := range options {
		if option.Type == OptionMeeting && option.StartAt.Before(closed.EndAt) && closed.StartAt.Before(*option.EndAt) {
			t.Fatalf("closed overlap proposed: %+v", option)
		}
	}
}
