package firestorestore

import (
	"fmt"
	"strings"
	"testing"

	"github.com/negotiable-calendar/negotiable-calendar/apps/api/internal/audit"
	"github.com/negotiable-calendar/negotiable-calendar/apps/api/internal/organization"
)

func TestAccountDeletionIsolatesActorAuditsWithoutDecodingEventBodies(t *testing.T) {
	b, ctx := emulatorBackend(t)
	seedDeletionAccount(t, b, ctx)
	// Keep the shared organization alive with another authorized owner.
	putDocument(t, ctx, b.Client.Collection("users").Doc("bob"), userRecord{ID: "bob"})
	putDocument(t, ctx, b.Client.Collection("organizations").Doc("org").Collection("members").Doc("bob"), membershipRecord{UserID: "bob", Role: organization.Owner})
	audits := b.Client.Collection("organizations").Doc("org").Collection("auditLogs")
	// The former full scan decoded CreatedAt before checking ActorUserID.
	// These unrelated, intentionally malformed rows would stop Alice's deletion.
	for i := 0; i < 405; i++ {
		putDocument(t, ctx, audits.Doc(fmt.Sprintf("keep-%03d", i)), map[string]any{"ActorUserID": "bob", "CreatedAt": "not-a-time", "UnrelatedBody": strings.Repeat("x", 2048)})
	}
	putDocument(t, ctx, audits.Doc("missing-actor"), map[string]any{"CreatedAt": "not-a-time"})
	unrelated, err := audits.Doc("keep-000").Get(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var legacyDecoded audit.Event
	if err := unrelated.DataTo(&legacyDecoded); err == nil {
		t.Fatal("fixture does not reproduce the former full-scan decoding failure")
	}
	for _, id := range []string{"delete-1", "delete-2"} {
		putDocument(t, ctx, audits.Doc(id), map[string]any{"ActorUserID": "alice", "CreatedAt": "not-a-time", "ResourceType": 123})
	}
	foreign := b.Client.Collection("organizations").Doc("foreign").Collection("auditLogs").Doc("keep")
	putDocument(t, ctx, foreign, map[string]any{"ActorUserID": "alice", "CreatedAt": "not-a-time"})
	if _, err := b.Auth().beginAccountDeletion(ctx, "alice"); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if err := b.Auth().ResumeAccountDeletion(ctx, "alice"); err != nil {
			t.Fatalf("authorized cleanup retry %d: %v", i, err)
		}
	}
	for _, id := range []string{"delete-1", "delete-2"} {
		if _, err := audits.Doc(id).Get(ctx); !firestoreNotFound(err) {
			t.Fatalf("matching audit remains: %s", id)
		}
	}
	docs, err := audits.Documents(ctx).GetAll()
	if err != nil || len(docs) != 406 {
		t.Fatalf("unrelated audits changed: %d %v", len(docs), err)
	}
	for _, doc := range docs {
		data := doc.Data()
		if data["CreatedAt"] != "not-a-time" {
			t.Fatal("unrelated audit body changed")
		}
		if doc.Ref.ID != "missing-actor" && (data["ActorUserID"] != "bob" || data["UnrelatedBody"] != strings.Repeat("x", 2048)) {
			t.Fatal("unrelated body lost")
		}
	}
	if _, err := foreign.Get(ctx); err != nil {
		t.Fatal("another organization's audit changed")
	}
	if _, err := b.Client.Collection("users").Doc("alice").Get(ctx); !firestoreNotFound(err) {
		t.Fatal("deletion did not complete")
	}
	if _, err := b.Client.Collection("users").Doc("bob").Get(ctx); err != nil {
		t.Fatal("other account changed")
	}
	if _, err := b.Client.Collection("organizations").Doc("org").Get(ctx); err != nil {
		t.Fatal("shared organization removed")
	}
}
