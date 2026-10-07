package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	coord "github.com/negotiable-calendar/negotiable-calendar/apps/api/internal/request"
)

type reservationRouteStore struct {
	*handoffTestStore
	readErr             error
	reads, historyReads int
	participants        [2]string
	budget              time.Duration
	cancel              context.CancelFunc
}

func (s *reservationRouteStore) LoadConfirmedRanges(ctx context.Context, requester, target string) ([]coord.ReservedRange, error) {
	if s.cancel != nil {
		s.cancel()
	}
	s.reads++
	s.participants = [2]string{requester, target}
	if end, ok := ctx.Deadline(); ok {
		s.budget = time.Until(end)
	}
	return []coord.ReservedRange{}, s.readErr
}
func (s *reservationRouteStore) ListForUser(context.Context, string) ([]coord.CoordinationRequest, error) {
	s.historyReads++
	return nil, errors.New("unbounded history must not be read")
}

type unsupportedReservationStore struct {
	coord.Store
	coord.HandoffStore
}

func TestCandidateRoutesRequireBoundedReservationSource(t *testing.T) {
	for _, flow := range []string{"create", "handoff"} {
		for _, scenario := range []string{"success", "unavailable", "corrupt", "unsupported", "cancelled"} {
			t.Run(flow+"/"+scenario, func(t *testing.T) {
				now := time.Now().UTC()
				base := &handoffTestStore{stubRequestStore: stubRequestStore{value: coord.CoordinationRequest{ID: "r", OrganizationID: "org", RequesterUserID: "alice", TargetUserID: "bob", Type: coord.Meeting, Title: "private title", DurationMinutes: 15, DeadlineAt: now.Add(time.Hour), SyncPreference: coord.Either, Priority: coord.PriorityNormal, Status: coord.Suggested, CreatedAt: now, UpdatedAt: now}}}
				store := &reservationRouteStore{handoffTestStore: base}
				var selected coord.Store = store
				switch scenario {
				case "unavailable":
					store.readErr = errors.New("private database detail")
				case "corrupt":
					store.readErr = coord.ErrAvailabilityChanged
				case "unsupported":
					selected = &unsupportedReservationStore{Store: store, HandoffStore: store}
				}
				notes, audits := &stubNotificationStore{}, &stubAuditStore{}
				handler := NewWithStores(stubDatabase{}, &stubPolicyStore{}, &stubProjectionStore{}, &stubOrganizationStore{}, selected, notes, audits, "", testLogger())
				path, actor, body := "/api/v1/requests/r/delegate", "bob", `{"delegateUserId":"carol"}`
				wantParticipants := [2]string{"alice", "carol"}
				if flow == "create" {
					path, actor = "/api/v1/requests", "alice"
					payload, _ := json.Marshal(map[string]any{"targetUserId": "bob", "type": "meeting", "title": "new request", "durationMinutes": 15, "deadlineAt": now.Add(time.Hour), "syncPreference": "either", "priority": "normal"})
					body = string(payload)
					wantParticipants = [2]string{"alice", "bob"}
				}
				r := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
				if scenario == "cancelled" {
					ctx, cancel := context.WithCancel(r.Context())
					defer cancel()
					store.cancel = cancel
					r = r.WithContext(ctx)
				}
				r.Header.Set("X-Demo-User-ID", actor)
				r.Header.Set("X-Organization-ID", "org")
				w := httptest.NewRecorder()
				handler.ServeHTTP(w, r)
				want := 503
				if scenario == "success" {
					want = 200
					if flow == "create" {
						want = 201
					}
				}
				if scenario == "corrupt" {
					want = 409
				}
				if w.Code != want || store.historyReads != 0 || strings.Contains(w.Body.String(), "private database") {
					t.Fatalf("status=%d body=%s historyReads=%d", w.Code, w.Body, store.historyReads)
				}
				if scenario != "unsupported" && (store.reads != 1 || store.participants != wantParticipants || store.budget <= 0 || store.budget > coord.ReservationReadTimeout) {
					t.Fatalf("wrong bounded source invocation: %+v", store)
				}
				if scenario != "success" && (base.commits != 0 || base.value.ID != "r" || base.value.TargetUserID != "bob" || len(notes.values) != 0 || len(audits.values) != 0) {
					t.Fatal("source failure mutated request/effects")
				}
			})
		}
	}
}
