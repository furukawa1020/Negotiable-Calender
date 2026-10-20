package calendar

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestPrivateCalendarRejectsInvalidEvidenceOnLaterPage(t *testing.T) {
	for _, item := range []string{
		`{"id":"bad","start":{"dateTime":"invalid"},"end":{"dateTime":"2026-10-21T00:00:00Z"}}`,
		`{"id":"bad","start":{"date":"2026-10-20"},"end":{"dateTime":"2026-10-21T00:00:00Z"}}`,
		`{"id":"bad","start":{"date":"2026-10-20","dateTime":"2026-10-20T00:00:00Z"},"end":{"date":"2026-10-21"}}`,
		`{"id":"bad","start":{"date":"2026-02-30"},"end":{"date":"2026-03-01"}}`,
		`{"id":"bad","start":{"date":"2026-10-21"},"end":{"date":"2026-10-20"}}`,
		`{"id":"bad","start":{"date":"2026-10-20"},"end":{"date":"2026-10-20"}}`,
		`{"id":"bad","start":{"dateTime":"2026-10-21T01:00:00Z"},"end":{"dateTime":"2026-10-21T00:00:00Z"}}`,
		`{"id":"bad","start":{"date":"2026-10-20"}}`,
		`{"id":" ","start":{"date":"2026-10-20"},"end":{"date":"2026-10-21"}}`,
		`{"start":{"date":"2026-10-20"},"end":{"date":"2026-10-21"}}`,
	} {
		t.Run(item, func(t *testing.T) {
			calls := 0
			provider := NewGoogleProvider(GoogleConfig{}, &http.Client{Transport: limitTransport(func(r *http.Request) (*http.Response, error) {
				calls++
				body := `{"items":[{"id":"valid","start":{"date":"2026-10-20"},"end":{"date":"2026-10-21"}}],"nextPageToken":"next"}`
				if calls == 2 {
					body = `{"items":[` + item + `]}`
				}
				return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
			})})
			got, err := provider.ListPrivateEvents(context.Background(), "access", time.Now(), time.Now().Add(time.Hour))
			if !errors.Is(err, errCalendarEvidence) || got != nil || calls != 2 {
				t.Fatalf("invalid event silently omitted/partial results returned: %v %v calls=%d", got, err, calls)
			}
		})
	}
}

func TestPrivateCalendarPreservesAllDayDatesAndDSTElapsedTimes(t *testing.T) {
	body := `{"items":[
		{"id":"date","start":{"date":"2026-03-08"},"end":{"date":"2026-03-09"}},
		{"id":"spring","start":{"dateTime":"2026-03-08T00:00:00-05:00"},"end":{"dateTime":"2026-03-09T00:00:00-04:00"}},
		{"id":"fall","start":{"dateTime":"2026-11-01T00:00:00-04:00"},"end":{"dateTime":"2026-11-02T00:00:00-05:00"}},
		{"id":"cancelled","status":"cancelled"}
	]}`
	provider := NewGoogleProvider(GoogleConfig{}, &http.Client{Transport: limitTransport(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
	})})
	got, err := provider.ListPrivateEvents(context.Background(), "access", time.Now(), time.Now().Add(time.Hour))
	if err != nil || len(got) != 3 {
		t.Fatalf("%v %v", got, err)
	}
	if !got[0].AllDay || got[0].StartDate != "2026-03-08" || got[0].EndDate != "2026-03-09" || got[0].StartAt != nil {
		t.Fatal("all-day dates reinterpreted")
	}
	for index, hours := range []time.Duration{23 * time.Hour, 25 * time.Hour} {
		event := got[index+1]
		if event.StartAt == nil || event.EndAt == nil || event.EndAt.Sub(*event.StartAt) != hours || event.StartAt.Location() != time.UTC {
			t.Fatal("DST elapsed interval changed")
		}
	}
}
