package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	cal "github.com/negotiable-calendar/negotiable-calendar/apps/api/internal/calendar"
	"github.com/negotiable-calendar/negotiable-calendar/apps/api/internal/policy"
	"github.com/negotiable-calendar/negotiable-calendar/apps/api/internal/projection"
	coord "github.com/negotiable-calendar/negotiable-calendar/apps/api/internal/request"
)

type candidateCalendarRouteStore struct {
	*handoffTestStore
	snapshot       cal.CandidateAvailability
	readErr        error
	reads, creates int
	user           string
	budget         time.Duration
	committed      bool
}

func (s *candidateCalendarRouteStore) LoadRequesterCalendar(ctx context.Context, user string) (cal.CandidateAvailability, error) {
	s.reads++
	s.user = user
	if end, ok := ctx.Deadline(); ok {
		s.budget = time.Until(end)
	}
	return s.snapshot, s.readErr
}
func (s *candidateCalendarRouteStore) LookupCreation(_ context.Context, v coord.CoordinationRequest) (coord.CoordinationRequest, error) {
	if !s.committed {
		return coord.CoordinationRequest{}, coord.ErrNotFound
	}
	if !coord.SameCreation(s.value, v) {
		return coord.CoordinationRequest{}, coord.ErrCreationConflict
	}
	return s.value, nil
}
func (s *candidateCalendarRouteStore) CreateOnce(_ context.Context, v coord.CoordinationRequest) (bool, error) {
	s.value = v
	s.creates++
	s.committed = true
	return true, nil
}

type missingCandidateCalendarStore struct {
	coord.Store
	coord.CreationStore
	coord.HandoffStore
	coord.ReservationStore
}

func TestCandidateCalendarRoutesFilterFailClosedAndReplay(t *testing.T) {
	for _, flow := range []string{"create", "handoff"} {
		for _, scenario := range []string{"busy", "unavailable", "failure", "zero", "unsupported", "async"} {
			t.Run(flow+"/"+scenario, func(t *testing.T) {
				now := time.Now().UTC().Truncate(time.Microsecond)
				start := now.Truncate(time.Hour).Add(2 * time.Hour)
				end := start.Add(3 * time.Hour)
				observed := now.Add(-time.Minute)
				snapshot, err := cal.NewCandidateAvailability(cal.SourceState{Managed: true, Snapshot: cal.SourceSnapshot{Revision: "private-receipt", ObservedAt: observed, From: start, To: end}, Connection: &cal.Connection{LastSyncedAt: &observed}}, []cal.BusyInterval{{StartAt: start, EndAt: start.Add(time.Hour), Status: "busy"}}, now)
				if err != nil {
					t.Fatal(err)
				}
				v := coord.CoordinationRequest{ID: "r", OrganizationID: "org", RequesterUserID: "alice", TargetUserID: "bob", Type: coord.Meeting, Title: "request", DurationMinutes: 30, DeadlineAt: end, SyncPreference: coord.Either, Priority: coord.PriorityNormal, Status: coord.Suggested, CreatedAt: now, UpdatedAt: now}
				if scenario == "async" {
					v.SyncPreference = coord.AsyncPreferred
				}
				store := &candidateCalendarRouteStore{handoffTestStore: &handoffTestStore{stubRequestStore: stubRequestStore{value: v}}, snapshot: snapshot}
				var selected coord.Store = store
				switch scenario {
				case "unavailable":
					store.readErr = cal.ErrSourceUnavailable
				case "failure", "async":
					store.readErr = errors.New("PRIVATE_SOURCE_DETAIL")
				case "zero":
					store.snapshot = cal.CandidateAvailability{}
				case "unsupported":
					selected = &missingCandidateCalendarStore{store, store, store, store}
				}
				target, path, actor, body := "carol", "/api/v1/requests/r/delegate", "bob", `{"delegateUserId":"carol"}`
				if flow == "create" {
					target, path, actor = "bob", "/api/v1/requests", "alice"
					data, _ := json.Marshal(map[string]any{"targetUserId": "bob", "type": "meeting", "title": "request", "durationMinutes": 30, "deadlineAt": end, "syncPreference": v.SyncPreference, "priority": "normal"})
					body = string(data)
				}
				projections := &stubProjectionStore{values: []projection.ScheduleProjection{{ID: "open", UserID: target, StartAt: start, EndAt: end, GeneratedAt: now, ExpiresAt: now.Add(time.Hour), ExpectedResponseBucket: "soon", State: policy.InteractionState{Availability: policy.Available, Interruptibility: policy.InterruptOpen, Requestability: policy.RequestOpen, Reschedulability: policy.RescheduleHigh}}}}
				notes, audits := &stubNotificationStore{}, &stubAuditStore{}
				handler := NewWithStores(stubDatabase{}, &stubPolicyStore{}, projections, &stubOrganizationStore{}, selected, notes, audits, "", testLogger())
				send := func() *httptest.ResponseRecorder {
					r := httptest.NewRequest("POST", path, strings.NewReader(body))
					r.Header.Set("X-Demo-User-ID", actor)
					r.Header.Set("X-Organization-ID", "org")
					if flow == "create" {
						r.Header.Set("Idempotency-Key", "candidate-calendar-command-123")
					}
					w := httptest.NewRecorder()
					handler.ServeHTTP(w, r)
					return w
				}
				w := send()
				want := 503
				if scenario == "busy" || scenario == "async" {
					want = 200
					if flow == "create" {
						want = 201
					}
				}
				if scenario == "unavailable" || scenario == "zero" {
					want = 409
				}
				if w.Code != want || strings.Contains(w.Body.String(), "PRIVATE_SOURCE_DETAIL") || strings.Contains(w.Body.String(), "private-receipt") || strings.Contains(w.Body.String(), "BusyStatus") {
					t.Fatal("unsafe result", w.Code, w.Body)
				}
				wantReads := 1
				if scenario == "unsupported" || scenario == "async" {
					wantReads = 0
				}
				if store.reads != wantReads || (wantReads == 1 && (store.user != "alice" || store.budget <= 0 || store.budget > coord.ReservationReadTimeout)) {
					t.Fatal("wrong requester/budget")
				}
				if want >= 400 {
					if store.creates != 0 || store.commits != 0 || store.value.TargetUserID != "bob" || len(notes.values) != 0 || len(audits.values) != 0 {
						t.Fatal("failed source mutated request/effects")
					}
					return
				}
				for _, o := range store.value.Options {
					if scenario == "busy" && (o.Type != coord.OptionMeeting || o.StartAt.Before(start.Add(time.Hour)) || o.EndAt.After(end)) {
						t.Fatal("busy candidate saved")
					}
					if scenario == "async" && o.Type != coord.OptionAsync {
						t.Fatal("async preference ignored")
					}
				}
				if scenario == "busy" && len(store.value.Options) != 3 {
					t.Fatal("did not rank free alternatives")
				}
				store.readErr = errors.New("do not refresh on replay")
				if retry := send(); retry.Code != 200 || retry.Header().Get("Idempotency-Replayed") != "true" || store.reads != wantReads {
					t.Fatal("saved command regenerated candidates", retry.Code, retry.Body)
				}
			})
		}
	}
}
