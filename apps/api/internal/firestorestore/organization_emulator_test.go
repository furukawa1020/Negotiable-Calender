package firestorestore

import (
	"context"
	"crypto/sha256"
	"errors"
	"os"
	"testing"
	"time"

	"cloud.google.com/go/firestore"
	"github.com/negotiable-calendar/negotiable-calendar/apps/api/internal/organization"
)

func emulatorBackend(t *testing.T) (*Backend, context.Context) {
	t.Helper()
	if os.Getenv("FIRESTORE_EMULATOR_HOST") == "" {
		t.Skip("requires FIRESTORE_EMULATOR_HOST")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	t.Cleanup(cancel)
	backend, err := New(ctx, "demo-nc-"+safeDigest(t.Name())[:16])
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { backend.Close() })
	return backend, ctx
}

func putDocument(t *testing.T, ctx context.Context, ref *firestore.DocumentRef, value any) {
	t.Helper()
	if _, err := ref.Set(ctx, value); err != nil {
		t.Fatal(err)
	}
}

func seedInvitation(t *testing.T, b *Backend, ctx context.Context) ([]byte, time.Time) {
	t.Helper()
	now := time.Now().UTC()
	hash := sha256.Sum256([]byte(t.Name()))
	putDocument(t, ctx, b.Client.Collection("organizations").Doc("org"), organizationRecord{ID: "org", Name: "Test"})
	for _, id := range []string{"alice", "bob"} {
		putDocument(t, ctx, b.Client.Collection("users").Doc(id), userRecord{ID: id})
	}
	putDocument(t, ctx, b.Client.Collection("organizationInvitations").Doc(hashID(hash[:])), organization.Invitation{
		ID: "invite", OrganizationID: "org", InvitedBy: "owner", Role: organization.Member, TokenHash: hash[:],
		CreatedAt: now.Add(-time.Hour), ExpiresAt: now.Add(time.Hour),
	})
	return hash[:], now
}

func TestInvitationConcurrentConsumers(t *testing.T) {
	b, ctx := emulatorBackend(t)
	token, now := seedInvitation(t, b, ctx)
	start := make(chan struct{})
	results := make(chan error, 2)
	for _, userID := range []string{"alice", "bob"} {
		go func(id string) {
			<-start
			_, err := b.Organization().AcceptInvitation(ctx, token, id, now)
			results <- err
		}(userID)
	}
	close(start)
	successes := 0
	for i := 0; i < 2; i++ {
		err := <-results
		if err == nil {
			successes++
		} else if !errors.Is(err, organization.ErrInvitationNotFound) {
			t.Fatal(err)
		}
	}
	if successes != 1 {
		t.Fatalf("got %d successful consumers, want one", successes)
	}
	members, err := b.Client.Collection("organizations").Doc("org").Collection("members").Documents(ctx).GetAll()
	if err != nil || len(members) != 1 {
		t.Fatalf("membership count=%d, error=%v", len(members), err)
	}
	audits, err := b.Client.Collection("organizations").Doc("org").Collection("auditLogs").Documents(ctx).GetAll()
	if err != nil || len(audits) != 1 {
		t.Fatalf("audit count=%d, error=%v", len(audits), err)
	}
}

func TestInvitationPreservesOwnerMembership(t *testing.T) {
	b, ctx := emulatorBackend(t)
	token, now := seedInvitation(t, b, ctx)
	ref := b.Client.Collection("organizations").Doc("org").Collection("members").Doc("alice")
	prior := membershipRecord{ID: "original-membership", OrganizationID: "org", UserID: "alice", Role: organization.Owner, CreatedAt: now.Add(-24 * time.Hour)}
	putDocument(t, ctx, ref, prior)
	workspace, err := b.Organization().AcceptInvitation(ctx, token, "alice", now)
	if err != nil {
		t.Fatal(err)
	}
	if workspace.Role != organization.Owner {
		t.Fatalf("owner changed to %s", workspace.Role)
	}
	snapshot, err := ref.Get(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var after membershipRecord
	if err := snapshot.DataTo(&after); err != nil {
		t.Fatal(err)
	}
	if after.ID != prior.ID || after.Role != prior.Role || !after.CreatedAt.Equal(prior.CreatedAt.Truncate(time.Microsecond)) {
		t.Fatalf("membership overwritten: %+v", after)
	}
	copy, err := b.Client.Collection("users").Doc("alice").Collection("workspaces").Doc("org").Get(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var cached organization.Workspace
	if err := copy.DataTo(&cached); err != nil {
		t.Fatal(err)
	}
	if cached.Role != organization.Owner {
		t.Fatal("workspace lost owner role")
	}
	if _, err := b.Organization().AcceptInvitation(ctx, token, "bob", now); !errors.Is(err, organization.ErrInvitationNotFound) {
		t.Fatalf("token reused: %v", err)
	}
}

func TestInvitationInvalidAcceptanceHasNoWrites(t *testing.T) {
	for _, scenario := range []string{"expired", "missing-user", "missing-org"} {
		t.Run(scenario, func(t *testing.T) {
			b, ctx := emulatorBackend(t)
			token, now := seedInvitation(t, b, ctx)
			userID := "alice"
			switch scenario {
			case "expired":
				now = now.Add(2 * time.Hour)
			case "missing-user":
				userID = "absent"
			case "missing-org":
				if _, err := b.Client.Collection("organizations").Doc("org").Delete(ctx); err != nil {
					t.Fatal(err)
				}
			}
			workspace, err := b.Organization().AcceptInvitation(ctx, token, userID, now)
			if !errors.Is(err, organization.ErrInvitationNotFound) || workspace.ID != "" {
				t.Fatalf("accepted invalid invitation: %+v %v", workspace, err)
			}
			if _, err := b.Client.Collection("organizationInvitations").Doc(hashID(token)).Get(ctx); err != nil {
				t.Fatal("failed acceptance consumed token")
			}
			for _, col := range []string{"members", "auditLogs"} {
				docs, err := b.Client.Collection("organizations").Doc("org").Collection(col).Documents(ctx).GetAll()
				if err != nil || len(docs) != 0 {
					t.Fatalf("unexpected %s writes: %d %v", col, len(docs), err)
				}
			}
		})
	}
}
