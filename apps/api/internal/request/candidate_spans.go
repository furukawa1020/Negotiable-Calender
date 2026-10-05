package request

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/negotiable-calendar/negotiable-calendar/apps/api/internal/policy"
)

type candidateSpan struct {
	start, end time.Time
	score      int
}

type candidateBoundary struct {
	at           time.Time
	score, delta int
	blocked      bool
}

// Sweep interval endpoints rather than joining rows blindly. Any overlapping
// closed/stale/unknown row vetoes that interval, just as confirmation does.
// Duplicate rows never duplicate starts; input ordering cannot change ranking.
func candidateSpans(ctx context.Context, input CandidateInput) ([]candidateSpan, error) {
	boundaries := make([]candidateBoundary, 0, 2*len(input.Projections))
	for _, segment := range input.Projections {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if err := segment.Validate(); err != nil {
			return nil, fmt.Errorf("invalid projection: %w", err)
		}
		if segment.UserID != input.Request.TargetUserID {
			return nil, fmt.Errorf("projection target mismatch")
		}
		blocked := segment.State.Requestability != policy.RequestOpen ||
			(segment.State.Availability != policy.Available && segment.State.Availability != policy.Limited) ||
			!segment.ExpiresAt.After(input.Now) || segment.GeneratedAt.After(input.Now)
		score := candidateQuality(segment)
		boundaries = append(boundaries,
			candidateBoundary{at: segment.StartAt, score: score, delta: 1, blocked: blocked},
			candidateBoundary{at: segment.EndAt, score: score, delta: -1, blocked: blocked})
	}
	sort.Slice(boundaries, func(i, j int) bool { return boundaries[i].at.Before(boundaries[j].at) })
	spans := []candidateSpan{}
	// Quality has a small fixed domain (availability x reschedulability), not one
	// entry per row. Overlapping input normalization therefore stays O(n log n).
	active := map[int]int{}
	blocked := 0
	for i := 0; i < len(boundaries); {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		at := boundaries[i].at
		for i < len(boundaries) && boundaries[i].at.Equal(at) {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			boundary := boundaries[i]
			if boundary.blocked {
				blocked += boundary.delta
			} else {
				active[boundary.score] += boundary.delta
				if active[boundary.score] == 0 {
					delete(active, boundary.score)
				}
			}
			i++
		}
		if i == len(boundaries) || blocked > 0 || len(active) == 0 {
			continue
		}
		score, first := 0, true
		for quality := range active {
			if first || quality < score {
				score, first = quality, false
			}
		}
		end := boundaries[i].at
		if len(spans) > 0 && spans[len(spans)-1].end.Equal(at) && spans[len(spans)-1].score == score {
			spans[len(spans)-1].end = end
		} else {
			spans = append(spans, candidateSpan{start: at, end: end, score: score})
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return spans, nil
}
