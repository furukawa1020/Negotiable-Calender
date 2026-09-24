package firestorestore

import (
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/negotiable-calendar/negotiable-calendar/apps/api/internal/accountcleanup"
)

func TestPendingDeletionPagesAreBoundedAndStableAcrossCompletion(t *testing.T) {
	b, ctx := emulatorBackend(t)
	for i := 0; i < 105; i++ {
		putDocument(t, ctx, b.accountDeletionRef(fmt.Sprintf("pending-%03d", i)), accountDeletion{Phase: "deleting"})
	}
	putDocument(t, ctx, b.accountDeletionRef("completed"), accountDeletion{Phase: "complete"})
	putDocument(t, ctx, b.accountDeletionRef("unknown"), accountDeletion{Phase: "invalid"})
	page, err := b.Auth().ListPendingAccountDeletions(ctx, 100, "")
	if err != nil || len(page.UserIDs) != 100 || !page.HasMore || page.UserIDs[0] != "pending-000" || page.UserIDs[99] != "pending-099" {
		t.Fatalf("bad bounded page: %d %v", len(page.UserIDs), err)
	}
	putDocument(t, ctx, b.accountDeletionRef("pending-099"), accountDeletion{Phase: "complete"})
	next, err := b.Auth().ListPendingAccountDeletions(ctx, 100, accountcleanup.EncodeCursor("pending-099"))
	if err != nil || len(next.UserIDs) != 5 || next.HasMore || next.UserIDs[0] != "pending-100" {
		t.Fatalf("bad continuation: %+v %v", next, err)
	}
	for _, limit := range []int{0, 101} {
		if _, err := b.Auth().ListPendingAccountDeletions(ctx, limit, ""); err == nil {
			t.Fatal("invalid limit accepted")
		}
	}
	if _, err := b.Auth().ListPendingAccountDeletions(ctx, 1, "invalid"); err == nil {
		t.Fatal("invalid cursor accepted")
	}
}

func TestPendingSweepContinuesAfterCorruptMarkerAndNeverStartsDeletion(t *testing.T) {
	b, ctx := emulatorBackend(t)
	seedDeletionAccount(t, b, ctx)
	if _, err := b.Auth().beginAccountDeletion(ctx, "alice"); err != nil {
		t.Fatal(err)
	}
	putDocument(t, ctx, b.Client.Collection("users").Doc("active"), userRecord{ID: "active"})
	putDocument(t, ctx, b.accountDeletionRef("00-corrupt"), map[string]any{"Phase": "deleting", "StartedAt": "invalid-time"})
	putDocument(t, ctx, b.accountDeletionRef("zz-next"), accountDeletion{Phase: "deleting"})
	opts := accountcleanup.Options{Pending: true, Limit: 2, Timeout: time.Minute, PerAccountTimeout: 30 * time.Second, DryRun: true}
	r, err := accountcleanup.Run(ctx, b.Auth(), opts)
	if err != nil || r.Selected != 2 || r.Attempted != 0 || !r.HasMore {
		t.Fatalf("dryrun: %+v %v", r, err)
	}
	if _, err := b.Client.Collection("users").Doc("alice").Get(ctx); err != nil {
		t.Fatal("dryrun deleted account")
	}
	opts.DryRun = false
	r, err = accountcleanup.Run(ctx, b.Auth(), opts)
	if !errors.Is(err, accountcleanup.ErrIncomplete) || r.Succeeded != 1 || r.Failed != 1 || !r.HasMore || r.NextCursor != accountcleanup.EncodeCursor("alice") {
		t.Fatalf("retry: %+v %v", r, err)
	}
	if _, err := b.Client.Collection("users").Doc("alice").Get(ctx); !firestoreNotFound(err) {
		t.Fatal("pending cleanup did not complete")
	}
	if _, err := b.Client.Collection("users").Doc("active").Get(ctx); err != nil {
		t.Fatal("active user modified")
	}
	if _, err := b.accountDeletionRef("active").Get(ctx); !firestoreNotFound(err) {
		t.Fatal("new deletion initiated")
	}
	opts.Cursor = r.NextCursor
	r, err = accountcleanup.Run(ctx, b.Auth(), opts)
	if err != nil || r.Succeeded != 1 || r.HasMore {
		t.Fatalf("next page failed: %+v %v", r, err)
	}
	page, err := b.Auth().ListPendingAccountDeletions(ctx, 100, "")
	if err != nil || len(page.UserIDs) != 1 || page.UserIDs[0] != "00-corrupt" {
		t.Fatal("failed marker lost instead of remaining retryable")
	}
}
