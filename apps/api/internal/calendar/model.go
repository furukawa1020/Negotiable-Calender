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
	LastAttemptAt      *time.Time `json:"lastAttemptAt,omitempty"`
	NextAttemptAt      *time.Time `json:"nextAttemptAt,omitempty"`
	LastErrorCode      string     `json:"lastErrorCode,omitempty"`
	FailureCount       int        `json:"-"`
	SyncToken          string     `json:"-"`
	ReconnectRequired  bool       `json:"reconnectRequired"`
	RefreshTokenCipher []byte     `json:"-"`
}

type BusySpan struct {
	ProviderEventID string
	CalendarID      string
	StartAt, EndAt  time.Time
	Busy            bool
}


type ChangeSet struct {
	Upserts                 []BusySpan
	DeletedProviderEventIDs []string
	NextSyncToken           string
	Full                    bool
}


type PrivateEventView struct {
	ID            string     `json:"id"`
	Title         string     `json:"title"`
	Description   string     `json:"description,omitempty"`
	Location      string     `json:"location,omitempty"`
	Attendees     []string   `json:"attendees,omitempty"`
	ConferenceURL string     `json:"conferenceUrl,omitempty"`
	StartAt       *time.Time `json:"startAt,omitempty"`
	EndAt         *time.Time `json:"endAt,omitempty"`
	StartDate     string     `json:"startDate,omitempty"`
	EndDate       string     `json:"endDate,omitempty"`
	AllDay        bool       `json:"allDay"`
}
