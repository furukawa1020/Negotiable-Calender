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

type proposalTestStore struct {
	stubRequestStore
	err           error
	confirmations int
}

func (s *proposalTestStore) ProposeMeeting(_ context.Context, id, actor, org string, start, end time.Time) (coord.Option, bool, error) {
	if s.err != nil {
		return coord.Option{}, false, s.err
	}
	return coord.PrepareProposal(&s.value, actor, org, start, end, time.Now().UTC())
}
func (s *proposalTestStore) ConfirmMeeting(_ context.Context, id, actor, org, option string) error {
	if s.err != nil {
		return s.err
	}
	if org != s.value.OrganizationID {
		return coord.ErrNotFound
	}
	if e := coord.AuthorizeConfirmation(s.value, actor, option); e != nil {
		return e
	}
	if s.value.Status == coord.Accepted && s.value.AcceptedOptionID == option {
		return coord.ErrAlreadyAccepted
	}
	s.value.Status, s.value.AcceptedOptionID = coord.Accepted, option
	s.confirmations++
	return nil
}
func TestCounterproposalRoutes(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Microsecond)
	s := &proposalTestStore{stubRequestStore: stubRequestStore{value: coord.CoordinationRequest{ID: "r", OrganizationID: "org", RequesterUserID: "alice", TargetUserID: "bob", Status: coord.Suggested, DurationMinutes: 30, DeadlineAt: now.Add(24 * time.Hour)}}}
	notes, audits := &stubNotificationStore{}, &stubAuditStore{}
	h := NewWithStores(stubDatabase{}, &stubPolicyStore{}, &stubProjectionStore{}, &stubOrganizationStore{}, s, notes, audits, "", testLogger())
	send := func(action, actor, org, body string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(http.MethodPost, "/api/v1/requests/r/"+action, strings.NewReader(body))
		r.Header.Set("X-Demo-User-ID", actor)
		r.Header.Set("X-Organization-ID", org)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	body := func(start, end time.Time) string {
		b, _ := json.Marshal(map[string]time.Time{"startAt": start, "endAt": end})
		return string(b)
	}
	valid := body(now.Add(time.Hour), now.Add(90*time.Minute))
	spoofed, _ := json.Marshal(map[string]any{"startAt": now.Add(time.Hour), "endAt": now.Add(90 * time.Minute), "proposedByUserId": "alice"})
	for _, c := range []struct {
		actor, org, body string
		status           int
	}{{"bob", "", valid, 401}, {"alice", "org", valid, 404}, {"bob", "other", valid, 404}, {"bob", "org", body(now.Add(time.Hour), now.Add(65*time.Minute)), 422}, {"bob", "org", string(spoofed), 400}} {
		if w := send("suggest", c.actor, c.org, c.body); w.Code != c.status {
			t.Fatal(c, w.Code, w.Body.String())
		}
	}
	s.err = errors.New("private datastore detail")
	if w := send("suggest", "bob", "org", valid); w.Code != 503 || strings.Contains(w.Body.String(), "datastore") {
		t.Fatal(w)
	}
	s.err = nil
	w := send("suggest", "bob", "org", valid)
	var option coord.Option
	if w.Code != 201 || json.Unmarshal(w.Body.Bytes(), &option) != nil || option.ProposedByUserID != "bob" {
		t.Fatal(w.Code, w.Body.String())
	}
	if w := send("suggest", "bob", "org", valid); w.Code != 200 || w.Header().Get("Idempotency-Replayed") != "true" {
		t.Fatal(w)
	}
	accept := `{"optionId":"` + option.ID + `"}`
	if w := send("accept", "bob", "org", accept); w.Code != 409 {
		t.Fatal("self acceptance", w.Code)
	}
	if w := send("accept", "alice", "other", accept); w.Code != 409 {
		t.Fatal("cross org", w.Code)
	}
	if w := send("accept", "alice", "org", accept); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	if w := send("accept", "alice", "org", accept); w.Code != 200 || w.Header().Get("Idempotency-Replayed") != "true" {
		t.Fatal("lost replay")
	}
	if s.confirmations != 1 || len(notes.values) != 0 || len(audits.values) != 0 {
		t.Fatal("duplicate HTTP effects")
	}
}
