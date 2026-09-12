package firestorestore

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"cloud.google.com/go/firestore"
	"cloud.google.com/go/firestore/apiv1/firestorepb"
	calendarintegration "github.com/negotiable-calendar/negotiable-calendar/apps/api/internal/calendar"
	"github.com/negotiable-calendar/negotiable-calendar/apps/api/internal/projection"
	"google.golang.org/api/option"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

func privateFixtures(now time.Time, prefix string, count int) []calendarintegration.BusySpan {
	values := make([]calendarintegration.BusySpan, count)
	for i := range values {
		values[i] = calendarintegration.BusySpan{ProviderEventID: fmt.Sprintf("%s-%04d", prefix, i), CalendarID: "primary", StartAt: now.Add(time.Duration(i) * time.Minute), EndAt: now.Add(time.Duration(i+1) * time.Minute), Busy: true}
	}
	return values
}

func TestPrivateInputsFailureAfter400AndFullRecovery(t *testing.T) {
	_, ctx := emulatorBackend(t)
	var inject atomic.Bool
	var dataCommits atomic.Int32
	// Fail actual Firestore Commit RPCs after the first 400-upsert transaction.
	// Controls/cleanup RPCs still work; no production hook or real Google account.
	intercept := func(ctx context.Context, method string, req, reply any, cc *grpc.ClientConn, invoke grpc.UnaryInvoker, opts ...grpc.CallOption) error {
		if request, ok := req.(*firestorepb.CommitRequest); ok && inject.Load() {
			data := false
			for _, write := range request.Writes {
				if doc := write.GetUpdate(); doc != nil && strings.Contains(doc.Name, "/privateEvents/") {
					data = true
					break
				}
			}
			if data && dataCommits.Add(1) > 1 {
				return status.Error(codes.PermissionDenied, "synthetic second-batch failure")
			}
		}
		ctx = metadata.AppendToOutgoingContext(ctx, "authorization", "Bearer owner") // emulator-only credential
		return invoke(ctx, method, req, reply, cc, opts...)
	}
	// The SDK's emulator path creates its own connection and ignores dial options.
 // Supply the intercepted connection explicitly so the failure reaches Commit.
 conn,err := grpc.NewClient(os.Getenv("FIRESTORE_EMULATOR_HOST"),grpc.WithTransportCredentials(insecure.NewCredentials()),grpc.WithUnaryInterceptor(intercept))
 if err != nil { t.Fatal(err) }
 t.Cleanup(func(){_ = conn.Close()})
 client, err := firestore.NewClient(ctx, "demo-nc-"+safeDigest(t.Name())[:16], option.WithGRPCConn(conn))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close() })
	b := &Backend{Client: client}
	now := time.Now().UTC().Truncate(time.Second)
	end := now.Add(24 * time.Hour)
	if err := b.Calendar().SaveConnection(ctx, calendarintegration.Connection{UserID: "alice", ConnectedAt: now}); err != nil {
		t.Fatal(err)
	}
	if err := b.Calendar().ApplyChanges(ctx, "alice", calendarintegration.ChangeSet{Full: true, Upserts: privateFixtures(now, "old", 3)}, now, end, now); err != nil {
		t.Fatal(err)
	}
	if err := b.Calendar().MarkSyncSuccess(ctx, "alice", "old-cursor", now, end); err != nil {
		t.Fatal(err)
	}
	if err := b.Projection().Replace(ctx, "alice", now, end, publicationFixtures(now, "old-public", 3)); err != nil {
		t.Fatal(err)
	}
	stale, err := b.Calendar().BeginRebuild(ctx, "alice")
	if err != nil {
		t.Fatal(err)
	}
	inject.Store(true)
	err = b.Calendar().ApplyChanges(ctx, "alice", calendarintegration.ChangeSet{Full: true, Upserts: privateFixtures(now, "partial", 405)}, now, end, now)
	if err == nil {
		t.Fatal("injected persistence failure was ignored")
	}
	inject.Store(false)
	docs, err := client.Collection("users").Doc("alice").Collection("privateEvents").Documents(ctx).GetAll()
	if err != nil || len(docs) != 400 || dataCommits.Load() < 2 {
		t.Fatalf("did not fail after committed batch: rows=%d commits=%d err=%v", len(docs), dataCommits.Load(), err)
	}
	requirePublicationHidden(t, b, ctx)
	if _, err := b.Calendar().ListPrivateEvents(ctx, "alice", now, end); !errors.Is(err, errPrivateInputsIncomplete) {
		t.Fatalf("partial input returned: %v", err)
	}
	if _, err := b.Calendar().BeginRebuild(ctx, "alice"); !errors.Is(err, errPrivateInputsIncomplete) {
		t.Fatalf("partial rebuild started: %v", err)
	}
	if err := b.Projection().Replace(stale, "alice", now, end, nil); !errors.Is(err, errPrivateInputsIncomplete) {
		t.Fatalf("stale captured input published: %v", err)
	}
	lease, err := b.Calendar().AcquireSync(ctx, "alice", time.Now().UTC(), time.Minute)
	if err != nil || lease.SyncToken != "" {
		t.Fatalf("retry did not force full sync: token=%q err=%v", lease.SyncToken, err)
	}
	active := calendarintegration.WithSyncLease(ctx, calendarintegration.SyncLease{UserID: "alice", ID: lease.SyncLeaseID})
	if err := b.Calendar().ApplyChanges(active, "alice", calendarintegration.ChangeSet{}, now, end, now); !errors.Is(err, errPrivateInputsIncomplete) {
		t.Fatalf("incremental repair accepted: %v", err)
	}
	shifted := now.Add(24 * time.Hour)
	if err := b.Calendar().ApplyChanges(active, "alice", calendarintegration.ChangeSet{Full: true, Upserts: privateFixtures(shifted, "repaired", 1)}, shifted, shifted.Add(time.Hour), shifted); err != nil {
		t.Fatal(err)
	}
	got, err := b.Calendar().ListPrivateEvents(ctx, "alice", now, shifted.Add(time.Hour))
	if err != nil || len(got) != 1 || got[0].ProviderEventID != "repaired-0000" {
		t.Fatalf("shifted full repair retained partial rows: %d %v", len(got), err)
	}
	requirePublicationHidden(t, b, ctx)
	if err := b.Projection().Replace(active, "alice", shifted, shifted.Add(time.Hour), publicationFixtures(shifted, "repaired-public", 1)); err != nil {
		t.Fatal(err)
	}
	if err := b.Calendar().MarkSyncSuccess(active, "alice", "new-cursor", shifted, shifted.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	published, err := b.Projection().ListForUser(ctx, "alice")
	if err != nil || len(published) != 1 || published[0].ID != "repaired-public-0000" {
		t.Fatalf("recovery publication: %v %v", published, err)
	}
}

