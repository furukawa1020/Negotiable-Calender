package calendar

import "time"

type Flow struct {
	ID, UserID, CodeVerifier string
	StateHash                []byte
	ExpiresAt, CreatedAt     time.Time
}

type TokenSet struct {
	AccessToken, RefreshToken string
	Scopes                    []string
	ExpiresAt                 time.Time
}

type Connection struct {
	UserID             string     `json:"userId"`
	GrantedScopes      []string   `json:"grantedScopes"`
	ConnectedAt        time.Time  `json:"connectedAt"`
	LastSyncedAt       *time.Time `json:"lastSyncedAt,omitempty"`
	ReconnectRequired  bool       `json:"reconnectRequired"`
	RefreshTokenCipher []byte     `json:"-"`
}

type BusySpan struct {
	ProviderEventID string
	CalendarID      string
	StartAt, EndAt  time.Time
	Busy            bool
}
