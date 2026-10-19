package firestorestore

import (
	"errors"
	coord "github.com/negotiable-calendar/negotiable-calendar/apps/api/internal/request"
	"testing"
	"time"
)

func TestWorkspaceRequestDetailSnapshot(t *testing.T) {
	b, ctx := emulatorBackend(t)
	now := time.Now().UTC().Truncate(time.Second)
	v := confirmationRequest("r", "alice", "bob", now, now.Add(time.Hour))
	v.OrganizationID = "org"
	putDocument(t, ctx, b.Client.Collection("coordinationRequests").Doc("r"), v)
	for _, org := range []string{"org", "other"} {
		for _, actor := range []string{"alice", "eve"} {
			putDocument(t, ctx, b.Client.Collection("organizations").Doc(org).Collection("members").Doc(actor), map[string]any{"UserID": actor})
		}
	}
	for _, tc := range []struct {
		id, actor, org string
		want           error
	}{{"r", "alice", "org", nil}, {"r", "alice", "other", coord.ErrNotFound}, {"r", "eve", "org", coord.ErrNotFound}, {"r", "bob", "org", coord.ErrCreationForbidden}, {"missing", "alice", "org", coord.ErrNotFound}, {"r", "alice", "", coord.ErrCreationForbidden}} {
		got, err := b.Request().GetInOrganization(ctx, tc.id, tc.actor, tc.org)
		if !errors.Is(err, tc.want) || (err != nil && got.ID != "") || (err == nil && (got.ID != "r" || len(got.Options) != 1)) {
			t.Fatalf("got=%+v err=%v want=%v", got, err, tc.want)
		}
	}
	v.ID = "corrupt"
	putDocument(t, ctx, b.Client.Collection("coordinationRequests").Doc("r"), v)
	if got, err := b.Request().GetInOrganization(ctx, "r", "alice", "org"); !errors.Is(err, coord.ErrNotFound) || got.ID != "" {
		t.Fatal("mismatched document leaked", err)
	}
	v.ID = "r"
	v.Options = nil
	putDocument(t, ctx, b.Client.Collection("coordinationRequests").Doc("r"), v)
	if got, err := b.Request().GetInOrganization(ctx, "r", "alice", "org"); err != nil || got.Options == nil {
		t.Fatal("nil option list", err)
	}
	if _, err := b.Client.Collection("organizations").Doc("org").Collection("members").Doc("alice").Delete(ctx); err != nil {
		t.Fatal(err)
	}
	if got, err := b.Request().GetInOrganization(ctx, "r", "alice", "org"); !errors.Is(err, coord.ErrCreationForbidden) || got.ID != "" {
		t.Fatal("revoked actor leaked", err)
	}
	putDocument(t, ctx, b.Client.Collection("organizations").Doc("org").Collection("members").Doc("alice"), map[string]any{"UserID": "alice"})
	putDocument(t, ctx, b.Client.Collection("accountDeletions").Doc("alice"), map[string]any{"Phase": "deleting"})
	if got, err := b.Request().GetInOrganization(ctx, "r", "alice", "org"); !errors.Is(err, coord.ErrCreationForbidden) || got.ID != "" {
		t.Fatal("deleting actor leaked", err)
	}
}
