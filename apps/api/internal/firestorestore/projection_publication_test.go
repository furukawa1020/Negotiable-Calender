package firestorestore

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	calendarintegration "github.com/negotiable-calendar/negotiable-calendar/apps/api/internal/calendar"
	"github.com/negotiable-calendar/negotiable-calendar/apps/api/internal/policy"
	"github.com/negotiable-calendar/negotiable-calendar/apps/api/internal/projection"
)

func publicationFixtures(now time.Time, prefix string, count int) []projection.ScheduleProjection {
	values := make([]projection.ScheduleProjection, count)
	for i := range values {
		values[i] = projection.ScheduleProjection{
			ID: fmt.Sprintf("%s-%04d", prefix, i), UserID: "alice",
			StartAt: now.Add(time.Duration(i) * time.Minute), EndAt: now.Add(time.Duration(i+1) * time.Minute),
			State:                  policy.InteractionState{Availability: "available", Interruptibility: "normal", Requestability: "open", Reschedulability: "medium"},
			ExpectedResponseBucket: "soon", GeneratedAt: now, ExpiresAt: now.Add(48 * time.Hour),
		}
	}
	return values
}

func requirePublicationHidden(t *testing.T, b *Backend, ctx context.Context) {
	t.Helper()
	values, err := b.Projection().ListForUser(ctx, "alice")
	if err != nil || values == nil || len(values) != 0 {
		t.Fatalf("publication not hidden: count=%d err=%v", len(values), err)
	}
}

func TestProjectionPublicationAcrossBatchBoundaries(t *testing.T) {
	for _, count := range []int{3, 400, 405} {
		t.Run(fmt.Sprint(count), func(t *testing.T) {
			b, ctx := emulatorBackend(t)
			now := time.Now().UTC().Truncate(time.Second)
			end := now.Add(24 * time.Hour)
			old := publicationFixtures(now, "old", count)
			if err := b.Projection().Replace(ctx, "alice", now, end, old); err != nil {
				t.Fatal(err)
			}
			revision, ready, err := b.projectionReadRevision(ctx, "alice")
			if err != nil || !ready {
				t.Fatalf("initial revision: %v", err)
			}
			// Read continuously during a real multi-batch replacement. Every nonempty
			// result must be one complete version, never a partial count or mixed IDs.
			started, done, results := make(chan struct{}), make(chan struct{}), make(chan error, 1)
			go func() {
				close(started)
				for {
					values, err := b.Projection().ListForUser(ctx, "alice")
					if err != nil {
						results <- err
						return
					}
					if len(values) != 0 {
						if len(values) != count {
							results <- fmt.Errorf("partial count %d", len(values))
							return
						}
						prefix := strings.Split(values[0].ID, "-")[0]
						for _, value := range values {
							if !strings.HasPrefix(value.ID, prefix+"-") {
								results <- fmt.Errorf("mixed versions")
								return
							}
						}
					}
					select {
					case <-done:
						results <- nil
						return
					default:
					}
				}
			}()
			<-started
			replaceErr := b.Projection().Replace(ctx, "alice", now, end, publicationFixtures(now, "new", count))
			close(done)
			readErr := <-results
			if replaceErr != nil {
				t.Fatal(replaceErr)
			}
			if readErr != nil {
				t.Fatal(readErr)
			}
			current, ready, err := b.projectionReadRevision(ctx, "alice")
			if err != nil || !ready || current == revision {
				t.Fatalf("ABA was not fenced: %v", err)
			}
			values, err := b.Projection().ListForUser(ctx, "alice")
			if err != nil || len(values) != count || !strings.HasPrefix(values[0].ID, "new-") {
				t.Fatalf("final snapshot: %d %v", len(values), err)
			}
		})
	}
}

func TestProjectionPublicationFailureAfterCommittedBatch(t *testing.T) {
	b, ctx := emulatorBackend(t)
	now := time.Now().UTC().Truncate(time.Second)
	end := now.Add(24 * time.Hour)
	collection := b.Client.Collection("users").Doc("alice").Collection("scheduleProjections")
	// Default query ordering puts this malformed record AFTER 400 valid records.
	// Replace must commit the deletion batch, then encounter a real decode error.
	values := publicationFixtures(now, "old", 400)
	if err := b.Projection().Replace(ctx, "alice", now, end, values); err != nil {
		t.Fatal(err)
	}
	putDocument(t, ctx, collection.Doc("zz-invalid"), map[string]any{"StartAt": "invalid"})
	if err := b.Projection().Replace(ctx, "alice", now, end, publicationFixtures(now, "new", 405)); err == nil {
		t.Fatal("expected injected failure")
	}
	docs, err := collection.Documents(ctx).GetAll()
	if err != nil || len(docs) != 1 {
		t.Fatalf("first batch was not committed: %d %v", len(docs), err)
	}
	requirePublicationHidden(t, b, ctx)
	// A partial repair must not declare the entire collection committed.
	if err := b.Projection().Replace(ctx, "alice", now.Add(time.Hour), end, nil); !errors.Is(err, errProjectionRepairRequired) {
		t.Fatalf("narrow repair accepted: %v", err)
	}
	requirePublicationHidden(t, b, ctx)
	if _, err := collection.Doc("zz-invalid").Delete(ctx); err != nil {
		t.Fatal(err)
	}
	if err := b.Projection().Replace(ctx, "alice", now, end, publicationFixtures(now, "new", 405)); err != nil {
		t.Fatal(err)
	}
	got, err := b.Projection().ListForUser(ctx, "alice")
	if err != nil || len(got) != 405 {
		t.Fatalf("retry did not repair: %d %v", len(got), err)
	}
}

