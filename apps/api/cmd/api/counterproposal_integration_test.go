package main

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	coord "github.com/negotiable-calendar/negotiable-calendar/apps/api/internal/request"
)

func testPostgresCounterproposal(t *testing.T, ctx context.Context, db *sql.DB, store *coord.PostgresStore, fixture func(string, string, string, time.Time) coord.CoordinationRequest, now time.Time) {
	t.Helper()
	t.Run("counterproposal-agreement", func(t *testing.T) {
		value := fixture("offer", "alice", "bob", now.Add(22*time.Hour))
		if e := store.Create(ctx, value); e != nil {
			t.Fatal(e)
		}
		start, end := *value.Options[0].StartAt, *value.Options[0].EndAt
		type result struct {
			option coord.Option
			replay bool
			err    error
		}
		gate, results := make(chan struct{}), make(chan result, 2)
		for range 2 {
			go func() {
				<-gate
				o, r, e := store.ProposeMeeting(ctx, value.ID, "bob", "org", start, end)
				results <- result{o, r, e}
			}()
		}
		close(gate)
		writes, replays := 0, 0
		var offer coord.Option
		for range 2 {
			r := <-results
			if r.err != nil {
				t.Fatal(r.err)
			}
			offer = r.option
			if r.replay {
				replays++
			} else {
				writes++
			}
		}
		if writes != 1 || replays != 1 {
			t.Fatal("duplicate offer")
		}
		got, e := store.GetForUser(ctx, value.ID, "alice")
		if e != nil || len(got.Options) != 2 {
			t.Fatal("missing offer", e)
		}
		note, event := coord.ProposalEffects(got, offer)
		for _, c := range []struct{ query, want string }{{"SELECT user_id FROM notifications WHERE id=$1", "alice"}, {"SELECT actor_user_id FROM audit_logs WHERE id=$1", "bob"}} {
			var actual string
			if e := db.QueryRowContext(ctx, c.query, event.ID).Scan(&actual); e != nil || actual != c.want {
				t.Fatal("effects", actual, e)
			}
		}
		if _, e := db.ExecContext(ctx, "UPDATE notifications SET read_at=$1 WHERE id=$2", now, note.ID); e != nil {
			t.Fatal(e)
		}
		for _, c := range []struct{ actor, org, option string }{{"bob", "org", offer.ID}, {"alice", "org", value.Options[0].ID}, {"carol", "org", offer.ID}, {"alice", "other", offer.ID}} {
			if e := store.ConfirmMeeting(ctx, value.ID, c.actor, c.org, c.option); !errors.Is(e, coord.ErrNotFound) {
				t.Fatal("unauthorized", c, e)
			}
		}
		gate = make(chan struct{})
		confirmed := make(chan error, 2)
		for range 2 {
			go func() { <-gate; confirmed <- store.ConfirmMeeting(ctx, value.ID, "alice", "org", offer.ID) }()
		}
		close(gate)
		success := 0
		for range 2 {
			e := <-confirmed
			if e == nil {
				success++
			} else if !errors.Is(e, coord.ErrAlreadyAccepted) && !errors.Is(e, coord.ErrBookingConflict) {
				t.Fatal(e)
			}
		}
		if success != 1 {
			t.Fatal("multiple reservations")
		}
		if e := store.ConfirmMeeting(ctx, value.ID, "alice", "org", offer.ID); !errors.Is(e, coord.ErrAlreadyAccepted) {
			t.Fatal(e)
		}
		got, e = store.GetForUser(ctx, value.ID, "bob")
		if e != nil || got.Status != coord.Accepted || got.AcceptedOptionID != offer.ID {
			t.Fatal(e)
		}
		bobICS, e := coord.CalendarExport(got)
		if e != nil {
			t.Fatal("target cannot export", e)
		}
		aliceValue, e := store.GetForUser(ctx, value.ID, "alice")
		if e != nil {
			t.Fatal(e)
		}
		aliceICS, e := coord.CalendarExport(aliceValue)
		if e != nil || aliceICS != bobICS {
			t.Fatal("participants export different meetings", e)
		}
		cn, ce := coord.ConfirmationEffectsForActor(got, "alice", now)
		var recipient, actor string
		if e := db.QueryRowContext(ctx, "SELECT user_id FROM notifications WHERE id=$1", cn.ID).Scan(&recipient); e != nil || recipient != "bob" {
			t.Fatal(e)
		}
		if e := db.QueryRowContext(ctx, "SELECT actor_user_id FROM audit_logs WHERE id=$1", ce.ID).Scan(&actor); e != nil || actor != "alice" {
			t.Fatal(e)
		}
		if _, r, e := store.ProposeMeeting(ctx, value.ID, "bob", "org", start, end); e != nil || !r {
			t.Fatal("retry", e)
		}
		var readAt sql.NullTime
		if e := db.QueryRowContext(ctx, "SELECT read_at FROM notifications WHERE id=$1", note.ID).Scan(&readAt); e != nil || !readAt.Valid {
			t.Fatal("read reset", e)
		}
		if _, e := db.ExecContext(ctx, "DELETE FROM coordination_requests WHERE id=$1", value.ID); e != nil {
			t.Fatal(e)
		}
	})
	for _, scenario := range []string{"note", "audit", "expired", "limit"} {
		t.Run("proposal-rollback-"+scenario, func(t *testing.T) {
			value := fixture("offer-"+scenario, "alice", "bob", now.Add(22*time.Hour))
			if e := store.Create(ctx, value); e != nil {
				t.Fatal(e)
			}
			start, end := *value.Options[0].StartAt, *value.Options[0].EndAt
			copy := value
			offer, _, e := coord.PrepareProposal(&copy, "bob", "org", start, end, now)
			if e != nil {
				t.Fatal(e)
			}
			note, event := coord.ProposalEffects(copy, offer)
			if scenario == "note" {
				if _, e := db.ExecContext(ctx, "INSERT INTO notifications(id,user_id,type,request_id,message,created_at) VALUES($1,$2,$3,$4,$5,$6)", note.ID, note.UserID, note.Type, note.RequestID, note.Message, now); e != nil {
					t.Fatal(e)
				}
			}
			if scenario == "audit" {
				if _, e := db.ExecContext(ctx, "INSERT INTO audit_logs(id,organization_id,actor_user_id,action,resource_type,resource_id,created_at) VALUES($1,$2,$3,$4,$5,$6,$7)", event.ID, event.OrganizationID, event.ActorUserID, event.Action, event.ResourceType, event.ResourceID, now); e != nil {
					t.Fatal(e)
				}
			}
			if scenario == "expired" {
				if _, e := db.ExecContext(ctx, "UPDATE coordination_requests SET deadline_at=$1 WHERE id=$2", now.Add(-time.Hour), value.ID); e != nil {
					t.Fatal(e)
				}
			}
			expectedOptions := 1
			if scenario == "limit" {
				for i := 1; i < coord.MaxRequestOptions; i++ {
					at := now.Add(time.Duration(i) * time.Hour)
					if _, _, e := store.ProposeMeeting(ctx, value.ID, "bob", "org", at, at.Add(30*time.Minute)); e != nil {
						t.Fatal(e)
					}
				}
				expectedOptions = coord.MaxRequestOptions
			}
			if _, _, e := store.ProposeMeeting(ctx, value.ID, "bob", "org", start, end); e == nil {
				t.Fatal("invalid proposal")
			}
			got, e := store.GetForUser(ctx, value.ID, "alice")
			if e != nil || len(got.Options) != expectedOptions {
				t.Fatal("partial options", e)
			}
			for _, c := range []struct {
				query  string
				seeded bool
			}{{"SELECT count(*) FROM notifications WHERE id=$1", scenario == "note"}, {"SELECT count(*) FROM audit_logs WHERE id=$1", scenario == "audit"}} {
				var count int
				want := 0
				if c.seeded {
					want = 1
				}
				if e := db.QueryRowContext(ctx, c.query, offer.ID).Scan(&count); e != nil || count != want {
					t.Fatal("partial effects", e, count)
				}
			}
		})
	}
	for _, scenario := range []string{"availability", "booking", "note", "audit", "membership"} {
		t.Run("offer-confirmation-"+scenario, func(t *testing.T) {
			value := fixture("offer-confirm-"+scenario, "alice", "bob", now.Add(22*time.Hour))
			if e := store.Create(ctx, value); e != nil {
				t.Fatal(e)
			}
			offer, _, e := store.ProposeMeeting(ctx, value.ID, "bob", "org", *value.Options[0].StartAt, *value.Options[0].EndAt)
			if e != nil {
				t.Fatal(e)
			}
			note, event := coord.ConfirmationEffectsForActor(value, "alice", now)
			if scenario == "note" {
				if _, e := db.ExecContext(ctx, "INSERT INTO notifications(id,user_id,type,request_id,message,created_at) VALUES($1,$2,$3,$4,$5,$6)", note.ID, note.UserID, note.Type, note.RequestID, note.Message, now); e != nil {
					t.Fatal(e)
				}
			}
			if scenario == "audit" {
				if _, e := db.ExecContext(ctx, "INSERT INTO audit_logs(id,organization_id,actor_user_id,action,resource_type,resource_id,created_at) VALUES($1,$2,$3,$4,$5,$6,$7)", event.ID, event.OrganizationID, event.ActorUserID, event.Action, event.ResourceType, event.ResourceID, now); e != nil {
					t.Fatal(e)
				}
			}
			if scenario == "availability" {
				if _, e := db.ExecContext(ctx, "UPDATE schedule_projections SET availability='busy' WHERE user_id='bob'"); e != nil {
					t.Fatal(e)
				}
				defer db.ExecContext(ctx, "UPDATE schedule_projections SET availability='available' WHERE user_id='bob'")
			}
			if scenario == "membership" {
				if _, e := db.ExecContext(ctx, "DELETE FROM memberships WHERE organization_id='org' AND user_id='alice'"); e != nil {
					t.Fatal(e)
				}
				defer db.ExecContext(ctx, "INSERT INTO memberships(id,organization_id,user_id,role,created_at) VALUES('restored-alice','org','alice','MANAGER',$1)", now)
			}
			if scenario == "booking" {
				other := fixture("offer-competing", "alice", "carol", *offer.StartAt)
				if e := store.Create(ctx, other); e != nil {
					t.Fatal(e)
				}
				if e := store.Respond(ctx, other.ID, "carol", coord.Accepted, other.Options[0].ID); e != nil {
					t.Fatal(e)
				}
				defer db.ExecContext(ctx, "DELETE FROM coordination_requests WHERE id=$1", other.ID)
			}
			if e := store.ConfirmMeeting(ctx, value.ID, "alice", "org", offer.ID); e == nil {
				t.Fatal("invalid confirmation")
			}
			got, e := store.GetForUser(ctx, value.ID, "alice")
			if e != nil || got.Status != coord.Suggested || got.AcceptedOptionID != "" {
				t.Fatal("partial confirmation", e)
			}
			for _, c := range []struct {
				query  string
				seeded bool
			}{{"SELECT count(*) FROM notifications WHERE id=$1", scenario == "note"}, {"SELECT count(*) FROM audit_logs WHERE id=$1", scenario == "audit"}} {
				var count int
				want := 0
				if c.seeded {
					want = 1
				}
				if e := db.QueryRowContext(ctx, c.query, event.ID).Scan(&count); e != nil || count != want {
					t.Fatal("partial effects", e, count)
				}
			}
		})
	}
	t.Run("offer-acceptance-versus-handoff", func(t *testing.T) {
		value := fixture("offer-race", "alice", "bob", now.Add(22*time.Hour))
		if e := store.Create(ctx, value); e != nil {
			t.Fatal(e)
		}
		offer, _, e := store.ProposeMeeting(ctx, value.ID, "bob", "org", *value.Options[0].StartAt, *value.Options[0].EndAt)
		if e != nil {
			t.Fatal(e)
		}
		gate, results := make(chan struct{}), make(chan error, 3)
		go func() { <-gate; results <- store.ConfirmMeeting(ctx, value.ID, "alice", "org", offer.ID) }()
		go func() {
			<-gate
			_, e := store.ResolveRequest(ctx, value.ID, "bob", "org", coord.ResolutionCommand{Status: coord.Async, Message: "answer"})
			results <- e
		}()
		go func() {
			<-gate
			_, e := store.Handoff(ctx, value.ID, "bob", "org", "carol", []coord.Option{{ID: "fresh", RequestID: value.ID, Type: coord.OptionAsync, ResponseBy: &value.DeadlineAt, CreatedAt: now}})
			results <- e
		}()
		close(gate)
		winners := 0
		for range 3 {
			e := <-results
			if e == nil {
				winners++
			} else if !errors.Is(e, coord.ErrNotFound) && !errors.Is(e, coord.ErrCandidateInvalid) && !errors.Is(e, coord.ErrBookingConflict) && !errors.Is(e, coord.ErrResolutionConflict) && !errors.Is(e, coord.ErrHandoffConflict) {
				t.Fatal(e)
			}
		}
		if winners != 1 {
			t.Fatal("conflicting winners", winners)
		}
		if _, e := db.ExecContext(ctx, "DELETE FROM coordination_requests WHERE id=$1", value.ID); e != nil {
			t.Fatal(e)
		}
	})
}
