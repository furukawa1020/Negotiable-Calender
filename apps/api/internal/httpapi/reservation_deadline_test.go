package httpapi

import (
	"context"
	"errors"
	"testing"
	"time"

	coord "github.com/negotiable-calendar/negotiable-calendar/apps/api/internal/request"
)

type reservationDeadlineStore struct {
	*stubRequestStore
	calls  int
	expire bool
}

func (s *reservationDeadlineStore) LoadConfirmedRanges(ctx context.Context, _, _ string) ([]coord.ReservedRange, error) {
	s.calls++
	if s.expire {
		<-ctx.Done()
	}
	return []coord.ReservedRange{{}}, nil
}
func TestReservationEvidenceHonorsContextBudget(t *testing.T) {
	for _, scenario := range []string{"cancelled", "deadline", "success"} {
		t.Run(scenario, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
			defer cancel()
			if scenario == "cancelled" {
				cancel()
			}
			s := &reservationDeadlineStore{stubRequestStore: &stubRequestStore{}, expire: scenario == "deadline"}
			api := &API{requests: s}
			values, err := api.candidateReservations(ctx, "requester", "target")
			if scenario == "success" {
				if err != nil || len(values) != 1 {
					t.Fatalf("values=%v error=%v", values, err)
				}
				return
			}
			want := context.DeadlineExceeded
			if scenario == "cancelled" {
				want = context.Canceled
			}
			if !errors.Is(err, want) || values != nil {
				t.Fatalf("values=%v error=%v", values, err)
			}
			if scenario == "cancelled" && s.calls != 0 {
				t.Fatal("cancelled request read storage")
			}
		})
	}
}
