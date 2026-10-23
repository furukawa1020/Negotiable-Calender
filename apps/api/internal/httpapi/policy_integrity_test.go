package httpapi

import (
	"github.com/negotiable-calendar/negotiable-calendar/apps/api/internal/policy"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestPolicyReadOwnership(t *testing.T) {
	for _, endpoint := range []string{"sharing-policy", "manual-overrides"} {
		for _, owner := range []string{"alice", "bob", ""} {
			t.Run(endpoint+owner, func(t *testing.T) {
				store := &stubPolicyStore{value: policy.SharingPolicy{ID: "PRIVATE", UserID: owner}, overrides: []policy.ManualOverride{{ID: "self", UserID: "alice"}, {ID: "PRIVATE", UserID: owner}}}
				h := New(nil, store, nil, nil, nil, "", testLogger())
				r := httptest.NewRequest("GET", "/api/v1/users/alice/"+endpoint, nil)
				r.Header.Set("X-Demo-User-ID", "alice")
				w := httptest.NewRecorder()
				h.ServeHTTP(w, r)
				want := 500
				if owner == "alice" {
					want = 200
				}
				if w.Code != want {
					t.Fatalf("status=%d body=%s", w.Code, w.Body)
				}
				if want != 200 && strings.Contains(w.Body.String(), "PRIVATE") {
					t.Fatal("foreign settings leaked")
				}
			})
		}
	}
}

func TestEmptyManualOverridesRemainArray(t *testing.T) {
	h := New(nil, &stubPolicyStore{}, nil, nil, nil, "", testLogger())
	r := httptest.NewRequest("GET", "/api/v1/users/alice/manual-overrides", nil)
	r.Header.Set("X-Demo-User-ID", "alice")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 200 || strings.TrimSpace(w.Body.String()) != `{"overrides":[]}` {
		t.Fatal(w.Code, w.Body)
	}
}
