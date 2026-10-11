package main

import (
	"context"
	"database/sql"
	"errors"
	coord "github.com/negotiable-calendar/negotiable-calendar/apps/api/internal/request"
	"testing"
	"time"
)

func testPostgresResolution(t *testing.T, ctx context.Context, db *sql.DB, store *coord.PostgresStore, fixture func(string, string, string, time.Time) coord.CoordinationRequest, now time.Time) {
	t.Helper()
	for _, scenario := range []string{"notification", "audit", "early", "stale", "proposal", "membership"} {
		t.Run("completion-rollback-"+scenario, func(t *testing.T) {
			at := now.Add(-2 * time.Hour)
			if scenario == "early" {
				at = now.Add(time.Hour)
			}
			v := fixture("complete-rollback-"+scenario, "alice", "bob", at)
			if scenario == "membership" {
				v.TargetUserID = "carol"
			}
			if err := store.Create(ctx, v); err != nil {
				t.Fatal(err)
			}
			v.Status, v.AcceptedOptionID = coord.Accepted, v.Options[0].ID
			if _, err := db.ExecContext(ctx, "UPDATE coordination_requests SET status=$1,accepted_option_id=$2 WHERE id=$3", v.Status, v.AcceptedOptionID, v.ID); err != nil {
				t.Fatal(err)
			}
			if scenario == "proposal" {
				if _, err := db.ExecContext(ctx, `UPDATE coordination_requests SET reschedule_proposal='{"status":"proposed"}'::jsonb WHERE id=$1`, v.ID); err != nil {
					t.Fatal(err)
				}
			}
			completed := v
			completed.Status = coord.Completed
			note, event := coord.ResolutionEffects(completed, "alice")
			if scenario == "notification" {
				if _, err := db.ExecContext(ctx, `INSERT INTO notifications(id,user_id,type,request_id,message,created_at) VALUES($1,$2,$3,$4,$5,$6)`, note.ID, note.UserID, note.Type, note.RequestID, note.Message, now); err != nil {
					t.Fatal(err)
				}
			}
			if scenario == "audit" {
				if _, err := db.ExecContext(ctx, `INSERT INTO audit_logs(id,organization_id,actor_user_id,action,resource_type,resource_id,created_at) VALUES($1,$2,$3,$4,$5,$6,$7)`, event.ID, event.OrganizationID, event.ActorUserID, event.Action, event.ResourceType, event.ResourceID, now); err != nil {
					t.Fatal(err)
				}
			}
			cmd := coord.ResolutionCommand{Status: coord.Completed, ExpectedOptionID: v.AcceptedOptionID}
			if scenario == "stale" {
				cmd.ExpectedOptionID = "other"
			}
			if _, err := store.ResolveRequest(ctx, v.ID, "alice", "org", cmd); err == nil {
				t.Fatal("unsafe completion committed")
			}
			got, err := store.GetForUser(ctx, v.ID, "alice")
			if err != nil || got.Status != coord.Accepted || got.AcceptedOptionID != v.AcceptedOptionID || !got.UpdatedAt.Equal(v.UpdatedAt) {
				t.Fatal("failure mutated request", err)
			}
			for _, table := range []string{"notifications", "audit_logs"} {
				var count int
				if err := db.QueryRowContext(ctx, "SELECT count(*) FROM "+table+" WHERE id=$1", note.ID).Scan(&count); err != nil {
					t.Fatal(err)
				}
				want := 0
				if (scenario == "notification" && table == "notifications") || (scenario == "audit" && table == "audit_logs") {
					want = 1
				}
				if count != want {
					t.Fatal("partial completion effects")
				}
			}
			// In this shared fixture, the deliberately still-accepted future meeting
			// must not reserve time for unrelated confirmation tests that follow.
			if _, err := db.ExecContext(ctx, "DELETE FROM coordination_requests WHERE id=$1", v.ID); err != nil {
				t.Fatal(err)
			}
		})
	}
	for _, state := range []coord.Status{coord.Async, coord.Declined, coord.Cancelled, coord.Completed} {
		t.Run("resolution-"+string(state), func(t *testing.T) {
			value := fixture("resolve-"+string(state), "alice", "bob", now.Add(20*time.Hour))
			if state == coord.Completed {
				start, end := now.Add(-2*time.Hour), now.Add(-time.Hour)
				value.Options[0].StartAt, value.Options[0].EndAt = &start, &end
			}
			if err := store.Create(ctx, value); err != nil {
				t.Fatal(err)
			}
			if state == coord.Completed {
				value.AcceptedOptionID = value.Options[0].ID
				if _, err := db.ExecContext(ctx, "UPDATE coordination_requests SET status=$1,accepted_option_id=$2 WHERE id=$3", coord.Accepted, value.AcceptedOptionID, value.ID); err != nil {
					t.Fatal(err)
				}
			}
			actor := "bob"
			cmd := coord.ResolutionCommand{Status: state}
			if state == coord.Completed {
				cmd.ExpectedOptionID = value.AcceptedOptionID
			}
			if state == coord.Cancelled {
				actor = "alice"
			}
			if state == coord.Async {
				cmd.Message = "private answer"
			}
			type result struct {
				replay bool
				err    error
			}
			gate, results := make(chan struct{}), make(chan result, 2)
			for range 2 {
				go func() {
					<-gate
					r, e := store.ResolveRequest(ctx, value.ID, actor, "org", cmd)
					results <- result{r, e}
				}()
			}
			close(gate)
			first, replays := 0, 0
			for range 2 {
				r := <-results
				if r.err != nil {
					t.Fatal(r.err)
				}
				if r.replay {
					replays++
				} else {
					first++
				}
			}
			if first != 1 || replays != 1 {
				t.Fatal("duplicate state change")
			}
			got, err := store.GetForUser(ctx, value.ID, "alice")
			if err != nil || got.Status != state || got.AsyncMessage != cmd.Message {
				t.Fatal("wrong result", err)
			}
			note, _ := coord.ResolutionEffects(got, actor)
			for _, table := range []string{"notifications", "audit_logs"} {
				var n int
				if e := db.QueryRowContext(ctx, "SELECT count(*) FROM "+table+" WHERE id=$1", note.ID).Scan(&n); e != nil || n != 1 {
					t.Fatal("missing effects", e, n)
				}
			}
			if _, e := db.ExecContext(ctx, "UPDATE notifications SET read_at=$1 WHERE id=$2", now, note.ID); e != nil {
				t.Fatal(e)
			}
			if _, e := db.ExecContext(ctx, "UPDATE coordination_requests SET deadline_at=$1 WHERE id=$2", now.Add(-time.Hour), value.ID); e != nil {
				t.Fatal(e)
			}
			if r, e := store.ResolveRequest(ctx, value.ID, actor, "org", cmd); e != nil || !r {
				t.Fatal("late replay lost", e)
			}
			var readAt sql.NullTime
			if e := db.QueryRowContext(ctx, "SELECT read_at FROM notifications WHERE id=$1", note.ID).Scan(&readAt); e != nil || !readAt.Valid {
				t.Fatal("read state reset", e)
			}
			if _, e := store.ResolveRequest(ctx, value.ID, "carol", "org", cmd); !errors.Is(e, coord.ErrNotFound) {
				t.Fatal("outsider access", e)
			}
			if _, e := store.ResolveRequest(ctx, value.ID, actor, "other", cmd); !errors.Is(e, coord.ErrNotFound) {
				t.Fatal("org leak", e)
			}
			if state == coord.Async {
				changed := cmd
				changed.Message = "changed"
				if _, e := store.ResolveRequest(ctx, value.ID, actor, "org", changed); !errors.Is(e, coord.ErrResolutionConflict) {
					t.Fatal("overwrite accepted", e)
				}
			}
		})
	}
	for _, scenario := range []string{"notification", "audit", "membership", "expired"} {
		t.Run("resolution-rollback-"+scenario, func(t *testing.T) {
			value := fixture("resolve-rollback-"+scenario, "alice", "bob", now.Add(20*time.Hour))
			if scenario == "membership" {
				value.TargetUserID = "carol"
			}
			if err := store.Create(ctx, value); err != nil {
				t.Fatal(err)
			}
			if scenario == "expired" {
				if _, err := db.ExecContext(ctx, "UPDATE coordination_requests SET deadline_at=$1 WHERE id=$2", now.Add(-time.Hour), value.ID); err != nil {
					t.Fatal(err)
				}
			}
			resolved := value
			resolved.Status = coord.Async
			resolved.UpdatedAt = now
			note, event := coord.ResolutionEffects(resolved, value.TargetUserID)
			if scenario == "notification" {
				if _, e := db.ExecContext(ctx, `INSERT INTO notifications(id,user_id,type,request_id,message,created_at,read_at) VALUES($1,$2,$3,$4,$5,$6,$6)`, note.ID, note.UserID, note.Type, note.RequestID, note.Message, now); e != nil {
					t.Fatal(e)
				}
			}
			if scenario == "audit" {
				if _, e := db.ExecContext(ctx, `INSERT INTO audit_logs(id,organization_id,actor_user_id,action,resource_type,resource_id,created_at) VALUES($1,$2,$3,$4,$5,$6,$7)`, event.ID, event.OrganizationID, event.ActorUserID, event.Action, event.ResourceType, event.ResourceID, now); e != nil {
					t.Fatal(e)
				}
			}
			if _, e := store.ResolveRequest(ctx, value.ID, value.TargetUserID, "org", coord.ResolutionCommand{Status: coord.Async, Message: "answer"}); e == nil {
				t.Fatal("invalid commit")
			}
			got, e := store.GetForUser(ctx, value.ID, "alice")
			if e != nil || got.Status != coord.Suggested || got.AsyncMessage != "" {
				t.Fatal("partial state", e)
			}
			for _, table := range []string{"notifications", "audit_logs"} {
				expected := 0
				if (scenario == "notification" && table == "notifications") || (scenario == "audit" && table == "audit_logs") {
					expected = 1
				}
				var n int
				if e := db.QueryRowContext(ctx, "SELECT count(*) FROM "+table+" WHERE id=$1", note.ID).Scan(&n); e != nil || n != expected {
					t.Fatal("partial effects", e, n)
				}
			}
		})
	}
	t.Run("resolution-race-with-acceptance", func(t *testing.T) {
		value := fixture("resolve-race", "alice", "bob", now.Add(20*time.Hour))
		if err := store.Create(ctx, value); err != nil {
			t.Fatal(err)
		}
		gate, results := make(chan struct{}), make(chan error, 4)
		for _, cmd := range []coord.ResolutionCommand{{Status: coord.Async, Message: "answer"}, {Status: coord.Declined}, {Status: coord.Cancelled}} {
			go func(cmd coord.ResolutionCommand) {
				<-gate
				actor := "bob"
				if cmd.Status == coord.Cancelled {
					actor = "alice"
				}
				_, e := store.ResolveRequest(ctx, value.ID, actor, "org", cmd)
				results <- e
			}(cmd)
		}
		go func() { <-gate; results <- store.Respond(ctx, value.ID, "bob", coord.Accepted, value.Options[0].ID) }()
		close(gate)
		winners := 0
		for range 4 {
			e := <-results
			if e == nil {
				winners++
			} else if !errors.Is(e, coord.ErrResolutionConflict) && !errors.Is(e, coord.ErrNotFound) && !errors.Is(e, coord.ErrBookingConflict) {
				t.Fatal("unexpected race error", e)
			}
		}
		if winners != 1 {
			t.Fatal("multiple winners", winners)
		}
		for _, query := range []string{"SELECT count(*) FROM notifications WHERE request_id=$1", "SELECT count(*) FROM audit_logs WHERE resource_id=$1"} {
			var n int
			if e := db.QueryRowContext(ctx, query, value.ID).Scan(&n); e != nil || n != 1 {
				t.Fatal("duplicate effects", e, n)
			}
		}
		// Remove only this synthetic reservation so later confirmation scenarios remain isolated.
		if _, e := db.ExecContext(ctx, "DELETE FROM coordination_requests WHERE id=$1", value.ID); e != nil {
			t.Fatal(e)
		}
	})
}
