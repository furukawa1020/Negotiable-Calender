package httpapi

import (
	"context"
	"github.com/negotiable-calendar/negotiable-calendar/apps/api/internal/projection"
	coord "github.com/negotiable-calendar/negotiable-calendar/apps/api/internal/request"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type interruptedPlanningStore struct {
	*boundedPlanningStub
	stage  string
	cancel context.CancelFunc
}

func (s *interruptedPlanningStore) GetForUser(ctx context.Context, id, actor string) (coord.CoordinationRequest, error) {
	value, err := s.boundedPlanningStub.GetForUser(ctx, id, actor)
	if s.stage == "request" {
		s.cancel()
	}
	return value, err
}
func (s *interruptedPlanningStore) LoadPlanningSources(ctx context.Context, target, requester string, from, to time.Time) ([]projection.ScheduleProjection, []coord.CoordinationRequest, error) {
	if s.stage == "sources" {
		s.cancel()
	}
	if s.stage == "expired-source" {
		s.projections.values[0].ExpiresAt = time.Now().UTC().Add(-time.Nanosecond)
	}
	return s.boundedPlanningStub.LoadPlanningSources(ctx, target, requester, from, to)
}
func TestPlanningNeverPublishesInterruptedOrExpiredEvidence(t *testing.T) {
	for _, stage := range []string{"entry", "request", "sources", "expired-source"} {
		t.Run(stage, func(t *testing.T) {
			r, p, o := planningFixture()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if stage == "entry" {
				cancel()
			}
			s := &interruptedPlanningStore{boundedPlanningStub: &boundedPlanningStub{stubRequestStore: r, projections: p}, stage: stage, cancel: cancel}
			h := New(nil, nil, p, o, s, "", testLogger())
			req := httptest.NewRequest("GET", "/api/v1/requests/private-request/planning-preview", nil).WithContext(ctx)
			req.Header.Set("X-Demo-User-ID", "owner")
			req.Header.Set("X-Organization-ID", "workspace")
			w := httptest.NewRecorder()
			h.ServeHTTP(w, req)
			want := 503
			if stage == "expired-source" {
				want = 409
			}
			if w.Code != want || strings.Contains(w.Body.String(), "revision") || strings.Contains(w.Body.String(), "private") {
				t.Fatal(w.Code, w.Body)
			}
			if stage == "entry" && s.reads != 0 || (stage == "entry" || stage == "request") && s.loads != 0 {
				t.Fatal("read continued after cancellation")
			}
		})
	}
}
