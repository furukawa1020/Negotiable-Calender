package firestorestore

import (
	"encoding/json"
	"errors"
	"fmt"
	coord "github.com/negotiable-calendar/negotiable-calendar/apps/api/internal/request"
	"testing"
	"time"
)

func TestConfirmedCancellationScopedAuthorization(t *testing.T) {

	for _, actor := range []string{"alice", "bob"} {
		for _, peerPresent := range []bool{true, false} {
			t.Run(fmt.Sprintf("cancel-scope-%s-peer-%t", actor, peerPresent), func(t *testing.T) {

				b, ctx := emulatorBackend(t)
				now := time.Now().UTC().Truncate(time.Second)
				store := b.Request()
				value := confirmationRequest("scoped-cancel", "alice", "bob", now, now.Add(time.Hour))
				value.Status, value.AcceptedOptionID = coord.Accepted, value.Options[0].ID
				putDocument(t, ctx, b.Client.Collection("coordinationRequests").Doc(value.ID), value)

				peer := "alice"
				if actor == peer {
					peer = "bob"
				}

				members := b.Client.Collection("organizations").Doc(value.OrganizationID).Collection("members")
				restoreMember := func(user string) { t.Helper(); putDocument(t, ctx, members.Doc(user), map[string]any{"UserID": user}) }
				removeMember := func(user string) {
					t.Helper()
					if _, err := members.Doc(user).Delete(ctx); err != nil {
						t.Fatal(err)
					}
				}
				restoreMember("alice")
				restoreMember("bob")

				if !peerPresent {
					removeMember(peer)
					defer restoreMember(peer)
				}
				read := func() string {
					t.Helper()
					got, err := store.GetForUser(ctx, value.ID, actor)
					if err != nil {
						t.Fatal(err)
					}
					data, err := json.Marshal(got)
					if err != nil {
						t.Fatal(err)
					}
					return string(data)
				}

				countEffects := func() int {
					t.Helper()
					audits, err := b.Client.Collection("organizations").Doc(value.OrganizationID).Collection("auditLogs").Documents(ctx).GetAll()
					if err != nil {
						t.Fatal(err)
					}
					n := len(audits)
					for _, user := range []string{"alice", "bob"} {
						notes, err := b.Client.Collection("users").Doc(user).Collection("notifications").Documents(ctx).GetAll()
						if err != nil {
							t.Fatal(err)
						}
						n += len(notes)
					}
					return n
				}

				checkDenied := func() {
					t.Helper()
					before, effects := read(), countEffects()
					for _, scope := range []struct{ actor, org string }{{actor, "other"}, {actor, ""}, {"carol", value.OrganizationID}, {"", value.OrganizationID}} {
						if err := store.CancelConfirmedInOrganization(ctx, value.ID, scope.actor, scope.org, value.AcceptedOptionID); !errors.Is(err, coord.ErrNotFound) {
							t.Fatalf("scope accepted: %v", err)
						}
					}
					removeMember(actor)
					err := store.CancelConfirmedInOrganization(ctx, value.ID, actor, value.OrganizationID, value.AcceptedOptionID)
					wrongOption := store.CancelConfirmedInOrganization(ctx, value.ID, actor, value.OrganizationID, "wrong-option")
					restoreMember(actor)
					if !errors.Is(err, coord.ErrCreationForbidden) || !errors.Is(wrongOption, coord.ErrCreationForbidden) {
						t.Fatalf("membership must precede replay/selection: %v, %v", err, wrongOption)
					}
					if before != read() || effects != countEffects() {
						t.Fatal("denial changed reservation/options/effects")
					}
				}
				checkDenied()
				effects := countEffects()
				if err := store.CancelConfirmedInOrganization(ctx, value.ID, actor, value.OrganizationID, value.AcceptedOptionID); err != nil {
					t.Fatal("authorized cancellation", err)
				}
				got, err := store.GetForUser(ctx, value.ID, actor)
				if err != nil || got.Status != coord.Cancelled || got.AcceptedOptionID != value.AcceptedOptionID {
					t.Fatal("wrong cancelled selection", err)
				}
				ranges, err := coord.ConfirmedRanges([]coord.CoordinationRequest{got})
				if err != nil || len(ranges) != 0 {
					t.Fatal("reservation retained", err)
				}
				if countEffects() != effects+2 {
					t.Fatal("missing atomic effects")
				}
				before := read()
				if err := store.CancelConfirmedInOrganization(ctx, value.ID, actor, value.OrganizationID, value.AcceptedOptionID); !errors.Is(err, coord.ErrAlreadyCancelled) {
					t.Fatal("authorized replay", err)
				}
				if before != read() || countEffects() != effects+2 {
					t.Fatal("replay changed state/effects")
				}
				checkDenied()
			})
		}
	}

}
