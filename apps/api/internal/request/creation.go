package request

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"time"

	"github.com/negotiable-calendar/negotiable-calendar/apps/api/internal/audit"
	"github.com/negotiable-calendar/negotiable-calendar/apps/api/internal/notification"
)

var ErrCreationConflict = errors.New("idempotency_key_conflict")
var ErrCreationForbidden = errors.New("creation_forbidden")
var ErrCreationExpired = errors.New("creation_expired")
var creationKeyPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{16,128}$`)

// CreationStore owns the request, initial options, notification and audit commit.
// A lookup/replay must check current account and membership access, not just ID.
type CreationStore interface {
	LookupCreation(context.Context, CoordinationRequest) (CoordinationRequest, error)
	CreateOnce(context.Context, CoordinationRequest) (bool, error)
}

// Recheck time after transactional reads/lock waits, immediately before writes.
// Only new commands are checked: a saved command must remain replayable after
// expiry, without regenerating options or resetting its lifecycle/effects.
func ValidateCreationFreshness(value CoordinationRequest, now time.Time) error {
	if now.IsZero() || !value.DeadlineAt.After(now) {
		return ErrCreationExpired
	}
	for _, option := range value.Options {
		if option.Type == OptionMeeting && (option.StartAt == nil || !option.StartAt.After(now)) {
			return ErrCreationExpired
		}
		if option.Type == OptionAsync && option.ResponseBy != nil && !option.ResponseBy.After(now) {
			return ErrCreationExpired
		}
	}
	return nil
}

func CreationID(organizationID, actor, key string) (string, error) {
	if !creationKeyPattern.MatchString(key) {
		return "", fmt.Errorf("invalid idempotency key")
	}
	encoded, _ := json.Marshal([]string{"request-create-v1", organizationID, actor, key})
	return fmt.Sprintf("request-%x", sha256.Sum256(encoded)), nil
}

// Lifecycle changes must not invalidate a retry of the original command.
// Normalize timestamps to the precision supported by both persistence backends.
func SameCreation(a, b CoordinationRequest) bool {
	return a.ID == b.ID && a.OrganizationID == b.OrganizationID &&
		a.RequesterUserID == b.RequesterUserID && CreationTarget(a) == CreationTarget(b) &&
		a.Type == b.Type && a.Title == b.Title && a.DurationMinutes == b.DurationMinutes &&
		a.DeadlineAt.Truncate(time.Microsecond).Equal(b.DeadlineAt.Truncate(time.Microsecond)) &&
		a.SyncPreference == b.SyncPreference && a.Priority == b.Priority
}

func CreationTarget(value CoordinationRequest) string {
	if value.DelegatedFromUserID != "" {
		return value.DelegatedFromUserID
	}
	return value.TargetUserID
}

func CreationEffects(value CoordinationRequest) (notification.Notification, audit.Event) {
	id := fmt.Sprintf("create-request-%x", sha256.Sum256([]byte(value.ID)))
	return notification.Notification{ID: id, UserID: value.TargetUserID, Type: notification.RequestReceived, RequestID: value.ID, Message: "新しい調整依頼が届きました。", CreatedAt: value.CreatedAt},
		audit.Event{ID: id, OrganizationID: value.OrganizationID, ActorUserID: value.RequesterUserID, Action: audit.RequestCreated, ResourceType: "request", ResourceID: value.ID, CreatedAt: value.CreatedAt}
}
