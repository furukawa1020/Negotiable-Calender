package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/negotiable-calendar/negotiable-calendar/apps/api/internal/policy"
	"github.com/negotiable-calendar/negotiable-calendar/apps/api/internal/projection"
	req "github.com/negotiable-calendar/negotiable-calendar/apps/api/internal/request"
)

func planningFixture() (*stubRequestStore, *stubProjectionStore, *stubOrganizationStore) {
	now := time.Now().UTC()
	start, end := now.Add(time.Hour), now.Add(90*time.Minute)
	value := req.CoordinationRequest{ID: "private-request", OrganizationID: "workspace", RequesterUserID: "requester", TargetUserID: "owner", Title: "private-title", Status: req.Suggested, DeadlineAt: now.Add(24 * time.Hour), UpdatedAt: now}
	value.Options = []req.Option{{ID: "private-option", RequestID: value.ID, Type: req.OptionMeeting, StartAt: &start, EndAt: &end, CreatedAt: now}}
	segment := projection.ScheduleProjection{ID: "private-segment", UserID: "owner", StartAt: start, EndAt: end, GeneratedAt: now.Add(-time.Minute), ExpiresAt: now.Add(time.Minute), ExpectedResponseBucket: "soon", State: policy.InteractionState{Availability: policy.Available, Interruptibility: policy.InterruptNormal, Requestability: policy.RequestOpen, Reschedulability: policy.RescheduleMedium}}
	return &stubRequestStore{value: value}, &stubProjectionStore{values: []projection.ScheduleProjection{segment}}, &stubOrganizationStore{}
}

type boundedPlanningStub struct {
	*stubRequestStore
	projections  *stubProjectionStore
	reads, loads int
}

func (s *boundedPlanningStub) GetForUser(ctx context.Context, id, user string) (req.CoordinationRequest, error) {
	s.reads++
	return s.stubRequestStore.GetForUser(ctx, id, user)
}

func (s *boundedPlanningStub) LoadPlanningSources(ctx context.Context, target, requester string, from, to time.Time) ([]projection.ScheduleProjection, []req.CoordinationRequest, error) {
	s.loads++
	if s.projections.err != nil {
		return nil, nil, s.projections.err
	}
	if s.err != nil {
		return nil, nil, s.err
	}
	return s.projections.values, s.values, nil
}

func planningCall(requests *stubRequestStore, projections *stubProjectionStore, organizations *stubOrganizationStore, user, org string) *httptest.ResponseRecorder {
	store := &boundedPlanningStub{stubRequestStore: requests, projections: projections}
	handler := New(&stubDatabase{}, &stubPolicyStore{}, projections, organizations, store, "http://localhost:5173", slog.New(slog.NewTextHandler(io.Discard, nil)))
	r := httptest.NewRequest(http.MethodGet, "/api/v1/requests/private-request/planning-preview", nil)
	r.Header.Set("X-Demo-User-ID", user)
	r.Header.Set("X-Organization-ID", org)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	return w
}

