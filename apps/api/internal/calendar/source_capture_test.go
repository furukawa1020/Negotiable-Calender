package calendar

import (
	"context"
	"reflect"
	"testing"
	"time"
)

func TestCapturedSourceIsolatedFromProducerAndConsumers(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Microsecond)
	fresh := true
	newSource := func() SourceState {
		return SourceState{Managed: true, Snapshot: SourceSnapshot{Revision: "receipt", ObservedAt: now, From: now, To: now.Add(time.Hour)}, PublishedRevision: "receipt", Connection: &Connection{
			UserID: "owner", SourceFresh: &fresh, LastSyncedAt: &now, SyncLeaseUntil: &now, LastAttemptAt: &now, NextAttemptAt: &now, GrantedScopes: []string{"scope"}, RefreshTokenCipher: []byte{1, 2, 3},
		}}
	}
	mutate := func(s *SourceState) {
		s.Managed = false
		s.Snapshot.Revision = "changed"
		c := s.Connection
		c.UserID = "other"
		c.ReconnectRequired = true
		c.LastErrorCode = "failure"
		c.SyncLeaseID = "lease"
		*c.SourceFresh = false
		for _, field := range []*time.Time{c.SyncLeaseUntil, c.LastSyncedAt, c.LastAttemptAt, c.NextAttemptAt} {
			*field = time.Time{}
		}
		c.GrantedScopes[0] = "changed"
		c.RefreshTokenCipher[0] = 9
	}
	source := newSource()
	ctx := WithSourceSnapshot(context.Background(), "owner", source)
	want := cloneSourceState(source)
	mutate(&source)
	got, ok := CapturedSource(ctx, "owner")
	if !ok || !reflect.DeepEqual(got, want) || !got.Readable(want.Snapshot.ObservedAt) {
		t.Fatal("producer changed captured receipt")
	}
	mutate(&got)
	again, ok := CapturedSource(ctx, "owner")
	if !ok || !reflect.DeepEqual(again, want) {
		t.Fatal("reader changed captured receipt")
	}
	if other, ok := CapturedSource(ctx, "other"); ok || !reflect.DeepEqual(other, SourceState{}) {
		t.Fatal("other user received captured evidence")
	}
	if absent, ok := CapturedSource(context.Background(), "owner"); ok || !reflect.DeepEqual(absent, SourceState{}) {
		t.Fatal("missing evidence was populated")
	}
}

func TestCapturedPolicyOnlyAndNilConnectionFields(t *testing.T) {
	for _, s := range []SourceState{{}, {Managed: true, Connection: &Connection{UserID: "owner"}}} {
		got, ok := CapturedSource(WithSourceSnapshot(context.Background(), "owner", s), "owner")
		if !ok || !reflect.DeepEqual(got, s) {
			t.Fatal("nil state changed")
		}
	}
}
