package main

import (
	"context"
	"database/sql"
	"errors"
	coord "github.com/negotiable-calendar/negotiable-calendar/apps/api/internal/request"
	"testing"
	"time"
)

func testPostgresHandoff(t *testing.T, ctx context.Context, db *sql.DB, store *coord.PostgresStore, fixture func(string, string, string, time.Time) coord.CoordinationRequest, now time.Time) {
	t.Helper()
	if _, e := db.ExecContext(ctx, `INSERT INTO memberships(id,organization_id,user_id,role,created_at) VALUES('handoff-carol','org','carol','MANAGER',$1)`, now); e != nil {
		t.Fatal(e)
	}
	t.Run("handoff-delivery-and-replay", func(t *testing.T) {
		value := fixture("handoff", "alice", "bob", now.Add(20*time.Hour))
		if e := store.Create(ctx, value); e != nil {
			t.Fatal(e)
		}
		option := coord.Option{ID: "fresh", RequestID: value.ID, Type: coord.OptionAsync, ResponseBy: &value.DeadlineAt, CreatedAt: now}
		if snapshot, r, e := store.InspectHandoff(ctx, value.ID, "bob", "org", "carol"); e != nil || r || snapshot.TargetUserID != "bob" {
			t.Fatal("inspect failed", e)
		}
		type result struct {
			replay bool
			err    error
		}
		gate, results := make(chan struct{}), make(chan result, 2)
		for range 2 {
			go func() {
				<-gate
				r, e := store.Handoff(ctx, value.ID, "bob", "org", "carol", []coord.Option{option})
				results <- result{r, e}
			}()
		}
		close(gate)
		writes, replays := 0, 0
		for range 2 {
			r := <-results
			if r.err != nil {
				t.Fatal(r.err)
			}
			if r.replay {
				replays++
			} else {
				writes++
			}
		}
		if writes != 1 || replays != 1 {
			t.Fatal("duplicate handoff")
		}
		got, e := store.GetForUser(ctx, value.ID, "carol")
		if e != nil || got.TargetUserID != "carol" || got.DelegatedFromUserID != "bob" || got.Options[0].ID == value.Options[0].ID {
			t.Fatal("routing failed", e)
		}
		if _, e := store.GetForUser(ctx, value.ID, "bob"); !errors.Is(e, coord.ErrNotFound) {
			t.Fatal("old reader retained access", e)
		}
		inbox, e := store.ListForTarget(ctx, "carol")
		if e != nil {
			t.Fatal(e)
		}
		found := false
		for _, r := range inbox {
			if r.ID == value.ID {
				found = true
			}
		}
		if !found {
			t.Fatal("missing new inbox")
		}
		if current, e := store.LookupCreation(ctx, value); e != nil || current.TargetUserID != "carol" {
			t.Fatal("creation retry lost", e)
		}
		notes, event := coord.HandoffEffects(got, "bob")
		for _, note := range notes {
			var recipient string
			if e := db.QueryRowContext(ctx, "SELECT user_id FROM notifications WHERE id=$1", note.ID).Scan(&recipient); e != nil || recipient != note.UserID {
				t.Fatal("wrong note", e)
			}
		}
		var n int
		if e := db.QueryRowContext(ctx, "SELECT count(*) FROM audit_logs WHERE id=$1", event.ID).Scan(&n); e != nil || n != 1 {
			t.Fatal("missing audit", e)
		}
		if _, e := db.ExecContext(ctx, "UPDATE notifications SET read_at=$1 WHERE id=$2", now, notes[0].ID); e != nil {
			t.Fatal(e)
		}
		if _, e := store.ResolveRequest(ctx, value.ID, "bob", "org", coord.ResolutionCommand{Status: coord.Async, Message: "old"}); !errors.Is(e, coord.ErrNotFound) {
			t.Fatal("old writer retained access", e)
		}
		if _, e := store.ResolveRequest(ctx, value.ID, "carol", "org", coord.ResolutionCommand{Status: coord.Async, Message: "private new answer"}); e != nil {
			t.Fatal("new owner cannot answer", e)
		}
		if snapshot, r, e := store.InspectHandoff(ctx, value.ID, "bob", "org", "carol"); e != nil || !r || snapshot.ID != "" || snapshot.AsyncMessage != "" {
			t.Fatal("unsafe replay", e)
		}
		if r, e := store.Handoff(ctx, value.ID, "bob", "org", "carol", nil); e != nil || !r {
			t.Fatal("replay failed", e)
		}
		var readAt sql.NullTime
		if e := db.QueryRowContext(ctx, "SELECT read_at FROM notifications WHERE id=$1", notes[0].ID).Scan(&readAt); e != nil || !readAt.Valid {
			t.Fatal("read state reset", e)
		}
	})
	for _, scenario := range []string{"recipient-note", "requester-note", "audit", "membership", "expired"} {
		t.Run("handoff-rollback-"+scenario, func(t *testing.T) {
			value := fixture("handoff-"+scenario, "alice", "bob", now.Add(20*time.Hour))
			if e := store.Create(ctx, value); e != nil {
				t.Fatal(e)
			}
			option := coord.Option{ID: "fresh", RequestID: value.ID, Type: coord.OptionAsync, ResponseBy: &value.DeadlineAt, CreatedAt: now}
			updated := value
			_ = coord.ApplyHandoff(&updated, "bob", "carol", []coord.Option{option}, now)
			notes, event := coord.HandoffEffects(updated, "bob")
			to := "carol"
			if scenario == "membership" {
				to = "missing-member"
			}
			if scenario == "expired" {
				if _, e := db.ExecContext(ctx, "UPDATE coordination_requests SET deadline_at=$1 WHERE id=$2", now.Add(-time.Hour), value.ID); e != nil {
					t.Fatal(e)
				}
			}
			for i, n := range notes {
				if (i == 0 && scenario == "recipient-note") || (i == 1 && scenario == "requester-note") {
					if _, e := db.ExecContext(ctx, `INSERT INTO notifications(id,user_id,type,request_id,message,created_at) VALUES($1,$2,$3,$4,$5,$6)`, n.ID, n.UserID, n.Type, n.RequestID, n.Message, now); e != nil {
						t.Fatal(e)
					}
				}
			}
			if scenario == "audit" {
				if _, e := db.ExecContext(ctx, `INSERT INTO audit_logs(id,organization_id,actor_user_id,action,resource_type,resource_id,created_at) VALUES($1,$2,$3,$4,$5,$6,$7)`, event.ID, event.OrganizationID, event.ActorUserID, event.Action, event.ResourceType, event.ResourceID, now); e != nil {
					t.Fatal(e)
				}
			}
			if _, e := store.Handoff(ctx, value.ID, "bob", "org", to, []coord.Option{option}); e == nil {
				t.Fatal("invalid handoff committed")
			}
			got, e := store.GetForUser(ctx, value.ID, "bob")
			if e != nil || got.TargetUserID != "bob" || got.DelegatedFromUserID != "" || got.Options[0].ID != value.Options[0].ID {
				t.Fatal("partial handoff", e)
			}
			for i, note := range notes {
				expected := 0
				if (i == 0 && scenario == "recipient-note") || (i == 1 && scenario == "requester-note") {
					expected = 1
				}
				var n int
				if e := db.QueryRowContext(ctx, "SELECT count(*) FROM notifications WHERE id=$1", note.ID).Scan(&n); e != nil || n != expected {
					t.Fatal("partial notification", e, n)
				}
			}
		})
	}
	t.Run("handoff-versus-response", func(t *testing.T) {
		value := fixture("handoff-race", "alice", "bob", now.Add(20*time.Hour))
		if e := store.Create(ctx, value); e != nil {
			t.Fatal(e)
		}
		option := coord.Option{ID: "fresh", RequestID: value.ID, Type: coord.OptionAsync, ResponseBy: &value.DeadlineAt, CreatedAt: now}
		gate, results := make(chan struct{}), make(chan error, 3)
		go func() {
			<-gate
			_, e := store.Handoff(ctx, value.ID, "bob", "org", "carol", []coord.Option{option})
			results <- e
		}()
		go func() {
			<-gate
			_, e := store.ResolveRequest(ctx, value.ID, "bob", "org", coord.ResolutionCommand{Status: coord.Async, Message: "answer"})
			results <- e
		}()
		go func() { <-gate; results <- store.Respond(ctx, value.ID, "bob", coord.Accepted, value.Options[0].ID) }()
		close(gate)
		winners := 0
		for range 3 {
			e := <-results
			if e == nil {
				winners++
			} else if !errors.Is(e, coord.ErrHandoffConflict) && !errors.Is(e, coord.ErrNotFound) && !errors.Is(e, coord.ErrResolutionConflict) && !errors.Is(e, coord.ErrBookingConflict) {
				t.Fatal("unexpected race result", e)
			}
		}
		if winners != 1 {
			t.Fatal("multiple winners", winners)
		}
		if _, e := db.ExecContext(ctx, "DELETE FROM coordination_requests WHERE id=$1", value.ID); e != nil {
			t.Fatal(e)
		}
	})
	t.Run("new-handoff-owner-confirms", func(t *testing.T) {
		value := fixture("handoff-meeting", "alice", "bob", now.Add(21*time.Hour))
		if e := store.Create(ctx, value); e != nil {
			t.Fatal(e)
		}
		if _, e := store.Handoff(ctx, value.ID, "bob", "org", "carol", value.Options); e != nil {
			t.Fatal(e)
		}
		got, e := store.GetForUser(ctx, value.ID, "carol")
		if e != nil {
			t.Fatal(e)
		}
		if e := store.Respond(ctx, value.ID, "bob", coord.Accepted, value.Options[0].ID); e == nil {
			t.Fatal("old owner accepted")
		}
		if e := store.Respond(ctx, value.ID, "carol", coord.Accepted, value.Options[0].ID); e == nil {
			t.Fatal("stale option accepted")
		}
		if e := store.Respond(ctx, value.ID, "carol", coord.Accepted, got.Options[0].ID); e != nil {
			t.Fatal("new owner cannot confirm", e)
		}
		got, e = store.GetForUser(ctx, value.ID, "alice")
		if e != nil || got.Status != coord.Accepted || got.TargetUserID != "carol" {
			t.Fatal("confirmation lost", e)
		}
		if _, e := db.ExecContext(ctx, "DELETE FROM coordination_requests WHERE id=$1", value.ID); e != nil {
			t.Fatal(e)
		}
	})
}