func TestPlanningPreviewOwnerBoundaryAndFreshness(t *testing.T) {
	tests := []struct {
		name      string
		change    func(*stubRequestStore, *stubProjectionStore, *stubOrganizationStore)
		user, org string
		status    int
	}{
		{"owner", nil, "owner", "workspace", 200},
		{"anonymous", nil, "", "workspace", 401},
		{"requester", nil, "requester", "workspace", 403},
		{"different workspace", nil, "owner", "other", 403},
		{"removed member", func(_ *stubRequestStore, _ *stubProjectionStore, o *stubOrganizationStore) {
			o.members = map[string]bool{}
		}, "owner", "workspace", 403},
		{"hidden request", func(r *stubRequestStore, _ *stubProjectionStore, _ *stubOrganizationStore) {
			r.getErr = req.ErrNotFound
		}, "owner", "workspace", 404},
		{"closed request", func(r *stubRequestStore, _ *stubProjectionStore, _ *stubOrganizationStore) {
			r.value.Status = req.Accepted
		}, "owner", "workspace", 409},
		{"expired deadline", func(r *stubRequestStore, _ *stubProjectionStore, _ *stubOrganizationStore) {
			r.value.DeadlineAt = time.Now().UTC().Add(-time.Second)
		}, "owner", "workspace", 409},
		{"expired projection", func(_ *stubRequestStore, p *stubProjectionStore, _ *stubOrganizationStore) {
			p.values[0].ExpiresAt = time.Now().UTC().Add(-time.Second)
		}, "owner", "workspace", 409},
		{"projection gap", func(_ *stubRequestStore, p *stubProjectionStore, _ *stubOrganizationStore) { p.values = nil }, "owner", "workspace", 409},
		{"private busy", func(_ *stubRequestStore, p *stubProjectionStore, _ *stubOrganizationStore) {
			p.values[0].State.Availability = policy.Unavailable
		}, "owner", "workspace", 409},
		{"source read failure", func(_ *stubRequestStore, p *stubProjectionStore, _ *stubOrganizationStore) {
			p.err = errors.New("secret backend detail")
		}, "owner", "workspace", 503},
		{"accepted conflict", func(r *stubRequestStore, _ *stubProjectionStore, _ *stubOrganizationStore) {
			b := r.value
			b.ID = "booking"
			b.Status = req.Accepted
			b.AcceptedOptionID = b.Options[0].ID
			r.values = []req.CoordinationRequest{b}
		}, "owner", "workspace", 409},
		{"corrupt accepted record", func(r *stubRequestStore, _ *stubProjectionStore, _ *stubOrganizationStore) {
			r.values = []req.CoordinationRequest{{ID: "bad", Status: req.Accepted}}
		}, "owner", "workspace", 409},
		{"oversized options", func(r *stubRequestStore, _ *stubProjectionStore, _ *stubOrganizationStore) {
			r.value.Options = make([]req.Option, 13)
		}, "owner", "workspace", 409},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			r, p, o := planningFixture()
			if test.change != nil {
				test.change(r, p, o)
			}
			w := planningCall(r, p, o, test.user, test.org)
			if w.Code != test.status {
				t.Fatalf("status %d: %s", w.Code, w.Body.String())
			}
			if strings.Contains(w.Body.String(), "secret backend") {
				t.Fatal("leaked internal error")
			}
		})
	}
}

func TestPlanningPreviewMinimizesDataAndFingerprintsChanges(t *testing.T) {
	r, p, o := planningFixture()
	read := func() planningPreview {
		t.Helper()
		w := planningCall(r, p, o, "owner", "workspace")
		if w.Code != 200 {
			t.Fatalf("%d %s", w.Code, w.Body.String())
		}
		if w.Header().Get("Cache-Control") != "no-store" {
			t.Fatal("preview is cacheable")
		}
		if strings.Contains(w.Body.String(), "private-") || strings.Contains(w.Body.String(), "owner") {
			t.Fatal("private fields in preview")
		}
		var value planningPreview
		if err := json.Unmarshal(w.Body.Bytes(), &value); err != nil {
			t.Fatal(err)
		}
		return value
	}
	first := read()
	if len(first.Revision) != 64 || len(first.Candidates) != 1 || first.Candidates[0].ID != "c1" {
		t.Fatalf("unexpected preview %#v", first)
	}
	if !first.ExpiresAt.Equal(p.values[0].ExpiresAt) {
		t.Fatal("expiry not clamped to projection")
	}
	if read().Revision != first.Revision {
		t.Fatal("unchanged source should be stable")
	}
	r.value.UpdatedAt = r.value.UpdatedAt.Add(time.Second)
	if read().Revision == first.Revision {
		t.Fatal("request revision change was missed")
	}
	next := read()
	p.values[0].GeneratedAt = p.values[0].GeneratedAt.Add(time.Second)
	if read().Revision == next.Revision {
		t.Fatal("projection change was missed")
	}
	if r.respondID != "" {
		t.Fatal("preview mutated request")
	}
}
