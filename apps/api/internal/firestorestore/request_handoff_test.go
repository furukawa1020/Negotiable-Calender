package firestorestore

import (
	"errors"
	"github.com/negotiable-calendar/negotiable-calendar/apps/api/internal/notification"
	"github.com/negotiable-calendar/negotiable-calendar/apps/api/internal/organization"
	"github.com/negotiable-calendar/negotiable-calendar/apps/api/internal/projection"
	coord "github.com/negotiable-calendar/negotiable-calendar/apps/api/internal/request"
	"testing"
	"time"
)

func TestHandoffDeliversAndRevokesOriginalAccess(t *testing.T) {
	b, ctx := emulatorBackend(t)
	now := time.Now().UTC().Truncate(time.Microsecond)
	value := confirmationRequest("handoff", "alice", "bob", now, now.Add(time.Hour))
	for _, u := range []string{"alice", "bob", "carol", "dave"} {
		putDocument(t, ctx, b.Client.Collection("organizations").Doc("org").Collection("members").Doc(u), membershipRecord{UserID: u, Role: organization.Manager})
	}
	store := b.Request()
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
	if e != nil || got.TargetUserID != "carol" || got.DelegatedFromUserID != "bob" || got.Status != coord.Suggested || len(got.Options) != 1 || got.Options[0].ID == value.Options[0].ID {
		t.Fatal("lost routing", e)
	}
	if _, e := store.GetForUser(ctx, value.ID, "bob"); !errors.Is(e, coord.ErrNotFound) {
		t.Fatal("old reader retained access", e)
	}
	for _, u := range []string{"bob", "carol"} {
		rows, e := store.ListForTarget(ctx, u)
		expected := 0
		if u == "carol" {
			expected = 1
		}
		if e != nil || len(rows) != expected {
			t.Fatal("wrong inbox", u, e)
		}
	}
	if current, e := store.LookupCreation(ctx, value); e != nil || current.TargetUserID != "carol" {
		t.Fatal("creation replay lost", e)
	}
	notes, event := coord.HandoffEffects(got, "bob")
	for _, note := range notes {
		doc, e := b.Client.Collection("users").Doc(note.UserID).Collection("notifications").Doc(note.ID).Get(ctx)
		if e != nil {
			t.Fatal(e)
		}
		var n notification.Notification
		if e := doc.DataTo(&n); e != nil || n.UserID != note.UserID {
			t.Fatal("wrong note", e)
		}
	}
	if _, e := b.Client.Collection("organizations").Doc("org").Collection("auditLogs").Doc(event.ID).Get(ctx); e != nil {
		t.Fatal(e)
	}
	note := notes[0]
	note.ReadAt = &now
	ref := b.Client.Collection("users").Doc("carol").Collection("notifications").Doc(note.ID)
	putDocument(t, ctx, ref, note)
	if _, e := store.ResolveRequest(ctx, value.ID, "bob", "org", coord.ResolutionCommand{Status: coord.Async, Message: "old"}); !errors.Is(e, coord.ErrNotFound) {
		t.Fatal("old writer retained access", e)
	}
	if _, e := store.ResolveRequest(ctx, value.ID, "carol", "org", coord.ResolutionCommand{Status: coord.Async, Message: "private new answer"}); e != nil {
		t.Fatal("new owner cannot answer", e)
	}
	if snapshot, r, e := store.InspectHandoff(ctx, value.ID, "bob", "org", "carol"); e != nil || !r || snapshot.ID != "" || snapshot.AsyncMessage != "" {
		t.Fatal("replay leaked response", e)
	}
	if r, e := store.Handoff(ctx, value.ID, "bob", "org", "carol", nil); e != nil || !r {
		t.Fatal("replay failed", e)
	}
	doc, _ := ref.Get(ctx)
	var saved notification.Notification
	if e := doc.DataTo(&saved); e != nil || saved.ReadAt == nil {
		t.Fatal("read state lost", e)
	}
	if _, e := store.Handoff(ctx, value.ID, "carol", "org", "dave", []coord.Option{option}); !errors.Is(e, coord.ErrHandoffConflict) {
		t.Fatal("repeat delegation", e)
	}
	if _, e := b.Client.Collection("organizations").Doc("org").Collection("members").Doc("carol").Delete(ctx); e != nil {
		t.Fatal(e)
	}
	if _, r, e := store.InspectHandoff(ctx, value.ID, "bob", "org", "carol"); e == nil || r {
		t.Fatal("lost membership replay")
	}
}

