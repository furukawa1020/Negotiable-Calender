package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"reflect"
	"testing"
	"time"

	cal "github.com/negotiable-calendar/negotiable-calendar/apps/api/internal/calendar"
	coord "github.com/negotiable-calendar/negotiable-calendar/apps/api/internal/request"
)

func testPostgresRequesterCalendar(t *testing.T, ctx context.Context, db *sql.DB, store *coord.PostgresStore, fixture func(string, string, string, time.Time) coord.CoordinationRequest, now time.Time) {
	if got, err := store.LoadRequesterCalendar(ctx, "missing"); err == nil || got.Validate(now) == nil {
		t.Fatal("missing requester treated as free")
	}
	if got, err := store.LoadRequesterCalendar(ctx, "alice"); err != nil || !got.Allows(now, now.Add(time.Hour), now) {
		t.Fatal("never-connected requester blocked", err)
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if got, err := store.LoadRequesterCalendar(cancelled, "alice"); err == nil || got.Validate(now) == nil {
		t.Fatal("cancelled source returned usable data")
	}
	for _, scenario := range []string{"busy", "free", "adjacent", "empty", "stale", "syncing", "failed", "unknown", "outside", "disconnect", "invalid-time", "invalid-status", "limit", "overflow", "reschedule-busy", "reschedule-stale"} {
		t.Run("requester-calendar/"+scenario, func(t *testing.T) {
			exec := func(q string, args ...any) {
				t.Helper()
				if _, err := db.ExecContext(ctx, q, args...); err != nil {
					t.Fatal(err)
				}
			}
			v := fixture("requester-calendar", "alice", "bob", now.Add(18*time.Hour))
			defer db.ExecContext(ctx, `DELETE FROM coordination_requests WHERE id=$1`, v.ID)
			defer db.ExecContext(ctx, `DELETE FROM notifications WHERE request_id=$1`, v.ID)
			defer db.ExecContext(ctx, `DELETE FROM audit_logs WHERE resource_id=$1`, v.ID)
			defer db.ExecContext(ctx, `DELETE FROM private_events WHERE user_id='alice'`)
			defer db.ExecContext(ctx, `DELETE FROM calendar_source_snapshots WHERE user_id='alice'`)
			defer db.ExecContext(ctx, `DELETE FROM calendar_connections WHERE user_id='alice'`)
			if err := store.Create(ctx, v); err != nil {
				t.Fatal(err)
			}
			rescheduling := scenario == "reschedule-busy" || scenario == "reschedule-stale"
			command := coord.RescheduleCommand{Action: "propose", ProposalID: "requester-proposal", ExpectedOptionID: v.Options[0].ID, StartAt: now.Add(19 * time.Hour)}
			start := *v.Options[0].StartAt
			if rescheduling {
				if err := store.ConfirmMeeting(ctx, v.ID, "bob", "org", v.Options[0].ID); err != nil {
					t.Fatal(err)
				}
				if err := store.RescheduleInOrganization(ctx, v.ID, "alice", "org", command); err != nil {
					t.Fatal(err)
				}
				start = command.StartAt
			}
			observed := now.Add(-time.Minute)
			snapshot := cal.SourceSnapshot{Revision: "requester-revision", ObservedAt: observed, From: now.Add(-time.Hour), To: now.Add(24 * time.Hour)}
			if scenario == "stale" || scenario == "reschedule-stale" {
				observed = now.Add(-cal.SourceMaxAge)
				snapshot.ObservedAt = observed
			}
			if scenario == "outside" {
				snapshot.To = start.Add(time.Minute)
			}
			if scenario == "unknown" {
				snapshot = cal.SourceSnapshot{}
			}
			data, err := json.Marshal(snapshot)
			if err != nil {
				t.Fatal(err)
			}
			exec(`INSERT INTO calendar_source_snapshots(user_id,snapshot) VALUES('alice',$1)`, data)
			if scenario != "disconnect" {
				exec(`INSERT INTO calendar_connections(user_id,refresh_token_cipher,granted_scopes,connected_at,last_synced_at) VALUES('alice','synthetic','',$1,$1)`, observed)
			}
			if scenario == "syncing" {
				exec(`UPDATE calendar_connections SET sync_lease_id='in-flight' WHERE user_id='alice'`)
			}
			if scenario == "failed" {
				exec(`UPDATE calendar_connections SET last_error_code='timeout' WHERE user_id='alice'`)
			}
			end, busy := start.Add(30*time.Minute), "free"
			if scenario == "busy" || scenario == "reschedule-busy" {
				busy = "busy"
			}
			if scenario == "free" {
				busy = "free"
			}
			if scenario == "adjacent" {
				end, start = start, start.Add(-time.Hour)
				busy = "busy"
			}
			if scenario == "invalid-time" {
				end = start
			}
			if scenario == "invalid-status" {
				busy = "corrupt"
			}
			if scenario == "limit" || scenario == "overflow" {
				count := 5000
				if scenario == "overflow" {
					count++
				}
				exec(`INSERT INTO private_events(user_id,provider_event_id,calendar_id,start_at,end_at,busy_status,visibility,created_at,updated_at) SELECT 'alice','synthetic-'||i,'primary',$1,$2,'free','default',$3,$3 FROM generate_series(1,$4::int) i`, start, end, now, count)
			} else if scenario != "empty" {
				exec(`INSERT INTO private_events(user_id,provider_event_id,calendar_id,start_at,end_at,busy_status,visibility,created_at,updated_at) VALUES('alice','synthetic','primary',$1,$2,$3,'default',$4,$4)`, start, end, busy, now)
			}
			candidateSnapshot, candidateErr := store.LoadRequesterCalendar(ctx, "alice")
			wantAllowed := scenario == "free" || scenario == "adjacent" || scenario == "empty" || scenario == "limit"
			wantSource := wantAllowed || scenario == "busy" || scenario == "reschedule-busy" || scenario == "outside"
			if (candidateErr == nil) != wantSource {
				t.Fatal("candidate source result", candidateErr)
			}
			candidateStart := *v.Options[0].StartAt
			if rescheduling {
				candidateStart = command.StartAt
			}
			if candidateErr == nil && candidateSnapshot.Allows(candidateStart, candidateStart.Add(30*time.Minute), time.Now().UTC()) != wantAllowed {
				t.Fatal("candidate source slot predicate")
			}
			before, err := store.GetForUser(ctx, v.ID, "alice")
			if err != nil {
				t.Fatal(err)
			}
			if rescheduling {
				command.Action = "accept"
				err = store.RescheduleInOrganization(ctx, v.ID, "bob", "org", command)
			} else {
				err = store.ConfirmMeeting(ctx, v.ID, "bob", "org", v.Options[0].ID)
			}
			allowed := scenario == "free" || scenario == "adjacent" || scenario == "empty" || scenario == "limit"
			if allowed && err != nil {
				t.Fatal("free requester blocked", err)
			}
			if !allowed && !errors.Is(err, coord.ErrAvailabilityChanged) {
				t.Fatalf("unsafe requester calendar accepted: %v", err)
			}
			after, err := store.GetForUser(ctx, v.ID, "alice")
			if err != nil {
				t.Fatal(err)
			}
			if !allowed && !reflect.DeepEqual(before, after) {
				t.Fatal("rejection changed booking")
			}
			want := 0
			if allowed {
				want = 1
			}
			if rescheduling {
				want = 2
			}
			for _, q := range []string{`SELECT count(*) FROM notifications WHERE request_id=$1`, `SELECT count(*) FROM audit_logs WHERE resource_id=$1`} {
				var n int
				if err := db.QueryRowContext(ctx, q, v.ID).Scan(&n); err != nil || n != want {
					t.Fatal("incorrect effects", n, err)
				}
			}
			if allowed {
				exec(`UPDATE calendar_connections SET last_error_code='timeout' WHERE user_id='alice'`)
				if got, err := store.LoadRequesterCalendar(ctx, "alice"); err == nil || got.Validate(time.Now().UTC()) == nil {
					t.Fatal("changed source reused old successful read")
				}
				if err := store.ConfirmMeeting(ctx, v.ID, "bob", "org", v.Options[0].ID); !errors.Is(err, coord.ErrAlreadyAccepted) {
					t.Fatal("saved confirmation blocked", err)
				}
				if err := store.CancelConfirmedInOrganization(ctx, v.ID, "alice", "org", v.Options[0].ID); err != nil {
					t.Fatal("cancellation blocked", err)
				}
			}
		})
	}
}
