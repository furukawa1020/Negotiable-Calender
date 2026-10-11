package httpapi

import (
	coord "github.com/negotiable-calendar/negotiable-calendar/apps/api/internal/request"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestCompleteMeetingHTTP(t *testing.T) {
	now := time.Now().UTC()
	start, end := now.Add(-2*time.Hour), now.Add(-time.Hour)
	for _, actor := range []string{"alice", "bob"} {
		store := &resolutionTestStore{stubRequestStore: stubRequestStore{value: coord.CoordinationRequest{ID: "r", OrganizationID: "org", RequesterUserID: "alice", TargetUserID: "bob", Status: coord.Accepted, AcceptedOptionID: "o", Options: []coord.Option{{ID: "o", RequestID: "r", Type: coord.OptionMeeting, StartAt: &start, EndAt: &end, CreatedAt: now}}}}}
		h := New(nil, nil, nil, nil, store, "", testLogger())
		send := func(user, org, body string) *httptest.ResponseRecorder {
			r := httptest.NewRequest(http.MethodPost, "/api/v1/requests/r/complete", strings.NewReader(body))
			r.Header.Set("X-Demo-User-ID", user)
			r.Header.Set("X-Organization-ID", org)
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			return w
		}
		for _, tc := range []struct {
			user, org, body string
			status          int
		}{{"", "org", `{"optionId":"o"}`, 401}, {actor, "", `{"optionId":"o"}`, 401}, {"other", "org", `{"optionId":"o"}`, 404}, {actor, "other", `{"optionId":"o"}`, 404}, {actor, "org", `{"optionId":"stale"}`, 409}, {actor, "org", `{}`, 400}, {actor, "org", `{"optionId":"o","extra":true}`, 400}, {actor, "org", `{"optionId":"o"} {}`, 400}} {
			if w := send(tc.user, tc.org, tc.body); w.Code != tc.status {
				t.Fatal("bad completion boundary", w.Code, w.Body.String())
			}
		}
		for i := 0; i < 2; i++ {
			w := send(actor, "org", `{"optionId":"o"}`)
			if w.Code != 200 || !strings.Contains(w.Body.String(), `"acceptedOptionId":"o"`) || !strings.Contains(w.Body.String(), `"status":"completed"`) || w.Header().Get("Cache-Control") != "no-store" || (i == 1 && w.Header().Get("Idempotency-Replayed") != "true") {
				t.Fatal("bad completion", w.Code, w.Body.String())
			}
		}
	}
}
