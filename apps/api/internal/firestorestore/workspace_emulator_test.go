package firestorestore

import (
	"crypto/sha256"
	"errors"
	"testing"
	"time"

	"github.com/negotiable-calendar/negotiable-calendar/apps/api/internal/audit"
	"github.com/negotiable-calendar/negotiable-calendar/apps/api/internal/auth"
	"github.com/negotiable-calendar/negotiable-calendar/apps/api/internal/organization"
)

func TestWorkspaceSwitchUsesCurrentMembershipAndAudit(t *testing.T) {
	for _, cached := range []bool{true, false} {
		name := "without-cache"
		if cached {
			name = "stale-cache"
		}
		t.Run(name, func(t *testing.T) {
			b, ctx := emulatorBackend(t)
			_, now := seedInvitation(t, b, ctx)
			orgRef := b.Client.Collection("organizations").Doc("org")
			putDocument(t, ctx, orgRef.Collection("members").Doc("alice"), membershipRecord{ID: "member", OrganizationID: "org", UserID: "alice", Role: organization.Member})
			if cached {
				putDocument(t, ctx, b.Client.Collection("users").Doc("alice").Collection("workspaces").Doc("org"), organization.Workspace{ID: "org", Name: "Stale", Role: organization.Owner})
			}
			token := sha256.Sum256([]byte("session"))
			ref := b.Client.Collection("authSessions").Doc(hashID(token[:]))
			putDocument(t, ctx, ref, auth.Session{UserID: "alice", OrganizationID: "previous", ExpiresAt: now.Add(time.Hour)})
			workspace, err := b.Organization().SwitchWorkspace(ctx, token[:], "alice", "org", now)
			if err != nil {
				t.Fatal(err)
			}
			if workspace.Role != organization.Member || workspace.Name != "Test" {
				t.Fatalf("used stale membership: %+v", workspace)
			}
			doc, err := ref.Get(ctx)
			if err != nil {
				t.Fatal(err)
			}
			var session auth.Session
			if err := doc.DataTo(&session); err != nil {
				t.Fatal(err)
			}
			if session.OrganizationID != "org" {
				t.Fatal("session not switched")
			}
			docs, err := orgRef.Collection("auditLogs").Documents(ctx).GetAll()
			if err != nil || len(docs) != 1 {
				t.Fatalf("audit count=%d error=%v", len(docs), err)
			}
			var event audit.Event
			if err := docs[0].DataTo(&event); err != nil {
				t.Fatal(err)
			}
			if event.Action != audit.WorkspaceSwitched || event.ActorUserID != "alice" || event.OrganizationID != "org" {
				t.Fatalf("wrong audit: %+v", event)
			}
		})
	}
}

func TestWorkspaceSwitchRejectsInvalidAuthorityWithoutWrites(t *testing.T) {
	for _, scenario := range []string{"removed-membership", "expired-session", "other-user-session", "missing-session", "missing-organization", "invalid-role"} {
		t.Run(scenario, func(t *testing.T) {
			b, ctx := emulatorBackend(t)
			_, now := seedInvitation(t, b, ctx)
			orgRef := b.Client.Collection("organizations").Doc("org")
			member := membershipRecord{ID: "member", OrganizationID: "org", UserID: "alice", Role: organization.Member}
			if scenario == "invalid-role" {
				member.Role = organization.Role("invalid")
			}
			if scenario != "removed-membership" {
				putDocument(t, ctx, orgRef.Collection("members").Doc("alice"), member)
			}
			putDocument(t, ctx, b.Client.Collection("users").Doc("alice").Collection("workspaces").Doc("org"), organization.Workspace{ID: "org", Name: "Stale", Role: organization.Owner})
			token := sha256.Sum256([]byte("session"))
			ref := b.Client.Collection("authSessions").Doc(hashID(token[:]))
			before := auth.Session{UserID: "alice", OrganizationID: "previous", ExpiresAt: now.Add(time.Hour)}
			if scenario == "expired-session" {
				before.ExpiresAt = now.Add(-time.Second)
			}
			if scenario == "other-user-session" {
				before.UserID = "bob"
			}
			if scenario != "missing-session" {
				putDocument(t, ctx, ref, before)
			}
			if scenario == "missing-organization" {
				if _, err := orgRef.Delete(ctx); err != nil {
					t.Fatal(err)
				}
			}
			workspace, err := b.Organization().SwitchWorkspace(ctx, token[:], "alice", "org", now)
			if !errors.Is(err, organization.ErrForbidden) || workspace.ID != "" {
				t.Fatalf("unauthorized switch: %+v %v", workspace, err)
			}
			doc, err := ref.Get(ctx)
			if scenario == "missing-session" {
				if !firestoreNotFound(err) {
					t.Fatalf("missing session was recreated: %v", err)
				}
			} else {
				if err != nil {
					t.Fatal(err)
				}
				var after auth.Session
				if err := doc.DataTo(&after); err != nil {
					t.Fatal(err)
				}
				if after.OrganizationID != before.OrganizationID {
					t.Fatal("denied switch changed session")
				}
			}
			docs, err := orgRef.Collection("auditLogs").Documents(ctx).GetAll()
			if err != nil || len(docs) != 0 {
				t.Fatalf("denied switch wrote audits: %d %v", len(docs), err)
			}
		})
	}
}
