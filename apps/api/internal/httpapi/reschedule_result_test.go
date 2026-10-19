package httpapi

import (
	"context"
	"errors"
	coord "github.com/negotiable-calendar/negotiable-calendar/apps/api/internal/request"
	"net/http/httptest"
	"strings"
	"testing"
)

type rescheduleResultStore struct {
	scopedRescheduleStub
	readErr error
	readOrg string
	reads   int
}

func (s *rescheduleResultStore) GetInOrganization(_ context.Context, id, actor, org string) (coord.CoordinationRequest, error) {
	s.reads++
	s.readOrg = org
	return s.value, s.readErr
}

type legacyRescheduleReader struct {
	coord.Store
	coord.ScopedRescheduleStore
}

func TestRescheduleResultMustMatchAuthorizedRequest(t *testing.T) {
	for _, replay := range []bool{false, true} {
		for _, scenario := range []string{"valid", "wrong-id", "wrong-org", "outsider", "revoked", "missing", "failed", "legacy"} {
			t.Run(scenario+map[bool]string{false: "/new", true: "/replay"}[replay], func(t *testing.T) {
				s := &rescheduleResultStore{}
				s.value = coord.CoordinationRequest{ID: "r", OrganizationID: "org", RequesterUserID: "alice", TargetUserID: "bob", Title: "PRIVATE"}
				if replay {
					s.failure = coord.ErrRescheduleRepeated
				}
				var store coord.Store = s
				switch scenario {
				case "wrong-id":
					s.value.ID = "other"
				case "wrong-org":
					s.value.OrganizationID = "other"
				case "outsider":
					s.value.RequesterUserID = "eve"
					s.value.TargetUserID = "mallory"
				case "revoked":
					s.readErr = coord.ErrCreationForbidden
				case "missing":
					s.readErr = coord.ErrNotFound
				case "failed":
					s.readErr = errors.New("PRIVATE")
				case "legacy":
					store = &legacyRescheduleReader{Store: s, ScopedRescheduleStore: s}
				}
				notes, audits := &stubNotificationStore{}, &stubAuditStore{}
				h := NewWithStores(nil, nil, nil, nil, store, notes, audits, "", testLogger())
				r := httptest.NewRequest("POST", "/api/v1/requests/r/reschedule", strings.NewReader(`{"action":"accept","proposalId":"proposal-new","expectedOptionId":"old"}`))
				r.Header.Set("X-Demo-User-ID", "alice")
				r.Header.Set("X-Organization-ID", "org")
				w := httptest.NewRecorder()
				h.ServeHTTP(w, r)
				want := 503
				if scenario == "valid" {
					want = 200
				}
				if w.Code != want || w.Header().Get("Cache-Control") != "no-store" {
					t.Fatalf("status=%d body=%s", w.Code, w.Body)
				}
				if scenario != "valid" && (strings.Contains(w.Body.String(), "PRIVATE") || w.Header().Get("Idempotency-Replayed") != "") {
					t.Fatal("unverified result leaked or reported success")
				}
				if scenario == "valid" && (w.Header().Get("Idempotency-Replayed") == "true") != replay {
					t.Fatal("replay signal lost")
				}
				if scenario == "legacy" {
					if s.scopedCalls != 0 || s.reads != 0 {
						t.Fatal("mutated without verified reader")
					}
				} else if s.scopedCalls != 1 || s.reads != 1 || s.readOrg != "org" {
					t.Fatal("lost scoped command/read")
				}
				if s.getUser != "" || len(notes.values) != 0 || len(audits.values) != 0 {
					t.Fatal("legacy read or duplicate effects")
				}
			})
		}
	}
}
