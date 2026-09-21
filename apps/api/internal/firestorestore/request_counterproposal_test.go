package firestorestore

import (
	"errors"
	"testing"
	"time"

	"github.com/negotiable-calendar/negotiable-calendar/apps/api/internal/notification"
	coord "github.com/negotiable-calendar/negotiable-calendar/apps/api/internal/request"
)

func TestCounterproposalAgreementAndReplay(t *testing.T) {
	b, ctx := emulatorBackend(t)
	now := time.Now().UTC().Truncate(time.Microsecond)
	value := confirmationRequest("offer", "alice", "bob", now, now.Add(time.Hour))
	value.DurationMinutes = 30
	for _, u := range []string{"alice", "bob", "carol"} {
		putDocument(t, ctx, b.Client.Collection("organizations").Doc("org").Collection("members").Doc(u), map[string]any{"Role": "MANAGER"})
	}
	p := publicationFixtures(now, "available", 1)[0]
	p.UserID, p.EndAt = "bob", now.Add(4*time.Hour)
	putDocument(t, ctx, b.Client.Collection("users").Doc("bob").Collection("scheduleProjections").Doc(p.ID), p)
	if e := b.Request().Create(ctx, value); e != nil {
		t.Fatal(e)
	}
	start, end := now.Add(2*time.Hour), now.Add(2*time.Hour+time.Duration(value.DurationMinutes)*time.Minute)
	type result struct {
		option coord.Option
		replay bool
		err    error
	}
	gate, results := make(chan struct{}), make(chan result, 2)
	for range 2 {
		go func() {
			<-gate
			o, r, e := b.Request().ProposeMeeting(ctx, value.ID, "bob", "org", start, end)
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
	got, e := b.Request().GetForUser(ctx, value.ID, "alice")
	if e != nil || len(got.Options) != 2 || got.Options[1].ProposedByUserID != "bob" {
		t.Fatal("missing offer", e)
	}
	note, event := coord.ProposalEffects(got, offer)
	note.ReadAt = &now
	noteRef := b.Client.Collection("users").Doc("alice").Collection("notifications").Doc(note.ID)
	if _, e := noteRef.Get(ctx); e != nil {
		t.Fatal("no recipient notification", e)
	}
	if _, e := b.Client.Collection("organizations").Doc("org").Collection("auditLogs").Doc(event.ID).Get(ctx); e != nil {
		t.Fatal(e)
	}
	putDocument(t, ctx, noteRef, note)
	for _, c := range []struct{ actor, org, option string }{{"bob", "org", offer.ID}, {"alice", "org", value.Options[0].ID}, {"carol", "org", offer.ID}, {"alice", "other", offer.ID}} {
		if e := b.Request().ConfirmMeeting(ctx, value.ID, c.actor, c.org, c.option); !errors.Is(e, coord.ErrNotFound) {
			t.Fatal("unauthorized acceptance", c, e)
		}
	}
	confirmResults := make(chan error, 2)
	gate = make(chan struct{})
	for range 2 {
		go func() { <-gate; confirmResults <- b.Request().ConfirmMeeting(ctx, value.ID, "alice", "org", offer.ID) }()
	}
	close(gate)
	confirmed := 0
	for range 2 {
		e := <-confirmResults
		if e == nil {
			confirmed++
		} else if !errors.Is(e, coord.ErrAlreadyAccepted) && !errors.Is(e, coord.ErrBookingConflict) {
			t.Fatal(e)
		}
	}
	if confirmed != 1 {
		t.Fatal("duplicate confirmation")
	}
	if e := b.Request().ConfirmMeeting(ctx, value.ID, "alice", "org", offer.ID); !errors.Is(e, coord.ErrAlreadyAccepted) {
		t.Fatal("confirm replay", e)
	}
	got, e = b.Request().GetForUser(ctx, value.ID, "bob")
	if e != nil || got.Status != coord.Accepted || got.AcceptedOptionID != offer.ID {
		t.Fatal(e)
	}
	bobICS, e := coord.CalendarExport(got)
	if e != nil {
		t.Fatal("target cannot export", e)
	}
	aliceValue, e := b.Request().GetForUser(ctx, value.ID, "alice")
	if e != nil {
		t.Fatal(e)
	}
	aliceICS, e := coord.CalendarExport(aliceValue)
	if e != nil || aliceICS != bobICS {
		t.Fatal("participants export different meetings", e)
	}
	confirmedNote, confirmedAudit := coord.ConfirmationEffectsForActor(got, "alice", now)
	if _, e := b.Client.Collection("users").Doc("bob").Collection("notifications").Doc(confirmedNote.ID).Get(ctx); e != nil {
		t.Fatal(e)
	}
	doc, e := b.Client.Collection("organizations").Doc("org").Collection("auditLogs").Doc(confirmedAudit.ID).Get(ctx)
	if e != nil || doc.Data()["ActorUserID"] != "alice" {
		t.Fatal("wrong actor", e)
	}
	if _, r, e := b.Request().ProposeMeeting(ctx, value.ID, "bob", "org", start, end); e != nil || !r {
		t.Fatal("offer retry after acceptance", e)
	}
	doc, e = noteRef.Get(ctx)
	var saved notification.Notification
	if e != nil || doc.DataTo(&saved) != nil || saved.ReadAt == nil {
		t.Fatal("read state lost")
	}
	if _, _, e := b.Request().ProposeMeeting(ctx, value.ID, "bob", "org", start.Add(time.Hour), end.Add(time.Hour)); !errors.Is(e, coord.ErrProposalConflict) {
		t.Fatal("new offer after close", e)
	}
	if _, e := b.Client.Collection("organizations").Doc("org").Collection("members").Doc("alice").Delete(ctx); e != nil {
		t.Fatal(e)
	}
	if e := b.Request().ConfirmMeeting(ctx, value.ID, "alice", "org", offer.ID); !errors.Is(e, coord.ErrCreationForbidden) {
		t.Fatal("membership replay bypass", e)
	}
}

func TestCounterproposalRollbackAndFences(t *testing.T) {
	for _, scenario := range []string{"note", "audit", "membership", "deletion", "expired"} {
		t.Run(scenario, func(t *testing.T) {
			b, ctx := emulatorBackend(t)
			now := time.Now().UTC().Truncate(time.Microsecond)
			value := confirmationRequest("offer-fail", "alice", "bob", now, now.Add(time.Hour))
			value.DurationMinutes = 30
			for _, u := range []string{"alice", "bob"} {
				if scenario == "membership" && u == "alice" {
					continue
				}
				putDocument(t, ctx, b.Client.Collection("organizations").Doc("org").Collection("members").Doc(u), map[string]any{"Role": "MANAGER"})
			}
			if e := b.Request().Create(ctx, value); e != nil {
				t.Fatal(e)
			}
			start, end := *value.Options[0].StartAt, *value.Options[0].EndAt
			copy := value
			offer, _, e := coord.PrepareProposal(&copy, "bob", "org", start, end, now)
			if e != nil {
				t.Fatal(e)
			}
			note, event := coord.ProposalEffects(copy, offer)
			nref := b.Client.Collection("users").Doc("alice").Collection("notifications").Doc(note.ID)
			aref := b.Client.Collection("organizations").Doc("org").Collection("auditLogs").Doc(event.ID)
			if scenario == "note" {
				putDocument(t, ctx, nref, note)
			}
			if scenario == "audit" {
				putDocument(t, ctx, aref, event)
			}
			if scenario == "deletion" {
				putDocument(t, ctx, b.accountDeletionRef("bob"), accountDeletion{Phase: "deleting", StartedAt: now})
			}
			if scenario == "expired" {
				value.DeadlineAt = now
				putDocument(t, ctx, b.Client.Collection("coordinationRequests").Doc(value.ID), value)
			}
			if _, _, e := b.Request().ProposeMeeting(ctx, value.ID, "bob", "org", start, end); e == nil {
				t.Fatal("invalid proposal")
			}
			got, e := b.Request().GetForUser(ctx, value.ID, "alice")
			if e != nil || len(got.Options) != 1 || !got.UpdatedAt.Equal(value.UpdatedAt) {
				t.Fatal("partial proposal", e)
			}
			if _, e := nref.Get(ctx); scenario != "note" && !firestoreNotFound(e) {
				t.Fatal("partial note", e)
			}
			if _, e := aref.Get(ctx); scenario != "audit" && !firestoreNotFound(e) {
				t.Fatal("partial audit", e)
			}
		})
	}
}

func TestCounterproposalConfirmationRejectsStaleAndRollsBack(t *testing.T) {
	for _, scenario := range []string{"availability", "booking", "note", "audit", "deletion", "membership"} {
		t.Run(scenario, func(t *testing.T) {
			b, ctx := emulatorBackend(t)
			now := time.Now().UTC().Truncate(time.Microsecond)
			value := confirmationRequest("offer-confirm", "alice", "bob", now, now.Add(time.Hour))
			value.DurationMinutes = 30
			for _, u := range []string{"alice", "bob"} {
				putDocument(t, ctx, b.Client.Collection("organizations").Doc("org").Collection("members").Doc(u), map[string]any{"Role": "MANAGER"})
			}
			p := publicationFixtures(now, "available", 1)[0]
			p.UserID, p.EndAt = "bob", now.Add(4*time.Hour)
			if scenario != "availability" {
				putDocument(t, ctx, b.Client.Collection("users").Doc("bob").Collection("scheduleProjections").Doc(p.ID), p)
			}
			if e := b.Request().Create(ctx, value); e != nil {
				t.Fatal(e)
			}
			offer, _, e := b.Request().ProposeMeeting(ctx, value.ID, "bob", "org", *value.Options[0].StartAt, *value.Options[0].EndAt)
			if e != nil {
				t.Fatal(e)
			}
			note, event := coord.ConfirmationEffectsForActor(value, "alice", now)
			nref := b.Client.Collection("users").Doc("bob").Collection("notifications").Doc(note.ID)
			aref := b.Client.Collection("organizations").Doc("org").Collection("auditLogs").Doc(event.ID)
			if scenario == "note" {
				putDocument(t, ctx, nref, note)
			}
			if scenario == "audit" {
				putDocument(t, ctx, aref, event)
			}
			if scenario == "deletion" {
				putDocument(t, ctx, b.accountDeletionRef("alice"), accountDeletion{Phase: "deleting", StartedAt: now})
			}
			if scenario == "membership" {
				if _, e := b.Client.Collection("organizations").Doc("org").Collection("members").Doc("bob").Delete(ctx); e != nil {
					t.Fatal(e)
				}
			}
			if scenario == "booking" {
				other := confirmationRequest("other", "alice", "carol", now, *offer.StartAt)
				other.Status, other.AcceptedOptionID = coord.Accepted, other.Options[0].ID
				putDocument(t, ctx, b.Client.Collection("coordinationRequests").Doc(other.ID), other)
			}
			e = b.Request().ConfirmMeeting(ctx, value.ID, "alice", "org", offer.ID)
			if e == nil {
				t.Fatal("invalid confirmation")
			}
			if scenario == "availability" && !errors.Is(e, coord.ErrAvailabilityChanged) {
				t.Fatal(e)
			}
			if scenario == "booking" && !errors.Is(e, coord.ErrBookingConflict) {
				t.Fatal(e)
			}
			got, e := b.Request().GetForUser(ctx, value.ID, "alice")
			if e != nil || got.Status != coord.Suggested || got.AcceptedOptionID != "" {
				t.Fatal("partial confirmation", e)
			}
			if _, e := nref.Get(ctx); scenario != "note" && !firestoreNotFound(e) {
				t.Fatal("partial note", e)
			}
			if _, e := aref.Get(ctx); scenario != "audit" && !firestoreNotFound(e) {
				t.Fatal("partial audit", e)
			}
		})
	}
}

func TestCounterproposalAgreementRacesHandoffAndAnswer(t *testing.T) {
	b, ctx := emulatorBackend(t)
	now := time.Now().UTC().Truncate(time.Microsecond)
	value := confirmationRequest("offer-race", "alice", "bob", now, now.Add(time.Hour))
	value.DurationMinutes = 30
	for _, u := range []string{"alice", "bob", "carol"} {
		putDocument(t, ctx, b.Client.Collection("organizations").Doc("org").Collection("members").Doc(u), map[string]any{"Role": "MANAGER"})
	}
	p := publicationFixtures(now, "available", 1)[0]
	p.UserID, p.EndAt = "bob", now.Add(4*time.Hour)
	putDocument(t, ctx, b.Client.Collection("users").Doc("bob").Collection("scheduleProjections").Doc(p.ID), p)
	if e := b.Request().Create(ctx, value); e != nil {
		t.Fatal(e)
	}
	offer, _, e := b.Request().ProposeMeeting(ctx, value.ID, "bob", "org", *value.Options[0].StartAt, *value.Options[0].EndAt)
	if e != nil {
		t.Fatal(e)
	}
	gate, results := make(chan struct{}), make(chan error, 3)
	go func() { <-gate; results <- b.Request().ConfirmMeeting(ctx, value.ID, "alice", "org", offer.ID) }()
	go func() {
		<-gate
		_, e := b.Request().ResolveRequest(ctx, value.ID, "bob", "org", coord.ResolutionCommand{Status: coord.Async, Message: "answer"})
		results <- e
	}()
	go func() {
		<-gate
		_, e := b.Request().Handoff(ctx, value.ID, "bob", "org", "carol", []coord.Option{{ID: "fresh", RequestID: value.ID, Type: coord.OptionAsync, ResponseBy: &value.DeadlineAt, CreatedAt: now}})
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
		t.Fatal("conflicting states committed", winners)
	}
	got, e := b.Request().GetForUser(ctx, value.ID, "alice")
	if e != nil {
		t.Fatal(e)
	}
	if got.TargetUserID == "carol" {
		for _, o := range got.Options {
			if o.ProposedByUserID != "" || o.ID == offer.ID {
				t.Fatal("old consent carried over")
			}
		}
	}
}
