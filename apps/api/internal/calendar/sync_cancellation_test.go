package calendar

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"testing"
	"time"
)

type cancellationSyncStore struct {
	stubStore
	hit   func(string)
	calls []string
}

func (s *cancellationSyncStore) GetConnection(ctx context.Context, id string) (Connection, error) {
	s.calls = append(s.calls, "load")
	s.hit("load")
	return s.stubStore.GetConnection(ctx, id)
}
func (s *cancellationSyncStore) ReplaceBusySpans(ctx context.Context, id string, v []BusySpan, a, b, c time.Time) error {
	s.calls = append(s.calls, "write")
	s.hit("write")
	return s.stubStore.ReplaceBusySpans(ctx, id, v, a, b, c)
}
func (s *cancellationSyncStore) MarkSynced(ctx context.Context, id string, at time.Time) error {
	s.calls = append(s.calls, "complete")
	s.hit("complete")
	return s.stubStore.MarkSynced(ctx, id, at)
}

type cancellationSyncProvider struct {
	stubProvider
	hit   func(string)
	calls []string
}

func (p *cancellationSyncProvider) Refresh(ctx context.Context, v string) (TokenSet, error) {
	p.calls = append(p.calls, "refresh")
	p.hit("refresh")
	return p.stubProvider.Refresh(ctx, v)
}
func (p *cancellationSyncProvider) ListBusy(ctx context.Context, v string, a, b time.Time) ([]BusySpan, error) {
	p.calls = append(p.calls, "read")
	p.hit("read")
	return p.stubProvider.ListBusy(ctx, v, a, b)
}

type cancellationSyncProjector struct {
	hit   func(string)
	calls int
}

func (p *cancellationSyncProjector) Rebuild(context.Context, string, time.Time, time.Time, time.Time) error {
	p.calls++
	p.hit("project")
	return nil
}

func TestSyncStopsAfterCancelledSuccessfulDependency(t *testing.T) {
	stages := []string{"entry", "load", "refresh", "read", "write", "project", "complete"}
	for index, stage := range stages {
		t.Run(stage, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			var visited []string
			hit := func(name string) {
				visited = append(visited, name)
				if name == stage {
					cancel()
				}
			}
			cipher := testCipher(t)
			encrypted, _ := cipher.Encrypt("refresh-secret")
			s := &cancellationSyncStore{stubStore: stubStore{connection: Connection{UserID: "u", RefreshTokenCipher: encrypted}}, hit: hit}
			p := &cancellationSyncProvider{hit: hit}
			projector := &cancellationSyncProjector{hit: hit}
			h := NewHandler(http.NotFoundHandler(), s, p, cipher, projector, HandlerConfig{}, slog.New(slog.NewTextHandler(io.Discard, nil)))
			if stage == "entry" {
				cancel()
			}
			result, err := h.SyncUser(ctx, "u")
			if !errors.Is(err, context.Canceled) || !result.LastSyncedAt.IsZero() {
				t.Fatalf("result=%+v error=%v", result, err)
			}
			if len(visited) != index {
				t.Fatalf("stages after cancellation: %v", visited)
			}
		})
	}
}

type cancelledDeltaProvider struct {
	incrementalStubProvider
	cancel context.CancelFunc
}

func (p *cancelledDeltaProvider) ListChanges(ctx context.Context, token, cursor string, a, b time.Time) (ChangeSet, error) {
	p.cancel()
	return p.incrementalStubProvider.ListChanges(ctx, token, cursor, a, b)
}
func TestCancelledDeltaDoesNotRetryExpiredCursor(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	cipher := testCipher(t)
	encrypted, _ := cipher.Encrypt("refresh-secret")
	s := &backgroundStubStore{stubStore: stubStore{connection: Connection{UserID: "u", RefreshTokenCipher: encrypted}}}
	p := &cancelledDeltaProvider{cancel: cancel, incrementalStubProvider: incrementalStubProvider{errs: []error{ErrSyncTokenExpired}}}
	projector := &stubProjector{}
	h := NewHandler(http.NotFoundHandler(), s, p, cipher, projector, HandlerConfig{}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	_, err := h.SyncUser(ctx, "u")
	if !errors.Is(err, context.Canceled) || len(p.cursors) != 1 || projector.rebuilt || !s.successAt.IsZero() || s.failureCode != "cancelled" {
		t.Fatalf("error=%v cursors=%v failure=%s", err, p.cursors, s.failureCode)
	}
}
