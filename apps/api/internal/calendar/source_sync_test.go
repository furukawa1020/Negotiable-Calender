package calendar

import (
	"context"
	"github.com/negotiable-calendar/negotiable-calendar/apps/api/internal/auth"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type sourceStubStore struct {
	backgroundStubStore
	source SourceSnapshot
}

func TestSourceStatusReturnsOnlyFreshnessDecision(t *testing.T) {
	now := time.Date(2026, 9, 18, 0, 0, 0, 0, time.UTC)
	for _, known := range []bool{false, true} {
		store := &sourceStubStore{}
		store.connection = Connection{UserID: "alice", LastSyncedAt: &now}
		if known {
			store.source = SourceSnapshot{Revision: "private-source-generation", ObservedAt: now, From: now.Add(-time.Hour), To: now.Add(time.Hour)}
		}
		h := NewHandler(http.NotFoundHandler(), store, &stubProvider{}, testCipher(t), &stubProjector{}, HandlerConfig{}, slog.New(slog.NewTextHandler(io.Discard, nil)))
		h.now = func() time.Time { return now }
		r := httptest.NewRequest(http.MethodGet, "/api/v1/calendar/connection", nil)
		r.Header.Set(auth.AuthenticatedUserHeader, "alice")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		want := `"sourceFresh":false`
		if known {
			want = `"sourceFresh":true`
		}
		if w.Code != 200 || !strings.Contains(w.Body.String(), want) || strings.Contains(w.Body.String(), "private-source-generation") {
			t.Fatalf("unsafe source status: %d %s", w.Code, w.Body.String())
		}
	}
}

func (s *sourceStubStore) LoadSourceSnapshot(context.Context, string) (SourceSnapshot, error) {
	return s.source, nil
}

func TestSyncRebasesUnknownUncommittedAndNarrowSources(t *testing.T) {
	now := time.Date(2026, 9, 18, 0, 0, 0, 0, time.UTC)
	for _, scenario := range []string{"delta", "unknown", "uncommitted", "narrow"} {
		t.Run(scenario, func(t *testing.T) {
			cipher := testCipher(t)
			encrypted, err := cipher.Encrypt("synthetic-refresh")
			if err != nil {
				t.Fatal(err)
			}
			store := &sourceStubStore{source: SourceSnapshot{Revision: "old", ObservedAt: now, From: now.Add(-30 * 24 * time.Hour), To: now.Add(90 * 24 * time.Hour)}}
			store.connection = Connection{UserID: "alice", RefreshTokenCipher: encrypted, SyncToken: "old-cursor", LastSyncedAt: &now}
			want := ""
			switch scenario {
			case "delta":
				want = "old-cursor"
			case "unknown":
				store.source = SourceSnapshot{}
			case "uncommitted":
				store.connection.LastSyncedAt = nil
			case "narrow":
				store.source.To = now.Add(24 * time.Hour)
			}
			provider := &incrementalStubProvider{results: []ChangeSet{{Full: true, NextSyncToken: "next"}}}
			h := NewHandler(http.NotFoundHandler(), store, provider, cipher, &stubProjector{}, HandlerConfig{}, slog.New(slog.NewTextHandler(io.Discard, nil)))
			h.now = func() time.Time { return now }
			if _, err := h.SyncUser(context.Background(), "alice"); err != nil {
				t.Fatal(err)
			}
			if len(provider.cursors) != 1 || provider.cursors[0] != want {
				t.Fatal("unsafe cursor base", provider.cursors)
			}
		})
	}
}
