package request

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestHandoffIdentityAndGuards(t *testing.T) {
	now := time.Now().UTC()
	source := CoordinationRequest{ID: "r", OrganizationID: "org", RequesterUserID: "alice", TargetUserID: "bob", Status: Suggested, DeadlineAt: now.Add(time.Hour), Title: "private request", CreatedAt: now, UpdatedAt: now}
	for _, v := range []struct {
		actor, org, to string
		err            error
	}{{"outsider", "org", "carol", ErrNotFound}, {"bob", "other", "carol", ErrNotFound}, {"bob", "org", "bob", ErrHandoffConflict}, {"bob", "org", "alice", ErrHandoffConflict}, {"bob", "org", "/bad", ErrHandoffConflict}} {
		if _, err := ValidateHandoff(source, v.actor, v.org, v.to, now); !errors.Is(err, v.err) {
			t.Fatal("guard failed", v, err)
		}
	}
	for _, state := range []Status{Pending, Accepted, Cancelled, Async, Declined, Delegated, Expired} {
		v := source
		v.Status = state
		if _, err := ValidateHandoff(v, "bob", "org", "carol", now); !errors.Is(err, ErrHandoffConflict) {
			t.Fatal("invalid state", state, err)
		}
	}
	if _, err := ValidateHandoff(source, "bob", "org", "carol", source.DeadlineAt); !errors.Is(err, ErrHandoffExpired) {
		t.Fatal("deadline ignored")
	}
	value := source
	option := Option{ID: "old-id", RequestID: "r", Type: OptionAsync, ResponseBy: &source.DeadlineAt, CreatedAt: now}
	if err := ApplyHandoff(&value, "bob", "carol", []Option{option}, now); err != nil {
		t.Fatal(err)
	}
	if value.TargetUserID != "carol" || value.DelegatedFromUserID != "bob" || value.Status != Suggested || value.Options[0].ID == option.ID || !SameCreation(source, value) {
		t.Fatal("lost identity")
	}
	value.Status = Async
	value.AsyncMessage = "private new answer"
	if replay, err := ValidateHandoff(value, "bob", "org", "carol", now.Add(2*time.Hour)); err != nil || !replay {
		t.Fatal("replay failed", err)
	}
	for _, pair := range [][2]string{{"bob", "dave"}, {"carol", "dave"}, {"carol", "bob"}} {
		if _, err := ValidateHandoff(value, pair[0], "org", pair[1], now); !errors.Is(err, ErrHandoffConflict) {
			t.Fatal("second handoff", err)
		}
	}
	notes, event := HandoffEffects(value, "bob")
	raw, _ := json.Marshal([]any{notes, event})
	if len(notes) != 2 || notes[0].UserID != "carol" || notes[1].UserID != "alice" || notes[0].ID == notes[1].ID || strings.Contains(string(raw), source.Title) || strings.Contains(string(raw), value.AsyncMessage) {
		t.Fatal("unsafe effects")
	}
	for _, options := range [][]Option{nil, {option, option}, {{ID: "bad", RequestID: "r", Type: OptionDelegate, DelegateUserID: "dave", CreatedAt: now}}} {
		v := source
		if ApplyHandoff(&v, "bob", "carol", options, now) == nil || v.TargetUserID != "bob" {
			t.Fatal("invalid candidates mutated source")
		}
	}
}
