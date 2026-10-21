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

func TestProviderRejectsNestedAmbiguousEventEvidence(t *testing.T) {
	for _, body := range []string{
		`{"items":[{"id":"a","ID":"b"}]}`,
		`{"items":[{"id":"a","status":"cancelled","\u017ftatus":"confirmed"}]}`,
		`{"items":[{"start":{"dateTime":"private-secret","dateTime":"2026-10-21T10:00:00Z"}}]}`,
		`{"items":[{"attendees":[{"email":"a@example.test","EMAIL":"b@example.test"}]}]}`,
		`{"items":[],"unknown":` + strings.Repeat("[", providerJSONDepth) + `0` + strings.Repeat("]", providerJSONDepth) + `}`,
	} {
		for _, private := range []bool{false, true} {
			closed := &countedCalendarBody{reader: strings.NewReader(body)}
			p := NewGoogleProvider(GoogleConfig{}, &http.Client{Transport: limitTransport(func(r *http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: 200, Body: closed, Header: make(http.Header), Request: r}, nil
			})})
			var err error
			if private {
				v, e := p.ListPrivateEvents(context.Background(), "access", time.Now(), time.Now().Add(time.Hour))
				err = e
				if len(v) != 0 {
					t.Fatal("partial private events escaped")
				}
			} else {
				v, e := p.ListChanges(context.Background(), "access", "", time.Now(), time.Now().Add(time.Hour))
				err = e
				if len(v.Upserts) != 0 || len(v.DeletedProviderEventIDs) != 0 {
					t.Fatal("partial source evidence escaped")
				}
			}
			if !errors.Is(err, errCalendarResponse) || !closed.closed || strings.Contains(err.Error(), "private-secret") {
				t.Fatalf("unsafe parser result: %v", err)
			}
		}
	}
}

func TestProviderJSONDepthAndIndependentObjects(t *testing.T) {
	for _, body := range []string{
		`{"items":[{"id":"a","attendees":[{"email":"a"},{"email":"b"}]},{"id":"b"}]}`,
		`{"unknown":` + strings.Repeat("[", providerJSONDepth-1) + `0` + strings.Repeat("]", providerJSONDepth-1) + `}`,
	} {
		if err := validateProviderJSON(context.Background(), []byte(body)); err != nil {
			t.Fatal("valid nested object rejected", err)
		}
		var target map[string]any
		if err := newCalendarReadBudget(false).decode(context.Background(), &http.Response{Body: io.NopCloser(strings.NewReader(body))}, &target); err != nil {
			t.Fatal(err)
		}
	}
	for _, body := range []string{`{"a":1} {}`, `{"a":[}`, `{"a":1,"\u0061":2}`, `null`} {
		if err := validateProviderJSON(context.Background(), []byte(body)); err == nil {
			t.Fatal("malformed provider object accepted")
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := validateProviderJSON(ctx, []byte(`{"items":[]}`)); !errors.Is(err, context.Canceled) {
		t.Fatal("cancellation identity lost", err)
	}
}
