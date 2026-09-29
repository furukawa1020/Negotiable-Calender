package firestorestore

import (
	"errors"
	"testing"
	"time"

	coord "github.com/negotiable-calendar/negotiable-calendar/apps/api/internal/request"
)

func TestWorkspaceRequestLists(t *testing.T) {
	b, ctx := emulatorBackend(t)
	store := b.Request()
	now := time.Now().UTC().Truncate(time.Second)
	for _, org := range []string{"org", "other"} {
		for _, user := range []string{"alice", "observer"} {
			putDocument(t, ctx, b.Client.Collection("organizations").Doc(org).Collection("members").Doc(user), map[string]any{"UserID": user})
		}
		// Bob has left both organizations. Alice must still see their history.
		for _, pair := range []struct{ id, requester, target string }{{"a", "bob", "alice"}, {"z", "bob", "alice"}, {"sent", "alice", "bob"}, {"unrelated", "bob", "carol"}} {
			value := confirmationRequest(org+"-"+pair.id, pair.requester, pair.target, now, now.Add(time.Hour))
			value.OrganizationID = org
			if pair.id == "a" {
				value.CreatedAt = now.Add(-time.Hour)
				value.Options = nil
			}
			putDocument(t, ctx, b.Client.Collection("coordinationRequests").Doc(value.ID), value)
		}
	}
	for _, org := range []string{"org", "other"} {
		for _, sent := range []bool{false, true} {
			values, err := store.ListInOrganization(ctx, "alice", org, sent)
			want := []string{org + "-z", org + "-a"}
			if sent {
				want = []string{org + "-sent"}
			}
			if err != nil || len(values) != len(want) {
				t.Fatalf("org=%s sent=%v values=%+v err=%v", org, sent, values, err)
			}
			for i, id := range want {
				if values[i].ID != id || values[i].OrganizationID != org || values[i].Options == nil {
					t.Fatalf("unexpected request: %+v", values[i])
				}
				if id != org+"-a" && (len(values[i].Options) != 1 || values[i].Options[0].ID != id+"-option") {
					t.Fatalf("options missing: %+v", values[i])
				}
			}
			values, err = store.ListInOrganization(ctx, "observer", org, sent)
			if err != nil || len(values) != 0 || values == nil {
				t.Fatalf("nonparticipant values=%+v err=%v", values, err)
			}
		}
	}
	global, err := store.ListForUser(ctx, "alice")
	if err != nil || len(global) != 6 {
		t.Fatalf("global conflict/export read changed: values=%d err=%v", len(global), err)
	}
	if _, err := b.Client.Collection("organizations").Doc("org").Collection("members").Doc("alice").Delete(ctx); err != nil {
		t.Fatal(err)
	}
	for _, sent := range []bool{false, true} {
		for _, pair := range [][2]string{{"alice", "org"}, {"bob", "other"}, {"alice", ""}, {"", "other"}} {
			values, err := store.ListInOrganization(ctx, pair[0], pair[1], sent)
			if !errors.Is(err, coord.ErrCreationForbidden) || values != nil {
				t.Fatalf("revoked/missing context leaked: %+v %v", values, err)
			}
		}
	}
	putDocument(t, ctx, b.Client.Collection("accountDeletions").Doc("alice"), map[string]any{"Phase": "deleting"})
	values, err := store.ListInOrganization(ctx, "alice", "other", false)
	if !errors.Is(err, coord.ErrCreationForbidden) || values != nil {
		t.Fatalf("deleting actor leaked: %+v %v", values, err)
	}
}

func TestWorkspaceRequestListDecodeFailureReturnsNoPartialData(t *testing.T) {
	b, ctx := emulatorBackend(t)
	putDocument(t, ctx, b.Client.Collection("organizations").Doc("org").Collection("members").Doc("alice"), map[string]any{"UserID": "alice"})
	putDocument(t, ctx, b.Client.Collection("coordinationRequests").Doc("invalid"), map[string]any{"OrganizationID": "org", "TargetUserID": "alice", "DurationMinutes": "invalid"})
	values, err := b.Request().ListInOrganization(ctx, "alice", "org", false)
	if err == nil || values != nil {
		t.Fatalf("invalid data must fail closed: %+v %v", values, err)
	}
}
