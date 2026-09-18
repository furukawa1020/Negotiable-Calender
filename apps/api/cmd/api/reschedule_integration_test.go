package main

import (
	"context"
	"database/sql"
	"errors"
	"github.com/negotiable-calendar/negotiable-calendar/apps/api/internal/audit"
	coordinationrequest "github.com/negotiable-calendar/negotiable-calendar/apps/api/internal/request"
	"testing"
	"time"
)

func testPostgresReschedule(t *testing.T, ctx context.Context, db *sql.DB, store *coordinationrequest.PostgresStore, fixture func(string, string, string, time.Time) coordinationrequest.CoordinationRequest, now time.Time) {
	for i, scenario := range []string{"alice", "bob", "rollback", "cancel-race", "busy"} {
		t.Run("reschedule-"+scenario, func(t *testing.T) {
			start := now.Add(time.Duration(14+i*2) * time.Hour)
			value := fixture("reschedule-"+scenario, "alice", "bob", start)
			if err := store.Create(ctx, value); err != nil {
				t.Fatal(err)
			}
			if err := store.Respond(ctx, value.ID, "bob", coordinationrequest.Accepted, value.Options[0].ID); err != nil {
				t.Fatal(err)
			}
			actor, other := "alice", "bob"
			if scenario == "bob" {
				actor, other = other, actor
			}
			command := coordinationrequest.RescheduleCommand{Action: "propose", ProposalID: "proposal-" + scenario, ExpectedOptionID: value.Options[0].ID, StartAt: start.Add(30 * time.Minute).Add(123456789 * time.Nanosecond)}
			if err := store.Reschedule(ctx, value.ID, actor, command); err != nil {
				t.Fatal(err)
			}
			if err := store.Reschedule(ctx, value.ID, actor, command); !errors.Is(err, coordinationrequest.ErrRescheduleRepeated) {
				t.Fatal(err)
			}
			got, err := store.GetForUser(ctx, value.ID, actor)
			if err != nil || got.AcceptedOptionID != value.Options[0].ID || got.RescheduleProposal.ID != command.ProposalID {
				t.Fatal("original/proposal not persisted", err)
			}
			command.Action = "accept"
			if err := store.Reschedule(ctx, value.ID, actor, command); !errors.Is(err, coordinationrequest.ErrRescheduleInvalid) {
				t.Fatal("self approved", err)
			}
			if scenario == "rollback" || scenario == "busy" {
				if scenario == "rollback" {
					_, event := coordinationrequest.RescheduleEffects(got, other, "accept", now)
					if err := audit.NewPostgresStore(db).Create(ctx, event); err != nil {
						t.Fatal(err)
					}
				} else {
					busy := fixture("reschedule-block", "carol", "bob", command.StartAt)
					if err := store.Create(ctx, busy); err != nil {
						t.Fatal(err)
					}
					if err := store.Respond(ctx, busy.ID, "bob", coordinationrequest.Accepted, busy.Options[0].ID); err != nil {
						t.Fatal(err)
					}
				}
				if err := store.Reschedule(ctx, value.ID, other, command); err == nil {
					t.Fatal("failed swap committed")
				}
				got, err = store.GetForUser(ctx, value.ID, actor)
				if err != nil || got.AcceptedOptionID != value.Options[0].ID || got.RescheduleProposal.Status != "proposed" {
					t.Fatal("old booking lost")
				}
				var count int
				if err := db.QueryRowContext(ctx, "SELECT count(*) FROM notifications WHERE request_id=$1", value.ID).Scan(&count); err != nil || count != 1 {
					t.Fatal("partial effect")
				}
				return
			}
			gate := make(chan struct{})
			results := make(chan error, 2)
			go func() { <-gate; results <- store.Reschedule(ctx, value.ID, other, command) }()
			go func() {
				<-gate
				if scenario == "cancel-race" {
					results <- store.CancelConfirmed(ctx, value.ID, actor, value.Options[0].ID)
				} else {
					results <- store.Reschedule(ctx, value.ID, other, command)
				}
			}()
			close(gate)
			success := 0
			for range 2 {
				err := <-results
				if err == nil {
					success++
				} else if !errors.Is(err, coordinationrequest.ErrBookingConflict) && !errors.Is(err, coordinationrequest.ErrRescheduleRepeated) && !errors.Is(err, coordinationrequest.ErrRescheduleInvalid) && !errors.Is(err, coordinationrequest.ErrCancellationInvalid) {
					t.Fatal(err)
				}
			}
			if success != 1 {
				t.Fatalf("success=%d", success)
			}
			got, err = store.GetForUser(ctx, value.ID, actor)
			if err != nil {
				t.Fatal(err)
			}
			if scenario == "cancel-race" {
				if !((got.Status == coordinationrequest.Cancelled && got.AcceptedOptionID == value.Options[0].ID) || (got.Status == coordinationrequest.Accepted && got.AcceptedOptionID == command.ProposalID)) {
					t.Fatal("mixed cancellation/swap")
				}
				return
			}
			if got.AcceptedOptionID != command.ProposalID || got.RescheduleProposal.Status != "accepted" {
				t.Fatal("not swapped")
			}
			if err := store.Reschedule(ctx, value.ID, other, command); !errors.Is(err, coordinationrequest.ErrRescheduleRepeated) {
				t.Fatal(err)
			}
			for _, query := range []string{"SELECT count(*) FROM notifications WHERE request_id=$1", "SELECT count(*) FROM audit_logs WHERE resource_id=$1"} {
				var count int
				if err := db.QueryRowContext(ctx, query, value.ID).Scan(&count); err != nil || count != 2 {
					t.Fatal("duplicated effect", count, err)
				}
			}
			reuse := fixture("reuse-"+scenario, "carol", "bob", start)
			if err := store.Create(ctx, reuse); err != nil {
				t.Fatal(err)
			}
			if err := store.Respond(ctx, reuse.ID, "bob", coordinationrequest.Accepted, reuse.Options[0].ID); err != nil {
				t.Fatal("old slot not released", err)
			}
		})
	}
}
