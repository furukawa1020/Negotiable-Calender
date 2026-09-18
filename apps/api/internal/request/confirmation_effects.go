package request

import (
	"crypto/sha256"
	"fmt"
	"time"

	"github.com/negotiable-calendar/negotiable-calendar/apps/api/internal/audit"
	"github.com/negotiable-calendar/negotiable-calendar/apps/api/internal/notification"
)

// ConfirmationEffects are inserted (never upserted) in the acceptance transaction.
// Acceptance happens once per request; later rescheduling has separate effects.
func ConfirmationEffects(value CoordinationRequest, now time.Time) (notification.Notification, audit.Event) {
	id := fmt.Sprintf("confirm-meeting-%x", sha256.Sum256([]byte(value.ID)))
	return notification.Notification{ID: id, UserID: value.RequesterUserID, Type: notification.RequestAccepted, RequestID: value.ID, Message: "依頼の候補を承認しました。", CreatedAt: now},
		audit.Event{ID: id, OrganizationID: value.OrganizationID, ActorUserID: value.TargetUserID, Action: audit.RequestAccepted, ResourceType: "request", ResourceID: value.ID, CreatedAt: now}
}
