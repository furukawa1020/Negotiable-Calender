package request

import (
	"fmt"
	"strings"
	"time"
)

type Type string
type Status string
type SyncPreference string
type Priority string
type OptionType string

const (
	QuickQuestion Type = "quick_question"
	Meeting       Type = "meeting"
	Review        Type = "review"
	Approval      Type = "approval"
	Decision      Type = "decision"
	AsyncResponse Type = "async_response"
	UrgentContact Type = "urgent_contact"

	Pending   Status = "pending"
	Suggested Status = "suggested"
	Accepted  Status = "accepted"
	Declined  Status = "declined"
	Delegated Status = "delegated"
	Cancelled Status = "cancelled"
	Expired   Status = "expired"
	Completed Status = "completed"

	SyncPreferred  SyncPreference = "sync"
	AsyncPreferred SyncPreference = "async"
	Either         SyncPreference = "either"

	PriorityLow    Priority = "low"
	PriorityNormal Priority = "normal"
	PriorityHigh   Priority = "high"
	PriorityUrgent Priority = "urgent"

	OptionMeeting  OptionType = "meeting"
	OptionAsync    OptionType = "async"
	OptionDelegate OptionType = "delegate"
	OptionDecline  OptionType = "decline"
)

func (v Type) Valid() bool {
	switch v {
	case QuickQuestion, Meeting, Review, Approval, Decision, AsyncResponse, UrgentContact:
		return true
	default:
		return false
	}
}

func (v Status) Valid() bool {
	switch v {
	case Pending, Suggested, Accepted, Declined, Delegated, Cancelled, Expired, Completed:
		return true
	default:
		return false
	}
}

func (v SyncPreference) Valid() bool {
	switch v {
	case SyncPreferred, AsyncPreferred, Either:
		return true
	default:
		return false
	}
}

func (v Priority) Valid() bool {
	switch v {
	case PriorityLow, PriorityNormal, PriorityHigh, PriorityUrgent:
		return true
	default:
		return false
	}
}

func (v OptionType) Valid() bool {
	switch v {
	case OptionMeeting, OptionAsync, OptionDelegate, OptionDecline:
		return true
	default:
		return false
	}
}

type CoordinationRequest struct {
	ID              string
	OrganizationID  string
	RequesterUserID string
	TargetUserID    string
	Type            Type
	Title           string
	DurationMinutes int
	DeadlineAt      time.Time
	SyncPreference  SyncPreference
	Priority        Priority
	Status          Status
	Options         []Option
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

func (r CoordinationRequest) Validate() error {
	if strings.TrimSpace(r.ID) == "" {
		return fmt.Errorf("request id is required")
	}
	if strings.TrimSpace(r.OrganizationID) == "" {
		return fmt.Errorf("organization id is required")
	}
	if strings.TrimSpace(r.RequesterUserID) == "" {
		return fmt.Errorf("requester id is required")
	}
	if strings.TrimSpace(r.TargetUserID) == "" {
		return fmt.Errorf("target id is required")
	}
	if r.RequesterUserID == r.TargetUserID {
		return fmt.Errorf("requester and target must differ")
	}
	if !r.Type.Valid() {
		return fmt.Errorf("invalid request type")
	}
	if strings.TrimSpace(r.Title) == "" {
		return fmt.Errorf("request title is required")
	}
	if r.DurationMinutes <= 0 {
		return fmt.Errorf("duration must be positive")
	}
	if err := validUTC("deadline_at", r.DeadlineAt); err != nil {
		return err
	}
	if !r.SyncPreference.Valid() {
		return fmt.Errorf("invalid sync preference")
	}
	if !r.Priority.Valid() {
		return fmt.Errorf("invalid priority")
	}
	if !r.Status.Valid() {
		return fmt.Errorf("invalid status")
	}
	for _, option := range r.Options {
		if err := option.Validate(); err != nil {
			return err
		}
	}
	return validTimes(r.CreatedAt, r.UpdatedAt)
}

type Option struct {
	ID             string
	RequestID      string
	Type           OptionType
	StartAt        *time.Time
	EndAt          *time.Time
	ResponseBy     *time.Time
	DelegateUserID string
	Score          int
	CreatedAt      time.Time
}

func (o Option) Validate() error {
	if strings.TrimSpace(o.ID) == "" {
		return fmt.Errorf("option id is required")
	}
	if strings.TrimSpace(o.RequestID) == "" {
		return fmt.Errorf("option request id is required")
	}
	if !o.Type.Valid() {
		return fmt.Errorf("invalid option type")
	}
	if o.Type == OptionMeeting {
		if o.StartAt == nil {
			return fmt.Errorf("meeting start is required")
		}
		if o.EndAt == nil {
			return fmt.Errorf("meeting end is required")
		}
		if err := validRange(*o.StartAt, *o.EndAt); err != nil {
			return err
		}
	}
	if o.Type == OptionDelegate {
		if strings.TrimSpace(o.DelegateUserID) == "" {
			return fmt.Errorf("delegate user is required")
		}
	}
	return validUTC("option created_at", o.CreatedAt)
}

func validTimes(createdAt, updatedAt time.Time) error {
	if err := validUTC("created_at", createdAt); err != nil {
		return err
	}
	if err := validUTC("updated_at", updatedAt); err != nil {
		return err
	}
	if updatedAt.Before(createdAt) {
		return fmt.Errorf("updated_at before created_at")
	}
	return nil
}

func validRange(startAt, endAt time.Time) error {
	if err := validUTC("start_at", startAt); err != nil {
		return err
	}
	if err := validUTC("end_at", endAt); err != nil {
		return err
	}
	if !endAt.After(startAt) {
		return fmt.Errorf("end_at must be after start_at")
	}
	return nil
}

func validUTC(name string, value time.Time) error {
	if value.IsZero() {
		return fmt.Errorf("%s is required", name)
	}
	if value.Location() != time.UTC {
		return fmt.Errorf("%s must be UTC", name)
	}
	return nil
}
