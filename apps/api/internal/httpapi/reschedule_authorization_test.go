package httpapi

import (
	"context"
	"io"
	"log/slog"
	"net/http/httptest"
	"strings"
	"testing"

	coord "github.com/negotiable-calendar/negotiable-calendar/apps/api/internal/request"
)

type scopedRescheduleStub struct {
	rescheduleStore
	organization string
	scopedCalls  int
}

func (s *scopedRescheduleStub) RescheduleInOrganization(_ context.Context, id, actor, org string, command coord.RescheduleCommand) error {
	s.scopedCalls++
	s.organization = org
	s.actor = actor
	s.command = command
	return s.failure
}

func TestRescheduleRequiresScopedAuthorization(t *testing.T) {
	for _, tc := range []struct {
		name, org   string
		failure     error
		want, calls int
	}{
		{"missing organization", "", nil, 401, 0},
		{"current organization", "org", nil, 200, 1},
		{"wrong organization", "other", coord.ErrNotFound, 404, 1},
		{"membership revoked", "org", coord.ErrCreationForbidden, 403, 1},
		{"authorized replay", "org", coord.ErrRescheduleRepeated, 200, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := &scopedRescheduleStub{}
			store.failure = tc.failure
			handler := NewWithStores(nil, nil, nil, nil, store, &stubNotificationStore{}, &stubAuditStore{}, "", slog.New(slog.NewTextHandler(io.Discard, nil)))
			req := httptest.NewRequest("POST", "/api/v1/requests/r/reschedule", strings.NewReader(`{"action":"accept","proposalId":"proposal-new","expectedOptionId":"old"}`))
			req.Header.Set("X-Demo-User-ID", "bob")
			req.Header.Set("X-Organization-ID", tc.org)
			res := httptest.NewRecorder()
			handler.ServeHTTP(res, req)
			if res.Code != tc.want || store.scopedCalls != tc.calls || store.calls != 0 {
				t.Fatalf("status=%d scoped=%d legacy=%d", res.Code, store.scopedCalls, store.calls)
			}
			if tc.calls > 0 && (store.organization != tc.org || store.actor != "bob") {
				t.Fatal("lost authenticated scope")
			}
		})
	}
}

func TestRescheduleRefusesLegacyStore(t *testing.T) {
	store := &rescheduleStore{}
	handler := NewWithStores(nil, nil, nil, nil, store, &stubNotificationStore{}, &stubAuditStore{}, "", slog.New(slog.NewTextHandler(io.Discard, nil)))
	req := httptest.NewRequest("POST", "/api/v1/requests/r/reschedule", strings.NewReader(`{"action":"accept","proposalId":"proposal-new","expectedOptionId":"old"}`))
	req.Header.Set("X-Demo-User-ID", "bob")
	req.Header.Set("X-Organization-ID", "org")
	res := httptest.NewRecorder()
	handler.ServeHTTP(res, req)
	if res.Code != 503 || store.calls != 0 {
		t.Fatalf("status=%d legacy writes=%d", res.Code, store.calls)
	}
}
