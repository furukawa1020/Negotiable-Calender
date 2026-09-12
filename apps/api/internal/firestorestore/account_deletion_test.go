package firestorestore

import (
	"context"
	"errors"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"cloud.google.com/go/firestore"
	"cloud.google.com/go/firestore/apiv1/firestorepb"
	"github.com/negotiable-calendar/negotiable-calendar/apps/api/internal/auth"
	calendarintegration "github.com/negotiable-calendar/negotiable-calendar/apps/api/internal/calendar"
	"github.com/negotiable-calendar/negotiable-calendar/apps/api/internal/organization"
	"google.golang.org/api/option"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

func seedDeletionAccount(t *testing.T, b *Backend, ctx context.Context) time.Time {
	t.Helper()
	now := time.Now().UTC().Truncate(time.Second)
	putDocument(t, ctx, b.Client.Collection("users").Doc("alice"), userRecord{ID: "alice", Email: "alice@example.test", Timezone: "UTC"})
	if err := b.Auth().createWorkspace(ctx, "alice", organization.Workspace{ID: "org", Name: "Team", Role: organization.Owner}, now); err != nil {
		t.Fatal(err)
	}
	if err := b.Calendar().SaveConnection(ctx, calendarintegration.Connection{UserID: "alice", ConnectedAt: now}); err != nil {
		t.Fatal(err)
	}
	return now
}

func TestAccountDeletionFencesOldAndNewWriters(t *testing.T) {
	b, ctx := emulatorBackend(t)
	now := seedDeletionAccount(t, b, ctx)
	session := auth.Session{UserID: "alice", OrganizationID: "org", TokenHash: []byte("synthetic-session"), ExpiresAt: now.Add(time.Hour)}
	if err := b.Auth().CreateSession(ctx, session); err != nil {
		t.Fatal(err)
	}
	lease, err := b.Calendar().AcquireSync(ctx, "alice", now, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	active := calendarintegration.WithSyncLease(ctx, calendarintegration.SyncLease{UserID: "alice", ID: lease.SyncLeaseID})
	if err := b.Calendar().ApplyChanges(active, "alice", calendarintegration.ChangeSet{Full: true, Upserts: privateFixtures(now, "private", 405)}, now, now.Add(24*time.Hour), now); err != nil {
		t.Fatal(err)
	}
	if err := b.Projection().Replace(active, "alice", now, now.Add(24*time.Hour), publicationFixtures(now, "public", 405)); err != nil {
		t.Fatal(err)
	}
	batch := newChunkedBatch(b.Client, "alice")
	if err := batch.Set(active, b.Client.Collection("users").Doc("alice").Collection("privateEvents").Doc("late"), privateEventRecord{ID: "late"}); err != nil {
		t.Fatal(err)
	}
	state, err := b.Auth().beginAccountDeletion(ctx, "alice")
	if err != nil || state.Phase != "deleting" {
		t.Fatalf("begin deletion: %v", err)
	}
	requirePublicationHidden(t, b, ctx)
	for name, write := range map[string]func() error{
		"late batch": func() error { return batch.Commit(active) },
		"connect":    func() error { return b.Calendar().SaveConnection(ctx, calendarintegration.Connection{UserID: "alice"}) },
		"acquire":    func() error { _, err := b.Calendar().AcquireSync(ctx, "alice", now, time.Minute); return err },
		"flow": func() error {
			return b.Calendar().CreateFlow(ctx, calendarintegration.Flow{ID: "late", UserID: "alice"})
		},
		"session": func() error { return b.Auth().CreateSession(ctx, session) },
		"policy":  func() error { return b.Policy().Upsert(ctx, revisionPolicy(now)) },
		"rebuild": func() error { _, err := b.Calendar().BeginRebuild(ctx, "alice"); return err },
	} {
		if err := write(); !errors.Is(err, errAccountDeleting) {
			t.Fatalf("%s not fenced: %v", name, err)
		}
	}
	if _, err := b.Auth().GetSession(ctx, session.TokenHash, now); !errors.Is(err, auth.ErrNotFound) {
		t.Fatalf("old session accepted: %v", err)
	}
	if err := b.Auth().DeleteAccount(ctx, "alice"); err != nil {
		t.Fatal(err)
	}
	if err := b.Auth().DeleteAccount(ctx, "alice"); err != nil {
		t.Fatalf("completed retry: %v", err)
	}
	for _, collection := range []string{"privateEvents", "scheduleProjections", "projectionControls", "workspaces"} {
		docs, err := b.Client.Collection("users").Doc("alice").Collection(collection).Documents(ctx).GetAll()
		if err != nil || len(docs) != 0 {
			t.Fatalf("remaining %s: %d %v", collection, len(docs), err)
		}
	}
	if err := batch.Commit(active); !errors.Is(err, errAccountDeleting) {
		t.Fatalf("removed controls allowed old writer: %v", err)
	}
	requirePublicationHidden(t, b, ctx)
	doc, err := b.accountDeletionRef("alice").Get(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := doc.DataTo(&state); err != nil {
		t.Fatal(err)
	}
	if state.Phase != "complete" || len(state.OrganizationIDs) != 0 {
		t.Fatal("completion retained membership data")
	}
}

func TestDeletionOwnerGuardUsesAuthoritativeMembership(t *testing.T) {
	for _, missing := range []bool{false, true} {
		t.Run(map[bool]string{false: "stale-cache", true: "missing-cache"}[missing], func(t *testing.T) {
			b, ctx := emulatorBackend(t)
			now := seedDeletionAccount(t, b, ctx)
			putDocument(t, ctx, b.Client.Collection("organizations").Doc("org").Collection("members").Doc("bob"), membershipRecord{UserID: "bob", Role: organization.Member})
			ref := b.Client.Collection("users").Doc("alice").Collection("workspaces").Doc("org")
			if missing {
				if _, err := ref.Delete(ctx); err != nil {
					t.Fatal(err)
				}
			} else {
				putDocument(t, ctx, ref, organization.Workspace{ID: "org", Role: organization.Member})
			}
			if _, err := b.Auth().beginAccountDeletion(ctx, "alice"); !errors.Is(err, auth.ErrLastOrganizationOwner) {
				t.Fatalf("last owner accepted: %v", err)
			}
			if _, err := b.accountDeletionRef("alice").Get(ctx); !firestoreNotFound(err) {
				t.Fatalf("guard rejection changed lifecycle: %v", err)
			}
			if _, err := b.Calendar().GetConnection(ctx, "alice"); err != nil {
				t.Fatalf("guard rejection revoked calendar: %v", err)
			}
			if err := b.Calendar().CreateFlow(ctx, calendarintegration.Flow{ID: "still-active", UserID: "alice", CreatedAt: now}); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestConcurrentOwnerDeletionsKeepAnActiveOwner(t *testing.T) {
	b, ctx := emulatorBackend(t)
	seedDeletionAccount(t, b, ctx)
	putDocument(t, ctx, b.Client.Collection("users").Doc("bob"), userRecord{ID: "bob"})
	putDocument(t, ctx, b.Client.Collection("organizations").Doc("org").Collection("members").Doc("bob"), membershipRecord{UserID: "bob", Role: organization.Owner})
	putDocument(t, ctx, b.Client.Collection("organizations").Doc("org").Collection("members").Doc("charlie"), membershipRecord{UserID: "charlie", Role: organization.Member})
	start := make(chan struct{})
	results := make(chan error, 2)
	for _, id := range []string{"alice", "bob"} {
		go func(id string) { <-start; _, err := b.Auth().beginAccountDeletion(ctx, id); results <- err }(id)
	}
	close(start)
	accepted, rejected := 0, 0
	for i := 0; i < 2; i++ {
		err := <-results
		if err == nil {
			accepted++
		} else if errors.Is(err, auth.ErrLastOrganizationOwner) {
			rejected++
		} else {
			t.Fatal(err)
		}
	}
	if accepted != 1 || rejected != 1 {
		t.Fatalf("accepted=%d rejected=%d", accepted, rejected)
	}
}

func TestAccountDeletionRetriesAfterCommittedCleanupBatch(t *testing.T) {
	_, ctx := emulatorBackend(t)
	var fail atomic.Bool
	var commits atomic.Int32
	interceptor := func(ctx context.Context, method string, req, reply any, cc *grpc.ClientConn, invoke grpc.UnaryInvoker, opts ...grpc.CallOption) error {
		ctx = metadata.AppendToOutgoingContext(ctx, "authorization", "Bearer owner")
		if request, ok := req.(*firestorepb.CommitRequest); ok && fail.Load() {
			deleting := false
			for _, write := range request.Writes {
				if strings.Contains(write.GetDelete(), "/scheduleProjections/") {
					deleting = true
					break
				}
			}
			if deleting && commits.Add(1) > 1 {
				return status.Error(codes.PermissionDenied, "synthetic cleanup failure")
			}
		}
		return invoke(ctx, method, req, reply, cc, opts...)
	}
	conn, err := grpc.NewClient(os.Getenv("FIRESTORE_EMULATOR_HOST"), grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithUnaryInterceptor(interceptor))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	client, err := firestore.NewClient(ctx, "demo-nc-"+safeDigest(t.Name())[:16], option.WithGRPCConn(conn))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close() })
	b := &Backend{Client: client}
	now := seedDeletionAccount(t, b, ctx)
	if err := b.Projection().Replace(ctx, "alice", now, now.Add(24*time.Hour), publicationFixtures(now, "old", 405)); err != nil {
		t.Fatal(err)
	}
	fail.Store(true)
	err = b.Auth().DeleteAccount(ctx, "alice")
	fail.Store(false)
	if err == nil {
		t.Fatal("cleanup failure was not injected")
	}
	docs, err := client.Collection("users").Doc("alice").Collection("scheduleProjections").Documents(ctx).GetAll()
	if err != nil || len(docs) != 205 {
		t.Fatalf("expected one committed deletion batch: %d %v", len(docs), err)
	}
	requirePublicationHidden(t, b, ctx)
	if err := b.Calendar().SaveConnection(ctx, calendarintegration.Connection{UserID: "alice"}); !errors.Is(err, errAccountDeleting) {
		t.Fatalf("failed cleanup permitted reconnect: %v", err)
	}
	if err := b.Auth().DeleteAccount(ctx, "alice"); err != nil {
		t.Fatalf("resume failed: %v", err)
	}
	requirePublicationHidden(t, b, ctx)
}

func TestNewSignInAfterDeletionUsesFreshAccount(t *testing.T) {
	b, ctx := emulatorBackend(t)
	now := time.Now().UTC()
	profile := auth.Profile{Subject: "synthetic-subject", Email: "alice@example.test", DisplayName: "Alice"}
	old, err := b.Auth().UpsertGoogleIdentity(ctx, profile, now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := b.Auth().beginAccountDeletion(ctx, old.UserID); err != nil {
		t.Fatal(err)
	}
	if _, err := b.Auth().UpsertGoogleIdentity(ctx, profile, now); !errors.Is(err, errAccountDeleting) {
		t.Fatalf("pending deletion bypassed: %v", err)
	}
	if err := b.Auth().DeleteAccount(ctx, old.UserID); err != nil {
		t.Fatal(err)
	}
	fresh, err := b.Auth().UpsertGoogleIdentity(ctx, profile, now)
	if err != nil {
		t.Fatal(err)
	}
	if fresh.UserID == old.UserID || fresh.OrganizationID == old.OrganizationID {
		t.Fatal("deleted identity reused")
	}
	if err := b.Calendar().SaveConnection(ctx, calendarintegration.Connection{UserID: old.UserID}); !errors.Is(err, errAccountDeleting) {
		t.Fatalf("old callback revived account: %v", err)
	}
	again, err := b.Auth().UpsertGoogleIdentity(ctx, profile, now)
	if err != nil || again.UserID != fresh.UserID {
		t.Fatalf("new identity unstable: %v", err)
	}
}
