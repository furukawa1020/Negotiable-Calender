package organization

import (
	"fmt"
	"net/mail"
	"strings"
	"time"
	_ "time/tzdata"
)

type Role string

const (
	Owner Role = "OWNER"
	Admin Role = "ADMIN"
	Manager Role = "MANAGER"
	Member Role = "MEMBER"
)

func (r Role) Valid() bool {
	switch r {
	case Owner, Admin, Manager, Member:
		return true
	default:
		return false
	}
}

type Organization struct {
	ID string
	Name string
	CreatedAt time.Time
	UpdatedAt time.Time
}

func (o Organization) Validate() error {
	if strings.TrimSpace(o.ID) == "" {
		return fmt.Errorf("organization id is required")
	}
	if strings.TrimSpace(o.Name) == "" {
		return fmt.Errorf("organization name is required")
	}
	return validTimes(o.CreatedAt, o.UpdatedAt)
}

type User struct {
	ID string
	Email string
	DisplayName string
	AvatarURL string
	Timezone string
	CreatedAt time.Time
	UpdatedAt time.Time
}

func (u User) Validate() error {
	if strings.TrimSpace(u.ID) == "" {
		return fmt.Errorf("user id is required")
	}
	address, err := mail.ParseAddress(u.Email)
	if err != nil {
		return fmt.Errorf("invalid user email")
	}
	if address.Address != u.Email {
		return fmt.Errorf("invalid user email")
	}
	if strings.TrimSpace(u.DisplayName) == "" {
		return fmt.Errorf("display name is required")
	}
	if _, err := time.LoadLocation(u.Timezone); err != nil {
		return fmt.Errorf("invalid timezone: %w", err)
	}
	return validTimes(u.CreatedAt, u.UpdatedAt)
}

type Membership struct {
	ID string
	OrganizationID string
	UserID string
	Role Role
	CreatedAt time.Time
}

func (m Membership) Validate() error {
	if strings.TrimSpace(m.ID) == "" {
		return fmt.Errorf("membership id is required")
	}
	if strings.TrimSpace(m.OrganizationID) == "" {
		return fmt.Errorf("organization id is required")
	}
	if strings.TrimSpace(m.UserID) == "" {
		return fmt.Errorf("user id is required")
	}
	if !m.Role.Valid() {
		return fmt.Errorf("invalid role")
	}
	return validUTC("created_at", m.CreatedAt)
}

func validTimes(createdAt, updatedAt time.Time) error {
	if err := validUTC("created_at", createdAt); err != nil {
		return err
	}
	if err := validUTC("updated_at", updatedAt); err != nil {
		return err
	}
	if updatedAt.Before(createdAt) {
		return fmt.Errorf("updated_at before created_at")
	}
	return nil
}

func validUTC(name string, value time.Time) error {
	if value.IsZero() {
		return fmt.Errorf("%s is required", name)
	}
	if value.Location() != time.UTC {
		return fmt.Errorf("%s must be UTC", name)
	}
	return nil
}
