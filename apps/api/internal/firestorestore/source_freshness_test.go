package firestorestore

import (
	"errors"
	"testing"
	"time"

	calendarintegration "github.com/negotiable-calendar/negotiable-calendar/apps/api/internal/calendar"
	"github.com/negotiable-calendar/negotiable-calendar/apps/api/internal/projection"
	coordinationrequest "github.com/negotiable-calendar/negotiable-calendar/apps/api/internal/request"
)

func TestCalendarSourceFreshnessGuardsPublicationAndConfirmation(t *testing.T) {
	for _, scenario := range []string{"fresh", "stale", "future", "unknown", "syncing", "failed", "uncommitted", "reconnect", "disconnect", "outside", "generation-changed", "reschedule-stale"} {
		t.Run(scenario, func(t *testing.T) {
			b, ctx := emulatorBackend(t)
			now := time.Now().UTC().Truncate(time.Second)
			observed := now.Add(-20 * time.Minute)
			from, to := now.Add(-time.Hour), now.Add(24*time.Hour)
			calendar := b.Calendar()
			if err := calendar.SaveConnection(ctx, calendarintegration.Connection{UserID: "alice", ConnectedAt: observed.Add(-time.Minute)}); err != nil {
				t.Fatal(err)
			}
			owner, err := calendar.AcquireSync(ctx, "alice", now, time.Minute)
			if err != nil {
				t.Fatal(err)
			}
			active := calendarintegration.WithSyncLease(ctx, calendarintegration.SyncLease{UserID: "alice", ID: owner.SyncLeaseID})
			if err := calendar.ApplyChanges(active, "alice", calendarintegration.ChangeSet{Full: true}, from, to, observed); err != nil {
				t.Fatal(err)
			}
			p := publicationFixtures(now, "source", 1)[0]
			p.EndAt = now.Add(4 * time.Hour)
			if err := b.Projection().Replace(active, "alice", from, to, []projection.ScheduleProjection{p}); err != nil {
				t.Fatal(err)
			}
			requirePublicationHidden(t, b, ctx) // Publication alone cannot commit a sync.
			if err := calendar.MarkSyncSuccess(active, "alice", "cursor", observed, now.Add(time.Minute)); err != nil {
				t.Fatal(err)
			}
			paused, err := calendar.BeginRebuild(ctx, "alice")
			if err != nil {
				t.Fatal(err)
			}
			if err := b.Projection().Replace(paused, "alice", from, to, []projection.ScheduleProjection{p}); err != nil {
				t.Fatal(err)
			}
			rows, err := b.Projection().ListForUser(ctx, "alice")
			if err != nil || len(rows) != 1 || !rows[0].ExpiresAt.Equal(observed.Add(calendarintegration.SourceMaxAge)) {
				t.Fatal("rebuild renewed source freshness", rows, err)
			}
			value := confirmationRequest("request", "bob", "alice", now, now.Add(time.Hour))
			if err := b.Request().Create(ctx, value); err != nil {
				t.Fatal(err)
			}
			var reschedule coordinationrequest.RescheduleCommand
			if scenario == "reschedule-stale" {
				if err := b.Request().Respond(ctx, value.ID, "alice", coordinationrequest.Accepted, value.Options[0].ID); err != nil {
					t.Fatal(err)
				}
				reschedule = coordinationrequest.RescheduleCommand{Action: "propose", ProposalID: "new-time", ExpectedOptionID: value.Options[0].ID, StartAt: now.Add(2 * time.Hour)}
				if err := b.Request().Reschedule(ctx, value.ID, "bob", reschedule); err != nil {
					t.Fatal(err)
				}
			}
			input, err := decodePrivateInputs(b.privateInputsRef("alice").Get(ctx))
			if err != nil {
				t.Fatal(err)
			}
			connection, err := calendar.GetConnection(ctx, "alice")
			if err != nil {
				t.Fatal(err)
			}
			switch scenario {
			case "stale", "reschedule-stale":
				old := now.Add(-calendarintegration.SourceMaxAge - time.Second)
				input.Source.ObservedAt = old
				connection.LastSyncedAt = &old
				putDocument(t, ctx, b.privateInputsRef("alice"), input)
				putDocument(t, ctx, b.Client.Collection("calendarConnections").Doc("alice"), connection)
			case "future":
				future := now.Add(time.Minute)
				input.Source.ObservedAt = future
				connection.LastSyncedAt = &future
				putDocument(t, ctx, b.privateInputsRef("alice"), input)
				putDocument(t, ctx, b.Client.Collection("calendarConnections").Doc("alice"), connection)
			case "unknown":
				input.Source = nil
				putDocument(t, ctx, b.privateInputsRef("alice"), input)
			case "syncing":
				if _, err := calendar.AcquireSync(ctx, "alice", now, time.Minute); err != nil {
					t.Fatal(err)
				}
			case "failed":
				if err := calendar.MarkSyncFailure(ctx, "alice", "timeout", now.Add(time.Minute), false); err != nil {
					t.Fatal(err)
				}
			case "uncommitted":
				input.Source.ObservedAt = now
				putDocument(t, ctx, b.privateInputsRef("alice"), input)
			case "reconnect":
				if err := calendar.SaveConnection(ctx, calendarintegration.Connection{UserID: "alice", ConnectedAt: now}); err != nil {
					t.Fatal(err)
				}
			case "disconnect":
				if err := calendar.DeleteConnection(ctx, "alice"); err != nil {
					t.Fatal(err)
				}
			case "outside":
				input.Source.To = now.Add(30 * time.Minute)
				putDocument(t, ctx, b.privateInputsRef("alice"), input)
			case "generation-changed":
				owner, err := calendar.AcquireSync(ctx, "alice", now, time.Minute)
				if err != nil {
					t.Fatal(err)
				}
				active = calendarintegration.WithSyncLease(ctx, calendarintegration.SyncLease{UserID: "alice", ID: owner.SyncLeaseID})
				if err := calendar.ApplyChanges(active, "alice", calendarintegration.ChangeSet{Full: true}, from, to, now); err != nil {
					t.Fatal(err)
				}
				if err := b.Projection().Replace(paused, "alice", from, to, []projection.ScheduleProjection{p}); err == nil {
					t.Fatal("old source generation published")
				}
			}
			if scenario == "fresh" {
				if err := b.Request().Respond(ctx, value.ID, "alice", coordinationrequest.Accepted, value.Options[0].ID); err != nil {
					t.Fatal(err)
				}
				return
			}
			requirePublicationHidden(t, b, ctx)
			if scenario == "reschedule-stale" {
				reschedule.Action = "accept"
				if err := b.Request().Reschedule(ctx, value.ID, "alice", reschedule); !errors.Is(err, coordinationrequest.ErrAvailabilityChanged) {
					t.Fatal("stale reschedule", err)
				}
				got, err := b.Request().GetForUser(ctx, value.ID, "alice")
				if err != nil || got.AcceptedOptionID != value.Options[0].ID || got.RescheduleProposal.Status != "proposed" {
					t.Fatal("old reservation lost", err)
				}
			} else if err := b.Request().Respond(ctx, value.ID, "alice", coordinationrequest.Accepted, value.Options[0].ID); !errors.Is(err, coordinationrequest.ErrAvailabilityChanged) {
				t.Fatal("stale confirmation", err)
			}
			if scenario != "outside" {
				if _, err := calendar.BeginRebuild(ctx, "alice"); err == nil {
					t.Fatal("policy rebuild laundered source state")
				}
			}
		})
	}
}
