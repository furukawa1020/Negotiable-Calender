package firestorestore

import (
	"fmt"
	"sort"
	"testing"
	"time"

	"cloud.google.com/go/firestore"
	"github.com/negotiable-calendar/negotiable-calendar/apps/api/internal/audit"
	"github.com/negotiable-calendar/negotiable-calendar/apps/api/internal/notification"
)

func TestHistoryListsBoundedAndIsolated(t *testing.T) {
	for _, kind := range []string{"notifications", "auditLogs"} {
		t.Run(kind, func(t *testing.T) {
			b, ctx := emulatorBackend(t)
			limit := 100
			collection := b.Client.Collection("users").Doc("alice").Collection(kind)
			other := b.Client.Collection("users").Doc("bob").Collection(kind)
			if kind == "auditLogs" {
				limit = 200
				collection = b.Client.Collection("organizations").Doc("org").Collection(kind)
				other = b.Client.Collection("organizations").Doc("other").Collection(kind)
			}
			now := time.Now().UTC().Truncate(time.Microsecond)
			type entry struct {
				id string
				created time.Time
			}
			want := make([]entry, 0, limit+9)
			batch := b.Client.Batch()
			for i := 0; i < limit+9; i++ {
				id := fmt.Sprintf("event-%03d", i)
				// IDs run opposite to time order; groups share timestamps,
				// including across the page boundary.
				created := now.Add(-time.Duration(i/3) * time.Minute)
				want = append(want, entry{id, created})
				if kind == "notifications" {
					batch.Create(collection.Doc(id), notification.Notification{ID: id, UserID: "alice", Type: notification.RequestReceived, CreatedAt: created})
				} else {
					batch.Create(collection.Doc(id), audit.Event{ID: id, OrganizationID: "org", ActorUserID: "alice", Action: audit.RequestCreated, CreatedAt: created})
				}
			}
			// Deliberately undecodable records must never be fetched: one is
			// beyond the limit, the other belongs to a different principal.
			batch.Create(collection.Doc("old-invalid"), map[string]any{"ID": 123, "CreatedAt": now.Add(-24*time.Hour)})
			batch.Create(other.Doc("foreign-invalid"), map[string]any{"ID": 123, "CreatedAt": now.Add(time.Hour)})
			if _, err := batch.Commit(ctx); err != nil {
				t.Fatal(err)
			}
			sort.Slice(want, func(i, j int) bool {
				if want[i].created.Equal(want[j].created) {
					return want[i].id > want[j].id
				}
				return want[i].created.After(want[j].created)
			})
			var got []entry
			if kind == "notifications" {
				values, err := b.Notification().List(ctx, "alice")
				if err != nil { t.Fatal(err) }
				for _, value := range values {
					if value.UserID != "alice" { t.Fatalf("wrong user: %s", value.UserID) }
					got = append(got, entry{value.ID, value.CreatedAt})
				}
				empty, err := b.Notification().List(ctx, "empty")
				if err != nil || empty == nil || len(empty) != 0 {
					t.Fatalf("empty notifications = %v, %v", empty, err)
				}
			} else {
				values, err := b.Audit().List(ctx, "org")
				if err != nil { t.Fatal(err) }
				for _, value := range values {
					if value.OrganizationID != "org" { t.Fatalf("wrong organization: %s", value.OrganizationID) }
					got = append(got, entry{value.ID, value.CreatedAt})
				}
				empty, err := b.Audit().List(ctx, "empty")
				if err != nil || empty == nil || len(empty) != 0 {
					t.Fatalf("empty audits = %v, %v", empty, err)
				}
			}
			if len(got) != limit { t.Fatalf("got %d records, want %d", len(got), limit) }
			for i := range got {
				if got[i].id != want[i].id || !got[i].created.Equal(want[i].created) {
					t.Fatalf("record %d = %+v, want %+v", i, got[i], want[i])
				}
			}
			// Listing is not retention: no records are removed.
			count, err := collection.Select(firestore.DocumentID).Documents(ctx).GetAll()
			if err != nil || len(count) != limit+10 {
				t.Fatalf("stored records = %d, error %v", len(count), err)
			}
		})
	}
}
