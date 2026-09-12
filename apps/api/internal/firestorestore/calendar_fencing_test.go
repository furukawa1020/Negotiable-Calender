package firestorestore

import (
	"context"
	"encoding/base64"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"reflect"
	"testing"
	"time"

	calendarintegration "github.com/negotiable-calendar/negotiable-calendar/apps/api/internal/calendar"
	"github.com/negotiable-calendar/negotiable-calendar/apps/api/internal/projection"
)

type pausedCalendarProvider struct {
	entered chan struct{}
	resume  chan struct{}
	fail    bool
	now     time.Time
}

func (*pausedCalendarProvider) Configured() bool                       { return true }
func (*pausedCalendarProvider) AuthorizationURL(string, string) string { return "" }
func (*pausedCalendarProvider) Exchange(context.Context, string, string) (calendarintegration.TokenSet, error) {
	return calendarintegration.TokenSet{}, nil
}
func (p *pausedCalendarProvider) Refresh(ctx context.Context, _ string) (calendarintegration.TokenSet, error) {
	close(p.entered)
	select {
	case <-p.resume:
	case <-ctx.Done():
		return calendarintegration.TokenSet{}, ctx.Err()
	}
	if p.fail {
		return calendarintegration.TokenSet{}, calendarintegration.ErrReconnectRequired
	}
	return calendarintegration.TokenSet{AccessToken: "synthetic"}, nil
}
func (p *pausedCalendarProvider) ListBusy(context.Context, string, time.Time, time.Time) ([]calendarintegration.BusySpan, error) {
	return []calendarintegration.BusySpan{{ProviderEventID: "old-event", CalendarID: "primary", StartAt: p.now, EndAt: p.now.Add(time.Hour), Busy: true}}, nil
}
func (p *pausedCalendarProvider) ListChanges(ctx context.Context, access, cursor string, from, to time.Time) (calendarintegration.ChangeSet, error) {
	spans, err := p.ListBusy(ctx, access, from, to)
	return calendarintegration.ChangeSet{Full: true, Upserts: spans, NextSyncToken: "old-cursor"}, err
}

