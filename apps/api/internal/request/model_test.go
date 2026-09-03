package request

import (
	"strings"
	"testing"
	"time"
)

func TestCoordinationRequestValidation(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 8, 23, 5, 0, 0, 0, time.UTC)
	request := CoordinationRequest{
		ID: "r1", OrganizationID: "o1", RequesterUserID: "member-1", TargetUserID: "manager-1",
		Type: Review, Title: "API design review", DurationMinutes: 15, DeadlineAt: now.Add(4 * time.Hour),
		SyncPreference: Either, Priority: PriorityNormal, Status: Pending, CreatedAt: now, UpdatedAt: now,
	}
	if err := request.Validate(); err != nil {
		t.Fatalf("valid request rejected: %v", err)
	}
	request.RequesterUserID = request.TargetUserID
	if err := request.Validate(); err == nil {
		t.Fatal("self-targeted coordination request accepted")
	}
}

func TestAsyncMessageValidation(t *testing.T) {
	t.Parallel()
	if err := ValidateAsyncMessage(" 文書で回答します "); err != nil {
		t.Fatalf("valid async message rejected: %v", err)
	}
	if err := ValidateAsyncMessage("   "); err == nil {
		t.Fatal("blank async message accepted")
	}
	if err := ValidateAsyncMessage(strings.Repeat("あ", MaxAsyncMessageRunes+1)); err == nil {
		t.Fatal("oversized async message accepted")
	}
}
