package httpapi

import (
	"context"
	"errors"
	coordinationrequest "github.com/negotiable-calendar/negotiable-calendar/apps/api/internal/request"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type confirmationErrorStore struct {
	stubRequestStore
	responseErr error
}

func TestAcceptanceHTTPDoesNotWriteTransactionalEffects(t *testing.T) {
	for _, failed := range []bool{false, true} {
		store := &confirmationErrorStore{}
		store.value = coordinationrequest.CoordinationRequest{ID: "r", TargetUserID: "bob", RequesterUserID: "alice"}
		want := 200
		if failed {
			store.responseErr = errors.New("synthetic storage failure")
			want = 500
		}
		notes, audits := &stubNotificationStore{}, &stubAuditStore{}
		handler := NewWithStores(nil, nil, nil, nil, store, notes, audits, "", slog.New(slog.NewTextHandler(io.Discard, nil)))
		r := httptest.NewRequest(http.MethodPost, "/api/v1/requests/r/accept", strings.NewReader(`{"optionId":"o"}`))
		r.Header.Set("X-Demo-User-ID", "bob")
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		if w.Code != want || len(notes.values) != 0 || len(audits.values) != 0 {
			t.Fatalf("failed=%v status=%d notes=%d audit=%d", failed, w.Code, len(notes.values), len(audits.values))
		}
	}
}

func (s *confirmationErrorStore) Respond(context.Context, string, string, coordinationrequest.Status, string) error {
	return s.responseErr
}

func TestConfirmationConflictCodesAndIdempotence(t *testing.T) {
	for _, failure := range []error{coordinationrequest.ErrCandidateExpired, coordinationrequest.ErrCandidateInvalid, coordinationrequest.ErrAvailabilityChanged, coordinationrequest.ErrBookingConflict, coordinationrequest.ErrAlreadyAccepted} {
		t.Run(failure.Error(), func(t *testing.T) {
			store := &confirmationErrorStore{responseErr: failure}
			store.value = coordinationrequest.CoordinationRequest{ID: "r", TargetUserID: "bob", RequesterUserID: "alice"}
			notifications := &stubNotificationStore{}
			audits := &stubAuditStore{}
			handler := NewWithStores(nil, nil, nil, nil, store, notifications, audits, "", slog.New(slog.NewTextHandler(io.Discard, nil)))
			req := httptest.NewRequest(http.MethodPost, "/api/v1/requests/r/accept", strings.NewReader(`{"optionId":"o"}`))
			req.Header.Set("X-Demo-User-ID", "bob")
			res := httptest.NewRecorder()
			handler.ServeHTTP(res, req)
			want := 409
			if failure == coordinationrequest.ErrAlreadyAccepted {
				want = 200
			}
			if res.Code != want {
				t.Fatalf("%d %s", res.Code, res.Body.String())
			}
			if want == 409 && !strings.Contains(res.Body.String(), failure.Error()) {
				t.Fatal("missing safe conflict code")
			}
			if len(notifications.values) != 0 || len(audits.values) != 0 {
				t.Fatal("failed/repeated acceptance repeated effects")
			}
		})
	}
}