func TestCalendarSyncFencingWithPausedProvider(t *testing.T) {
	for _, scenario := range []string{"disconnect", "reconnect", "stale-failure", "expired-lease", "overlapping-sync"} {
		t.Run(scenario, func(t *testing.T) {
			b, ctx := emulatorBackend(t)
			now := time.Now().UTC()
			putDocument(t, ctx, b.Client.Collection("users").Doc("alice"), userRecord{ID: "alice", Timezone: "Asia/Tokyo"})
			cipher, err := calendarintegration.NewTokenCipher(base64.StdEncoding.EncodeToString(make([]byte, 32)))
			if err != nil {
				t.Fatal(err)
			}
			encrypted, err := cipher.Encrypt("synthetic")
			if err != nil {
				t.Fatal(err)
			}
			connection := calendarintegration.Connection{UserID: "alice", ConnectedAt: now, RefreshTokenCipher: encrypted}
			if err := b.Calendar().SaveConnection(ctx, connection); err != nil {
				t.Fatal(err)
			}
			provider := &pausedCalendarProvider{entered: make(chan struct{}), resume: make(chan struct{}), fail: scenario == "stale-failure", now: now}
			syncctx, cancel := context.WithCancel(ctx)
			defer cancel()
			handler := calendarintegration.NewHandler(http.NotFoundHandler(), b.Calendar(), provider, cipher, projection.NewRebuilder(b.Calendar(), b.Policy()), calendarintegration.HandlerConfig{SyncPast: time.Hour, SyncFuture: 24 * time.Hour}, slog.New(slog.NewTextHandler(io.Discard, nil)))
			done := make(chan error, 1)
			go func() { _, err := handler.SyncUser(syncctx, "alice"); done <- err }()
			select {
			case <-provider.entered:
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
			ref := b.Client.Collection("calendarConnections").Doc("alice")
			switch scenario {
			case "disconnect":
				if err := b.Calendar().DeleteConnection(ctx, "alice"); err != nil {
					t.Fatal(err)
				}
			case "reconnect", "stale-failure":
				connection.ConnectedAt = now.Add(time.Second)
				if err := b.Calendar().SaveConnection(ctx, connection); err != nil {
					t.Fatal(err)
				}
			case "expired-lease":
				doc, err := ref.Get(ctx)
				if err != nil {
					t.Fatal(err)
				}
				var current calendarintegration.Connection
				if err := doc.DataTo(&current); err != nil {
					t.Fatal(err)
				}
				expired := now.Add(-time.Minute)
				current.SyncLeaseUntil = &expired
				putDocument(t, ctx, ref, current)
				if _, err := b.Calendar().AcquireSync(ctx, "alice", time.Now().UTC(), time.Minute); err != nil {
					t.Fatal(err)
				}
			case "overlapping-sync":
				_, err := handler.SyncUser(ctx, "alice")
				if !errors.Is(err, calendarintegration.ErrSyncBusy) {
					t.Fatalf("overlap not rejected: %v", err)
				}
			}
			var before map[string]any
			if scenario != "disconnect" {
				doc, err := ref.Get(ctx)
				if err != nil {
					t.Fatal(err)
				}
				before = doc.Data()
			}
			close(provider.resume)
			select {
			case err = <-done:
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
			if scenario == "overlapping-sync" {
				if err != nil {
					t.Fatal(err)
				}
				current, err := b.Calendar().GetConnection(ctx, "alice")
				if err != nil || current.SyncToken != "old-cursor" || current.SyncLeaseID != "" {
					t.Fatalf("valid owner did not finish: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatal("obsolete sync succeeded")
			}
			if scenario == "disconnect" {
				if _, err := ref.Get(ctx); !firestoreNotFound(err) {
					t.Fatalf("connection resurrected: %v", err)
				}
			} else {
				doc, err := ref.Get(ctx)
				if err != nil {
					t.Fatal(err)
				}
				if !reflect.DeepEqual(before, doc.Data()) {
					t.Fatal("obsolete sync changed new connection or lease")
				}
			}
			for _, name := range []string{"privateEvents", "scheduleProjections"} {
				docs, err := b.Client.Collection("users").Doc("alice").Collection(name).Documents(ctx).GetAll()
				if err != nil || len(docs) != 0 {
					t.Fatalf("obsolete sync wrote %s: %d %v", name, len(docs), err)
				}
			}
		})
	}
}

func TestCalendarFencesEveryWriteAfterReconnect(t *testing.T) {
	b, ctx := emulatorBackend(t)
	now := time.Now().UTC()
	connection := calendarintegration.Connection{UserID: "alice", ConnectedAt: now}
	if err := b.Calendar().SaveConnection(ctx, connection); err != nil {
		t.Fatal(err)
	}
	lease, err := b.Calendar().AcquireSync(ctx, "alice", now, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	stale := calendarintegration.WithSyncLease(ctx, calendarintegration.SyncLease{UserID: "alice", ID: lease.SyncLeaseID})
	if err := b.Calendar().SaveConnection(ctx, connection); err != nil {
		t.Fatal(err)
	}
	span := calendarintegration.BusySpan{ProviderEventID: "stale", CalendarID: "primary", StartAt: now, EndAt: now.Add(time.Hour), Busy: true}
	for name, write := range map[string]func() error{
		"events": func() error {
			return b.Calendar().ApplyChanges(stale, "alice", calendarintegration.ChangeSet{Upserts: []calendarintegration.BusySpan{span}}, now, now.Add(time.Hour), now)
		},
		"success":            func() error { return b.Calendar().MarkSyncSuccess(stale, "alice", "old", now, now.Add(time.Hour)) },
		"failure":            func() error { return b.Calendar().MarkSyncFailure(stale, "alice", "old", now, true) },
		"legacy-success":     func() error { return b.Calendar().MarkSynced(stale, "alice", now) },
		"reconnect-required": func() error { return b.Calendar().MarkReconnectRequired(stale, "alice") },
	} {
		if err := write(); !errors.Is(err, calendarintegration.ErrSyncLeaseLost) {
			t.Fatalf("%s allowed stale writer: %v", name, err)
		}
	}
	// A queued second batch cannot commit after reconnect, even after the first committed.
	current, err := b.Calendar().AcquireSync(ctx, "alice", time.Now().UTC(), time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	active := calendarintegration.WithSyncLease(ctx, calendarintegration.SyncLease{UserID: "alice", ID: current.SyncLeaseID})
	writer := newChunkedBatch(b.Client, "alice")
	ref := b.Client.Collection("users").Doc("alice").Collection("scheduleProjections").Doc("old")
	if err := writer.Set(active, ref, map[string]any{"synthetic": true}); err != nil {
		t.Fatal(err)
	}
	if err := writer.Commit(active); err != nil {
		t.Fatal(err)
	}
	if err := writer.Delete(active, ref); err != nil {
		t.Fatal(err)
	}
	if err := b.Calendar().SaveConnection(ctx, connection); err != nil {
		t.Fatal(err)
	}
	if err := writer.Commit(active); !errors.Is(err, calendarintegration.ErrSyncLeaseLost) {
		t.Fatalf("stale batch committed: %v", err)
	}
	if _, err := ref.Get(ctx); err != nil {
		t.Fatal("stale batch removed published data")
	}
}

func TestCalendarCleanupFencingAndRetry(t *testing.T) {
	b, ctx := emulatorBackend(t)
	now := time.Now().UTC()
	until := now.Add(time.Minute)
	putDocument(t, ctx, b.projectionBlock("alice"), publicationControl{BlockedAt: now, CleanupID: "old", CleanupUntil: &until, InProgress: true})
	if err := b.Calendar().SaveConnection(ctx, calendarintegration.Connection{UserID: "alice"}); !errors.Is(err, calendarintegration.ErrSyncBusy) {
		t.Fatalf("reconnect during cleanup: %v", err)
	}
	if err := b.Calendar().DeleteConnection(ctx, "alice"); !errors.Is(err, calendarintegration.ErrSyncBusy) {
		t.Fatalf("overlapping cleanup: %v", err)
	}
	expired := now.Add(-time.Minute)
	putDocument(t, ctx, b.projectionBlock("alice"), publicationControl{BlockedAt: now, CleanupID: "old", CleanupUntil: &expired, InProgress: true})
	old := context.WithValue(ctx, cleanupLeaseKey{}, cleanupLease{UserID: "alice", ID: "old"})
	if err := b.Calendar().DeleteConnection(ctx, "alice"); err != nil {
		t.Fatal(err)
	}
	if err := b.Calendar().SaveConnection(ctx, calendarintegration.Connection{UserID: "alice", ConnectedAt: now}); err != nil {
		t.Fatal(err)
	}
	putDocument(t, ctx, b.Client.Collection("users").Doc("alice").Collection("privateEvents").Doc("new"), map[string]any{"synthetic": true})
	if err := deleteCollection(old, b.Client, b.Client.Collection("users").Doc("alice").Collection("privateEvents"), 200); !errors.Is(err, calendarintegration.ErrSyncLeaseLost) {
		t.Fatalf("old cleanup resumed: %v", err)
	}
}
