package firestorestore

import (
	"errors"
	"fmt"
	"reflect"
	"testing"
	"time"

	cal "github.com/negotiable-calendar/negotiable-calendar/apps/api/internal/calendar"
	"github.com/negotiable-calendar/negotiable-calendar/apps/api/internal/privateevent"
	coord "github.com/negotiable-calendar/negotiable-calendar/apps/api/internal/request"
)

func TestRequesterCalendarGuardsConfirmationAndReschedule(t *testing.T) {
	for _, scenario := range []string{"busy", "free", "adjacent", "empty", "stale", "syncing", "failed", "unknown", "outside", "incomplete", "disconnect", "blocked", "foreign", "invalid-time", "invalid-status", "limit", "overflow", "reschedule-busy", "reschedule-stale"} {
		t.Run(scenario, func(t *testing.T) {
			b, ctx := emulatorBackend(t)
			now := time.Now().UTC().Truncate(time.Microsecond)
			v := confirmationRequest("requester-calendar", "alice", "bob", now, now.Add(time.Hour))
			store := b.Request()
			putDocument(t, ctx, b.Client.Collection("users").Doc("alice"), userRecord{ID: "alice"})
			if err := store.Create(ctx, v); err != nil {
				t.Fatal(err)
			}
			for _, user := range []string{"alice", "bob"} {
				putDocument(t, ctx, b.Client.Collection("organizations").Doc(v.OrganizationID).Collection("members").Doc(user), map[string]any{"Role": "MANAGER"})
			}
			p := publicationFixtures(now, "target-open", 1)[0]
			p.UserID, p.EndAt = "bob", now.Add(4*time.Hour)
			putDocument(t, ctx, b.Client.Collection("users").Doc("bob").Collection("scheduleProjections").Doc(p.ID), p)
			rescheduling := scenario == "reschedule-busy" || scenario == "reschedule-stale"
			command := coord.RescheduleCommand{Action: "propose", ProposalID: "requester-proposal", ExpectedOptionID: v.Options[0].ID, StartAt: now.Add(2 * time.Hour)}
			start := *v.Options[0].StartAt
			if rescheduling {
				if err := store.ConfirmMeeting(ctx, v.ID, "bob", v.OrganizationID, v.Options[0].ID); err != nil {
					t.Fatal(err)
				}
				if err := store.RescheduleInOrganization(ctx, v.ID, "alice", v.OrganizationID, command); err != nil {
					t.Fatal(err)
				}
				start = command.StartAt
			}
			observed := now.Add(-time.Minute)
			snapshot := cal.SourceSnapshot{Revision: "requester-revision", ObservedAt: observed, From: now.Add(-time.Hour), To: now.Add(24 * time.Hour)}
			connection := cal.Connection{UserID: "alice", LastSyncedAt: &observed}
			inputs := privateInputsControl{ID: snapshot.Revision, Ready: true, Source: &snapshot}
			event := privateEventRecord{UserID: "alice", StartAt: start, EndAt: start.Add(30 * time.Minute), BusyStatus: privateevent.Free}
			allowed := scenario == "free" || scenario == "adjacent" || scenario == "empty" || scenario == "limit"
			switch scenario {
			case "busy", "reschedule-busy":
				event.BusyStatus = privateevent.Busy
			case "free":
				event.BusyStatus = privateevent.Free
			case "adjacent":
				event.EndAt, event.StartAt = start, start.Add(-time.Hour)
				event.BusyStatus = privateevent.Busy
			case "stale", "reschedule-stale":
				snapshot.ObservedAt = now.Add(-cal.SourceMaxAge)
				connection.LastSyncedAt = &snapshot.ObservedAt
			case "syncing":
				connection.SyncLeaseID = "in-flight"
			case "failed":
				connection.LastErrorCode = "timeout"
			case "unknown":
				inputs.Source = nil
			case "outside":
				snapshot.To = start.Add(time.Minute)
			case "incomplete":
				inputs.Ready = false
			case "blocked":
				putDocument(t, ctx, b.projectionBlock("alice"), map[string]any{"Blocked": true})
			case "foreign":
				event.UserID = "carol"
			case "invalid-time":
				event.EndAt = event.StartAt
			case "invalid-status":
				event.BusyStatus = "corrupt"
			}
			putDocument(t, ctx, b.privateInputsRef("alice"), inputs)
			if scenario != "disconnect" {
				putDocument(t, ctx, b.Client.Collection("calendarConnections").Doc("alice"), connection)
			}
			if scenario == "limit" || scenario == "overflow" {
				count := 5000
				if scenario == "overflow" {
					count++
				}
				batch := b.Client.Batch()
				for i := 0; i < count; i++ {
					batch.Set(b.Client.Collection("users").Doc("alice").Collection("privateEvents").Doc(fmt.Sprintf("synthetic-%05d", i)), event)
					if (i+1)%400 == 0 || i == count-1 {
						if _, err := batch.Commit(ctx); err != nil {
							t.Fatal(err)
						}
						batch = b.Client.Batch()
					}
				}
			} else if scenario != "empty" {
				putDocument(t, ctx, b.Client.Collection("users").Doc("alice").Collection("privateEvents").Doc("synthetic"), event)
			}
			candidateSnapshot, candidateErr := store.LoadRequesterCalendar(ctx, "alice")
			wantSource := allowed || scenario == "busy" || scenario == "reschedule-busy" || scenario == "outside"
			if (candidateErr == nil) != wantSource {
				t.Fatal("candidate source result", candidateErr)
			}
			if candidateErr == nil && candidateSnapshot.Allows(start, start.Add(30*time.Minute), time.Now().UTC()) != allowed {
				t.Fatal("candidate source slot predicate")
			}
			before, err := store.GetForUser(ctx, v.ID, "alice")
			if err != nil {
				t.Fatal(err)
			}
			if rescheduling {
				command.Action = "accept"
				err = store.RescheduleInOrganization(ctx, v.ID, "bob", v.OrganizationID, command)
			} else {
				err = store.ConfirmMeeting(ctx, v.ID, "bob", v.OrganizationID, v.Options[0].ID)
			}
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
			notes := 0
			for _, user := range []string{"alice", "bob"} {
				docs, err := b.Client.Collection("users").Doc(user).Collection("notifications").Documents(ctx).GetAll()
				if err != nil {
					t.Fatal(err)
				}
				notes += len(docs)
			}
			audits, err := b.Client.Collection("organizations").Doc(v.OrganizationID).Collection("auditLogs").Documents(ctx).GetAll()
			if err != nil || notes != want || len(audits) != want {
				t.Fatal("incorrect effects", notes, len(audits), err)
			}
			if allowed {
				connection.LastErrorCode = "timeout"
				putDocument(t, ctx, b.Client.Collection("calendarConnections").Doc("alice"), connection)
				if err := store.ConfirmMeeting(ctx, v.ID, "bob", v.OrganizationID, v.Options[0].ID); !errors.Is(err, coord.ErrAlreadyAccepted) {
					t.Fatal("saved confirmation blocked", err)
				}
				if err := store.CancelConfirmedInOrganization(ctx, v.ID, "alice", v.OrganizationID, v.Options[0].ID); err != nil {
					t.Fatal("cancellation blocked", err)
				}
			}
		})
	}
}
