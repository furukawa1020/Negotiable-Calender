package audit

import "time"

type Action string

const (
	RequestCreated   Action = "request_created"
	RequestAccepted  Action = "request_accepted"
	RequestChanged   Action = "request_changed"
	RequestDeclined  Action = "request_declined"
	RequestDelegated Action = "request_delegated"
	RequestCancelled Action = "request_cancelled"
)

type Event struct {
	ID             string    `json:"id"`
	OrganizationID string    `json:"organizationId"`
	ActorUserID    string    `json:"actorUserId"`
	Action         Action    `json:"action"`
	ResourceType   string    `json:"resourceType"`
	ResourceID     string    `json:"resourceId"`
	CreatedAt      time.Time `json:"createdAt"`
}
