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

func TestCalendarReadsRejectAmbiguousEnvelopeWithoutPartialResults(t *testing.T) {
	for _, private := range []bool{false, true} {
		for _, duplicate := range []string{
			`"timeZone":"UTC","TIMEZONE":"Asia/Tokyo"`,
			`"nextPageToken":"","nextPageToken":"secret-marker"`,
			`"nextSyncToken":"one","nextſyncToken":"secret-marker"`,
			`"unknown":"one","UNKNOWN":"secret-marker"`,
		} {
			t.Run(fmt.Sprintf("private=%v/%s", private, duplicate), func(t *testing.T) {
				calls := 0
				provider := NewGoogleProvider(GoogleConfig{}, &http.Client{Transport: limitTransport(func(r *http.Request) (*http.Response, error) {
					calls++
					body := limitPage("next")
					if calls == 2 {
						body = `{"items":[],` + duplicate + `}`
					}
					return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
				})})
				var err error
				if private {
					var values []PrivateEventView
					values, err = provider.ListPrivateEvents(context.Background(), "access", time.Now(), time.Now().Add(time.Hour))
					if values != nil {
						t.Fatal("partial private result")
					}
				} else {
					var changes ChangeSet
					changes, err = provider.ListChanges(context.Background(), "access", "", time.Now(), time.Now().Add(time.Hour))
					if changes.Full || changes.NextSyncToken != "" || len(changes.Upserts) != 0 {
						t.Fatal("partial sync result")
					}
				}
				if !errors.Is(err, errCalendarResponse) || calls != 2 {
					t.Fatalf("ambiguous envelope accepted: calls=%d err=%v", calls, err)
				}
				if strings.Contains(err.Error(), "secret-marker") {
					t.Fatal("response content leaked")
				}
			})
		}
	}
}
