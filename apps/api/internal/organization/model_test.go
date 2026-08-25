package organization

import (
	"testing"
	"time"
)

func TestUserAndMembershipValidation(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 8, 23, 0, 0, 0, 0, time.UTC)
	user := User{ID: "u1", Email: "manager@example.com", DisplayName: "Manager", Timezone: "Asia/Tokyo", CreatedAt: now, UpdatedAt: now}
	if err := user.Validate(); err != nil {
		t.Fatalf("valid user rejected: %v", err)
	}
	membership := Membership{ID: "m1", OrganizationID: "o1", UserID: "u1", Role: Manager, CreatedAt: now}
	if err := membership.Validate(); err != nil {
		t.Fatalf("valid membership rejected: %v", err)
	}
	membership.Role = Role("SUPER_ADMIN")
	if err := membership.Validate(); err == nil {
		t.Fatal("undocumented elevated role accepted")
	}
}
