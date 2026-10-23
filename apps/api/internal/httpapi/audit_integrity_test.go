package httpapi

import (
	"github.com/negotiable-calendar/negotiable-calendar/apps/api/internal/audit"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAuditResultOrganization(t *testing.T) {
	for _, scenario := range []string{"own", "foreign", "missing-org", "mixed", "empty", "nonmember"} {
		t.Run(scenario, func(t *testing.T) {
			values := []audit.Event{{ID: "PRIVATE", OrganizationID: "org"}}
			want := 200
			members := map[string]bool{"org:alice": true}
			switch scenario {
			case "foreign":
				values[0].OrganizationID = "other"
				want = 500
			case "missing-org":
				values[0].OrganizationID = ""
				want = 500
			case "mixed":
				values = append(values, audit.Event{ID: "PRIVATE", OrganizationID: "other"})
				want = 500
			case "empty":
				values = nil
			case "nonmember":
				members = map[string]bool{}
				want = 403
			}
			store := &stubAuditStore{values: values}
			h := NewWithStores(nil, nil, nil, &stubOrganizationStore{members: members}, nil, nil, store, "", testLogger())
			r := httptest.NewRequest("GET", "/api/v1/audit-logs", nil)
			r.Header.Set("X-Demo-User-ID", "alice")
			r.Header.Set("X-Organization-ID", "org")
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			if w.Code != want {
				t.Fatalf("status=%d body=%s", w.Code, w.Body)
			}
			if want != 200 && strings.Contains(w.Body.String(), "PRIVATE") {
				t.Fatal("audit leaked")
			}
			if scenario == "empty" && strings.TrimSpace(w.Body.String()) != `{"auditLogs":[]}` {
				t.Fatal(w.Body)
			}
			if scenario == "nonmember" && store.listOrganization != "" {
				t.Fatal("unauthorized read")
			}
		})
	}
}
