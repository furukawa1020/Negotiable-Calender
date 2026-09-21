package request

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"github.com/negotiable-calendar/negotiable-calendar/apps/api/internal/audit"
	"github.com/negotiable-calendar/negotiable-calendar/apps/api/internal/notification"
	"strings"
	"time"
)

var ErrHandoffConflict = errors.New("handoff_conflict")
var ErrHandoffExpired = errors.New("handoff_expired")

type HandoffStore interface {
	InspectHandoff(context.Context, string, string, string, string) (CoordinationRequest, bool, error)
	Handoff(context.Context, string, string, string, string, []Option) (bool, error)
}

func ValidHandoffID(id string) bool {
	return id != "" && len(id) <= 256 && !strings.ContainsAny(id, "/\\\r\n")
}

// A single handoff preserves the original creation identity. A previous owner
// can recover only the handoff acknowledgement, never the recipient's response.
func ValidateHandoff(value CoordinationRequest, actor, org, recipient string, now time.Time) (bool, error) {
	if org == "" || value.OrganizationID != org || actor == "" || (value.TargetUserID != actor && value.DelegatedFromUserID != actor) {
		return false, ErrNotFound
	}
	if !ValidHandoffID(recipient) || recipient == actor || recipient == value.RequesterUserID {
		return false, ErrHandoffConflict
	}
	if value.DelegatedFromUserID != "" {
		if value.DelegatedFromUserID == actor && value.TargetUserID == recipient && value.DelegatedUserID == recipient {
			return true, nil
		}
		return false, ErrHandoffConflict
	}
	if value.Status != Suggested || value.AcceptedOptionID != "" || value.DelegatedUserID != "" {
		return false, ErrHandoffConflict
	}
	if !value.DeadlineAt.After(now) {
		return false, ErrHandoffExpired
	}
	return false, nil
}

func ApplyHandoff(value *CoordinationRequest, actor, recipient string, options []Option, now time.Time) error {
	if len(options) < 1 || len(options) > 3 {
		return ErrHandoffConflict
	}
	updated := make([]Option, 0, len(options))
	seen := map[string]bool{}
	for _, option := range options {
		if option.RequestID != value.ID || option.Validate() != nil || (option.Type != OptionMeeting && option.Type != OptionAsync) {
			return ErrHandoffConflict
		}
		if option.Type == OptionMeeting && (!option.StartAt.After(now) || option.EndAt.After(value.DeadlineAt)) {
			return ErrHandoffExpired
		}
		if option.Type == OptionAsync && (option.ResponseBy == nil || !option.ResponseBy.After(now) || option.ResponseBy.After(value.DeadlineAt)) {
			return ErrHandoffExpired
		}
		option.ID = fmt.Sprintf("handoff-option-%x", sha256.Sum256([]byte(value.ID+"\x00"+recipient+"\x00"+option.ID)))
		option.ProposedByUserID = ""
		if seen[option.ID] {
			return ErrHandoffConflict
		}
		seen[option.ID] = true
		updated = append(updated, option)
	}
	value.DelegatedFromUserID, value.DelegatedUserID, value.TargetUserID = actor, recipient, recipient
	value.Options, value.UpdatedAt = updated, now
	return nil
}

func HandoffEffects(value CoordinationRequest, actor string) ([]notification.Notification, audit.Event) {
	id := fmt.Sprintf("handoff-%x", sha256.Sum256([]byte(value.ID)))
	notes := []notification.Notification{
		{ID: id + "-recipient", UserID: value.TargetUserID, Type: notification.RequestReceived, RequestID: value.ID, Message: "調整依頼を引き継ぎました。内容と期限を確認してください。", CreatedAt: value.UpdatedAt},
		{ID: id + "-requester", UserID: value.RequesterUserID, Type: notification.RequestDelegated, RequestID: value.ID, Message: "依頼の担当者が変更されました。送信済みの依頼で確認できます。", CreatedAt: value.UpdatedAt},
	}
	return notes, audit.Event{ID: id, OrganizationID: value.OrganizationID, ActorUserID: actor, Action: audit.RequestDelegated, ResourceType: "request", ResourceID: value.ID, CreatedAt: value.UpdatedAt}
}
