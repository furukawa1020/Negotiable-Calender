package httpapi

import (
	"context"
	"errors"
	coordinationrequest "github.com/negotiable-calendar/negotiable-calendar/apps/api/internal/request"
	"io"
	"log/slog"
	"net/http/httptest"
	"strings"
	"testing"
)

type rescheduleStore struct {
	stubRequestStore
	failure error
	calls   int
	actor   string
	command coordinationrequest.RescheduleCommand
}

func (s *rescheduleStore) Reschedule(_ context.Context, id, actor string, command coordinationrequest.RescheduleCommand) error {
	s.calls++
	s.actor = actor
	s.command = command
	return s.failure
}

func TestRescheduleHTTP(t *testing.T) {
	for _, tc := range []struct {
		name, actor, body string
		failure           error
		want, calls       int
	}{
		{"success", "alice", `{"action":"propose","proposalId":"proposal-new","expectedOptionId":"old","startAt":"2099-01-01T00:00:00Z"}`, nil, 200, 1},
		{"accept", "bob", `{"action":"accept","proposalId":"proposal-new","expectedOptionId":"old"}`, nil, 200, 1},
		{"replay", "bob", `{"action":"accept","proposalId":"proposal-new","expectedOptionId":"old"}`, coordinationrequest.ErrRescheduleRepeated, 200, 1},
		{"outsider", "eve", `{"action":"accept","proposalId":"proposal-new","expectedOptionId":"old"}`, coordinationrequest.ErrNotFound, 404, 1},
		{"stale", "bob", `{"action":"accept","proposalId":"proposal-new","expectedOptionId":"old"}`, coordinationrequest.ErrRescheduleInvalid, 409, 1},
		{"busy", "bob", `{"action":"accept","proposalId":"proposal-new","expectedOptionId":"old"}`, coordinationrequest.ErrBookingConflict, 409, 1},
		{"dirty", "bob", `{"action":"accept","proposalId":"proposal-new","expectedOptionId":"old"}`, coordinationrequest.ErrAvailabilityChanged, 409, 1},
		{"storage", "bob", `{"action":"accept","proposalId":"proposal-new","expectedOptionId":"old"}`, errors.New("private detail"), 500, 1},
		{"anonymous", "", `{}`, nil, 401, 0},
		{"unknown", "bob", `{"actor":"alice"}`, nil, 400, 0},
		{"action", "bob", `{"action":"overwrite","proposalId":"proposal-new","expectedOptionId":"old"}`, nil, 400, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := &scopedRescheduleStub{rescheduleStore: rescheduleStore{failure: tc.failure}}
			store.value = coordinationrequest.CoordinationRequest{ID: "r", Status: coordinationrequest.Accepted, AcceptedOptionID: "old"}
			notes := &stubNotificationStore{}
			audits := &stubAuditStore{}
			handler := NewWithStores(nil, nil, nil, nil, store, notes, audits, "", slog.New(slog.NewTextHandler(io.Discard, nil)))
			req := httptest.NewRequest("POST", "/api/v1/requests/r/reschedule", strings.NewReader(tc.body))
			req.Header.Set("X-Demo-User-ID", tc.actor)
			req.Header.Set("X-Organization-ID", "org")
			res := httptest.NewRecorder()
			handler.ServeHTTP(res, req)
			if res.Code != tc.want || store.scopedCalls != tc.calls || store.calls != 0 {
				t.Fatalf("%d %s calls=%d", res.Code, res.Body.String(), store.scopedCalls)
			}
			if store.scopedCalls > 0 && (store.actor != tc.actor || store.command.ExpectedOptionID != "old") {
				t.Fatal("lost precondition/actor")
			}
			if len(notes.values) != 0 || len(audits.values) != 0 || strings.Contains(res.Body.String(), "private detail") {
				t.Fatal("effects repeated or detail leaked")
			}
		})
	}
}
