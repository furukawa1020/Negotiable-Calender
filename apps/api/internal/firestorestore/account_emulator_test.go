package firestorestore

import (
	"errors"
	"testing"
	"time"

	"github.com/negotiable-calendar/negotiable-calendar/apps/api/internal/auth"
	calendarintegration "github.com/negotiable-calendar/negotiable-calendar/apps/api/internal/calendar"
	"github.com/negotiable-calendar/negotiable-calendar/apps/api/internal/organization"
)

func TestDeleteAccountCalendarFlows(t *testing.T) {
	for _, blocked := range []bool{false, true} {
		name := "deletes-owned-flows"
		if blocked {
			name = "last-owner-keeps-flows"
		}
		t.Run(name, func(t *testing.T) {
			b, ctx := emulatorBackend(t)
			now := time.Now().UTC().Truncate(time.Microsecond)
			for _, userID := range []string{"alice", "bob"} {
				putDocument(t, ctx, b.Client.Collection("users").Doc(userID), userRecord{ID: userID})
			}
			if blocked {
				if err := b.Auth().createWorkspace(ctx, "alice", organization.Workspace{ID: "org", Name: "Team", Role: organization.Owner}, now); err != nil {
					t.Fatal(err)
				}
				putDocument(t, ctx, b.Client.Collection("organizations").Doc("org").Collection("members").Doc("bob"), membershipRecord{ID: "bob-member", OrganizationID: "org", UserID: "bob", Role: organization.Member, CreatedAt: now})
			}
			flows := []calendarintegration.Flow{
				{ID: "alice-active", UserID: "alice", CodeVerifier: "synthetic-active", StateHash: []byte("synthetic-state"), CreatedAt: now, ExpiresAt: now.Add(time.Hour)},
				{ID: "alice-expired", UserID: "alice", CodeVerifier: "synthetic-expired", StateHash: []byte("synthetic-state"), CreatedAt: now.Add(-2*time.Hour), ExpiresAt: now.Add(-time.Hour)},
				{ID: "bob-active", UserID: "bob", CodeVerifier: "synthetic-other", StateHash: []byte("synthetic-state"), CreatedAt: now, ExpiresAt: now.Add(time.Hour)},
			}
			for _, flow := range flows {
				if err := b.Calendar().CreateFlow(ctx, flow); err != nil {
					t.Fatal(err)
				}
			}
			err := b.Auth().DeleteAccount(ctx, "alice")
			if blocked {
				if !errors.Is(err, auth.ErrLastOrganizationOwner) {
					t.Fatalf("expected last-owner rejection, got %v", err)
				}
			} else if err != nil {
				t.Fatal(err)
			}
			for _, flow := range flows {
				doc, err := b.Client.Collection("calendarOAuthFlows").Doc(flow.ID).Get(ctx)
				if !blocked && flow.UserID == "alice" {
					if !firestoreNotFound(err) {
						t.Fatalf("deleted flow %s still exists: %v", flow.ID, err)
					}
					continue
				}
				if err != nil {
					t.Fatalf("retained flow %s: %v", flow.ID, err)
				}
				var got calendarintegration.Flow
				if err := doc.DataTo(&got); err != nil {
					t.Fatal(err)
				}
				if got.ID != flow.ID || got.UserID != flow.UserID || got.CodeVerifier != flow.CodeVerifier || string(got.StateHash) != string(flow.StateHash) || !got.ExpiresAt.Equal(flow.ExpiresAt) || !got.CreatedAt.Equal(flow.CreatedAt) {
					t.Fatalf("retained flow %s changed", flow.ID)
				}
			}
			for _, userID := range []string{"alice", "bob"} {
				_, err := b.Client.Collection("users").Doc(userID).Get(ctx)
				if userID == "alice" && !blocked {
					if !firestoreNotFound(err) {
						t.Fatalf("deleted account still exists: %v", err)
					}
				} else if err != nil {
					t.Fatalf("retained account %s: %v", userID, err)
				}
			}
		})
	}
}
