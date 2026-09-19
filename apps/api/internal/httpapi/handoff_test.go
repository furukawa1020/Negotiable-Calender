package httpapi

import (
	"context"
	"errors"
	coord "github.com/negotiable-calendar/negotiable-calendar/apps/api/internal/request"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type handoffTestStore struct {
	stubRequestStore
	commits   int
	commitErr error
}

func (s *handoffTestStore) InspectHandoff(_ context.Context, id, actor, org, to string) (coord.CoordinationRequest, bool, error) {
	r, e := coord.ValidateHandoff(s.value, actor, org, to, time.Now().UTC())
	if e != nil || r {
		return coord.CoordinationRequest{}, r, e
	}
	return s.value, false, nil
}
func (s *handoffTestStore) Handoff(_ context.Context, id, actor, org, to string, options []coord.Option) (bool, error) {
	if s.commitErr != nil {
		return false, s.commitErr
	}
	r, e := coord.ValidateHandoff(s.value, actor, org, to, time.Now().UTC())
	if e != nil || r {
		return r, e
	}
	e = coord.ApplyHandoff(&s.value, actor, to, options, time.Now().UTC())
	if e == nil {
		s.commits++
	}
	return false, e
}

func TestHandoffRouteRegeneratesAndReplaysWithoutDisclosure(t *testing.T) {
	now := time.Now().UTC()
	store := &handoffTestStore{stubRequestStore: stubRequestStore{value: coord.CoordinationRequest{ID: "r", OrganizationID: "org", RequesterUserID: "alice", TargetUserID: "bob", Type: coord.Review, Title: "private title", DurationMinutes: 15, DeadlineAt: now.Add(time.Hour), SyncPreference: coord.Either, Priority: coord.PriorityNormal, Status: coord.Suggested, CreatedAt: now, UpdatedAt: now}}}
	projections, notes, audits := &stubProjectionStore{}, &stubNotificationStore{}, &stubAuditStore{}
	handler := NewWithStores(stubDatabase{}, &stubPolicyStore{}, projections, &stubOrganizationStore{}, store, notes, audits, "", testLogger())
	send := func(actor, org, to string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(http.MethodPost, "/api/v1/requests/r/delegate", strings.NewReader(`{"delegateUserId":"`+to+`"}`))
		r.Header.Set("X-Demo-User-ID", actor)
		r.Header.Set("X-Organization-ID", org)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w
	}
	for _, input := range []struct {
		actor, org, to string
		status         int
	}{{"bob", "", "carol", 401}, {"outsider", "org", "carol", 404}, {"bob", "other", "carol", 404}, {"bob", "org", "alice", 409}, {"bob", "org", "bad/id", 400}} {
		if got := send(input.actor, input.org, input.to); got.Code != input.status {
			t.Fatal(input, got.Code, got.Body.String())
		}
	}
	projections.err = errors.New("private storage detail")
	if got := send("bob", "org", "carol"); got.Code != 503 || store.commits != 0 || strings.Contains(got.Body.String(), "private storage") {
		t.Fatal("candidate failure mutated state")
	}
	projections.err = nil
	store.commitErr = coord.ErrCreationForbidden
	if got := send("bob", "org", "carol"); got.Code != 403 {
		t.Fatal("commit authority bypass")
	}
	store.commitErr = nil
	if got := send("bob", "org", "carol"); got.Code != 200 || !strings.Contains(got.Body.String(), `"handedOff":true`) {
		t.Fatal(got.Code, got.Body.String())
	}
	if store.value.TargetUserID != "carol" || store.value.Options[0].Type != coord.OptionAsync || store.commits != 1 || len(notes.values) != 0 || len(audits.values) != 0 {
		t.Fatal("incorrect handoff/effects")
	}
	store.value.Status = coord.Async
	store.value.AsyncMessage = "private answer"
	projections.err = errors.New("should not regenerate")
	if got := send("bob", "org", "carol"); got.Code != 200 || got.Header().Get("Idempotency-Replayed") != "true" || strings.Contains(got.Body.String(), "private") || store.commits != 1 {
		t.Fatal("unsafe replay", got.Code, got.Body.String())
	}
	if got := send("bob", "org", "dave"); got.Code != 409 {
		t.Fatal("changed recipient accepted")
	}
}
