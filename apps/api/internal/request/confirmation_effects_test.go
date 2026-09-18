package request

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestConfirmationEffectsStableIdentityAndPrivacy(t *testing.T) {
	now := time.Now().UTC()
	value := CoordinationRequest{ID: "r", OrganizationID: "org", RequesterUserID: "alice", TargetUserID: "bob", Title: "private-source-title"}
	note, event := ConfirmationEffects(value, now)
	retry, retryEvent := ConfirmationEffects(value, now.Add(time.Minute))
	if note.ID != retry.ID || event.ID != retryEvent.ID || note.ID != event.ID || note.ReadAt != nil || note.UserID != "alice" || event.ActorUserID != "bob" || event.OrganizationID != "org" {
		t.Fatal("unstable or misdirected effects")
	}
	encoded, err := json.Marshal([]any{note, event})
	if err != nil || strings.Contains(string(encoded), value.Title) {
		t.Fatal("private title leaked")
	}
	value.ID = "other"
	other, _ := ConfirmationEffects(value, now)
	if other.ID == note.ID {
		t.Fatal("different requests collide")
	}
	cancel, _ := ConfirmedCancellationEffects(value, "alice", now)
	if other.ID == cancel.ID {
		t.Fatal("lifecycle effects collide")
	}
}
