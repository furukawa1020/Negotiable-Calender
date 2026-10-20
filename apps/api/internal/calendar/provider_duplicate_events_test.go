package calendar

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestSyncRejectsDuplicateEventIdentities(t *testing.T) {
	live := `{"id":"same","start":{"date":"2026-10-20"},"end":{"date":"2026-10-21"}}`
	deleted := `{"id":"same","status":"cancelled"}`
	for _, delta := range []bool{false, true} {
		for _, paginated := range []bool{false, true} {
			for _, second := range []string{live, deleted} {
				t.Run(fmt.Sprintf("delta=%v/pages=%v/deleted=%v", delta, paginated, second == deleted), func(t *testing.T) {
					calls := 0
					provider := NewGoogleProvider(GoogleConfig{}, &http.Client{Transport: limitTransport(func(r *http.Request) (*http.Response, error) {
						calls++
						items, next := live+","+second, ""
						if paginated {
							if calls == 1 {
								items, next = live, "next"
							} else {
								items = second
							}
						}
						body := fmt.Sprintf(`{"timeZone":"UTC","items":[%s],"nextPageToken":%q,"nextSyncToken":"end"}`, items, next)
						return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
					})})
					token := ""
					if delta {
						token = encodeGoogleBusyCursor("UTC", "delta")
					}
					got, err := provider.ListChanges(context.Background(), "access", token, time.Now(), time.Now().Add(time.Hour))
					if !errors.Is(err, errCalendarResponse) || got.Full || got.NextSyncToken != "" || len(got.Upserts) != 0 || len(got.DeletedProviderEventIDs) != 0 {
						t.Fatalf("ambiguous changes escaped: %+v %v", got, err)
					}
					wantCalls := 1
					if paginated {
						wantCalls = 2
					}
					if calls != wantCalls {
						t.Fatalf("calls=%d want %d", calls, wantCalls)
					}
				})
			}
		}
	}
}
