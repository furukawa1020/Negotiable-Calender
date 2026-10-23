package httpapi

import (
	"github.com/negotiable-calendar/negotiable-calendar/apps/api/internal/policy"
	"github.com/negotiable-calendar/negotiable-calendar/apps/api/internal/projection"
	coord "github.com/negotiable-calendar/negotiable-calendar/apps/api/internal/request"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestExportResultOwnership(t *testing.T) {
	for _, scenario := range []string{"self", "target", "no-policy", "foreign-request", "missing-participants", "foreign-projection", "missing-projection-owner", "foreign-policy", "missing-policy-owner"} {
		t.Run(scenario, func(t *testing.T) {
			requests := &stubRequestStore{values: []coord.CoordinationRequest{{ID: "r", RequesterUserID: "alice", TargetUserID: "bob", Title: "PRIVATE"}}}
			projections := &stubProjectionStore{values: []projection.ScheduleProjection{{ID: "PRIVATE", UserID: "alice"}}}
			policies := &stubPolicyStore{value: policy.SharingPolicy{ID: "PRIVATE", UserID: "alice"}}
			want := 500
			switch scenario {
			case "self":
				want = 200
			case "target":
				requests.values[0].RequesterUserID = "bob"
				requests.values[0].TargetUserID = "alice"
				want = 200
			case "no-policy":
				policies.err = policy.ErrNotFound
				want = 200
			case "foreign-request":
				requests.values = append(requests.values, coord.CoordinationRequest{RequesterUserID: "eve", TargetUserID: "bob", Title: "PRIVATE"})
			case "missing-participants":
				requests.values[0].RequesterUserID = ""
				requests.values[0].TargetUserID = ""
			case "foreign-projection":
				projections.values = append(projections.values, projection.ScheduleProjection{UserID: "bob"})
			case "missing-projection-owner":
				projections.values[0].UserID = ""
			case "foreign-policy":
				policies.value.UserID = "bob"
			case "missing-policy-owner":
				policies.value.UserID = ""
			}
			h := New(nil, policies, projections, nil, requests, "", testLogger())
			r := httptest.NewRequest("GET", "/api/v1/users/alice/export", nil)
			r.Header.Set("X-Demo-User-ID", "alice")
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			if w.Code != want {
				t.Fatalf("status=%d body=%s", w.Code, w.Body)
			}
			if want != 200 && (strings.Contains(w.Body.String(), "PRIVATE") || w.Header().Get("Content-Disposition") != "") {
				t.Fatal("partial export leaked")
			}
		})
	}
}
