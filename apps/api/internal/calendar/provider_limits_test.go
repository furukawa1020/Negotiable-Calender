package calendar

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

type limitTransport func(*http.Request) (*http.Response, error)

func (f limitTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func limitPage(token string) string {
	return fmt.Sprintf(`{"timeZone":"UTC","items":[],"nextPageToken":%q,"nextSyncToken":"end"}`, token)
}

func TestCalendarReadsRejectUnboundedOrIncompleteResponses(t *testing.T) {
	for _, private := range []bool{false, true} {
		for _, scenario := range []string{"page-loop", "page-cycle", "pages", "page-bytes", "total-bytes", "raw-items", "cursor-size", "trailing-json", "late-page-bytes"} {
			t.Run(fmt.Sprintf("private=%v/%s", private, scenario), func(t *testing.T) {
				calls := 0
				client := &http.Client{Transport: limitTransport(func(r *http.Request) (*http.Response, error) {
					calls++
					body := limitPage("")
					switch scenario {
					case "page-loop":
						if calls < 4 {
							body = limitPage("loop-private-token")
						}
					case "page-cycle":
						if calls < 5 {
							token := "a"
							if calls%2 == 0 {
								token = "b"
							}
							body = limitPage(token)
						}
					case "pages":
						if calls < 21 {
							body = limitPage(fmt.Sprint(calls))
						}
					case "page-bytes":
						body += strings.Repeat(" ", 2*1024*1024)
					case "late-page-bytes":
						if calls == 1 {
							body = limitPage("next")
						} else {
							body += strings.Repeat(" ", 2*1024*1024)
						}
					case "total-bytes":
						last := 9
						if private {
							last = 5
						}
						if calls < last {
							body = limitPage(fmt.Sprint(calls))
						}
						body += strings.Repeat(" ", 2*1024*1024-len(body))
					case "raw-items":
						count := 10001
						if private {
							count = 1001
						}
						item := `{"id":"cancelled","status":"cancelled"}`
						body = `{"timeZone":"UTC","items":[` + strings.TrimSuffix(strings.Repeat(item+",", count), ",") + `],"nextSyncToken":"end"}`
					case "cursor-size":
						if calls == 1 {
							body = limitPage(strings.Repeat("x", 16*1024+1))
						}
					case "trailing-json":
						body += ` {"private":"must-not-ignore"}`
					}
					return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
				})}
				provider := NewGoogleProvider(GoogleConfig{}, client)
				var err error
				if private {
					var values []PrivateEventView
					values, err = provider.ListPrivateEvents(context.Background(), "access", time.Now(), time.Now().Add(time.Hour))
					if values != nil {
						t.Error("returned partial private calendar")
					}
				} else {
					var changes ChangeSet
					changes, err = provider.ListChanges(context.Background(), "access", "", time.Now(), time.Now().Add(time.Hour))
					if changes.NextSyncToken != "" || len(changes.Upserts) != 0 || len(changes.DeletedProviderEventIDs) != 0 || changes.Full {
						t.Error("returned partial sync evidence")
					}
				}
				if err == nil {
					t.Fatal("unbounded/incomplete response accepted")
				}
				if strings.Contains(err.Error(), "private-token") || strings.Contains(err.Error(), "must-not-ignore") {
					t.Fatal("provider content leaked in error")
				}
				if scenario == "page-loop" && calls != 2 {
					t.Errorf("loop requests=%d want 2", calls)
				}
				if scenario == "page-cycle" && calls != 3 {
					t.Errorf("cycle requests=%d want 3", calls)
				}
				if scenario == "pages" && calls != 20 {
					t.Errorf("page requests=%d want 20", calls)
				}
				if scenario == "cursor-size" && calls != 1 {
					t.Errorf("oversized cursor was requested")
				}
			})
		}
	}
}

func TestCalendarReadExactPageByteAndItemLimits(t *testing.T) {
	for _, private := range []bool{false, true} {
		for _, boundary := range []string{"pages", "page-bytes", "items", "total-bytes"} {
			t.Run(fmt.Sprintf("private=%v/%s", private, boundary), func(t *testing.T) {
				calls := 0
				provider := NewGoogleProvider(GoogleConfig{}, &http.Client{Transport: limitTransport(func(r *http.Request) (*http.Response, error) {
					calls++
					body := limitPage("")
					if boundary == "pages" && calls < 20 {
						body = limitPage(fmt.Sprint(calls))
					}
					if boundary == "items" {
						count := 10000
						if private {
							count = 1000
						}
						item := `{"id":"cancelled","status":"cancelled"}`
						body = `{"timeZone":"UTC","items":[` + strings.TrimSuffix(strings.Repeat(item+",", count), ",") + `],"nextSyncToken":"end"}`
					}
					if boundary == "total-bytes" {
						last := 8
						if private {
							last = 4
						}
						if calls < last {
							body = limitPage(fmt.Sprint(calls))
						}
					}
					if boundary == "page-bytes" || boundary == "total-bytes" {
						body += strings.Repeat(" ", 2*1024*1024-len(body))
					}
					return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
				})})
				var err error
				if private {
					_, err = provider.ListPrivateEvents(context.Background(), "access", time.Now(), time.Now().Add(time.Hour))
				} else {
					_, err = provider.ListChanges(context.Background(), "access", "", time.Now(), time.Now().Add(time.Hour))
				}
				if err != nil {
					t.Fatal("exact boundary rejected", err)
				}
			})
		}
	}
}
