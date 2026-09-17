package httpapi

import (
	coordinationrequest "github.com/negotiable-calendar/negotiable-calendar/apps/api/internal/request"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestCalendarExportAuthorizationAndConfirmation(t *testing.T) {
	start := time.Date(2026, 9, 21, 1, 0, 0, 0, time.UTC)
	end := start.Add(time.Hour)
	for _, tc := range []struct {
		user  string
		state coordinationrequest.Status
		want  int
	}{
		{"alice", coordinationrequest.Accepted, 200}, {"bob", coordinationrequest.Accepted, 200},
		{"outsider", coordinationrequest.Accepted, 404}, {"", coordinationrequest.Accepted, 401},
		{"alice", coordinationrequest.Suggested, 409}, {"bob", coordinationrequest.Cancelled, 409},
	} {
		t.Run(tc.user+string(tc.state), func(t *testing.T) {
			store := &stubRequestStore{value: coordinationrequest.CoordinationRequest{ID: "r", OrganizationID: "org", RequesterUserID: "alice", TargetUserID: "bob", Title: "相談", Status: tc.state, AcceptedOptionID: "o", UpdatedAt: start, Options: []coordinationrequest.Option{{ID: "o", RequestID: "r", Type: coordinationrequest.OptionMeeting, StartAt: &start, EndAt: &end, CreatedAt: start}}}}
			handler := New(nil, nil, nil, nil, store, "", slog.New(slog.NewTextHandler(io.Discard, nil)))
			req := httptest.NewRequest(http.MethodGet, "/api/v1/requests/r/calendar.ics", nil)
			req.Header.Set("X-Demo-User-ID", tc.user)
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, req)
			if response.Code != tc.want {
				t.Fatalf("got %d: %s", response.Code, response.Body.String())
			}
			if response.Header().Get("Cache-Control") != "no-store" {
				t.Fatal("cacheable export")
			}
			if tc.want == 200 && (!strings.Contains(response.Body.String(), "BEGIN:VEVENT") || !strings.HasPrefix(response.Header().Get("Content-Type"), "text/calendar")) {
				t.Fatal("invalid calendar response")
			}
			if tc.want != 200 && strings.Contains(response.Body.String(), "BEGIN:VEVENT") {
				t.Fatal("unauthorized export leaked")
			}
		})
	}
}
