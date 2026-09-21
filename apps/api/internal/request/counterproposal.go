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

var ErrProposalConflict = errors.New("proposal_conflict")
var ErrProposalLimit = errors.New("proposal_limit")

const MaxRequestOptions = 10

type CounterproposalStore interface {
	ProposeMeeting(context.Context, string, string, string, time.Time, time.Time) (Option, bool, error)
	ConfirmMeeting(context.Context, string, string, string, string) error
}

// Equal semantic times map to one immutable offer, including across retries.
func PrepareProposal(value *CoordinationRequest, actor, org string, start, end, now time.Time) (Option, bool, error) {
	if actor == "" || value.TargetUserID != actor || org == "" || value.OrganizationID != org {
		return Option{}, false, ErrNotFound
	}
	start, end = start.UTC().Truncate(time.Microsecond), end.UTC().Truncate(time.Microsecond)
	if start.IsZero() || end.IsZero() || !end.After(start) {
		return Option{}, false, ErrCandidateInvalid
	}
	id := fmt.Sprintf("offer-%x", sha256.Sum256([]byte(value.ID+"\x00"+actor+"\x00"+start.Format(time.RFC3339Nano)+"\x00"+end.Format(time.RFC3339Nano))))
	for _, old := range value.Options {
		if old.ID == id {
			if old.ProposedByUserID != actor || old.Type != OptionMeeting || old.StartAt == nil || old.EndAt == nil || !old.StartAt.Equal(start) || !old.EndAt.Equal(end) {
				return Option{}, false, ErrProposalConflict
			}
			return old, true, nil
		}
	}
	if value.Status != Suggested || value.AcceptedOptionID != "" {
		return Option{}, false, ErrProposalConflict
	}
	if !value.DeadlineAt.After(now) || !start.After(now) || end.After(value.DeadlineAt) {
		return Option{}, false, ErrCandidateExpired
	}
	if end.Sub(start) != time.Duration(value.DurationMinutes)*time.Minute {
		return Option{}, false, ErrCandidateInvalid
	}
	if len(value.Options) >= MaxRequestOptions {
		return Option{}, false, ErrProposalLimit
	}
	option := Option{ID: id, RequestID: value.ID, Type: OptionMeeting, StartAt: &start, EndAt: &end, ProposedByUserID: actor, CreatedAt: now}
	value.Options = append(value.Options, option)
	value.UpdatedAt = now
	return option, false, nil
}

// The offer is the target's consent; only the other party can agree to it.
// Legacy/generated options retain the existing target-approval semantics.
func AuthorizeConfirmation(value CoordinationRequest, actor, optionID string) error {
	if actor == "" || (actor != value.RequesterUserID && actor != value.TargetUserID) {
		return ErrNotFound
	}
	for _, option := range value.Options {
		if option.ID != optionID {
			continue
		}
		if option.ProposedByUserID == "" && actor == value.TargetUserID {
			return nil
		}
		if option.ProposedByUserID == value.TargetUserID && actor == value.RequesterUserID {
			return nil
		}
		return ErrNotFound
	}
	return ErrCandidateInvalid
}

func ProposalEffects(value CoordinationRequest, option Option) (notification.Notification, audit.Event) {
	return notification.Notification{ID: option.ID, UserID: value.RequesterUserID, Type: notification.RequestChanged, RequestID: value.ID, Message: "別の時間が提案されました。送信済みの依頼から確認・承認できます。", CreatedAt: option.CreatedAt},
		audit.Event{ID: option.ID, OrganizationID: value.OrganizationID, ActorUserID: option.ProposedByUserID, Action: audit.RequestChanged, ResourceType: "request", ResourceID: value.ID, CreatedAt: option.CreatedAt}
}
