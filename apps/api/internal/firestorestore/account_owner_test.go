package firestorestore

import (
	"errors"
	"testing"

	"github.com/negotiable-calendar/negotiable-calendar/apps/api/internal/auth"
	"github.com/negotiable-calendar/negotiable-calendar/apps/api/internal/organization"
)

func TestDeletionWithoutWorkspaceCacheRemovesAuthoritativeMembership(t *testing.T) {
	for _, scenario := range []string{"member", "other-owner", "solo"} {
		t.Run(scenario, func(t *testing.T) {
			b, ctx := emulatorBackend(t)
			seedDeletionAccount(t, b, ctx)
			members := b.Client.Collection("organizations").Doc("org").Collection("members")
			if scenario == "member" {
				putDocument(t, ctx, members.Doc("alice"), membershipRecord{UserID: "alice", Role: organization.Member})
			}
			if scenario != "solo" {
				putDocument(t, ctx, members.Doc("bob"), membershipRecord{UserID: "bob", Role: organization.Owner})
			}
			if _, err := b.Client.Collection("users").Doc("alice").Collection("workspaces").Doc("org").Delete(ctx); err != nil {
				t.Fatal(err)
			}
			if err := b.Auth().DeleteAccount(ctx, "alice"); err != nil {
				t.Fatal(err)
			}
			if _, err := members.Doc("alice").Get(ctx); !firestoreNotFound(err) {
				t.Fatalf("orphan membership: %v", err)
			}
			if scenario != "solo" {
				if _, err := members.Doc("bob").Get(ctx); err != nil {
					t.Fatal("successor membership removed")
				}
			}
		})
	}
}

func TestDeletionAcrossOrganizationsRejectsBeforeAnyMutation(t *testing.T) {
	b, ctx := emulatorBackend(t)
	seedDeletionAccount(t, b, ctx)
	org := b.Client.Collection("organizations").Doc("second-org")
	putDocument(t, ctx, org, organizationRecord{ID: "second-org", Name: "Second"})
	putDocument(t, ctx, org.Collection("members").Doc("alice"), membershipRecord{UserID: "alice", Role: organization.Owner})
	putDocument(t, ctx, org.Collection("members").Doc("bob"), membershipRecord{UserID: "bob", Role: organization.Member})
	if err := b.Auth().DeleteAccount(ctx, "alice"); !errors.Is(err, auth.ErrLastOrganizationOwner) {
		t.Fatalf("multi-org guard bypassed: %v", err)
	}
	if _, err := b.accountDeletionRef("alice").Get(ctx); !firestoreNotFound(err) {
		t.Fatal("rejected multi-org deletion has marker")
	}
	if _, err := b.Calendar().GetConnection(ctx, "alice"); err != nil {
		t.Fatal("rejected multi-org deletion revoked grant")
	}
	for _, id := range []string{"org", "second-org"} {
		if _, err := b.Client.Collection("organizations").Doc(id).Collection("members").Doc("alice").Get(ctx); err != nil {
			t.Fatalf("membership %s changed: %v", id, err)
		}
	}
}
