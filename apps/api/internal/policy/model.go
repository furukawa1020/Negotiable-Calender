package policy

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

type WorkingWindow struct {
	Weekday     time.Weekday `json:"weekday"`
	StartMinute int          `json:"startMinute"`
	EndMinute   int          `json:"endMinute"`
}

func (w WorkingWindow) Validate() error {
	if w.Weekday < time.Sunday {
		return fmt.Errorf("invalid weekday")
	}
	if w.Weekday > time.Saturday {
		return fmt.Errorf("invalid weekday")
	}
	if w.StartMinute < 0 {
		return fmt.Errorf("invalid working start")
	}
	if w.EndMinute > 24*60 {
		return fmt.Errorf("invalid working end")
	}
	if w.EndMinute <= w.StartMinute {
		return fmt.Errorf("working window must end after start")
	}
	return nil
}

type SharingPolicy struct {
	ID           string           `json:"id"`
	UserID       string           `json:"userId"`
	Default      InteractionState `json:"default"`
	WorkingHours []WorkingWindow  `json:"workingHours"`
	Rules        []Rule           `json:"rules"`
	CreatedAt    time.Time        `json:"createdAt"`
	UpdatedAt    time.Time        `json:"updatedAt"`
}

func (p SharingPolicy) Validate() error {
	if strings.TrimSpace(p.ID) == "" {
		return fmt.Errorf("policy id is required")
	}
	if strings.TrimSpace(p.UserID) == "" {
		return fmt.Errorf("policy user id is required")
	}
	if err := p.Default.Validate(); err != nil {
		return fmt.Errorf("default state: %w", err)
	}
	for _, window := range p.WorkingHours {
		if err := window.Validate(); err != nil {
			return err
		}
	}
	for _, rule := range p.Rules {
		if err := rule.Validate(); err != nil {
			return err
		}
	}
	return validRecordTimes(p.CreatedAt, p.UpdatedAt)
}

type Rule struct {
	ID            string           `json:"id"`
	PolicyID      string           `json:"policyId"`
	ConditionType string           `json:"conditionType"`
	Condition     json.RawMessage  `json:"condition"`
	State         InteractionState `json:"state"`
	Priority      int              `json:"priority"`
	Enabled       bool             `json:"enabled"`
}

func (r Rule) Validate() error {
	if strings.TrimSpace(r.ID) == "" {
		return fmt.Errorf("rule id is required")
	}
	if strings.TrimSpace(r.PolicyID) == "" {
		return fmt.Errorf("rule policy id is required")
	}
	if strings.TrimSpace(r.ConditionType) == "" {
		return fmt.Errorf("condition type is required")
	}
	switch r.ConditionType {
	case "organization", "calendar", "event":
	default:
		return fmt.Errorf("invalid condition type")
	}
	if !json.Valid(r.Condition) {
		return fmt.Errorf("condition must be valid JSON")
	}
	return r.State.Validate()
}

type ManualOverride struct {
	ID        string           `json:"id"`
	UserID    string           `json:"userId"`
	StartAt   time.Time        `json:"startAt"`
	EndAt     time.Time        `json:"endAt"`
	State     InteractionState `json:"state"`
	ExpiresAt time.Time        `json:"expiresAt"`
	CreatedAt time.Time        `json:"createdAt"`
}

func (o ManualOverride) Validate() error {
	if strings.TrimSpace(o.ID) == "" {
		return fmt.Errorf("override id is required")
	}
	if strings.TrimSpace(o.UserID) == "" {
		return fmt.Errorf("override user id is required")
	}
	if err := validRange(o.StartAt, o.EndAt); err != nil {
		return err
	}
	if err := validUTC("expires_at", o.ExpiresAt); err != nil {
		return err
	}
	if err := validUTC("created_at", o.CreatedAt); err != nil {
		return err
	}
	if !o.ExpiresAt.After(o.CreatedAt) {
		return fmt.Errorf("expires_at must be after created_at")
	}
	return o.State.Validate()
}

func validRecordTimes(createdAt, updatedAt time.Time) error {
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
