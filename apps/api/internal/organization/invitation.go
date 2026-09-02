package organization

import (
	"fmt"
	"strings"
	"time"
)

type Invitation struct {
	ID, OrganizationID, InvitedBy string
	Role                          Role
	TokenHash                     []byte
	ExpiresAt, CreatedAt          time.Time
}

func (value Invitation) Validate() error {
	if strings.TrimSpace(value.ID) == "" || strings.TrimSpace(value.OrganizationID) == "" || strings.TrimSpace(value.InvitedBy) == "" {
		return fmt.Errorf("invitation identity is required")
	}
	if !value.Role.Valid() || value.Role == Owner {
		return fmt.Errorf("invalid invitation role")
	}
	if len(value.TokenHash) != 32 {
		return fmt.Errorf("invitation token hash is required")
	}
	if err := validUTC("created_at", value.CreatedAt); err != nil {
		return err
	}
	if err := validUTC("expires_at", value.ExpiresAt); err != nil || !value.ExpiresAt.After(value.CreatedAt) {
		return fmt.Errorf("invalid invitation expiry")
	}
	return nil
}

type InvitationPreview struct {
	ID               string    `json:"invitationId"`
	OrganizationID   string    `json:"organizationId"`
	OrganizationName string    `json:"organizationName"`
	Role             Role      `json:"role"`
	ExpiresAt        time.Time `json:"expiresAt"`
}

type Workspace struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Role Role   `json:"role"`
}

func CanInvite(actor, target Role) bool {
	switch actor {
	case Owner:
		return target == Admin || target == Manager || target == Member
	case Admin:
		return target == Manager || target == Member
	default:
		return false
	}
}
