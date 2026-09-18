package request

import (
	"github.com/negotiable-calendar/negotiable-calendar/apps/api/internal/policy"
	"github.com/negotiable-calendar/negotiable-calendar/apps/api/internal/projection"
	"testing"
	"time"
)

func TestCandidatesRejectExpiredFutureAndUnknownProjections(t *testing.T) {
	now := time.Date(2026, 9, 18, 0, 0, 0, 0, time.UTC)
	for _, scenario := range []string{"expired", "future", "unknown"} {
		t.Run(scenario, func(t *testing.T) {
			p := validCandidateProjection(now, now.Add(time.Hour))
			switch scenario {
			case "expired":
				p.ExpiresAt = now
			case "future":
				p.GeneratedAt = now.Add(time.Second)
			case "unknown":
				p.State.Availability = policy.Unknown
			}
			got, err := GenerateCandidates(CandidateInput{Request: validCandidateRequest(now), Projections: []projection.ScheduleProjection{p}, Now: now})
			if err != nil || len(got) != 1 || got[0].Type != OptionAsync {
				t.Fatal("unsafe meeting candidate", got, err)
			}
		})
	}
}
