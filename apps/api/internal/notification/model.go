package notification

import "time"

type Type string

const (
	RequestReceived  Type = "request_received"
	RequestAccepted  Type = "request_accepted"
	RequestChanged   Type = "request_changed"
	RequestDeclined  Type = "request_declined"
	RequestAsync     Type = "request_async"
	RequestDelegated Type = "request_delegated"
	RequestCancelled Type = "request_cancelled"
)

type Notification struct {
	ID        string     `json:"id"`
	UserID    string     `json:"userId"`
	Type      Type       `json:"type"`
	RequestID string     `json:"requestId"`
	Message   string     `json:"message"`
	ReadAt    *time.Time `json:"readAt,omitempty"`
	CreatedAt time.Time  `json:"createdAt"`
}