func TestHandoffRollbackAndDeletion(t *testing.T) {
	for _, scenario := range []string{"recipient-note", "requester-note", "audit", "membership", "deleting"} {
		t.Run(scenario, func(t *testing.T) {
			b, ctx := emulatorBackend(t)
			now := time.Now().UTC()
			value := confirmationRequest("handoff-fail", "alice", "bob", now, now.Add(time.Hour))
			for _, u := range []string{"alice", "bob", "carol"} {
				if scenario == "membership" && u == "carol" {
					continue
				}
				putDocument(t, ctx, b.Client.Collection("organizations").Doc("org").Collection("members").Doc(u), membershipRecord{UserID: u, Role: organization.Manager})
			}
			if e := b.Request().Create(ctx, value); e != nil {
				t.Fatal(e)
			}
			option := coord.Option{ID: "fresh", RequestID: value.ID, Type: coord.OptionAsync, ResponseBy: &value.DeadlineAt, CreatedAt: now}
			updated := value
			_ = coord.ApplyHandoff(&updated, "bob", "carol", []coord.Option{option}, now)
			notes, event := coord.HandoffEffects(updated, "bob")
			if scenario == "deleting" {
				putDocument(t, ctx, b.accountDeletionRef("carol"), accountDeletion{Phase: "deleting", StartedAt: now})
			}
			for i, n := range notes {
				if (i == 0 && scenario == "recipient-note") || (i == 1 && scenario == "requester-note") {
					putDocument(t, ctx, b.Client.Collection("users").Doc(n.UserID).Collection("notifications").Doc(n.ID), n)
				}
			}
			if scenario == "audit" {
				putDocument(t, ctx, b.Client.Collection("organizations").Doc("org").Collection("auditLogs").Doc(event.ID), event)
			}
			if _, e := b.Request().Handoff(ctx, value.ID, "bob", "org", "carol", []coord.Option{option}); e == nil {
				t.Fatal("invalid commit")
			}
			got, e := b.Request().GetForUser(ctx, value.ID, "bob")
			if e != nil || got.TargetUserID != "bob" || got.DelegatedFromUserID != "" || got.Options[0].ID != value.Options[0].ID {
				t.Fatal("partial handoff", e)
			}
			for i, n := range notes {
				seeded := (i == 0 && scenario == "recipient-note") || (i == 1 && scenario == "requester-note")
				if _, e := b.Client.Collection("users").Doc(n.UserID).Collection("notifications").Doc(n.ID).Get(ctx); !seeded && !firestoreNotFound(e) {
					t.Fatal("partial notification", e)
				}
			}
			if _, e := b.Client.Collection("organizations").Doc("org").Collection("auditLogs").Doc(event.ID).Get(ctx); scenario != "audit" && !firestoreNotFound(e) {
				t.Fatal("partial audit", e)
			}
		})
	}
}

func TestDeletionOfOriginalHandoffOwnerRemovesLinkedData(t *testing.T) {
	b, ctx := emulatorBackend(t)
	now := seedDeletionAccount(t, b, ctx)
	for _, u := range []string{"bob", "carol"} {
		putDocument(t, ctx, b.Client.Collection("users").Doc(u), userRecord{ID: u, Timezone: "UTC"})
		putDocument(t, ctx, b.Client.Collection("organizations").Doc("org").Collection("members").Doc(u), membershipRecord{UserID: u, Role: organization.Owner})
	}
	value := confirmationRequest("handed-off", "bob", "alice", now, now.Add(time.Hour))
	if e := b.Request().Create(ctx, value); e != nil {
		t.Fatal(e)
	}
	options := []coord.Option{{ID: "fresh", RequestID: value.ID, Type: coord.OptionAsync, ResponseBy: &value.DeadlineAt, CreatedAt: now}}
	if _, e := b.Request().Handoff(ctx, value.ID, "alice", "org", "carol", options); e != nil {
		t.Fatal(e)
	}
	if e := b.Auth().DeleteAccount(ctx, "alice"); e != nil {
		t.Fatal(e)
	}
	if _, e := b.Client.Collection("coordinationRequests").Doc(value.ID).Get(ctx); !firestoreNotFound(e) {
		t.Fatal("former owner PII retained", e)
	}
	for _, u := range []string{"bob", "carol"} {
		notes, e := b.Client.Collection("users").Doc(u).Collection("notifications").Where("RequestID", "==", value.ID).Documents(ctx).GetAll()
		if e != nil || len(notes) != 0 {
			t.Fatal("linked note retained", e)
		}
	}
	audits, e := b.Client.Collection("organizations").Doc("org").Collection("auditLogs").Where("ResourceID", "==", value.ID).Documents(ctx).GetAll()
	if e != nil || len(audits) != 0 {
		t.Fatal("linked audit retained", e)
	}
}

