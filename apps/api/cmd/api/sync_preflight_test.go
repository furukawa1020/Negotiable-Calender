package main

import (
	"context"
	"errors"
	"strings"
	"testing"
)

type syncQueryStub struct {
	calls int
	err   error
}

func (s *syncQueryStub) CheckSyncQueries(ctx context.Context) error {
	s.calls++
	if ctx.Err() != nil {
		return ctx.Err()
	}
	return s.err
}

func TestFirestoreSyncPreflightModes(t *testing.T) {
	for _, mode := range []string{"off", "external", "background"} {
		t.Run(mode, func(t *testing.T) {
			store := &syncQueryStub{}
			if err := preflightFirestoreSync(context.Background(), store, mode); err != nil {
				t.Fatal(err)
			}
			want := 1
			if mode == "off" {
				want = 0
			}
			if store.calls != want {
				t.Fatal("incorrect startup check count")
			}
		})
	}
}

func TestFirestoreSyncPreflightFailureIsSanitized(t *testing.T) {
	secret := "private-user secret-token https://console.example/private-index"
	store := &syncQueryStub{err: errors.New(secret)}
	for _, mode := range []string{"external", "background"} {
		err := preflightFirestoreSync(context.Background(), store, mode)
		if err == nil || strings.Contains(err.Error(), secret) || errors.Unwrap(err) != nil {
			t.Fatal("provider failure hidden or exposed")
		}
	}
	if err := preflightFirestoreSync(context.Background(), store, "off"); err != nil {
		t.Fatal("disabled sync must not block startup")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := preflightFirestoreSync(ctx, &syncQueryStub{}, "external"); err == nil {
		t.Fatal("cancellation ignored")
	}
}
