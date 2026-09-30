package request

import (
	"fmt"
	"math/rand"
	"reflect"
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

func TestCandidatesRespectEveryBlockingBoundary(t *testing.T) {
	for _, kind := range []string{"gap", "closed", "later", "unknown", "unavailable", "expired", "future", "reservation"} {
		t.Run(kind, func(t *testing.T) {
			input := continuousCandidateInput(60)
			blocked := input.Projections[2]
			switch kind {
			case "gap":
				input.Projections = append(input.Projections[:2], input.Projections[3:]...)
			case "closed":
				input.Projections[2].State.Requestability = policy.RequestClosed
			case "later":
				input.Projections[2].State.Requestability = policy.RequestLater
			case "unknown":
				input.Projections[2].State.Availability = policy.Unknown
			case "unavailable":
				input.Projections[2].State.Availability = policy.Unavailable
			case "expired":
				input.Projections[2].ExpiresAt = input.Now
			case "future":
				input.Projections[2].GeneratedAt = input.Now.Add(time.Second)
			case "reservation":
				input.Reserved = []ReservedRange{{StartAt: blocked.StartAt, EndAt: blocked.EndAt}}
			}
			options, err := GenerateCandidates(input)
			if err != nil || len(options) != 2 {
				t.Fatalf("options=%+v err=%v", options, err)
			}
			for _, option := range options {
				if option.Type != OptionMeeting || option.StartAt.Before(blocked.EndAt) {
					t.Fatalf("crossed blocking interval: %+v", option)
				}
				if err := ValidateMeetingAvailability(input.Request.TargetUserID, option, input.Projections, input.Now); err != nil {
					t.Fatal(err)
				}
			}
		})
	}
}

func TestCandidatesUseWorstCoveredQualityAndAllowStateTransitions(t *testing.T) {
	input := continuousCandidateInput(60)
	input.Projections[1].State.Availability = policy.Limited
	input.Projections[1].State.Reschedulability = policy.RescheduleLow
	options, err := GenerateCandidates(input)
	if err != nil || len(options) != 3 {
		t.Fatal(options, err)
	}
	// The earliest start has an unfavorable second bucket. Prefer the later free hour.
	if !options[0].StartAt.Equal(input.Now.Add(90 * time.Minute)) {
		t.Fatalf("first-bucket-only ranking: %+v", options)
	}
	input.Projections = input.Projections[:4]
	options, err = GenerateCandidates(input)
	if err != nil || len(options) != 1 || options[0].Type != OptionMeeting || !options[0].StartAt.Equal(input.Now.Add(time.Hour)) {
		t.Fatalf("allowed state transition broke continuity: %+v %v", options, err)
	}
}

func TestCandidatesNormalizeDuplicatesOverlapAndInputOrder(t *testing.T) {
	input := continuousCandidateInput(30)
	want, err := GenerateCandidates(input)
	if err != nil {
		t.Fatal(err)
	}
	input.Projections = append(input.Projections, input.Projections...)
	overlap := input.Projections[0]
	overlap.StartAt, overlap.EndAt = input.Now.Add(70*time.Minute), input.Now.Add(95*time.Minute)
	input.Projections = append(input.Projections, overlap)
	for seed := int64(0); seed < 12; seed++ {
		rand.New(rand.NewSource(seed)).Shuffle(len(input.Projections), func(i, j int) {
			input.Projections[i], input.Projections[j] = input.Projections[j], input.Projections[i]
		})
		before := append([]projection.ScheduleProjection(nil), input.Projections...)
		got, err := GenerateCandidates(input)
		if err != nil || !reflect.DeepEqual(got, want) {
			t.Fatalf("order/duplicate changed candidates: %+v %v", got, err)
		}
		if !reflect.DeepEqual(input.Projections, before) {
			t.Fatal("caller input mutated")
		}
	}
}

func TestCandidatesDeadlinePrecisionAndFutureStarts(t *testing.T) {
	for _, offset := range []time.Duration{0, time.Nanosecond, 7 * time.Minute} {
		input := continuousCandidateInput(30)
		input.Projections = []projection.ScheduleProjection{input.Projections[0]}
		input.Projections[0].StartAt = input.Now
		input.Projections[0].EndAt = input.Now.Add(time.Hour)
		input.Now = input.Now.Add(offset)
		input.Request.DeadlineAt = input.Projections[0].StartAt.Add(45 * time.Minute)
		options, err := GenerateCandidates(input)
		if err != nil || len(options) != 1 || options[0].Type != OptionMeeting {
			t.Fatal(options, err)
		}
		if !options[0].StartAt.After(input.Now) || !options[0].EndAt.Equal(input.Request.DeadlineAt) {
			t.Fatal("invalid clock/deadline boundary", options)
		}
		input.Request.DeadlineAt = input.Request.DeadlineAt.Add(-time.Nanosecond)
		options, err = GenerateCandidates(input)
		if err != nil || len(options) != 1 || options[0].Type != OptionAsync {
			t.Fatal("candidate exceeded deadline", options, err)
		}
	}
}

func TestCandidatesRejectMalformedProjectionAndDurationOverflow(t *testing.T) {
	for _, kind := range []string{"foreign-user", "invalid-range", "invalid-state", "duration-overflow"} {
		t.Run(kind, func(t *testing.T) {
			input := continuousCandidateInput(30)
			switch kind {
			case "foreign-user":
				input.Projections[1].UserID = "other"
			case "invalid-range":
				input.Projections[1].EndAt = input.Projections[1].StartAt
			case "invalid-state":
				input.Projections[1].State.Availability = "invalid"
			case "duration-overflow":
				input.Request.DurationMinutes = int(^uint(0) >> 1)
			}
			if got, err := GenerateCandidates(input); err == nil || got != nil {
				t.Fatalf("invalid input accepted: %+v %v", got, err)
			}
		})
	}
}

func TestCandidateSweepAgreesWithConfirmationCoverage(t *testing.T) {
	// Independent coverage oracle also rejects contradictory/stale overlaps.
	rng := rand.New(rand.NewSource(185))
	for round := 0; round < 100; round++ {
		input := continuousCandidateInput([]int{5, 15, 30, 60}[round%4])
		for i := range input.Projections {
			switch rng.Intn(7) {
			case 0:
				input.Projections[i].State.Requestability = policy.RequestClosed
			case 1:
				input.Projections[i].ExpiresAt = input.Now
			case 2:
				input.Projections[i].State.Availability = policy.Limited
			case 3:
				input.Projections[i].GeneratedAt = input.Now.Add(time.Second)
			}
		}
		if round%3 == 0 {
			overlap := input.Projections[rng.Intn(len(input.Projections))]
			overlap.StartAt = overlap.StartAt.Add(-7 * time.Minute)
			overlap.EndAt = overlap.EndAt.Add(7 * time.Minute)
			input.Projections = append(input.Projections, overlap)
		}
		count := 0
		for start := input.Now.Add(candidateStep); !start.Add(time.Duration(input.Request.DurationMinutes) * time.Minute).After(input.Request.DeadlineAt); start = start.Add(candidateStep) {
			end := start.Add(time.Duration(input.Request.DurationMinutes) * time.Minute)
			option := Option{StartAt: &start, EndAt: &end}
			if ValidateMeetingAvailability(input.Request.TargetUserID, option, input.Projections, input.Now) == nil {
				count++
			}
		}
		options, err := GenerateCandidates(input)
		if err != nil {
			t.Fatal(err)
		}
		if count == 0 {
			if len(options) != 1 || options[0].Type != OptionAsync {
				t.Fatalf("expected fallback: %+v", options)
			}
			continue
		}
		if len(options) != min(3, count) {
			t.Fatalf("lost valid windows: count=%d options=%+v", count, options)
		}
		seen := map[string]bool{}
		for _, option := range options {
			if option.Type != OptionMeeting || seen[option.ID] {
				t.Fatalf("invalid/duplicate meeting: %+v", option)
			}
			seen[option.ID] = true
			if err := ValidateMeetingAvailability(input.Request.TargetUserID, option, input.Projections, input.Now); err != nil {
				t.Fatalf("round=%d candidate failed oracle: %v", round, err)
			}
		}
	}
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