func TestPrivateInputSnapshotFencesPausedRealRebuild(t *testing.T) {
	b, ctx := emulatorBackend(t)
	now := time.Now().UTC().Truncate(time.Second)
	end := now.Add(time.Hour)
	putDocument(t, ctx, b.Client.Collection("users").Doc("alice"), userRecord{ID: "alice", Timezone: "UTC"})
	if err := b.Policy().Upsert(ctx, revisionPolicy(now)); err != nil {
		t.Fatal(err)
	}
	if err := b.Calendar().ApplyChanges(ctx, "alice", calendarintegration.ChangeSet{Full: true}, now, end, now); err != nil {
		t.Fatal(err)
	}
	paused := &pausedRebuildInputs{Calendar: b.Calendar(), entered: make(chan struct{}), resume: make(chan struct{})}
	result := make(chan error, 1)
	go func() { result <- projection.NewRebuilder(paused, b.Policy()).Rebuild(ctx, "alice", now, end, now) }()
	select {
	case <-paused.entered:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	changeErr := b.Calendar().ApplyChanges(ctx, "alice", calendarintegration.ChangeSet{Upserts: privateFixtures(now, "new-busy", 1)}, now, end, now)
	close(paused.resume)
	rebuildErr := <-result
	if changeErr != nil {
		t.Fatal(changeErr)
	}
	if !errors.Is(rebuildErr, errProjectionInputsChanged) {
		t.Fatalf("mixed input used by real rebuild: %v", rebuildErr)
	}
	requirePublicationHidden(t, b, ctx)
	if err := projection.NewRebuilder(b.Calendar(), b.Policy()).Rebuild(ctx, "alice", now, end, now); err != nil {
		t.Fatal(err)
	}
}

func TestPrivateInputCancellationAndDisconnectFence(t *testing.T) {
	b, ctx := emulatorBackend(t)
	now := time.Now().UTC().Truncate(time.Second)
	active, err := b.beginPrivateInputs(ctx, "alice", true)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := b.beginPrivateInputs(ctx, "alice", true); !errors.Is(err, calendarintegration.ErrSyncBusy) {
		t.Fatalf("overlap accepted: %v", err)
	}
	cancelled, cancel := context.WithCancel(active)
	cancel()
	if err := b.finishPrivateInputs(cancelled, "alice"); err == nil {
		t.Fatal("cancelled input committed")
	}
	b.abandonPrivateInputs(cancelled, "alice")
	requirePublicationHidden(t, b, ctx)
	if err := b.finishPrivateInputs(active, "alice"); !errors.Is(err, calendarintegration.ErrSyncLeaseLost) {
		t.Fatalf("abandoned input committed: %v", err)
	}
	active, err = b.beginPrivateInputs(ctx, "alice", true)
	if err != nil {
		t.Fatal(err)
	}
	batch := newChunkedBatch(b.Client, "alice")
	if err := batch.Set(active, b.Client.Collection("users").Doc("alice").Collection("privateEvents").Doc("late"), privateEventRecord{ID: "late"}); err != nil {
		t.Fatal(err)
	}
	if err := b.Calendar().DeleteConnection(ctx, "alice"); err != nil {
		t.Fatal(err)
	}
	if err := batch.Commit(active); !errors.Is(err, calendarintegration.ErrSyncLeaseLost) {
		t.Fatalf("late input recreated after disconnect: %v", err)
	}
	if err := b.finishPrivateInputs(active, "alice"); !errors.Is(err, calendarintegration.ErrSyncLeaseLost) {
		t.Fatalf("old completion reset disconnect: %v", err)
	}
	values, err := b.Calendar().ListPrivateEvents(ctx, "alice", now, now.Add(time.Hour))
	if err != nil || len(values) != 0 {
		t.Fatalf("disconnect did not leave committed empty cache: %v", err)
	}
	requirePublicationHidden(t, b, ctx)
}

func TestPrivateInputsFullAndIncrementalBoundaries(t *testing.T) {
	for _, count := range []int{3, 400, 405} {
		t.Run(fmt.Sprint(count), func(t *testing.T) {
			b, ctx := emulatorBackend(t)
			now := time.Now().UTC().Truncate(time.Second)
			end := now.Add(24 * time.Hour)
			spans := privateFixtures(now, "event", count)
			if err := b.Calendar().ApplyChanges(ctx, "alice", calendarintegration.ChangeSet{Full: true, Upserts: spans}, now, end, now); err != nil {
				t.Fatal(err)
			}
			before, err := b.privateInputRevision(ctx, "alice")
			if err != nil {
				t.Fatal(err)
			}
			spans[1].Busy = false
			if err := b.Calendar().ApplyChanges(ctx, "alice", calendarintegration.ChangeSet{DeletedProviderEventIDs: []string{spans[0].ProviderEventID}, Upserts: spans[1:2]}, now, end, now); err != nil {
				t.Fatal(err)
			}
			after, err := b.privateInputRevision(ctx, "alice")
			if err != nil || before == after {
				t.Fatalf("incremental revision unchanged: %v", err)
			}
			got, err := b.Calendar().ListPrivateEvents(ctx, "alice", now, end)
			if err != nil || len(got) != count-1 {
				t.Fatalf("incremental data: %d %v", len(got), err)
			}
		})
	}
}

func TestPrivateInputExpiredLeaseRequiresFullSync(t *testing.T) {
	b, ctx := emulatorBackend(t)
	now := time.Now().UTC().Truncate(time.Second)
	expired := now.Add(-time.Minute)
	putDocument(t, ctx, b.privateInputsRef("alice"), privateInputsControl{ID: "crashed", LeaseUntil: &expired})
	requirePublicationHidden(t, b, ctx)
	if _, err := b.Calendar().BeginRebuild(ctx, "alice"); !errors.Is(err, errPrivateInputsIncomplete) {
		t.Fatalf("expired lease reopened input: %v", err)
	}
	if err := b.Calendar().ApplyChanges(ctx, "alice", calendarintegration.ChangeSet{Full: true, Upserts: privateFixtures(now, "recovered", 1)}, now, now.Add(time.Hour), now); err != nil {
		t.Fatal(err)
	}
}
