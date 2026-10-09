package calendar

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func testTimezoneCursor(zone, token string) string {
	data, _ := json.Marshal(map[string]string{"timeZone": zone, "token": token})
	return "gcal-busy-v2." + base64.RawURLEncoding.EncodeToString(data)
}

func TestGoogleAllDayBusyUsesCalendarTimezone(t *testing.T) {
	for _, tc := range []struct{ zone, start, end, utcStart, utcEnd string }{
		{"Asia/Tokyo", "2026-10-09", "2026-10-10", "2026-10-08T15:00:00Z", "2026-10-09T15:00:00Z"},
		{"America/Los_Angeles", "2026-10-09", "2026-10-10", "2026-10-09T07:00:00Z", "2026-10-10T07:00:00Z"},
		{"America/New_York", "2026-03-08", "2026-03-09", "2026-03-08T05:00:00Z", "2026-03-09T04:00:00Z"},
		{"America/New_York", "2026-11-01", "2026-11-02", "2026-11-01T04:00:00Z", "2026-11-02T05:00:00Z"},
		{"Asia/Kathmandu", "2026-10-09", "2026-10-12", "2026-10-08T18:15:00Z", "2026-10-11T18:15:00Z"},
	} {
		t.Run(tc.zone+tc.start, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if !strings.Contains(r.URL.Query().Get("fields"), "timeZone") || r.URL.Query().Get("timeZone") != "" {
					t.Error("calendar timezone metadata missing or overridden")
				}
				fmt.Fprintf(w, `{"timeZone":%q,"items":[{"id":"all-day","start":{"date":%q},"end":{"date":%q}}],"nextSyncToken":"next"}`, tc.zone, tc.start, tc.end)
			}))
			defer server.Close()
			provider := NewGoogleProvider(GoogleConfig{}, server.Client())
			provider.eventsURL = server.URL
			changes, err := provider.ListChanges(context.Background(), "access", "", time.Now(), time.Now().Add(time.Hour))
			if err != nil || len(changes.Upserts) != 1 {
				t.Fatal("missing busy evidence", err)
			}
			span := changes.Upserts[0]
			if span.StartAt.Format(time.RFC3339) != tc.utcStart || span.EndAt.Format(time.RFC3339) != tc.utcEnd || !span.Busy {
				t.Fatalf("wrong busy range: %v - %v", span.StartAt, span.EndAt)
			}
			if changes.NextSyncToken != testTimezoneCursor(tc.zone, "next") {
				t.Error("cursor not bound to interpretation and timezone")
			}
		})
	}
}

func TestGoogleCalendarRebasesLegacyCursorAndTimezoneChanges(t *testing.T) {
	for _, tc := range []struct {
		name, cursor, zone string
		full, expired      bool
	}{
		{"legacy", "old-raw-token", "Asia/Tokyo", true, false},
		{"corrupt-envelope", "gcal-busy-v2.not-json", "Asia/Tokyo", true, false},
		{"delta", testTimezoneCursor("Asia/Tokyo", "old"), "Asia/Tokyo", false, false},
		{"zone-change", testTimezoneCursor("Asia/Tokyo", "old"), "America/New_York", false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				q := r.URL.Query()
				if tc.full && (q.Get("syncToken") != "" || q.Get("timeMin") == "") {
					t.Error("legacy cursor did not rebase")
				}
				if !tc.full && (q.Get("syncToken") != "old" || q.Get("timeMin") != "") {
					t.Error("local cursor envelope sent to Google")
				}
				fmt.Fprintf(w, `{"timeZone":%q,"items":[],"nextSyncToken":"next"}`, tc.zone)
			}))
			defer server.Close()
			provider := NewGoogleProvider(GoogleConfig{}, server.Client())
			provider.eventsURL = server.URL
			changes, err := provider.ListChanges(context.Background(), "access", tc.cursor, time.Now(), time.Now().Add(time.Hour))
			if tc.expired {
				if !errors.Is(err, ErrSyncTokenExpired) || changes.NextSyncToken != "" {
					t.Fatal("timezone change did not invalidate delta", err)
				}
			} else if err != nil || changes.Full != tc.full || changes.NextSyncToken != testTimezoneCursor(tc.zone, "next") {
				t.Fatal("invalid result", err)
			}
		})
	}
}

