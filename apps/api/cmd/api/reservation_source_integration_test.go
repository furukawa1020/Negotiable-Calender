package main

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	coord "github.com/negotiable-calendar/negotiable-calendar/apps/api/internal/request"
)

func testPostgresReservationSource(t *testing.T, ctx context.Context, db *sql.DB, store *coord.PostgresStore, fixture func(string, string, string, time.Time) coord.CoordinationRequest, now time.Time) {
	t.Run("bounded-reservation-source", func(t *testing.T) {
		exec := func(query string, args ...any) {
			t.Helper()
			if _, err := db.ExecContext(ctx, query, args...); err != nil {
				t.Fatal(err)
			}
		}
		// The caller owns an isolated test schema; clean only this fixture prefix.
		defer exec(`DELETE FROM coordination_requests WHERE id LIKE 'reservation-source-%'`)
		for _, pair := range [][3]string{{"one", "alice", "bob"}, {"two", "carol", "alice"}, {"three", "bob", "carol"}} {
			v := fixture("reservation-source-"+pair[0], pair[1], pair[2], now.Add(time.Hour))
			v.OrganizationID = "other"
			if err := store.Create(ctx, v); err != nil {
				t.Fatal(err)
			}
			exec(`UPDATE coordination_requests SET status='accepted',accepted_option_id=$2 WHERE id=$1`, v.ID, v.Options[0].ID)
		}
		// Non-accepted history has no options and must never enter the join/read budget.
		exec(`INSERT INTO coordination_requests(id,organization_id,requester_user_id,target_user_id,type,title,duration_minutes,deadline_at,sync_preference,priority,status,created_at,updated_at)
SELECT 'reservation-source-history-'||n,organization_id,requester_user_id,target_user_id,type,title,duration_minutes,deadline_at,sync_preference,priority,'completed',created_at,updated_at
FROM coordination_requests CROSS JOIN generate_series(1,5001) n WHERE id='reservation-source-one'`)
		if ranges, err := store.LoadConfirmedRanges(ctx, "alice", "bob"); err != nil || len(ranges) != 3 {
			t.Fatal("history or duplicate roles broke reservation read", len(ranges), err)
		}
		for _, selected := range []string{"missing", "reservation-source-two-option"} {
			exec(`UPDATE coordination_requests SET accepted_option_id=$1 WHERE id='reservation-source-one'`, selected)
			if ranges, err := store.LoadConfirmedRanges(ctx, "alice", "bob"); !errors.Is(err, coord.ErrAvailabilityChanged) || ranges != nil {
				t.Fatal("missing/wrong-request selection returned partial ranges", err)
			}
		}
		exec(`UPDATE coordination_requests SET accepted_option_id='reservation-source-one-option' WHERE id='reservation-source-one'`)
		exec(`UPDATE coordination_request_options SET type='unknown' WHERE id='reservation-source-one-option'`)
		if ranges, err := store.LoadConfirmedRanges(ctx, "alice", "bob"); !errors.Is(err, coord.ErrAvailabilityChanged) || ranges != nil {
			t.Fatal("unknown option became free time", err)
		}
		exec(`UPDATE coordination_request_options SET type='meeting' WHERE id='reservation-source-one-option'`)
		for _, participant := range []string{"", "alice", "missing-user"} {
			if ranges, err := store.LoadConfirmedRanges(ctx, "alice", participant); err == nil || ranges != nil {
				t.Fatal("invalid/missing participant accepted")
			}
		}
		cancelled, cancel := context.WithCancel(ctx)
		cancel()
		if ranges, err := store.LoadConfirmedRanges(cancelled, "alice", "bob"); err == nil || ranges != nil {
			t.Fatal("cancellation ignored")
		}
		exec(`UPDATE coordination_requests SET status='accepted' WHERE id LIKE 'reservation-source-history-%'`)
		if ranges, err := store.LoadConfirmedRanges(ctx, "alice", "bob"); !errors.Is(err, coord.ErrReservationUnavailable) || ranges != nil {
			t.Fatal("accepted overflow returned partial ranges", err)
		}
	})
}
