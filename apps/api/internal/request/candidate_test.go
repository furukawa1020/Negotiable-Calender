package request

import (
	"testing"
	"time"

	"github.com/negotiable-calendar/negotiable-calendar/apps/api/internal/policy"
	"github.com/negotiable-calendar/negotiable-calendar/apps/api/internal/projection"
)

func TestGenerateCandidatesReturnsAtMostThreeAndSkipsReservations(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 8, 27, 6, 0, 0, 0, time.UTC)
	value := validCandidateRequest(now)
	segment := validCandidateProjection(now, now.Add(2*time.Hour))
	options, err := GenerateCandidates(CandidateInput{
		Request: value, Projections: []projection.ScheduleProjection{segment}, Now: now,
		Reserved: []ReservedRange{{StartAt: now, EndAt: now.Add(15 * time.Minute)}},
	})
	if err != nil {
		t.Fatalf("candidate generation failed: %v", err)
	}
	if len(options) != 3 {
		t.Fatalf("expected 3 options, got %d", len(options))
	}
	if options[0].StartAt.Equal(now) {
		t.Fatal("reserved time was proposed")
	}
	for _, option := range options {
		if option.Score != 0 {
			t.Fatal("internal score leaked into candidate output")
		}
	}
}

func TestGenerateCandidatesFallsBackToAsync(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 8, 27, 6, 0, 0, 0, time.UTC)
	value := validCandidateRequest(now)
	segment := validCandidateProjection(now, now.Add(time.Hour))
	segment.State.Requestability = policy.RequestClosed
	options, err := GenerateCandidates(CandidateInput{Request: value, Projections: []projection.ScheduleProjection{segment}, Now: now})
	if err != nil {
		t.Fatalf("candidate generation failed: %v", err)
	}
	if len(options) != 1 || options[0].Type != OptionAsync {
		t.Fatalf("expected async fallback, got %#v", options)
	}
}

func TestGenerateCandidatesHonorsAsyncPreference(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 8, 27, 6, 0, 0, 0, time.UTC)
	value := validCandidateRequest(now)
	value.SyncPreference = AsyncPreferred
	options, err := GenerateCandidates(CandidateInput{
		Request:     value,
		Projections: []projection.ScheduleProjection{validCandidateProjection(now, now.Add(time.Hour))},
		Now:         now,
	})
	if err != nil {
		t.Fatalf("candidate generation failed: %v", err)
	}
	if len(options) != 1 || options[0].Type != OptionAsync {
		t.Fatalf("async preference was not honored: %#v", options)
	}
}

func validCandidateRequest(now time.Time) CoordinationRequest {
	return CoordinationRequest{
		ID: "request-1", OrganizationID: "org-1", RequesterUserID: "member-1", TargetUserID: "manager-1",
		Type: Review, Title: "API review", DurationMinutes: 15, DeadlineAt: now.Add(3 * time.Hour),
		SyncPreference: Either, Priority: PriorityNormal, Status: Pending, CreatedAt: now, UpdatedAt: now,
	}
}

func validCandidateProjection(startAt, endAt time.Time) projection.ScheduleProjection {
	return projection.ScheduleProjection{
		ID: "projection-1", UserID: "manager-1", StartAt: startAt, EndAt: endAt,
		State: policy.InteractionState{
			Availability: policy.Available, Interruptibility: policy.InterruptOpen,
			Requestability: policy.RequestOpen, Reschedulability: policy.RescheduleHigh,
		},
		ExpectedResponseBucket: "within_window", GeneratedAt: startAt, ExpiresAt: endAt.Add(time.Hour),
	}
}
