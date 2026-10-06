package httpapi

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	coord "github.com/negotiable-calendar/negotiable-calendar/apps/api/internal/request"
)

func TestRescheduleReplayReturnsLatestStateWithoutEffects(t *testing.T) {
	for _, status := range []coord.Status{coord.Accepted, coord.Cancelled, coord.Completed} {
		store := &scopedRescheduleStub{rescheduleStore: rescheduleStore{failure: coord.ErrRescheduleRepeated}}
		store.value = coord.CoordinationRequest{ID: "r", OrganizationID: "org", RequesterUserID: "alice", TargetUserID: "bob", Status: status, AcceptedOptionID: "new", RescheduleProposal: &coord.RescheduleProposal{ID: "proposal-new", ProposerUserID: "alice", ExpectedOptionID: "old", Status: "accepted"}}
		notes, audits := &stubNotificationStore{}, &stubAuditStore{}
		handler := NewWithStores(nil, nil, nil, nil, store, notes, audits, "", testLogger())
		req := httptest.NewRequest("POST", "/api/v1/requests/r/reschedule", strings.NewReader(`{"action":"propose","proposalId":"proposal-new","expectedOptionId":"old","startAt":"2099-01-01T00:00:00Z"}`))
		req.Header.Set("X-Demo-User-ID", "alice")
		req.Header.Set("X-Organization-ID", "org")
		res := httptest.NewRecorder()
		handler.ServeHTTP(res, req)
		var got coord.CoordinationRequest
		if err := json.Unmarshal(res.Body.Bytes(), &got); err != nil {
			t.Fatal(err)
		}
		if res.Code != 200 || res.Header().Get("Idempotency-Replayed") != "true" || got.Status != status || got.RescheduleProposal == nil || got.RescheduleProposal.Status != "accepted" || got.AcceptedOptionID != "new" {
			t.Fatalf("latest state lost: %d %s", res.Code, res.Body)
		}
		if store.scopedCalls != 1 || store.calls != 0 || len(notes.values) != 0 || len(audits.values) != 0 {
			t.Fatal("replayed effects or unscoped call")
		}
	}
}
