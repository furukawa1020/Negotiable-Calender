package organization

import (
	"crypto/sha256"
	"testing"
	"time"
)

func TestCanInviteEnforcesRoleHierarchy(t *testing.T) {
	tests := []struct {
		actor, target Role
		allowed       bool
	}{
		{Owner, Admin, true}, {Owner, Manager, true}, {Owner, Member, true},
		{Owner, Owner, false}, {Admin, Admin, false}, {Admin, Manager, true},
		{Admin, Member, true}, {Manager, Member, false}, {Member, Member, false},
	}
	for _, test := range tests {
		if result := CanInvite(test.actor, test.target); result != test.allowed {
			t.Errorf("CanInvite(%s,%s)=%t want %t", test.actor, test.target, result, test.allowed)
		}
	}
}

func TestInvitationRequiresHashedTokenAndExpiry(t *testing.T) {
	now := time.Date(2026, 9, 3, 0, 0, 0, 0, time.UTC)
	valid := Invitation{ID: "invite-1", OrganizationID: "org-1", InvitedBy: "owner-1", Role: Member, TokenHash: make([]byte, sha256.Size), CreatedAt: now, ExpiresAt: now.Add(time.Hour)}
	if err := valid.Validate(); err != nil {
		t.Fatal(err)
	}
	plaintext := valid
	plaintext.TokenHash = []byte("raw-token")
	if err := plaintext.Validate(); err == nil {
		t.Fatal("plaintext-sized invitation token was accepted")
	}
	expired := valid
	expired.ExpiresAt = now
	if err := expired.Validate(); err == nil {
		t.Fatal("non-future invitation expiry was accepted")
	}
}
