package httpapi

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	coord "github.com/negotiable-calendar/negotiable-calendar/apps/api/internal/request"
)

type scopedListStub struct {
	*stubRequestStore
	org   string
	sent  bool
	calls int
}

func (store *scopedListStub) ListInOrganization(_ context.Context, actor, org string, sent bool) ([]coord.CoordinationRequest, error) {
	store.target, store.org, store.sent = actor, org, sent
	store.calls++
	return store.values, store.err
}

func TestRequestListWorkspaceBoundary(t *testing.T) {
	for _, tc := range []struct {
		name, actor, org, scope string
		legacy                  bool
		err                     error
		value                   *coord.CoordinationRequest
		status, calls           int
	}{
		{name: "inbox", actor: "actor", org: "org", status: 200, calls: 1},
		{name: "explicit-inbox", actor: "actor", org: "org", scope: "inbox", status: 200, calls: 1},
		{name: "sent", actor: "actor", org: "org", scope: "sent", status: 200, calls: 1},
		{name: "missing-actor", org: "org", status: 401},
		{name: "missing-org", actor: "actor", status: 401},
		{name: "invalid-scope", actor: "actor", org: "org", scope: "all", status: 400},
		{name: "legacy-store", actor: "actor", org: "org", legacy: true, status: 503},
		{name: "revoked", actor: "actor", org: "org", err: coord.ErrCreationForbidden, status: 403, calls: 1},
		{name: "store-failure", actor: "actor", org: "org", err: errors.New("private storage failure"), status: 500, calls: 1},
		{name: "foreign-org", actor: "actor", org: "org", value: &coord.CoordinationRequest{OrganizationID: "other", TargetUserID: "actor"}, status: 500, calls: 1},
		{name: "wrong-inbox-role", actor: "actor", org: "org", value: &coord.CoordinationRequest{OrganizationID: "org", RequesterUserID: "actor", TargetUserID: "peer"}, status: 500, calls: 1},
		{name: "wrong-sent-role", actor: "actor", org: "org", scope: "sent", value: &coord.CoordinationRequest{OrganizationID: "org", RequesterUserID: "peer", TargetUserID: "actor"}, status: 500, calls: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			base := &stubRequestStore{err: tc.err}
			if tc.value != nil {
				tc.value.Title = "private request title"
				base.values = []coord.CoordinationRequest{*tc.value}
			}
			scoped := &scopedListStub{stubRequestStore: base}
			var store coord.Store = scoped
			if tc.legacy {
				store = base
			}
			handler := New(stubDatabase{}, &stubPolicyStore{}, &stubProjectionStore{}, &stubOrganizationStore{}, store, "", testLogger())
			req := httptest.NewRequest(http.MethodGet, "/api/v1/requests?scope="+tc.scope, nil)
			req.Header.Set("X-Demo-User-ID", tc.actor)
			req.Header.Set("X-Organization-ID", tc.org)
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, req)
			if response.Code != tc.status || scoped.calls != tc.calls {
				t.Fatalf("status=%d calls=%d body=%s", response.Code, scoped.calls, response.Body)
			}
			if tc.calls > 0 && (scoped.target != tc.actor || scoped.org != tc.org || scoped.sent != (tc.scope == "sent")) {
				t.Fatal("scope routing mismatch")
			}
			body := response.Body.String()
			if strings.Contains(body, "private") {
				t.Fatalf("private data leaked: %s", body)
			}
			if tc.status == 200 && strings.TrimSpace(body) != `{"requests":[]}` {
				t.Fatalf("empty result must be an array: %s", body)
			}
		})
	}
}
