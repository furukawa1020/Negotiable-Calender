package request

import (
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestResolutionStateMatrix(t *testing.T) {
	now := time.Now().UTC()
	for _, state := range []Status{Pending, Suggested, Accepted, Declined, Delegated, Cancelled, Expired, Completed, Async} {
		for _, target := range []Status{Async, Declined, Cancelled} {
			t.Run(string(state)+"-to-"+string(target), func(t *testing.T) {
				value := CoordinationRequest{Status: state, DeadlineAt: now.Add(time.Hour), AsyncMessage: "answer", UpdatedAt: now.Add(-time.Minute)}
				before := value
				command := ResolutionCommand{Status: target}
				if target == Async {
					command.Message = "answer"
				}
				replayed, err := PrepareResolution(&value, command, now)
				allowed := state == target || state == Suggested || (target == Cancelled && (state == Pending || state == Delegated))
				if !allowed {
					if !errors.Is(err, ErrResolutionConflict) || !reflect.DeepEqual(value, before) {
						t.Fatal("invalid transition mutated request", err)
					}
					return
				}
				if err != nil || replayed != (state == target) || value.Status != target {
					t.Fatal("wrong transition", err)
				}
				if replayed && !reflect.DeepEqual(value, before) {
					t.Fatal("replay changed data")
				}
			})
		}
	}
}

func TestResolutionGuardsAndPrivacy(t *testing.T) {
	now := time.Now().UTC()
	base := CoordinationRequest{ID: "request", OrganizationID: "org", RequesterUserID: "alice", TargetUserID: "bob", Title: "secret title", Status: Suggested, DeadlineAt: now}
	for _, target := range []Status{Async, Declined, Cancelled} {
		cmd := ResolutionCommand{Status: target}
		actor := "bob"
		if target == Async {
			cmd.Message = "secret answer"
		}
		if target == Cancelled {
			actor = "alice"
		}
		if err := AuthorizeResolution(base, actor, "org", cmd); err != nil {
			t.Fatal(err)
		}
		for _, identity := range [][2]string{{"stranger", "org"}, {actor, "other"}, {"", "org"}, {actor, ""}} {
			if !errors.Is(AuthorizeResolution(base, identity[0], identity[1], cmd), ErrNotFound) {
				t.Fatal("authorization bypass")
			}
		}
		value := base
		_, err := PrepareResolution(&value, cmd, now)
		if target != Cancelled && !errors.Is(err, ErrResolutionExpired) {
			t.Fatal("late answer accepted", err)
		}
		value = base
		value.DeadlineAt = now.Add(time.Hour)
		if _, err := PrepareResolution(&value, cmd, now); err != nil {
			t.Fatal(err)
		}
		if replay, err := PrepareResolution(&value, cmd, now.Add(2*time.Hour)); err != nil || !replay {
			t.Fatal("expired replay rejected", err)
		}
		note, event := ResolutionEffects(value, actor)
		encoded, _ := json.Marshal([]any{note, event})
		if strings.Contains(string(encoded), base.Title) || strings.Contains(string(encoded), "secret answer") || event.ActorUserID != actor || note.UserID == actor || note.ID != event.ID {
			t.Fatal("unsafe effects")
		}
		value.AcceptedOptionID = "confirmed-option"
		if _, err := PrepareResolution(&value, cmd, now); !errors.Is(err, ErrResolutionConflict) {
			t.Fatal("confirmed cancellation bypass")
		}
	}
	for _, cmd := range []ResolutionCommand{{Status: Accepted}, {Status: Async}, {Status: Async, Message: strings.Repeat("a", 501)}, {Status: Declined, Message: "extra"}} {
		value := base
		if _, err := PrepareResolution(&value, cmd, now); !errors.Is(err, ErrResolutionConflict) {
			t.Fatal("invalid command accepted")
		}
	}
	value := base
	value.Status = Async
	value.AsyncMessage = "original"
	if _, err := PrepareResolution(&value, ResolutionCommand{Status: Async, Message: "changed"}, now); !errors.Is(err, ErrResolutionConflict) {
		t.Fatal("answer overwritten")
	}
}