func TestHandoffRacesWithAnswerAndConfirmation(t *testing.T) {
	b, ctx := emulatorBackend(t)
	now := time.Now().UTC()
	value := confirmationRequest("handoff-race", "alice", "bob", now, now.Add(time.Hour))
	for _, u := range []string{"alice", "bob", "carol"} {
		putDocument(t, ctx, b.Client.Collection("organizations").Doc("org").Collection("members").Doc(u), membershipRecord{UserID: u, Role: organization.Manager})
	}
	p := publicationFixtures(now, "available", 1)[0]
	p.UserID, p.EndAt = "bob", now.Add(4*time.Hour)
	putDocument(t, ctx, b.Client.Collection("users").Doc("bob").Collection("scheduleProjections").Doc(p.ID), p)
	if err := coord.ValidateMeetingAvailability("bob", value.Options[0], []projection.ScheduleProjection{p}, now); err != nil {
		t.Fatal("invalid race fixture", err)
	}
	if err := b.Request().Create(ctx, value); err != nil {
		t.Fatal(err)
	}
	options := []coord.Option{{ID: "fresh", RequestID: value.ID, Type: coord.OptionAsync, ResponseBy: &value.DeadlineAt, CreatedAt: now}}
	gate, results := make(chan struct{}), make(chan error, 3)
	go func() {
		<-gate
		_, e := b.Request().Handoff(ctx, value.ID, "bob", "org", "carol", options)
		results <- e
	}()
	go func() {
		<-gate
		_, e := b.Request().ResolveRequest(ctx, value.ID, "bob", "org", coord.ResolutionCommand{Status: coord.Async, Message: "answer"})
		results <- e
	}()
	go func() {
		<-gate
		results <- b.Request().Respond(ctx, value.ID, "bob", coord.Accepted, value.Options[0].ID)
	}()
	close(gate)
	winners := 0
	for range 3 {
		err := <-results
		if err == nil {
			winners++
		} else if !errors.Is(err, coord.ErrHandoffConflict) && !errors.Is(err, coord.ErrResolutionConflict) && !errors.Is(err, coord.ErrNotFound) && !errors.Is(err, coord.ErrBookingConflict) {
			t.Fatal(err)
		}
	}
	if winners != 1 {
		t.Fatal("multiple transitions committed", winners)
	}
	got, err := b.Request().GetForUser(ctx, value.ID, "alice")
	if err != nil {
		t.Fatal(err)
	}
	expectedNotes := 1
	if got.TargetUserID == "carol" {
		expectedNotes = 2
		if got.Status != coord.Suggested || got.AsyncMessage != "" || got.AcceptedOptionID != "" {
			t.Fatal("mixed handoff state")
		}
	} else if got.Status != coord.Async && got.Status != coord.Accepted {
		t.Fatal("missing winner")
	}
	count := 0
	for _, u := range []string{"alice", "bob", "carol"} {
		docs, e := b.Client.Collection("users").Doc(u).Collection("notifications").Documents(ctx).GetAll()
		if e != nil {
			t.Fatal(e)
		}
		count += len(docs)
	}
	audits, err := b.Client.Collection("organizations").Doc("org").Collection("auditLogs").Documents(ctx).GetAll()
	if err != nil || len(audits) != 1 || count != expectedNotes {
		t.Fatal("partial or duplicate effects", err, count)
	}
}

func TestNewHandoffOwnerCanConfirmMeeting(t *testing.T) {
	b, ctx := emulatorBackend(t)
	now := time.Now().UTC()
	value := confirmationRequest("handoff-meeting", "alice", "bob", now, now.Add(time.Hour))
	for _, u := range []string{"alice", "bob", "carol"} {
		putDocument(t, ctx, b.Client.Collection("organizations").Doc("org").Collection("members").Doc(u), membershipRecord{UserID: u, Role: organization.Manager})
	}
	p := publicationFixtures(now, "available", 1)[0]
	p.UserID, p.EndAt = "carol", now.Add(4*time.Hour)
	putDocument(t, ctx, b.Client.Collection("users").Doc("carol").Collection("scheduleProjections").Doc(p.ID), p)
	if e := b.Request().Create(ctx, value); e != nil {
		t.Fatal(e)
	}
	if _, e := b.Request().Handoff(ctx, value.ID, "bob", "org", "carol", value.Options); e != nil {
		t.Fatal(e)
	}
	got, e := b.Request().GetForUser(ctx, value.ID, "carol")
	if e != nil {
		t.Fatal(e)
	}
	if e := b.Request().Respond(ctx, value.ID, "bob", coord.Accepted, value.Options[0].ID); e == nil {
		t.Fatal("old owner accepted")
	}
	if e := b.Request().Respond(ctx, value.ID, "carol", coord.Accepted, value.Options[0].ID); e == nil {
		t.Fatal("stale option accepted")
	}
	if e := b.Request().Respond(ctx, value.ID, "carol", coord.Accepted, got.Options[0].ID); e != nil {
		t.Fatal("new owner cannot confirm", e)
	}
	got, e = b.Request().GetForUser(ctx, value.ID, "alice")
	if e != nil || got.Status != coord.Accepted || got.TargetUserID != "carol" {
		t.Fatal("confirmation lost", e)
	}
}
