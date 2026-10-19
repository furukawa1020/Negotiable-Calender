package httpapi

import (
	"context"
	"github.com/negotiable-calendar/negotiable-calendar/apps/api/internal/projection"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"
)

func TestProjectionCalendarDayAcrossDST(t *testing.T) {
	for _, tc := range []struct {
		day   string
		hours int
	}{{"2026-03-08T12:00:00Z", 23}, {"2026-11-01T12:00:00Z", 25}, {"2026-10-19T12:00:00Z", 24}} {
		now, _ := time.Parse(time.RFC3339, tc.day)
		zone, from, to, err := projectionRange(url.Values{"timezone": {"America/New_York"}}, now)
		loc, _ := time.LoadLocation(zone)
		if err != nil || to.Sub(from) != time.Duration(tc.hours)*time.Hour || from.In(loc).Hour() != 0 || to.In(loc).Hour() != 0 {
			t.Fatalf("from=%v to=%v err=%v", from, to, err)
		}
	}
}

type rangeProjectionStub struct {
	stubProjectionStore
	calls int
}

func (s *rangeProjectionStub) GetView(context.Context, string, string, time.Time, time.Time) (projection.View, error) {
	s.calls++
	return projection.View{}, nil
}

func TestProjectionRangeRejectsBeforeStorage(t *testing.T) {
	for _, query := range []string{
		"from=bad", "to=bad", "timezone=missing/zone", "from=0001-01-01T00:00:00Z", "to=0001-01-01T00:00:00Z",
		"from=2026-01-01T00:00:00Z&to=2027-01-01T00:00:00Z", "from=2026-01-01T00:00:00Z&to=2026-01-01T00:00:00Z",
		"from=2026-01-02T00:00:00Z&to=2026-01-01T00:00:00Z", "from=a&from=b", "to=a&to=b", "timezone=UTC&timezone=Asia/Tokyo", "from=",
	} {
		s := &rangeProjectionStub{}
		h := New(nil, nil, s, &stubOrganizationStore{}, nil, "", testLogger())
		r := httptest.NewRequest("GET", "/api/v1/people/peer/projection?"+query, nil)
		r.Header.Set("X-Demo-User-ID", "actor")
		r.Header.Set("X-Organization-ID", "org")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != 400 || s.calls != 0 {
			t.Fatalf("query=%s status=%d calls=%d body=%s", query, w.Code, s.calls, w.Body)
		}
	}
}

func TestProjectionRangeBoundariesAndMonth(t *testing.T) {
	start := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	for _, duration := range []time.Duration{31 * 24 * time.Hour, maxProjectionRange, maxProjectionRange + time.Second} {
		_, from, to, err := projectionRange(url.Values{"from": {start.Format(time.RFC3339)}, "to": {start.Add(duration).Format(time.RFC3339)}}, start)
		if (err == nil) != (duration <= maxProjectionRange) {
			t.Fatal(duration, err)
		}
		if err == nil && (!from.Equal(start) || !to.Equal(start.Add(duration)) || from.Location() != time.UTC) {
			t.Fatal("range changed")
		}
	}
	_, from, to, err := projectionRange(url.Values{"from": {start.Format(time.RFC3339)}, "timezone": {"UTC"}}, start.Add(365*24*time.Hour))
	if err != nil || !from.Equal(start) || !to.Equal(start.Add(24*time.Hour)) {
		t.Fatal("from-only range must be relative to from", err)
	}
}
