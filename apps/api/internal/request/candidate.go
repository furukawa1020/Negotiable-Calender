package request

import (
	"fmt"
	"sort"
	"time"

	"github.com/negotiable-calendar/negotiable-calendar/apps/api/internal/policy"
	"github.com/negotiable-calendar/negotiable-calendar/apps/api/internal/projection"
)

const candidateStep = 15 * time.Minute

type ReservedRange struct {
	StartAt time.Time
	EndAt   time.Time
}

type CandidateInput struct {
	Request     CoordinationRequest
	Projections []projection.ScheduleProjection
	Reserved    []ReservedRange
	Now         time.Time
}

type scoredOption struct {
	option Option
	score  int
}

func GenerateCandidates(input CandidateInput) ([]Option, error) {
	if err := input.Request.Validate(); err != nil {
		return nil, fmt.Errorf("invalid request: %w", err)
	}
	if input.Now.IsZero() || input.Now.Location() != time.UTC {
		return nil, fmt.Errorf("now must be UTC")
	}
	if input.Request.SyncPreference == AsyncPreferred {
		return []Option{asyncCandidate(input.Request, input.Now)}, nil
	}
	duration := time.Duration(input.Request.DurationMinutes) * time.Minute
	var candidates []scoredOption
	for _, segment := range input.Projections {
		if err := segment.Validate(); err != nil {
			return nil, fmt.Errorf("invalid projection: %w", err)
		}
		if segment.UserID != input.Request.TargetUserID {
			return nil, fmt.Errorf("projection target mismatch")
		}
		if segment.State.Requestability != policy.RequestOpen {
			continue
		}
		if segment.State.Availability == policy.Unavailable {
			continue
		}
		start := segment.StartAt
		if input.Now.After(start) {
			start = input.Now
		}
		start = ceilCandidateStep(start)
		endLimit := segment.EndAt
		if input.Request.DeadlineAt.Before(endLimit) {
			endLimit = input.Request.DeadlineAt
		}
		for cursor := start; !cursor.Add(duration).After(endLimit); cursor = cursor.Add(candidateStep) {
			end := cursor.Add(duration)
			if overlapsReserved(cursor, end, input.Reserved) {
				continue
			}
			option := Option{
				ID:        fmt.Sprintf("%s:candidate:%d", input.Request.ID, cursor.Unix()),
				RequestID: input.Request.ID, Type: OptionMeeting,
				StartAt: timePointer(cursor), EndAt: timePointer(end),
				Score: candidateScore(segment, cursor, end), CreatedAt: input.Now,
			}
			candidates = append(candidates, scoredOption{option: option, score: option.Score})
		}
	}
	sort.SliceStable(candidates, func(left, right int) bool {
		if candidates[left].score == candidates[right].score {
			return candidates[left].option.StartAt.Before(*candidates[right].option.StartAt)
		}
		return candidates[left].score > candidates[right].score
	})
	limit := len(candidates)
	if limit > 3 {
		limit = 3
	}
	options := make([]Option, 0, limit)
	for _, candidate := range candidates[:limit] {
		candidate.option.Score = 0
		options = append(options, candidate.option)
	}
	if len(options) == 0 {
		return []Option{asyncCandidate(input.Request, input.Now)}, nil
	}
	return options, nil
}

func candidateScore(segment projection.ScheduleProjection, startAt, endAt time.Time) int {
	score := map[policy.Availability]int{
		policy.Available: 100, policy.Limited: 40, policy.Unknown: -100,
	}[segment.State.Availability]
	score += map[policy.Reschedulability]int{
		policy.RescheduleHigh: 30, policy.RescheduleMedium: 10,
		policy.RescheduleLow: -20, policy.RescheduleFixed: -100,
	}[segment.State.Reschedulability]
	score += 20
	before := startAt.Sub(segment.StartAt)
	after := segment.EndAt.Sub(endAt)
	if before > 0 && before < candidateStep {
		score -= 20
	}
	if after > 0 && after < candidateStep {
		score -= 20
	}
	return score
}

func asyncCandidate(value CoordinationRequest, now time.Time) Option {
	responseBy := value.DeadlineAt
	return Option{
		ID: value.ID + ":async", RequestID: value.ID, Type: OptionAsync,
		ResponseBy: &responseBy, CreatedAt: now,
	}
}

func overlapsReserved(startAt, endAt time.Time, reserved []ReservedRange) bool {
	for _, value := range reserved {
		if startAt.Before(value.EndAt) && value.StartAt.Before(endAt) {
			return true
		}
	}
	return false
}

func ceilCandidateStep(value time.Time) time.Time {
	seconds := int64(candidateStep / time.Second)
	floor := time.Unix(value.Unix()/seconds*seconds, 0).UTC()
	if floor.Equal(value) {
		return floor
	}
	return floor.Add(candidateStep)
}

func timePointer(value time.Time) *time.Time {
	return &value
}
