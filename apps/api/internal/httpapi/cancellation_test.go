package httpapi

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	coordinationrequest "github.com/negotiable-calendar/negotiable-calendar/apps/api/internal/request"
)

type cancellationStore struct {
	stubRequestStore
	failure       error
	calls         int
	actor, option string
}

func (s *cancellationStore) CancelConfirmed(_ context.Context, id, actor, option string) error {
	s.calls++
	s.actor = actor
	s.option = option
	return s.failure
}

func TestConfirmedCancellationHTTP(t *testing.T) {
	for _, tc := range []struct {
		name, actor, body string
		failure           error
		want, calls       int
	}{
		{"success", "alice", `{"optionId":"chosen"}`, nil, 200, 1},
		{"recipient", "bob", `{"optionId":"chosen"}`, nil, 200, 1},
		{"retry", "bob", `{"optionId":"chosen"}`, coordinationrequest.ErrAlreadyCancelled, 200, 1},
		{"outsider", "eve", `{"optionId":"chosen"}`, coordinationrequest.ErrNotFound, 404, 1},
		{"stale", "bob", `{"optionId":"chosen"}`, coordinationrequest.ErrCancellationInvalid, 409, 1},
		{"conflict", "bob", `{"optionId":"chosen"}`, coordinationrequest.ErrBookingConflict, 409, 1},
		{"storage", "bob", `{"optionId":"chosen"}`, errors.New("private detail"), 500, 1},
		{"anonymous", "", `{"optionId":"chosen"}`, nil, 401, 0},
		{"missing", "bob", `{}`, nil, 400, 0},
		{"unknown", "bob", `{"optionId":"chosen","actor":"alice"}`, nil, 400, 0},
		{"trailing", "bob", `{"optionId":"chosen"}{}`, nil, 400, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := &scopedCancellationStub{cancellationStore: cancellationStore{failure: tc.failure}}
			notes := &stubNotificationStore{}
			audits := &stubAuditStore{}
			handler := NewWithStores(nil, nil, nil, nil, store, notes, audits, "", slog.New(slog.NewTextHandler(io.Discard, nil)))
			req := httptest.NewRequest(http.MethodPost, "/api/v1/requests/r/cancel-confirmed", strings.NewReader(tc.body))
			req.Header.Set("X-Demo-User-ID", tc.actor)
			req.Header.Set("X-Organization-ID", "org")
			res := httptest.NewRecorder()
			handler.ServeHTTP(res, req)
			if res.Code != tc.want || store.scopedCalls != tc.calls || store.calls != 0 {
				t.Fatalf("%d %s calls=%d", res.Code, res.Body.String(), store.scopedCalls)
			}
			if tc.calls > 0 && (store.actor != tc.actor || store.option != "chosen" || store.organization != "org") {
				t.Fatal("identity/selection lost")
			}
			if len(notes.values) != 0 || len(audits.values) != 0 {
				t.Fatal("HTTP duplicated transactional effects")
			}
			if strings.Contains(res.Body.String(), "private detail") {
				t.Fatal("internal error leaked")
			}
		})
	}
}
