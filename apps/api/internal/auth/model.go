package auth

import "time"

var ErrNotFound = errNotFound("authentication record not found")

type errNotFound string

func (err errNotFound) Error() string { return string(err) }

type Profile struct {
	Subject       string
	Email         string
	EmailVerified bool
	DisplayName   string
	AvatarURL     string
}

type Identity struct {
	UserID         string `json:"userId"`
	OrganizationID string `json:"organizationId"`
	Email          string `json:"email"`
	DisplayName    string `json:"displayName"`
	AvatarURL      string `json:"avatarUrl,omitempty"`
	Role           string `json:"role"`
}

type Flow struct {
	ID           string
	StateHash    []byte
	CodeVerifier string
	ExpiresAt    time.Time
	CreatedAt    time.Time
}

type Session struct {
	TokenHash      []byte
	UserID         string
	OrganizationID string
	ExpiresAt      time.Time
	CreatedAt      time.Time
}
