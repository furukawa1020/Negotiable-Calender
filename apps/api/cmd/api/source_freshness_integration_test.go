package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"testing"
	"time"

	calendarintegration "github.com/negotiable-calendar/negotiable-calendar/apps/api/internal/calendar"
	"github.com/negotiable-calendar/negotiable-calendar/apps/api/internal/policy"
	"github.com/negotiable-calendar/negotiable-calendar/apps/api/internal/projection"
	coordinationrequest "github.com/negotiable-calendar/negotiable-calendar/apps/api/internal/request"
)

func testPostgresSourceFreshness(t *testing.T, ctx context.Context, db *sql.DB, requests *coordinationrequest.PostgresStore, fixture func(string, string, string, time.Time) coordinationrequest.CoordinationRequest, now time.Time) {
	for _, scenario := range []string{"fresh", "stale", "future", "unknown", "syncing", "failed", "uncommitted", "reconnect", "disconnect", "outside", "generation-changed", "reschedule-stale"} {
		t.Run("source-"+scenario, func(t *testing.T) {
			calendar := calendarintegration.NewPostgresStore(db)
			projections := projection.NewPostgresStore(db)
			rebuilder := projection.NewPostgresRebuildStore(db)
			observed := now.Add(-20 * time.Minute)
			from, to := now.Add(-time.Hour), now.Add(24*time.Hour)
			if err := calendar.SaveConnection(ctx, calendarintegration.Connection{UserID: "bob", ConnectedAt: observed.Add(-time.Minute), RefreshTokenCipher: []byte("synthetic")}); err != nil {
				t.Fatal(err)
			}
			owner, err := calendar.AcquireSync(ctx, "bob", now, time.Minute)
			if err != nil {
				t.Fatal(err)
			}
			active := calendarintegration.WithSyncLease(ctx, calendarintegration.SyncLease{UserID: "bob", ID: owner.SyncLeaseID})
			if err := calendar.ApplyChanges(active, "bob", calendarintegration.ChangeSet{Full: true}, from, to, observed); err != nil {
				t.Fatal(err)
			}
			p := projection.ScheduleProjection{ID: "freshness", UserID: "bob", StartAt: now.Add(12 * time.Hour), EndAt: now.Add(15 * time.Hour), GeneratedAt: now, ExpiresAt: now.Add(time.Hour), ExpectedResponseBucket: "soon", State: policy.InteractionState{Availability: policy.Available, Requestability: policy.RequestOpen, Interruptibility: policy.InterruptNormal, Reschedulability: policy.RescheduleMedium}}
			build, err := rebuilder.BeginRebuild(active, "bob")
			if err != nil {
				t.Fatal(err)
			}
			if err := projections.Replace(build, "bob", from, to, []projection.ScheduleProjection{p}); err != nil {
				t.Fatal(err)
			}
			hidden := func() {
				t.Helper()
				rows, err := projections.ListForUser(ctx, "bob")
				if err != nil || len(rows) != 0 {
					t.Fatal("source publication not hidden", len(rows), err)
				}
			}
			hidden()
			if err := calendar.MarkSyncSuccess(active, "bob", "cursor", observed, now.Add(time.Minute)); err != nil {
				t.Fatal(err)
			}
			paused, err := rebuilder.BeginRebuild(ctx, "bob")
			if err != nil {
				t.Fatal(err)
			}
			if err := projections.Replace(paused, "bob", from, to, []projection.ScheduleProjection{p}); err != nil {
				t.Fatal(err)
			}
			rows, err := projections.ListForUser(ctx, "bob")
			if err != nil || len(rows) != 1 || !rows[0].ExpiresAt.Equal(observed.Add(calendarintegration.SourceMaxAge)) {
				t.Fatal("rebuild extended freshness", err)
			}
			value := fixture("source-"+scenario, "alice", "bob", now.Add(13*time.Hour))
			if err := requests.Create(ctx, value); err != nil {
				t.Fatal(err)
			}
			defer db.ExecContext(ctx, "DELETE FROM coordination_requests WHERE id=$1", value.ID)
			var command coordinationrequest.RescheduleCommand
			if scenario == "reschedule-stale" {
				if err := requests.Respond(ctx, value.ID, "bob", coordinationrequest.Accepted, value.Options[0].ID); err != nil {
					t.Fatal(err)
				}
				command = coordinationrequest.RescheduleCommand{Action: "propose", ProposalID: "source-proposal", ExpectedOptionID: value.Options[0].ID, StartAt: now.Add(14 * time.Hour)}
				if err := requests.Reschedule(ctx, value.ID, "alice", command); err != nil {
					t.Fatal(err)
				}
			}
			source, err := calendar.LoadSourceSnapshot(ctx, "bob")
			if err != nil {
				t.Fatal(err)
			}
			setSource := func() {
				t.Helper()
				data, err := json.Marshal(source)
				if err != nil {
					t.Fatal(err)
				}
				if _, err := db.ExecContext(ctx, "UPDATE calendar_source_snapshots SET snapshot=$1 WHERE user_id='bob'", data); err != nil {
					t.Fatal(err)
				}
			}
			switch scenario {
			case "stale", "reschedule-stale", "future":
				source.ObservedAt = now.Add(-calendarintegration.SourceMaxAge - time.Second)
				if scenario == "future" {
					source.ObservedAt = now.Add(time.Minute)
				}
				setSource()
				if _, err := db.ExecContext(ctx, "UPDATE calendar_connections SET last_synced_at=$1 WHERE user_id='bob'", source.ObservedAt); err != nil {
					t.Fatal(err)
				}
			case "unknown":
				source = calendarintegration.SourceSnapshot{}
				setSource()
			case "syncing":
				if _, err := calendar.AcquireSync(ctx, "bob", now, time.Minute); err != nil {
					t.Fatal(err)
				}
			case "failed":
				if err := calendar.MarkSyncFailure(ctx, "bob", "timeout", now.Add(time.Minute), false); err != nil {
					t.Fatal(err)
				}
			case "uncommitted":
				source.ObservedAt = now
				setSource()
			case "reconnect":
				if err := calendar.SaveConnection(ctx, calendarintegration.Connection{UserID: "bob", ConnectedAt: now, RefreshTokenCipher: []byte("new-synthetic")}); err != nil {
					t.Fatal(err)
				}
			case "disconnect":
				if err := calendar.DeleteConnection(ctx, "bob"); err != nil {
					t.Fatal(err)
				}
			case "outside":
				source.To = now.Add(13*time.Hour + 15*time.Minute)
				setSource()
			case "generation-changed":
				owner, err := calendar.AcquireSync(ctx, "bob", now, time.Minute)
				if err != nil {
					t.Fatal(err)
				}
				active = calendarintegration.WithSyncLease(ctx, calendarintegration.SyncLease{UserID: "bob", ID: owner.SyncLeaseID})
				if err := calendar.ApplyChanges(active, "bob", calendarintegration.ChangeSet{Full: true}, from, to, now); err != nil {
					t.Fatal(err)
				}
				if err := projections.Replace(paused, "bob", from, to, []projection.ScheduleProjection{p}); !errors.Is(err, calendarintegration.ErrSourceUnavailable) {
					t.Fatal("old generation published", err)
				}
			}
			if scenario == "fresh" {
				if err := requests.Respond(ctx, value.ID, "bob", coordinationrequest.Accepted, value.Options[0].ID); err != nil {
					t.Fatal(err)
				}
				return
			}
			hidden()
			if scenario == "reschedule-stale" {
				command.Action = "accept"
				if err := requests.Reschedule(ctx, value.ID, "bob", command); !errors.Is(err, coordinationrequest.ErrAvailabilityChanged) {
					t.Fatal("stale reschedule", err)
				}
				got, err := requests.GetForUser(ctx, value.ID, "bob")
				if err != nil || got.AcceptedOptionID != value.Options[0].ID || got.RescheduleProposal.Status != "proposed" {
					t.Fatal("old reservation lost", err)
				}
			} else if err := requests.Respond(ctx, value.ID, "bob", coordinationrequest.Accepted, value.Options[0].ID); !errors.Is(err, coordinationrequest.ErrAvailabilityChanged) {
				t.Fatal("stale confirmation", err)
			}
			if scenario != "outside" {
				if _, err := rebuilder.BeginRebuild(ctx, "bob"); !errors.Is(err, calendarintegration.ErrSourceUnavailable) {
					t.Fatal("policy rebuild laundered source", err)
				}
			}
		})
	}
}
