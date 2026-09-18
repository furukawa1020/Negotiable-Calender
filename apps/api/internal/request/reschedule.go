package request

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"regexp"
	"time"

	"github.com/negotiable-calendar/negotiable-calendar/apps/api/internal/audit"
	"github.com/negotiable-calendar/negotiable-calendar/apps/api/internal/notification"
)

var ErrRescheduleInvalid = errors.New("reschedule_conflict")
var ErrRescheduleRepeated = errors.New("reschedule_already_applied")
var proposalIDPattern = regexp.MustCompile(`^[a-zA-Z0-9_-]{8,80}$`)

type RescheduleProposal struct {
	ID               string `json:"id"`
	ProposerUserID   string `json:"proposerUserId"`
	ExpectedOptionID string `json:"expectedOptionId"`
	Status           string `json:"status"`
}

type RescheduleCommand struct {
	Action           string    `json:"action"`
	ProposalID       string    `json:"proposalId"`
	ExpectedOptionID string    `json:"expectedOptionId"`
	StartAt          time.Time `json:"startAt"`
}

type RescheduleStore interface {
	Reschedule(context.Context, string, string, RescheduleCommand) error
}

// ApplyReschedule performs the state transition on a transaction-local copy.
// Acceptance still requires the store's transactional conflict/availability checks.
func ApplyReschedule(value *CoordinationRequest, actor string, command RescheduleCommand, now time.Time) error {
	if actor == "" || (actor != value.RequesterUserID && actor != value.TargetUserID) {
		return ErrNotFound
	}
	if value.Status != Accepted || !proposalIDPattern.MatchString(command.ProposalID) {
		return ErrRescheduleInvalid
	}
	p := value.RescheduleProposal
	if command.Action == "propose" {
		if p != nil && p.ID == command.ProposalID {
			for _, option := range value.Options {
				if option.ID == p.ID && option.StartAt != nil && option.StartAt.Equal(command.StartAt) && p.ProposerUserID == actor && p.ExpectedOptionID == command.ExpectedOptionID && p.Status == "proposed" {
					return ErrRescheduleRepeated
				}
			}
			return ErrRescheduleInvalid
		}
		if (p != nil && p.Status == "proposed") || len(value.Options) >= 100 {
			return ErrRescheduleInvalid
		}
		if err := ValidateConfirmedCancellation(*value, actor, command.ExpectedOptionID, now); err != nil {
			return ErrRescheduleInvalid
		}
		for _, option := range value.Options {
			if option.ID == command.ProposalID {
				return ErrRescheduleInvalid
			}
		}
		start := command.StartAt.UTC()
		end := start.Add(time.Duration(value.DurationMinutes) * time.Minute)
		option := Option{ID: command.ProposalID, RequestID: value.ID, Type: OptionMeeting, StartAt: &start, EndAt: &end, CreatedAt: now}
		if command.StartAt.IsZero() || value.DurationMinutes <= 0 || option.Validate() != nil || !start.After(now) || end.After(value.DeadlineAt) {
			return ErrRescheduleInvalid
		}
		for _, old := range value.Options {
			if old.ID == value.AcceptedOptionID && old.StartAt != nil && old.StartAt.Equal(start) {
				return ErrRescheduleInvalid
			}
		}
		value.Options = append(value.Options, option)
		value.RescheduleProposal = &RescheduleProposal{ID: command.ProposalID, ProposerUserID: actor, ExpectedOptionID: command.ExpectedOptionID, Status: "proposed"}
		value.UpdatedAt = now
		return nil
	}
	if p == nil || p.ID != command.ProposalID || p.ExpectedOptionID != command.ExpectedOptionID {
		return ErrRescheduleInvalid
	}
	terminal := ""
	switch command.Action {
	case "accept":
		terminal = "accepted"
	case "decline":
		terminal = "declined"
	case "withdraw":
		terminal = "withdrawn"
	default:
		return ErrRescheduleInvalid
	}
	if (command.Action == "withdraw") != (actor == p.ProposerUserID) {
		return ErrRescheduleInvalid
	}
	if p.Status == terminal {
		return ErrRescheduleRepeated
	}
	if p.Status != "proposed" || value.AcceptedOptionID != p.ExpectedOptionID {
		return ErrRescheduleInvalid
	}
	if command.Action == "accept" {
		if err := ValidateConfirmedCancellation(*value, actor, p.ExpectedOptionID, now); err != nil {
			return ErrRescheduleInvalid
		}
		if _, err := ConfirmableMeeting(*value, p.ID, now); err != nil {
			return err
		}
		value.AcceptedOptionID = p.ID
	}
	copy := *p
	copy.Status = terminal
	value.RescheduleProposal = &copy
	value.UpdatedAt = now
	return nil
}

func RescheduleEffects(value CoordinationRequest, actor, action string, now time.Time) (notification.Notification, audit.Event) {
	id := fmt.Sprintf("reschedule-%x", sha256.Sum256([]byte(value.ID+":"+value.RescheduleProposal.ID+":"+action)))
	recipient := value.TargetUserID
	if recipient == actor {
		recipient = value.RequesterUserID
	}
	message := map[string]string{"propose": "日時変更の提案が届きました。承認までは元の予約が維持されます。", "accept": "日時変更が確定しました。外部カレンダーの予定は手動で更新してください。", "decline": "日時変更の提案が辞退されました。元の予約は維持されています。", "withdraw": "日時変更の提案が撤回されました。元の予約は維持されています。"}[action]
	auditAction := map[string]audit.Action{"propose": audit.RescheduleProposed, "accept": audit.RescheduleAccepted, "decline": audit.RescheduleDeclined, "withdraw": audit.RescheduleWithdrawn}[action]
	return notification.Notification{ID: id, UserID: recipient, Type: notification.RequestChanged, RequestID: value.ID, Message: message, CreatedAt: now}, audit.Event{ID: id, OrganizationID: value.OrganizationID, ActorUserID: actor, Action: auditAction, ResourceType: "request", ResourceID: value.ID, CreatedAt: now}
}
