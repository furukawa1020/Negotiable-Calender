package request

import (
	"errors"
	"testing"
	"time"
)

func TestProposalIdentityAndConsent(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Microsecond)
	value := CoordinationRequest{ID: "r", OrganizationID: "org", RequesterUserID: "alice", TargetUserID: "bob", Status: Suggested, DurationMinutes: 30, DeadlineAt: now.Add(24 * time.Hour)}
	start, end := now.Add(time.Hour), now.Add(90*time.Minute)
	for _, input := range []struct{ actor, org string }{{"alice", "org"}, {"outsider", "org"}, {"bob", "other"}, {"bob", ""}} {
		if _, _, e := PrepareProposal(&value, input.actor, input.org, start, end, now); !errors.Is(e, ErrNotFound) {
			t.Fatal(input, e)
		}
	}
	for _, input := range []struct {
		start, end time.Time
		want       error
	}{{now, end, ErrCandidateExpired}, {start, value.DeadlineAt.Add(time.Minute), ErrCandidateExpired}, {start, start.Add(15 * time.Minute), ErrCandidateInvalid}} {
		if _, _, e := PrepareProposal(&value, "bob", "org", input.start, input.end, now); !errors.Is(e, input.want) {
			t.Fatal(e)
		}
	}
	option, replay, err := PrepareProposal(&value, "bob", "org", start, end, now)
	if err != nil || replay || option.ProposedByUserID != "bob" || len(value.Options) != 1 {
		t.Fatal(option, replay, err)
	}
	note, audit := ProposalEffects(value, option)
	if note.UserID != "alice" || audit.ActorUserID != "bob" {
		t.Fatal("wrong counterpart")
	}
	if e := AuthorizeConfirmation(value, "bob", option.ID); !errors.Is(e, ErrNotFound) {
		t.Fatal("self consent")
	}
	if e := AuthorizeConfirmation(value, "alice", option.ID); e != nil {
		t.Fatal(e)
	}
	if e := AuthorizeConfirmation(value, "outsider", option.ID); !errors.Is(e, ErrNotFound) {
		t.Fatal("outsider")
	}
	value.Status = Accepted
	old := value.UpdatedAt
	repeated, replay, err := PrepareProposal(&value, "bob", "org", start.In(time.FixedZone("JST", 9*3600)), end, now.Add(48*time.Hour))
	if err != nil || !replay || repeated.ID != option.ID || len(value.Options) != 1 || !old.Equal(value.UpdatedAt) {
		t.Fatal("retry changed offer", err)
	}
	confirmation, event := ConfirmationEffectsForActor(value, "alice", now)
	if confirmation.UserID != "bob" || event.ActorUserID != "alice" {
		t.Fatal("wrong confirmation counterpart")
	}
	value.Options[0].ProposedByUserID = ""
	if e := AuthorizeConfirmation(value, "alice", option.ID); !errors.Is(e, ErrNotFound) {
		t.Fatal("self-approved generated candidate")
	}
	if e := AuthorizeConfirmation(value, "bob", option.ID); e != nil {
		t.Fatal(e)
	}
}

func TestProposalLimitAndTerminalStates(t *testing.T) {
	now := time.Now().UTC()
	base := CoordinationRequest{ID: "r", OrganizationID: "org", RequesterUserID: "alice", TargetUserID: "bob", Status: Suggested, DurationMinutes: 30, DeadlineAt: now.Add(24 * time.Hour)}
	for _, state := range []Status{Pending, Accepted, Cancelled, Declined, Async, Delegated, Expired, Completed} {
		value := base
		value.Status = state
		if _, _, e := PrepareProposal(&value, "bob", "org", now.Add(time.Hour), now.Add(90*time.Minute), now); !errors.Is(e, ErrProposalConflict) {
			t.Fatal(state, e)
		}
	}
	for i := 0; i < MaxRequestOptions; i++ {
		start := now.Add(time.Duration(i+1) * time.Hour)
		if _, _, e := PrepareProposal(&base, "bob", "org", start, start.Add(30*time.Minute), now); e != nil {
			t.Fatal(e)
		}
	}
	if _, _, e := PrepareProposal(&base, "bob", "org", now.Add(12*time.Hour), now.Add(750*time.Minute), now); !errors.Is(e, ErrProposalLimit) {
		t.Fatal(e)
	}
	if _, r, e := PrepareProposal(&base, "bob", "org", now.Add(time.Hour), now.Add(90*time.Minute), now); e != nil || !r {
		t.Fatal("limit blocked replay", e)
	}
}
