package main

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	coord "github.com/negotiable-calendar/negotiable-calendar/apps/api/internal/request"
)

func testPostgresMeetingEvidence(t *testing.T, ctx context.Context, db *sql.DB, store *coord.PostgresStore, fixture func(string, string, string, time.Time) coord.CoordinationRequest, now time.Time) {
	t.Helper()
	// SQL keys enforce selection uniqueness/ownership. A valid non-meeting row
	// still must not be accepted as evidence of a completed meeting command.
	for _, action := range []string{"confirm-replay", "cancel", "cancel-replay"} {
		t.Run("meeting-evidence/"+action, func(t *testing.T) {
			value := fixture("evidence-"+action, "alice", "bob", now.Add(time.Hour))
			value.Status, value.AcceptedOptionID = coord.Accepted, value.Options[0].ID
			if action == "cancel-replay" {
				value.Status = coord.Cancelled
			}
			if err := store.Create(ctx, value); err != nil {
				t.Fatal(err)
			}
			defer db.ExecContext(ctx, "DELETE FROM coordination_requests WHERE id=$1", value.ID)
			if _, err := db.ExecContext(ctx, "UPDATE coordination_request_options SET type='async' WHERE request_id=$1", value.ID); err != nil {
				t.Fatal(err)
			}
			var err error
			want := coord.ErrCancellationInvalid
			if action == "confirm-replay" {
				want = coord.ErrCandidateInvalid
				err = store.Respond(ctx, value.ID, "bob", coord.Accepted, value.AcceptedOptionID)
			} else {
				err = store.CancelConfirmed(ctx, value.ID, "alice", value.AcceptedOptionID)
			}
			if !errors.Is(err, want) {
				t.Errorf("got %v want %v", err, want)
			}
			got, err := store.GetForUser(ctx, value.ID, "alice")
			if err != nil || got.Status != value.Status || got.AcceptedOptionID != value.AcceptedOptionID || !got.UpdatedAt.Equal(value.UpdatedAt) || len(got.Options) != 1 || got.Options[0].Type != coord.OptionAsync {
				t.Fatal("rejection mutated request", err)
			}
			for _, query := range []string{"SELECT COUNT(*) FROM notifications WHERE request_id=$1", "SELECT COUNT(*) FROM audit_logs WHERE resource_id=$1"} {
				var count int
				if err := db.QueryRowContext(ctx, query, value.ID).Scan(&count); err != nil || count != 0 {
					t.Fatal("partial effect", count, err)
				}
			}
		})
	}
}
