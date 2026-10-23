package httpapi

import (
	"context"
	coord "github.com/negotiable-calendar/negotiable-calendar/apps/api/internal/request"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type interruptedWorkspaceStore struct {
	*stubRequestStore
	calls int
	wait  bool
	t     *testing.T
}

func (s *interruptedWorkspaceStore) read(ctx context.Context) {
	s.calls++
	deadline, ok := ctx.Deadline()
	if !ok || time.Until(deadline) > workspaceReadTimeout {
		s.t.Fatal("read has no bounded deadline")
	}
	if s.wait {
		<-ctx.Done()
	}
}
func (s *interruptedWorkspaceStore) ListInOrganization(ctx context.Context, _, _ string, _ bool) ([]coord.CoordinationRequest, error) {
	s.read(ctx)
	return []coord.CoordinationRequest{s.value}, nil
}
func (s *interruptedWorkspaceStore) GetInOrganization(ctx context.Context, _, _, _ string) (coord.CoordinationRequest, error) {
	s.read(ctx)
	return s.value, nil
}

func TestWorkspaceReadInterruptionNeverPublishesLateSuccess(t *testing.T) {
	for _, path := range []string{"/api/v1/requests", "/api/v1/requests/r", "/api/v1/requests/r/calendar.ics"} {
		for _, mode := range []string{"pre-cancelled", "late-success"} {
			t.Run(path+mode, func(t *testing.T) {
				ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
				defer cancel()
				if mode == "pre-cancelled" {
					cancel()
				}
				s := &interruptedWorkspaceStore{stubRequestStore: &stubRequestStore{value: coord.CoordinationRequest{ID: "r", OrganizationID: "org", RequesterUserID: "bob", TargetUserID: "alice", Title: "PRIVATE"}}, wait: mode == "late-success", t: t}
				h := New(nil, nil, nil, nil, s, "", testLogger())
				r := httptest.NewRequest("GET", path, nil).WithContext(ctx)
				r.Header.Set("X-Demo-User-ID", "alice")
				r.Header.Set("X-Organization-ID", "org")
				w := httptest.NewRecorder()
				h.ServeHTTP(w, r)
				wantCalls := 0
				if s.wait {
					wantCalls = 1
				}
				if w.Code != 503 || s.calls != wantCalls || strings.Contains(w.Body.String(), "PRIVATE") || strings.Contains(w.Body.String(), "VEVENT") {
					t.Fatal(w.Code, s.calls, w.Body)
				}
			})
		}
	}
}

func TestWorkspaceReadPreservesSuccessfulResult(t *testing.T) {
	got, err := readWorkspace(context.Background(), func(ctx context.Context) (string, error) {
		deadline, ok := ctx.Deadline()
		if !ok || time.Until(deadline) > workspaceReadTimeout {
			t.Fatal("missing deadline")
		}
		return "result", nil
	})
	if err != nil || got != "result" {
		t.Fatal(got, err)
	}
}
