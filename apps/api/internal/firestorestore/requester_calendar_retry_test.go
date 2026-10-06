package firestorestore

import (
	"context"
	"errors"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"cloud.google.com/go/firestore"
	"cloud.google.com/go/firestore/apiv1/firestorepb"
	cal "github.com/negotiable-calendar/negotiable-calendar/apps/api/internal/calendar"
	coord "github.com/negotiable-calendar/negotiable-calendar/apps/api/internal/request"
	"google.golang.org/api/option"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"
)

func TestRequesterCalendarIsRecheckedAfterTransactionRetry(t *testing.T) {
	b, ctx := emulatorBackend(t)
	now := time.Now().UTC().Truncate(time.Microsecond)
	v := confirmationRequest("retry-requester", "alice", "bob", now, now.Add(time.Hour))
	if err := b.Request().Create(ctx, v); err != nil {
		t.Fatal(err)
	}
	p := publicationFixtures(now, "target-open", 1)[0]
	p.UserID, p.EndAt = "bob", now.Add(4*time.Hour)
	putDocument(t, ctx, b.Client.Collection("users").Doc("bob").Collection("scheduleProjections").Doc(p.ID), p)
	snapshot := cal.SourceSnapshot{Revision: "fresh", ObservedAt: now, From: now.Add(-time.Hour), To: now.Add(24 * time.Hour)}
	inputs := privateInputsControl{ID: snapshot.Revision, Ready: true, Source: &snapshot}
	putDocument(t, ctx, b.privateInputsRef("alice"), inputs)
	putDocument(t, ctx, b.Client.Collection("calendarConnections").Doc("alice"), cal.Connection{UserID: "alice", LastSyncedAt: &now})
	var injected atomic.Bool
	// Emulate a rejected commit, release its locks, then change the source before
	// the SDK retries. Never mutate production or add runtime failure hooks.
	intercept := func(callCtx context.Context, method string, req, reply any, cc *grpc.ClientConn, invoke grpc.UnaryInvoker, opts ...grpc.CallOption) error {
		callCtx = metadata.AppendToOutgoingContext(callCtx, "authorization", "Bearer owner")
		if commit, ok := req.(*firestorepb.CommitRequest); ok && len(commit.Transaction) > 0 && injected.CompareAndSwap(false, true) {
			if err := invoke(callCtx, "/google.firestore.v1.Firestore/Rollback", &firestorepb.RollbackRequest{Database: commit.Database, Transaction: commit.Transaction}, &emptypb.Empty{}, cc, opts...); err != nil {
				return err
			}
			inputs.Ready = false
			if _, err := b.privateInputsRef("alice").Set(callCtx, inputs); err != nil {
				return err
			}
			return status.Error(codes.Aborted, "synthetic source change")
		}
		return invoke(callCtx, method, req, reply, cc, opts...)
	}
	conn, err := grpc.NewClient(os.Getenv("FIRESTORE_EMULATOR_HOST"), grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithUnaryInterceptor(intercept))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	client, err := firestore.NewClient(ctx, "demo-nc-"+safeDigest(t.Name())[:16], option.WithGRPCConn(conn))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close() })
	store := (&Backend{Client: client}).Request()
	if err := store.Respond(ctx, v.ID, "bob", coord.Accepted, v.Options[0].ID); !errors.Is(err, coord.ErrAvailabilityChanged) || !injected.Load() {
		t.Fatal("retry trusted old source", err)
	}
	got, err := b.Request().GetForUser(ctx, v.ID, "alice")
	if err != nil || got.Status != coord.Suggested || got.AcceptedOptionID != "" {
		t.Fatal("rejected retry wrote booking", err)
	}
	for _, user := range []string{"alice", "bob"} {
		docs, err := b.Client.Collection("users").Doc(user).Collection("notifications").Documents(ctx).GetAll()
		if err != nil || len(docs) != 0 {
			t.Fatal("rejected retry notified", err)
		}
	}
	docs, err := b.Client.Collection("organizations").Doc(v.OrganizationID).Collection("auditLogs").Documents(ctx).GetAll()
	if err != nil || len(docs) != 0 {
		t.Fatal("rejected retry audited", err)
	}
}
