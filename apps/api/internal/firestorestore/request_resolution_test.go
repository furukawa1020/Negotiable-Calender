package firestorestore

import (
	"errors"
	"github.com/negotiable-calendar/negotiable-calendar/apps/api/internal/notification"
	coord "github.com/negotiable-calendar/negotiable-calendar/apps/api/internal/request"
	"testing"
	"time"
)

func TestAtomicResolutionReplay(t *testing.T) {
	for _, state := range []coord.Status{coord.Async, coord.Declined, coord.Cancelled} {
		t.Run(string(state), func(t *testing.T) {
			b, ctx := emulatorBackend(t)
			now := time.Now().UTC().Truncate(time.Microsecond)
			value := confirmationRequest("resolution", "alice", "bob", now, now.Add(time.Hour))
			for _, user := range []string{"alice", "bob"} {
				putDocument(t, ctx, b.Client.Collection("organizations").Doc(value.OrganizationID).Collection("members").Doc(user), map[string]any{"Role": "MANAGER"})
			}
			store := b.Request()
			if err := store.Create(ctx, value); err != nil {
				t.Fatal(err)
			}
			actor := "bob"
			cmd := coord.ResolutionCommand{Status: state}
			if state == coord.Async {
				cmd.Message = "private answer"
			}
			if state == coord.Cancelled {
				actor = "alice"
			}
			type result struct {
				replay bool
				err    error
			}
			gate, results := make(chan struct{}), make(chan result, 2)
			for range 2 {
				go func() {
					<-gate
					r, e := store.ResolveRequest(ctx, value.ID, actor, value.OrganizationID, cmd)
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
				t.Fatal("duplicate state changes", first, replays)
			}
			got, err := store.GetForUser(ctx, value.ID, "alice")
			if err != nil || got.Status != state || got.AsyncMessage != cmd.Message {
				t.Fatal("wrong persisted result", err)
			}
			note, event := coord.ResolutionEffects(got, actor)
			noteRef := b.Client.Collection("users").Doc(note.UserID).Collection("notifications").Doc(note.ID)
			noteDoc, err := noteRef.Get(ctx)
			if err != nil {
				t.Fatal(err)
			}
			var saved notification.Notification
			if err := noteDoc.DataTo(&saved); err != nil {
				t.Fatal(err)
			}
			if saved.UserID == actor || saved.Message == cmd.Message {
				t.Fatal("wrong notification")
			}
			if _, err := b.Client.Collection("organizations").Doc(event.OrganizationID).Collection("auditLogs").Doc(event.ID).Get(ctx); err != nil {
				t.Fatal(err)
			}
			saved.ReadAt = &now
			putDocument(t, ctx, noteRef, saved)
			// Replays preserve read state and are possible after the deadline.
			got.DeadlineAt = now.Add(-time.Hour)
			putDocument(t, ctx, b.Client.Collection("coordinationRequests").Doc(value.ID), got)
			if r, e := store.ResolveRequest(ctx, value.ID, actor, value.OrganizationID, cmd); e != nil || !r {
				t.Fatal("lost replay", e)
			}
			doc, _ := noteRef.Get(ctx)
			if err := doc.DataTo(&saved); err != nil || saved.ReadAt == nil {
				t.Fatal("read state reset", err)
			}
			if _, e := store.ResolveRequest(ctx, value.ID, "outsider", value.OrganizationID, cmd); !errors.Is(e, coord.ErrNotFound) {
				t.Fatal("outsider access", e)
			}
			if _, e := store.ResolveRequest(ctx, value.ID, actor, "other", cmd); !errors.Is(e, coord.ErrNotFound) {
				t.Fatal("organization leak", e)
			}
			if state == coord.Async {
				changed := cmd
				changed.Message = "changed"
				if _, e := store.ResolveRequest(ctx, value.ID, actor, value.OrganizationID, changed); !errors.Is(e, coord.ErrResolutionConflict) {
					t.Fatal("answer overwritten", e)
				}
			}
			putDocument(t, ctx, b.accountDeletionRef("alice"), accountDeletion{Phase: "deleting", StartedAt: now})
			if _, e := store.ResolveRequest(ctx, value.ID, actor, value.OrganizationID, cmd); e == nil {
				t.Fatal("deletion replay bypass")
			}
		})
	}
}

func TestResolutionRollbackAndFences(t *testing.T) {
	for _, scenario := range []string{"notification", "audit", "membership", "deletion", "expired"} {
		t.Run(scenario, func(t *testing.T) {
			b, ctx := emulatorBackend(t)
			now := time.Now().UTC().Truncate(time.Microsecond)
			value := confirmationRequest("rollback", "alice", "bob", now, now.Add(time.Hour))
			for _, user := range []string{"alice", "bob"} {
				if scenario == "membership" && user == "bob" {
					continue
				}
				putDocument(t, ctx, b.Client.Collection("organizations").Doc(value.OrganizationID).Collection("members").Doc(user), map[string]any{"Role": "MANAGER"})
			}
			if err := b.Request().Create(ctx, value); err != nil {
				t.Fatal(err)
			}
			if scenario == "expired" {
				value.DeadlineAt = now
				putDocument(t, ctx, b.Client.Collection("coordinationRequests").Doc(value.ID), value)
			}
			if scenario == "deletion" {
				putDocument(t, ctx, b.accountDeletionRef("bob"), accountDeletion{Phase: "deleting", StartedAt: now})
			}
			resolved := value
			resolved.Status = coord.Async
			resolved.UpdatedAt = now
			note, event := coord.ResolutionEffects(resolved, "bob")
			noteRef := b.Client.Collection("users").Doc(note.UserID).Collection("notifications").Doc(note.ID)
			eventRef := b.Client.Collection("organizations").Doc(event.OrganizationID).Collection("auditLogs").Doc(event.ID)
			if scenario == "notification" {
				note.ReadAt = &now
				putDocument(t, ctx, noteRef, note)
			}
			if scenario == "audit" {
				putDocument(t, ctx, eventRef, event)
			}
			if _, err := b.Request().ResolveRequest(ctx, value.ID, "bob", value.OrganizationID, coord.ResolutionCommand{Status: coord.Async, Message: "answer"}); err == nil {
				t.Fatal("invalid transition committed")
			}
			got, err := b.Request().GetForUser(ctx, value.ID, "alice")
			if err != nil || got.Status != coord.Suggested || got.AsyncMessage != "" {
				t.Fatal("partial state", err)
			}
			if scenario != "notification" {
				if _, e := noteRef.Get(ctx); !firestoreNotFound(e) {
					t.Fatal("partial notification", e)
				}
			}
			if scenario != "audit" {
				if _, e := eventRef.Get(ctx); !firestoreNotFound(e) {
					t.Fatal("partial audit", e)
				}
			}
		})
	}
}

func TestResolutionRacesWithConfirmation(t *testing.T) {
	b, ctx := emulatorBackend(t)
	now := time.Now().UTC()
	value := confirmationRequest("race-resolution", "alice", "bob", now, now.Add(time.Hour))
	for _, user := range []string{"alice", "bob"} {
		putDocument(t, ctx, b.Client.Collection("organizations").Doc(value.OrganizationID).Collection("members").Doc(user), map[string]any{"Role": "MANAGER"})
	}
	p := publicationFixtures(now, "available", 1)[0]
	p.UserID = "bob"
	p.EndAt = now.Add(4 * time.Hour)
	putDocument(t, ctx, b.Client.Collection("users").Doc("bob").Collection("scheduleProjections").Doc(p.ID), p)
	available, err := b.Projection().ListForUser(ctx, "bob")
	if err != nil {
		t.Fatal(err)
	}
	if err := coord.ValidateMeetingAvailability("bob", value.Options[0], available, now); err != nil {
		t.Fatal("invalid race fixture", err)
	}
	if err := b.Request().Create(ctx, value); err != nil {
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
			_, err := b.Request().ResolveRequest(ctx, value.ID, actor, value.OrganizationID, cmd)
			results <- err
		}(cmd)
	}
	go func() {
		<-gate
		results <- b.Request().Respond(ctx, value.ID, "bob", coord.Accepted, value.Options[0].ID)
	}()
	close(gate)
	winners := 0
	for range 4 {
		err := <-results
		if err == nil {
			winners++
		} else if !errors.Is(err, coord.ErrResolutionConflict) && !errors.Is(err, coord.ErrNotFound) && !errors.Is(err, coord.ErrBookingConflict) {
			t.Fatal("unexpected race result", err)
		}
	}
	if winners != 1 {
		t.Fatal("multiple terminal transitions", winners)
	}
	got, err := b.Request().GetForUser(ctx, value.ID, "alice")
	if err != nil || got.Status == coord.Suggested {
		t.Fatal("missing final state", err)
	}
	for _, collection := range []string{"notifications", "auditLogs"} {
		var count int
		if collection == "notifications" {
			for _, user := range []string{"alice", "bob"} {
				docs, e := b.Client.Collection("users").Doc(user).Collection(collection).Documents(ctx).GetAll()
				if e != nil {
					t.Fatal(e)
				}
				count += len(docs)
			}
		} else {
			docs, e := b.Client.Collection("organizations").Doc(value.OrganizationID).Collection(collection).Documents(ctx).GetAll()
			if e != nil {
				t.Fatal(e)
			}
			count = len(docs)
		}
		if count != 1 {
			t.Fatal("duplicate/missing side effects", collection, count)
		}
	}
}