func TestGoogleMalformedBusyEvidenceDoesNotReturnPartialChanges(t *testing.T) {
	for _, tc := range []struct{ name, zone, item string }{
		{"missing-zone", "", `{"id":"bad","start":{"date":"2026-10-09"},"end":{"date":"2026-10-10"}}`},
		{"invalid-zone", "Not/AZone", `{"id":"bad","start":{"date":"2026-10-09"},"end":{"date":"2026-10-10"}}`},
		{"host-local-zone", "Local", `{"id":"bad","start":{"date":"2026-10-09"},"end":{"date":"2026-10-10"}}`},
		{"missing-id", "UTC", `{"start":{"date":"2026-10-09"},"end":{"date":"2026-10-10"}}`},
		{"invalid-time", "UTC", `{"id":"bad","start":{"dateTime":"private-invalid"},"end":{"dateTime":"2026-10-10T00:00:00Z"}}`},
		{"mixed-kinds", "UTC", `{"id":"bad","start":{"date":"2026-10-09"},"end":{"dateTime":"2026-10-10T00:00:00Z"}}`},
		{"both-kinds", "UTC", `{"id":"bad","start":{"date":"2026-10-09","dateTime":"2026-10-09T00:00:00Z"},"end":{"date":"2026-10-10"}}`},
		{"empty-range", "UTC", `{"id":"bad","start":{"date":"2026-10-09"},"end":{"date":"2026-10-09"}}`},
		{"invalid-date", "UTC", `{"id":"bad","start":{"date":"2026-02-30"},"end":{"date":"2026-03-02"}}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Query().Get("pageToken") == "" {
					io.WriteString(w, `{"timeZone":"UTC","items":[{"id":"good","start":{"dateTime":"2026-10-09T00:00:00Z"},"end":{"dateTime":"2026-10-09T01:00:00Z"}}],"nextPageToken":"second"}`)
				} else {
					fmt.Fprintf(w, `{"timeZone":%q,"items":[%s],"nextSyncToken":"next"}`, tc.zone, tc.item)
				}
			}))
			defer server.Close()
			provider := NewGoogleProvider(GoogleConfig{}, server.Client())
			provider.eventsURL = server.URL
			changes, err := provider.ListChanges(context.Background(), "access", "", time.Now(), time.Now().Add(time.Hour))
			if err == nil || len(changes.Upserts) != 0 || len(changes.DeletedProviderEventIDs) != 0 || changes.NextSyncToken != "" {
				t.Fatal("partial source returned as success")
			}
			if strings.Contains(err.Error(), "private-invalid") || strings.Contains(err.Error(), "bad") {
				t.Fatal("provider evidence leaked in error")
			}
		})
	}
}

func TestMalformedProviderSyncDoesNotPublishOrAdvanceCursor(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/token" {
			io.WriteString(w, `{"access_token":"access","expires_in":3600}`)
			return
		}
		io.WriteString(w, `{"timeZone":"Asia/Tokyo","items":[{"id":"bad","start":{"date":"invalid"},"end":{"date":"2026-10-10"}}],"nextSyncToken":"must-not-commit"}`)
	}))
	defer server.Close()
	provider := NewGoogleProvider(GoogleConfig{ClientID: "client", RedirectURL: "https://example.test/callback"}, server.Client())
	provider.tokenURL, provider.eventsURL = server.URL+"/token", server.URL+"/events"
	cipher := testCipher(t)
	encrypted, err := cipher.Encrypt("refresh")
	if err != nil {
		t.Fatal(err)
	}
	store := &backgroundStubStore{stubStore: stubStore{connection: Connection{UserID: "u", RefreshTokenCipher: encrypted, SyncToken: "legacy"}}}
	projector := &stubProjector{}
	handler := NewHandler(http.NotFoundHandler(), store, provider, cipher, projector, HandlerConfig{}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	_, err = handler.SyncUser(context.Background(), "u")
	var failure *SyncFailure
	if !errors.As(err, &failure) || failure.Code != "calendar_read_failed" {
		t.Fatal("invalid source not rejected", err)
	}
	if store.changes.NextSyncToken != "" || projector.rebuilt || store.successToken != "" || store.connection.SyncToken != "legacy" || store.failureCode == "" || store.reconnect {
		t.Fatal("failure published or committed source")
	}
}

func TestGoogleTimezonePaginationAndCancellation(t *testing.T) {
	for _, delta := range []bool{false, true} {
		t.Run(fmt.Sprint(delta), func(t *testing.T) {
			pages := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				pages++
				if delta && r.URL.Query().Get("syncToken") != "old" {
					t.Error("delta cursor changed across pages")
				}
				if r.URL.Query().Get("pageToken") == "" {
					io.WriteString(w, `{"timeZone":"Asia/Tokyo","items":[{"id":"day","start":{"date":"2026-10-09"},"end":{"date":"2026-10-10"}}],"nextPageToken":"page2"}`)
				} else {
					io.WriteString(w, `{"timeZone":"Asia/Tokyo","items":[{"id":"cancelled","status":"cancelled"},{"id":"free","transparency":"transparent","start":{"dateTime":"2026-10-09T10:00:00+09:00"},"end":{"dateTime":"2026-10-09T11:00:00+09:00"}}],"nextSyncToken":"new"}`)
				}
			}))
			defer server.Close()
			provider := NewGoogleProvider(GoogleConfig{}, server.Client())
			provider.eventsURL = server.URL
			cursor := ""
			if delta {
				cursor = testTimezoneCursor("Asia/Tokyo", "old")
			}
			changes, err := provider.ListChanges(context.Background(), "access", cursor, time.Now(), time.Now().Add(time.Hour))
			if err != nil || pages != 2 || len(changes.Upserts) != 2 || changes.Upserts[1].Busy || changes.Upserts[1].StartAt.Format(time.RFC3339) != "2026-10-09T01:00:00Z" {
				t.Fatal("pagination lost evidence", err)
			}
			wantDeleted := 0
			if delta {
				wantDeleted = 1
			}
			if len(changes.DeletedProviderEventIDs) != wantDeleted || changes.NextSyncToken != testTimezoneCursor("Asia/Tokyo", "new") {
				t.Fatal("cursor or cancellation mismatch")
			}
		})
	}
}

func TestCalendarTimezoneChangeRecoversThroughFullSync(t *testing.T) {
	queries := []string{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/token" {
			io.WriteString(w, `{"access_token":"access","expires_in":3600}`)
			return
		}
		queries = append(queries, r.URL.Query().Get("syncToken"))
		io.WriteString(w, `{"timeZone":"Asia/Tokyo","items":[{"id":"day","start":{"date":"2026-10-09"},"end":{"date":"2026-10-10"}}],"nextSyncToken":"new"}`)
	}))
	defer server.Close()
	provider := NewGoogleProvider(GoogleConfig{ClientID: "client", RedirectURL: "https://example.test/callback"}, server.Client())
	provider.tokenURL, provider.eventsURL = server.URL+"/token", server.URL+"/events"
	cipher := testCipher(t)
	encrypted, err := cipher.Encrypt("refresh")
	if err != nil {
		t.Fatal(err)
	}
	store := &backgroundStubStore{stubStore: stubStore{connection: Connection{UserID: "u", RefreshTokenCipher: encrypted, SyncToken: testTimezoneCursor("UTC", "old")}}}
	projector := &stubProjector{}
	handler := NewHandler(http.NotFoundHandler(), store, provider, cipher, projector, HandlerConfig{}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if _, err := handler.SyncUser(context.Background(), "u"); err != nil {
		t.Fatal(err)
	}
	if len(queries) != 2 || queries[0] != "old" || queries[1] != "" || !store.changes.Full || !projector.rebuilt || store.successToken != testTimezoneCursor("Asia/Tokyo", "new") {
		t.Fatal("timezone change did not fully rebase")
	}
}
