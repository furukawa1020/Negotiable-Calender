package firestorestore

import (
	"errors"
	"testing"
	"time"

	"github.com/negotiable-calendar/negotiable-calendar/apps/api/internal/auth"
)

func TestResumeAccountDeletionCannotInitiateDeletion(t *testing.T) {
	b, ctx := emulatorBackend(t)
	seedDeletionAccount(t, b, ctx)
	if err := b.Auth().ResumeAccountDeletion(ctx, "alice"); !firestoreNotFound(err) {
		t.Fatalf("resume started a new deletion: %v", err)
	}
	if _, err := b.accountDeletionRef("alice").Get(ctx); !firestoreNotFound(err) {
		t.Fatal("resume changed lifecycle")
	}
	if _, err := b.Auth().beginAccountDeletion(ctx, "alice"); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if err := b.Auth().ResumeAccountDeletion(ctx, "alice"); err != nil {
			t.Fatalf("resume %d: %v", i, err)
		}
	}
}

func TestConcurrentSignInAfterDeletionDoesNotOrphanAccounts(t *testing.T) {
	b, ctx := emulatorBackend(t)
	now := time.Now().UTC()
	profile := auth.Profile{Subject: "concurrent-synthetic", Email: "alice@example.test", DisplayName: "Alice"}
	old, err := b.Auth().UpsertGoogleIdentity(ctx, profile, now)
	if err != nil {
		t.Fatal(err)
	}
	if err := b.Auth().DeleteAccount(ctx, old.UserID); err != nil {
		t.Fatal(err)
	}
	type result struct {
		id  string
		err error
	}
	start := make(chan struct{})
	results := make(chan result, 2)
	for i := 0; i < 2; i++ {
		go func() {
			<-start
			identity, err := b.Auth().UpsertGoogleIdentity(ctx, profile, now)
			results <- result{identity.UserID, err}
		}()
	}
	close(start)
	successes := 0
	var id string
	for i := 0; i < 2; i++ {
		r := <-results
		if r.err != nil {
			if !errors.Is(r.err, errAccountDeleting) {
				t.Fatal(r.err)
			}
			continue
		}
		successes++
		if id != "" && id != r.id {
			t.Fatal("concurrent sign-ins created different accounts")
		}
		id = r.id
	}
	if successes == 0 || id == old.UserID {
		t.Fatal("fresh account not created")
	}
	users, err := b.Client.Collection("users").Documents(ctx).GetAll()
	if err != nil || len(users) != 1 || users[0].Ref.ID != id {
		t.Fatalf("orphan account remained: %d %v", len(users), err)
	}
}
