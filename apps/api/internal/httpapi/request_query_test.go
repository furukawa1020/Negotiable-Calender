package httpapi

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRequestListRejectsAmbiguousQueryBeforeStorage(t *testing.T) {
	for _, query := range []string{"scope=inbox&scope=sent", "scope=sent&scope=sent", "scope=%zz", "scope=sent;scope=inbox", "unexpected=sent", "scope=inbox&unexpected=1", "%73cope=sent&scope=inbox"} {
		t.Run(query, func(t *testing.T) {
			s := &scopedListStub{stubRequestStore: &stubRequestStore{}}
			h := New(stubDatabase{}, &stubPolicyStore{}, &stubProjectionStore{}, &stubOrganizationStore{}, s, "", testLogger())
			r := httptest.NewRequest(http.MethodGet, "/api/v1/requests?"+query, nil)
			r.Header.Set("X-Demo-User-ID", "actor")
			r.Header.Set("X-Organization-ID", "org")
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			if w.Code != 400 || s.calls != 0 {
				t.Fatalf("status=%d calls=%d", w.Code, s.calls)
			}
		})
	}
}
