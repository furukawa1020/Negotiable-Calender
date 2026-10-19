package httpapi

import (
	coord "github.com/negotiable-calendar/negotiable-calendar/apps/api/internal/request"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestCalendarExportRejectsAmbiguousAndMismatchedEvidence(t *testing.T) {
	for _, scenario := range []string{"duplicate", "mixed-duplicate", "wrong-request", "wrong-resource", "missing-org"} {
		for _, actor := range []string{"alice", "bob"} {
			t.Run(scenario+"/"+actor, func(t *testing.T) {
				start := time.Date(2026, 9, 21, 1, 0, 0, 0, time.UTC)
				end := start.Add(time.Hour)
				v := coord.CoordinationRequest{ID: "r", OrganizationID: "org", RequesterUserID: "alice", TargetUserID: "bob", Title: "PRIVATE_TITLE", Status: coord.Accepted, AcceptedOptionID: "o", UpdatedAt: start, Options: []coord.Option{{ID: "o", RequestID: "r", Type: coord.OptionMeeting, StartAt: &start, EndAt: &end, CreatedAt: start}}}
				want := 409
				switch scenario {
				case "duplicate":
					v.Options = append(v.Options, v.Options[0])
				case "mixed-duplicate":
					other := v.Options[0]
					other.Type = coord.OptionAsync
					v.Options = append(v.Options, other)
				case "wrong-request":
					v.Options[0].RequestID = "other"
				case "wrong-resource":
					v.ID = "other"
					v.Options[0].RequestID = "other"
					want = 404
				case "missing-org":
					v.OrganizationID = ""
					want = 404
				}
				h := New(nil, nil, nil, nil, &stubRequestStore{value: v}, "", slog.New(slog.NewTextHandler(io.Discard, nil)))
				r := httptest.NewRequest(http.MethodGet, "/api/v1/requests/r/calendar.ics", nil)
				r.Header.Set("X-Demo-User-ID", actor)
				r.Header.Set("X-Organization-ID", "org")
				w := httptest.NewRecorder()
				h.ServeHTTP(w, r)
				if w.Code != want || w.Header().Get("Cache-Control") != "no-store" || w.Header().Get("Content-Disposition") != "" || strings.Contains(w.Body.String(), "PRIVATE_TITLE") || strings.Contains(w.Body.String(), "BEGIN:VEVENT") {
					t.Fatal("unsafe export", w.Code, w.Body.String())
				}
			})
		}
	}
}
