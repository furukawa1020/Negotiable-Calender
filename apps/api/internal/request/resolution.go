package request

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/negotiable-calendar/negotiable-calendar/apps/api/internal/audit"
	"github.com/negotiable-calendar/negotiable-calendar/apps/api/internal/notification"
)

var ErrResolutionConflict = errors.New("request_resolution_conflict")
var ErrResolutionExpired = errors.New("request_resolution_expired")

type ResolutionCommand struct {
	Status  Status
	Message string
}

// ResolutionStore commits terminal state, counterpart notification and audit
// together. The bool reports an identical replay, not a second state change.
type ResolutionStore interface {
	ResolveRequest(context.Context, string, string, string, ResolutionCommand) (bool, error)
}

func AuthorizeResolution(value CoordinationRequest, actor, organization string, command ResolutionCommand) error {
	if actor == "" || organization == "" || organization != value.OrganizationID {
		return ErrNotFound
	}
	if command.Status == Cancelled {
		if actor != value.RequesterUserID {
			return ErrNotFound
		}
	} else if actor != value.TargetUserID {
		return ErrNotFound
	}
	return nil
}

// PrepareResolution is deterministic; storage must serialize on the request and
// verify live memberships/account fences before either first commit or replay.
func PrepareResolution(value *CoordinationRequest, command ResolutionCommand, now time.Time) (bool, error) {
	command.Message = strings.TrimSpace(command.Message)
	if command.Status != Async && command.Status != Declined && command.Status != Cancelled {
		return false, ErrResolutionConflict
	}
	if command.Status == Async {
		if err := ValidateAsyncMessage(command.Message); err != nil {
			return false, ErrResolutionConflict
		}
	} else if command.Message != "" {
		return false, ErrResolutionConflict
	}
	// Never treat a confirmed-meeting cancellation as an unconfirmed cancellation.
	if value.AcceptedOptionID != "" {
		return false, ErrResolutionConflict
	}
	if value.Status == command.Status {
		if command.Status == Async && value.AsyncMessage != command.Message {
			return false, ErrResolutionConflict
		}
		return true, nil
	}
	if command.Status == Cancelled {
		if value.Status != Pending && value.Status != Suggested && value.Status != Delegated {
			return false, ErrResolutionConflict
		}
	} else {
		if value.Status != Suggested {
			return false, ErrResolutionConflict
		}
		if !value.DeadlineAt.After(now) {
			return false, ErrResolutionExpired
		}
	}
	value.Status, value.AsyncMessage, value.UpdatedAt = command.Status, command.Message, now
	return false, nil
}

func ResolutionEffects(value CoordinationRequest, actor string) (notification.Notification, audit.Event) {
	id := fmt.Sprintf("resolve-request-%x", sha256.Sum256([]byte(value.ID)))
	recipient, kind, action, message := value.RequesterUserID, notification.RequestAsync, audit.RequestAsync, "依頼に非同期の回答が届きました。"
	if value.Status == Declined {
		kind, action, message = notification.RequestDeclined, audit.RequestDeclined, "依頼が辞退されました。"
	}
	if value.Status == Cancelled {
		recipient, kind, action, message = value.TargetUserID, notification.RequestCancelled, audit.RequestCancelled, "調整依頼が取り下げられました。"
	}
	return notification.Notification{ID: id, UserID: recipient, Type: kind, RequestID: value.ID, Message: message, CreatedAt: value.UpdatedAt},
		audit.Event{ID: id, OrganizationID: value.OrganizationID, ActorUserID: actor, Action: action, ResourceType: "request", ResourceID: value.ID, CreatedAt: value.UpdatedAt}
}
