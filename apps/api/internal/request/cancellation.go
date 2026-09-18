package request

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"time"

	"github.com/negotiable-calendar/negotiable-calendar/apps/api/internal/audit"
	"github.com/negotiable-calendar/negotiable-calendar/apps/api/internal/notification"
)

var ErrAlreadyCancelled = errors.New("meeting_already_cancelled")
var ErrCancellationInvalid = errors.New("meeting_not_cancellable")

// ConfirmedLifecycleStore commits cancellation, counterpart notification and audit atomically.
type ConfirmedLifecycleStore interface {
	CancelConfirmed(context.Context, string, string, string) error
}

func ValidateConfirmedCancellation(value CoordinationRequest, actor, optionID string, now time.Time) error {
	if actor == "" || (actor != value.RequesterUserID && actor != value.TargetUserID) {
		return ErrNotFound
	}
	if optionID == "" || value.AcceptedOptionID != optionID {
		return ErrCancellationInvalid
	}
	if value.Status == Cancelled {
		return ErrAlreadyCancelled
	}
	if value.Status != Accepted {
		return ErrCancellationInvalid
	}
	for _, option := range value.Options {
		if option.ID == optionID && option.RequestID == value.ID && option.Type == OptionMeeting && option.Validate() == nil && option.StartAt.After(now) {
			return nil
		}
	}
	return ErrCancellationInvalid
}

// Stable IDs ensure effects cannot be duplicated or silently overwritten on replay.
func ConfirmedCancellationEffects(value CoordinationRequest, actor string, now time.Time) (notification.Notification, audit.Event) {
	id := fmt.Sprintf("cancel-confirmed-%x", sha256.Sum256([]byte(value.ID)))
	recipient := value.TargetUserID
	if actor == recipient {
		recipient = value.RequesterUserID
	}
	return notification.Notification{ID: id, UserID: recipient, Type: notification.RequestCancelled, RequestID: value.ID, Message: "確定会議が取り消されました。外部カレンダーに登録した予定は手動で削除してください。", CreatedAt: now},
		audit.Event{ID: id, OrganizationID: value.OrganizationID, ActorUserID: actor, Action: audit.RequestCancelled, ResourceType: "request", ResourceID: value.ID, CreatedAt: now}
}
