package request

import (
	"fmt"
	"math"
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
	if int64(input.Request.DurationMinutes) > math.MaxInt64/int64(time.Minute) {
		return nil, fmt.Errorf("request duration exceeds supported range")
	}
	duration := time.Duration(input.Request.DurationMinutes) * time.Minute
	spans, err := candidateSpans(input)
	if err != nil {
		return nil, err
	}
	// Retain only the best three, not every possible start in a long calendar.
	candidates := make([]scoredOption, 0, 4)
	for first := 0; first < len(spans); {
		last := first
		for last+1 < len(spans) && spans[last].end.Equal(spans[last+1].start) {
			last++
		}
		runStart, runEnd := spans[first].start, spans[last].end
		start := ceilCandidateStep(runStart)
		if !start.After(input.Now) {
			start = ceilCandidateStep(input.Now)
			if !start.After(input.Now) {
				start = start.Add(candidateStep)
			}
		}
		endLimit := runEnd
		if input.Request.DeadlineAt.Before(endLimit) {
			endLimit = input.Request.DeadlineAt
		}
		part := first
		for cursor := start; !cursor.Add(duration).After(endLimit); cursor = cursor.Add(candidateStep) {
			end := cursor.Add(duration)
			if overlapsReserved(cursor, end, input.Reserved) {
				continue
			}
			for part <= last && !spans[part].end.After(cursor) {
				part++
			}
			// The least favorable covered span determines quality, not the first bucket.
			score := spans[part].score
			for i := part + 1; i <= last && spans[i].start.Before(end); i++ {
				score = min(score, spans[i].score)
			}
			score += candidateFragmentPenalty(runStart, runEnd, cursor, end)
			option := Option{
				ID:        fmt.Sprintf("%s:candidate:%d", input.Request.ID, cursor.Unix()),
				RequestID: input.Request.ID, Type: OptionMeeting,
				StartAt: timePointer(cursor), EndAt: timePointer(end),
				CreatedAt: input.Now,
			}
			candidates = append(candidates, scoredOption{option: option, score: score})
			sort.Slice(candidates, func(left, right int) bool {
				if candidates[left].score == candidates[right].score {
					return candidates[left].option.StartAt.Before(*candidates[right].option.StartAt)
				}
				return candidates[left].score > candidates[right].score
			})
			if len(candidates) > 3 {
				candidates = candidates[:3]
			}
		}
		first = last + 1
	}
	options := make([]Option, 0, len(candidates))
	for _, candidate := range candidates {
		options = append(options, candidate.option)
	}
	if len(options) == 0 {
		return []Option{asyncCandidate(input.Request, input.Now)}, nil
	}
	return options, nil
}

func candidateQuality(segment projection.ScheduleProjection) int {
	score := map[policy.Availability]int{
		policy.Available: 100, policy.Limited: 40, policy.Unknown: -100,
	}[segment.State.Availability]
	score += map[policy.Reschedulability]int{
		policy.RescheduleHigh: 30, policy.RescheduleMedium: 10,
		policy.RescheduleLow: -20, policy.RescheduleFixed: -100,
	}[segment.State.Reschedulability]
	return score + 20
}

func candidateFragmentPenalty(runStart, runEnd, startAt, endAt time.Time) int {
	score := 0
	before := startAt.Sub(runStart)
	after := runEnd.Sub(endAt)
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
