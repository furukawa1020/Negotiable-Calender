package audit

import "time"

type Action string

const (
	RequestCreated      Action = "request_created"
	RequestAccepted     Action = "request_accepted"
	RequestChanged      Action = "request_changed"
	RescheduleProposed  Action = "reschedule_proposed"
	RescheduleAccepted  Action = "reschedule_accepted"
	RescheduleDeclined  Action = "reschedule_declined"
	RescheduleWithdrawn Action = "reschedule_withdrawn"
	RequestDeclined     Action = "request_declined"
	RequestAsync        Action = "request_async"
	RequestDelegated    Action = "request_delegated"
	RequestCancelled    Action = "request_cancelled"
	InvitationCreated   Action = "invitation_created"
	InvitationAccepted  Action = "invitation_accepted"
	WorkspaceSwitched   Action = "workspace_switched"
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
