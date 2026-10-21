package httpapi

import (
	"context"
	"errors"
	"testing"
	"time"

	cal "github.com/negotiable-calendar/negotiable-calendar/apps/api/internal/calendar"
	coord "github.com/negotiable-calendar/negotiable-calendar/apps/api/internal/request"
)

type cancelCandidateReadStore struct {
	*candidateCalendarRouteStore
	cancel context.CancelFunc
}

func (s *cancelCandidateReadStore) LoadRequesterCalendar(ctx context.Context, user string) (cal.CandidateAvailability, error) {
	s.cancel()
	return s.candidateCalendarRouteStore.LoadRequesterCalendar(ctx, user)
}

func TestCandidateReadCancellationAdmission(t *testing.T) {
	for _, before := range []bool{false, true} {
		ctx, cancel := context.WithCancel(context.Background())
		now := time.Now().UTC()
		snapshot, err := cal.NewCandidateAvailability(cal.SourceState{}, nil, now)
		if err != nil {
			t.Fatal(err)
		}
		s := &cancelCandidateReadStore{candidateCalendarRouteStore: &candidateCalendarRouteStore{snapshot: snapshot}, cancel: cancel}
		api := &API{requests: s}
		v := coord.CoordinationRequest{ID: "r", OrganizationID: "org", RequesterUserID: "alice", TargetUserID: "bob", Type: coord.Meeting, Title: "request", DurationMinutes: 30, DeadlineAt: now.Add(time.Hour), SyncPreference: coord.Either, Priority: coord.PriorityNormal, Status: coord.Pending, CreatedAt: now, UpdatedAt: now}
		if before {
			cancel()
		}
		got, err := api.generateRequestCandidates(ctx, v, nil, nil, now)
		cancel()
		if !errors.Is(err, context.Canceled) || got != nil {
			t.Fatal("cancelled evidence generated options", err)
		}
		wantReads := 1
		if before {
			wantReads = 0
		}
		if s.reads != wantReads {
			t.Fatal("unexpected dependency call", s.reads)
		}
	}
}
