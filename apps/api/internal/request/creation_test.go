package request

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestCreationIdentityAndEffects(t *testing.T) {
	key := "9a154628-675f-477b-a405-b7c6448e3bf1"
	id, err := CreationID("org", "alice", key)
	if err != nil {
		t.Fatal(err)
	}
	again, _ := CreationID("org", "alice", key)
	other, _ := CreationID("other", "alice", key)
	actor, _ := CreationID("org", "bob", key)
	if id != again || id == other || id == actor || strings.Contains(id, key) {
		t.Fatal("incorrect idempotency scope")
	}
	for _, key := range []string{"", "short", strings.Repeat("a", 129), "123456789012345/"} {
		if _, err := CreationID("org", "alice", key); err == nil {
			t.Fatal("invalid key accepted")
		}
	}
	now := time.Now().UTC().Truncate(time.Microsecond)
	a := CoordinationRequest{ID: id, OrganizationID: "org", RequesterUserID: "alice", TargetUserID: "bob", Title: "confidential-title", DeadlineAt: now.Add(time.Hour), CreatedAt: now}
	b := a
	b.Status = Cancelled
	b.UpdatedAt = now.Add(time.Minute)
	if !SameCreation(a, b) {
		t.Fatal("lifecycle broke replay")
	}
	b.Title = "different"
	if SameCreation(a, b) {
		t.Fatal("changed command replayed")
	}
	note, event := CreationEffects(a)
	payload, _ := json.Marshal([]any{note, event})
	if strings.Contains(string(payload), a.Title) || note.UserID != "bob" || event.ActorUserID != "alice" || note.ID != event.ID {
		t.Fatal("unsafe creation effects")
	}
}