func TestProjectionPublicationCancellationAndDeletionFence(t *testing.T) {
	b, ctx := emulatorBackend(t)
	now := time.Now().UTC().Truncate(time.Second)
	end := now.Add(24 * time.Hour)
	writeCtx, err := b.beginProjectionWrite(ctx, "alice", now, end, false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := b.beginProjectionWrite(ctx, "alice", now, end, false); !errors.Is(err, calendarintegration.ErrSyncBusy) {
		t.Fatalf("overlap accepted: %v", err)
	}
	collection := b.Client.Collection("users").Doc("alice").Collection("scheduleProjections")
	batch := newChunkedBatch(b.Client, "alice")
	for _, value := range publicationFixtures(now, "partial", 400) {
		if err := batch.Set(writeCtx, collection.Doc(value.ID), value); err != nil {
			t.Fatal(err)
		}
	}
	requirePublicationHidden(t, b, ctx)
	cancelled, cancel := context.WithCancel(writeCtx)
	cancel()
	value := publicationFixtures(now, "late", 1)[0]
	if err := batch.Set(cancelled, collection.Doc(value.ID), value); err != nil {
		t.Fatal(err)
	}
	if err := batch.Commit(cancelled); err == nil {
		t.Fatal("cancelled commit succeeded")
	}
	b.abandonProjectionWrite(cancelled, "alice")
	requirePublicationHidden(t, b, ctx)
	if err := b.finishProjectionWrite(writeCtx, "alice", true); !errors.Is(err, calendarintegration.ErrSyncLeaseLost) {
		t.Fatalf("abandoned writer published: %v", err)
	}
	// Whole-collection invalidation can recover without decoding corrupt rows.
	if err := b.Projection().DeleteForUser(ctx, "alice"); err != nil {
		t.Fatal(err)
	}
	requirePublicationHidden(t, b, ctx)
	if err := batch.Commit(writeCtx); !errors.Is(err, calendarintegration.ErrSyncLeaseLost) {
		t.Fatalf("late batch recreated deleted rows: %v", err)
	}
	if err := b.Projection().Replace(ctx, "alice", now, end, publicationFixtures(now, "recovered", 3)); err != nil {
		t.Fatal(err)
	}
	if err := b.finishProjectionWrite(writeCtx, "alice", true); !errors.Is(err, calendarintegration.ErrSyncLeaseLost) {
		t.Fatalf("old completion changed new publication: %v", err)
	}
}

func TestProjectionPublicationExpiredLeaseAndInterruptedDeletion(t *testing.T) {
	b, ctx := emulatorBackend(t)
	now := time.Now().UTC().Truncate(time.Second)
	end := now.Add(24 * time.Hour)
	expired := now.Add(-time.Minute)
	putDocument(t, ctx, b.projectionPublicationRef("alice"), projectionPublication{ID: "crashed", Dirty: true, From: now, To: end, LeaseUntil: &expired})
	requirePublicationHidden(t, b, ctx)
	if err := b.Projection().Replace(ctx, "alice", now, end, publicationFixtures(now, "repaired", 3)); err != nil {
		t.Fatal(err)
	}
	deletionCtx, err := b.beginProjectionWrite(ctx, "alice", time.Time{}, time.Time{}, true)
	if err != nil {
		t.Fatal(err)
	}
	requirePublicationHidden(t, b, ctx)
	b.abandonProjectionWrite(deletionCtx, "alice")
	if err := b.Projection().Replace(ctx, "alice", now, end, nil); !errors.Is(err, errProjectionRepairRequired) {
		t.Fatalf("incomplete full deletion republished: %v", err)
	}
	if err := b.Projection().DeleteForUser(ctx, "alice"); err != nil {
		t.Fatal(err)
	}
	if err := b.finishProjectionWrite(deletionCtx, "alice", false); !errors.Is(err, calendarintegration.ErrSyncLeaseLost) {
		t.Fatalf("old cleanup modified new gate: %v", err)
	}
	if err := b.Projection().Replace(ctx, "alice", now, end, publicationFixtures(now, "repaired", 3)); err != nil {
		t.Fatal(err)
	}
}

func TestProjectionPublicationLegacyAndInvalidInput(t *testing.T) {
	b, ctx := emulatorBackend(t)
	now := time.Now().UTC().Truncate(time.Second)
	values := publicationFixtures(now, "legacy", 1)
	putDocument(t, ctx, b.Client.Collection("users").Doc("alice").Collection("scheduleProjections").Doc(values[0].ID), values[0])
	got, err := b.Projection().ListForUser(ctx, "alice")
	if err != nil || len(got) != 1 {
		t.Fatalf("legacy read: %v", err)
	}
	if err := b.Projection().Replace(ctx, "alice", now.Add(time.Hour), now.Add(2*time.Hour), values); err == nil {
		t.Fatal("out-of-range insert accepted")
	}
	if _, err := b.projectionPublicationRef("alice").Get(ctx); !firestoreNotFound(err) {
		t.Fatalf("invalid input changed gate: %v", err)
	}
	putDocument(t, ctx, b.projectionPublicationRef("alice"), map[string]any{"Ready": "invalid"})
	if _, err := b.Projection().ListForUser(ctx, "alice"); err == nil {
		t.Fatal("malformed gate failed open")
	}
}
