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
	"github.com/negotiable-calendar/negotiable-calendar/apps/api/internal/audit"
	coordinationrequest "github.com/negotiable-calendar/negotiable-calendar/apps/api/internal/request"
	"google.golang.org/api/option"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

func deletionRequest(now time.Time) coordinationrequest.CoordinationRequest {
	return coordinationrequest.CoordinationRequest{
		ID: "request", OrganizationID: "org", RequesterUserID: "alice", TargetUserID: "bob",
		Type: coordinationrequest.Meeting, Title: "Synthetic request", DurationMinutes: 30,
		DeadlineAt: now.Add(time.Hour), SyncPreference: coordinationrequest.Either,
		Priority: coordinationrequest.PriorityNormal, Status: coordinationrequest.Suggested,
		CreatedAt: now, UpdatedAt: now,
	}
}

func TestRequestAuditCleanupResumesAtEveryBoundary(t *testing.T) {
	for _, boundary := range []string{"audit", "request", "actor"} {
		t.Run(boundary, func(t *testing.T) {
			_, ctx := emulatorBackend(t)
			var armed atomic.Bool
			var auditDeletes atomic.Int32
			interceptor := func(ctx context.Context, method string, req, reply any, cc *grpc.ClientConn, invoke grpc.UnaryInvoker, opts ...grpc.CallOption) error {
				ctx = metadata.AppendToOutgoingContext(ctx, "authorization", "Bearer owner")
				if commit, ok := req.(*firestorepb.CommitRequest); ok && armed.Load() {
					for _, write := range commit.Writes {
						path := write.GetDelete()
						fail := boundary == "request" && strings.HasSuffix(path, "/coordinationRequests/request")
						fail = fail || boundary == "actor" && strings.HasSuffix(path, "/auditLogs/actor")
						if boundary == "audit" && strings.Contains(path, "/auditLogs/related-") {
							fail = auditDeletes.Add(1) > 1
						}
						if fail {
							return status.Error(codes.PermissionDenied, "synthetic cleanup boundary")
						}
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
			request := deletionRequest(now)
			if err := b.Request().Create(ctx, request); err != nil {
				t.Fatal(err)
			}
			audits := client.Collection("organizations").Doc("org").Collection("auditLogs")
			for _, id := range []string{"related-1", "related-2"} {
				if err := b.Audit().Create(ctx, audit.Event{ID: id, OrganizationID: "org", ActorUserID: "bob", ResourceType: "request", ResourceID: request.ID}); err != nil {
					t.Fatal(err)
				}
			}
			putDocument(t, ctx, audits.Doc("actor"), audit.Event{ID: "actor", ActorUserID: "alice", ResourceType: "workspace"})
			putDocument(t, ctx, audits.Doc("keep"), audit.Event{ID: "keep", ActorUserID: "bob", ResourceType: "invitation", ResourceID: request.ID})
			foreign := client.Collection("organizations").Doc("foreign").Collection("auditLogs").Doc("keep")
			putDocument(t, ctx, foreign, audit.Event{ID: "keep", ActorUserID: "bob", ResourceType: "request", ResourceID: request.ID})
			armed.Store(true)
			if err := b.Auth().DeleteAccount(ctx, "alice"); err == nil {
				t.Fatal("failure not injected")
			}
			armed.Store(false)
			_, err = client.Collection("coordinationRequests").Doc(request.ID).Get(ctx)
			if boundary == "actor" {
				if !firestoreNotFound(err) {
					t.Fatalf("request should already be removed: %v", err)
				}
			} else if err != nil {
				t.Fatalf("lost durable request reference: %v", err)
			}
			if boundary == "audit" {
				docs, err := audits.Where("ResourceType", "==", "request").Documents(ctx).GetAll()
				if err != nil || len(docs) != 1 {
					t.Fatalf("expected partial audit progress: %d %v", len(docs), err)
				}
			}
			for i := 0; i < 2; i++ {
				if err := b.Auth().ResumeAccountDeletion(ctx, "alice"); err != nil {
					t.Fatal(err)
				}
			}
			docs, err := audits.Documents(ctx).GetAll()
			if err != nil || len(docs) != 1 || docs[0].Ref.ID != "keep" {
				t.Fatalf("audit cleanup/isolation: %v %v", docs, err)
			}
			if _, err := foreign.Get(ctx); err != nil {
				t.Fatalf("foreign audit touched: %v", err)
			}
			if _, err := client.Collection("coordinationRequests").Doc(request.ID).Get(ctx); !firestoreNotFound(err) {
				t.Fatalf("request remains: %v", err)
			}
			late := audit.Event{ID: "late", OrganizationID: "org", ActorUserID: "bob", ResourceType: "request", ResourceID: request.ID}
			if err := b.Audit().Create(ctx, late); err == nil {
				t.Fatal("late audit resurrected deleted resource")
			}
		})
	}
}

func TestRequestWritesFenceEveryParticipant(t *testing.T) {
	for _, participant := range []string{"requester", "target", "delegate", "option"} {
		t.Run(participant, func(t *testing.T) {
			b, ctx := emulatorBackend(t)
			request := deletionRequest(time.Now().UTC())
			deleting := "alice"
			switch participant {
			case "target":
				deleting = "bob"
			case "delegate":
				deleting = "carol"
				request.DelegatedUserID = deleting
			case "option":
				deleting = "carol"
				request.Options = []coordinationrequest.Option{{ID: "option", RequestID: request.ID, Type: coordinationrequest.OptionDelegate, DelegateUserID: deleting, CreatedAt: request.CreatedAt}}
			}
			if err := b.Request().Create(ctx, request); err != nil {
				t.Fatal(err)
			}
			event := audit.Event{ID: "before", ActorUserID: "dave", ResourceType: "request", ResourceID: request.ID}
			if err := b.Audit().Create(ctx, event); err != nil {
				t.Fatalf("normal inferred audit: %v", err)
			}
			event.ID, event.OrganizationID = "wrong-tenant", "other"
			if err := b.Audit().Create(ctx, event); err == nil {
				t.Fatal("organization mismatch accepted")
			}
			putDocument(t, ctx, b.accountDeletionRef(deleting), accountDeletion{Phase: "deleting", StartedAt: request.CreatedAt})
			request.ID = "new-request"
			for i := range request.Options {
				request.Options[i].RequestID = request.ID
			}
			if err := b.Request().Create(ctx, request); !errors.Is(err, errAccountDeleting) {
				t.Fatalf("create bypass: %v", err)
			}
			if err := b.Request().Cancel(ctx, "request", "alice"); !errors.Is(err, errAccountDeleting) {
				t.Fatalf("mutation bypass: %v", err)
			}
			for _, org := range []string{"", "org"} {
				event.ID, event.OrganizationID = "late", org
				if err := b.Audit().Create(ctx, event); !errors.Is(err, errAccountDeleting) {
					t.Fatalf("audit bypass: %v", err)
				}
			}
		})
	}
}

func TestDelegateCannotIntroduceDeletingAccount(t *testing.T) {
	b, ctx := emulatorBackend(t)
	request := deletionRequest(time.Now().UTC())
	if err := b.Request().Create(ctx, request); err != nil {
		t.Fatal(err)
	}
	putDocument(t, ctx, b.Client.Collection("organizations").Doc("org").Collection("members").Doc("carol"), membershipRecord{UserID: "carol"})
	putDocument(t, ctx, b.accountDeletionRef("carol"), accountDeletion{Phase: "complete"})
	option := coordinationrequest.Option{ID: "delegate", RequestID: request.ID, Type: coordinationrequest.OptionDelegate, DelegateUserID: "carol", CreatedAt: request.CreatedAt}
	if err := b.Request().Delegate(ctx, request.ID, "bob", option); !errors.Is(err, errAccountDeleting) {
		t.Fatalf("new delegate bypass: %v", err)
	}
	got, err := b.Request().GetForUser(ctx, request.ID, "alice")
	if err != nil || got.Status != coordinationrequest.Suggested || got.DelegatedUserID != "" {
		t.Fatalf("failed mutation persisted: %+v %v", got, err)
	}
	if err := b.Audit().Create(ctx, audit.Event{ID: "late-actor", OrganizationID: "org", ActorUserID: "carol", ResourceType: "workspace"}); !errors.Is(err, errAccountDeleting) {
		t.Fatalf("actor bypass: %v", err)
	}
}
