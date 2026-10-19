package httpapi

import (
	"context"
	"errors"
	coord "github.com/negotiable-calendar/negotiable-calendar/apps/api/internal/request"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type detailStub struct {
	*stubRequestStore
	org   string
	calls int
}

func (s *detailStub) GetInOrganization(_ context.Context, _, actor, org string) (coord.CoordinationRequest, error) {
	s.org = org
	s.target = actor
	s.calls++
	return s.value, s.err
}

type legacyDetailStore struct{ coord.Store }

func TestWorkspaceDetailAndExportFailClosed(t *testing.T) {
	for _, suffix := range []string{"", "/calendar.ics"} {
		for _, scenario := range []string{"valid", "missing-actor", "missing-org", "legacy", "revoked", "storage", "foreign", "wrong-id", "outsider", "missing"} {
			t.Run(suffix+scenario, func(t *testing.T) {
				start := time.Now().UTC()
				end := start.Add(time.Hour)
				s := &detailStub{stubRequestStore: &stubRequestStore{value: coord.CoordinationRequest{ID: "r", OrganizationID: "org", RequesterUserID: "alice", TargetUserID: "bob", Title: "PRIVATE", Status: coord.Accepted, AcceptedOptionID: "o", UpdatedAt: start, Options: []coord.Option{{ID: "o", RequestID: "r", Type: coord.OptionMeeting, StartAt: &start, EndAt: &end, CreatedAt: start}}}}}
				actor, org, want := "alice", "org", 200
				var store coord.Store = s
				switch scenario {
				case "missing-actor":
					actor = ""
					want = 401
				case "missing-org":
					org = ""
					want = 401
				case "legacy":
					store = &legacyDetailStore{Store: s}
					want = 503
				case "revoked":
					s.err = coord.ErrCreationForbidden
					want = 403
				case "storage":
					s.err = errors.New("PRIVATE")
					want = 500
				case "foreign":
					s.value.OrganizationID = "other"
					want = 404
				case "wrong-id":
					s.value.ID = "other"
					want = 404
				case "outsider":
					actor = "eve"
					want = 404
				case "missing":
					s.err = coord.ErrNotFound
					want = 404
				}
				h := New(nil, nil, nil, nil, store, "", testLogger())
				r := httptest.NewRequest("GET", "/api/v1/requests/r"+suffix, nil)
				r.Header.Set("X-Demo-User-ID", actor)
				r.Header.Set("X-Organization-ID", org)
				w := httptest.NewRecorder()
				h.ServeHTTP(w, r)
				if w.Code != want || w.Header().Get("Cache-Control") != "no-store" || s.getUser != "" {
					t.Fatalf("status=%d unscoped=%s body=%s", w.Code, s.getUser, w.Body)
				}
				if s.calls > 0 && (s.target != actor || s.org != org) {
					t.Fatal("lost scope")
				}
				if want != 200 && (strings.Contains(w.Body.String(), "PRIVATE") || strings.Contains(w.Body.String(), "BEGIN:VEVENT")) {
					t.Fatal("private data leaked")
				}
			})
		}
	}
}
