package httpapi

import (
	"context"
	"errors"
	coordinationrequest "github.com/negotiable-calendar/negotiable-calendar/apps/api/internal/request"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type resolutionTestStore struct {
	stubRequestStore
	err   error
	calls int
}

func (s *resolutionTestStore) ResolveRequest(_ context.Context, id, actor, org string, cmd coordinationrequest.ResolutionCommand) (bool, error) {
	s.calls++
	if s.err != nil {
		return false, s.err
	}
	if id != s.value.ID {
		return false, coordinationrequest.ErrNotFound
	}
	if err := coordinationrequest.AuthorizeResolution(s.value, actor, org, cmd); err != nil {
		return false, err
	}
	return coordinationrequest.PrepareResolution(&s.value, cmd, time.Now().UTC())
}

func TestAtomicResolutionRoutes(t *testing.T) {
	for _, action := range []string{"async", "decline", "cancel"} {
		t.Run(action, func(t *testing.T) {
			store := &resolutionTestStore{stubRequestStore: stubRequestStore{value: coordinationrequest.CoordinationRequest{ID: "r", OrganizationID: "org", RequesterUserID: "alice", TargetUserID: "bob", Status: coordinationrequest.Suggested, DeadlineAt: time.Now().UTC().Add(time.Hour)}}}
			notes, audits := &stubNotificationStore{}, &stubAuditStore{}
			handler := NewWithStores(stubDatabase{}, &stubPolicyStore{}, &stubProjectionStore{}, &stubOrganizationStore{}, store, notes, audits, "", testLogger())
			actor, body := "bob", ""
			if action == "cancel" {
				actor = "alice"
			}
			if action == "async" {
				body = `{"message":" answer "}`
			}
			send := func(who, org, payload string) *httptest.ResponseRecorder {
				r := httptest.NewRequest(http.MethodPost, "/api/v1/requests/r/"+action, strings.NewReader(payload))
				r.Header.Set("X-Demo-User-ID", who)
				r.Header.Set("X-Organization-ID", org)
				w := httptest.NewRecorder()
				handler.ServeHTTP(w, r)
				return w
			}
			if got := send(actor, "", body); got.Code != 401 {
				t.Fatal(got.Code)
			}
			if got := send(actor, "other", body); got.Code != 404 {
				t.Fatal(got.Code)
			}
			if got := send("outsider", "org", body); got.Code != 404 {
				t.Fatal(got.Code)
			}
			if got := send(actor, "org", body); got.Code != 200 {
				t.Fatal(got.Code, got.Body.String())
			}
			if got := send(actor, "org", body); got.Code != 200 || got.Header().Get("Idempotency-Replayed") != "true" {
				t.Fatal("replay failed", got.Code)
			}
			if len(notes.values) != 0 || len(audits.values) != 0 {
				t.Fatal("duplicate HTTP effects")
			}
			if action == "async" {
				if got := send(actor, "org", `{"message":"changed"}`); got.Code != 409 {
					t.Fatal("overwrite accepted")
				}
			}
			for _, entry := range []struct {
				err  error
				code int
			}{{coordinationrequest.ErrResolutionExpired, 409}, {coordinationrequest.ErrResolutionConflict, 409}, {coordinationrequest.ErrCreationForbidden, 403}, {errors.New("storage secret"), 503}} {
				store.err = entry.err
				got := send(actor, "org", body)
				if got.Code != entry.code || strings.Contains(got.Body.String(), "storage secret") {
					t.Fatal("wrong error", got.Code)
				}
			}
		})
	}
}
