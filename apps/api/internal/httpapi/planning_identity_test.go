package httpapi

import (
	"net/http/httptest"
	"strings"
	"testing"
)

func TestPlanningPreviewBindsExactRequestBeforeSourceRead(t *testing.T) {
	for _, scenario := range []string{"valid", "wrong-id", "missing-id", "missing-requester", "same-participants"} {
		t.Run(scenario, func(t *testing.T) {
			r, p, o := planningFixture()
			want := 404
			switch scenario {
			case "valid":
				want = 200
			case "wrong-id":
				r.value.ID = "other-private-request"
			case "missing-id":
				r.value.ID = ""
			case "missing-requester":
				r.value.RequesterUserID = ""
			case "same-participants":
				r.value.RequesterUserID = r.value.TargetUserID
			}
			s := &boundedPlanningStub{stubRequestStore: r, projections: p}
			h := New(nil, nil, p, o, s, "", testLogger())
			req := httptest.NewRequest("GET", "/api/v1/requests/private-request/planning-preview", nil)
			req.Header.Set("X-Demo-User-ID", "owner")
			req.Header.Set("X-Organization-ID", "workspace")
			w := httptest.NewRecorder()
			h.ServeHTTP(w, req)
			if w.Code != want {
				t.Fatal(w.Code, w.Body)
			}
			if want != 200 && (s.loads != 0 || strings.Contains(w.Body.String(), "private")) {
				t.Fatal("foreign evidence read or exposed")
			}
		})
	}
}
