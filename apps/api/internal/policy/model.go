package policy

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"
)

const (
	MaxWorkingWindows = 14
	MaxPolicyRules = 50
	MaxCalendarIDRunes = 200
	MaxRulePriority = 1000
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
	if len(p.WorkingHours) > MaxWorkingWindows {
		return fmt.Errorf("too many working windows")
	}
	for index, window := range p.WorkingHours {
		if err := window.Validate(); err != nil {
			return err
		}
		for otherIndex := index + 1; otherIndex < len(p.WorkingHours); otherIndex++ {
			other := p.WorkingHours[otherIndex]
			if window.Weekday == other.Weekday && window.StartMinute < other.EndMinute && other.StartMinute < window.EndMinute {
				return fmt.Errorf("working windows must not overlap")
			}
		}
	}
	if len(p.Rules) > MaxPolicyRules {
		return fmt.Errorf("too many policy rules")
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
	if r.Priority < 0 || r.Priority > MaxRulePriority {
		return fmt.Errorf("rule priority must be between 0 and %d", MaxRulePriority)
	}
	if err := validateRuleCondition(r.ConditionType, r.Condition); err != nil {
		return err
	}
	return r.State.Validate()
}

type calendarRuleCondition struct {
	CalendarID string `json:"calendarId"`
}

type eventRuleCondition struct {
	BusyStatus string `json:"busyStatus"`
}

func validateRuleCondition(conditionType string, raw json.RawMessage) error {
	if !json.Valid(raw) {
		return fmt.Errorf("condition must be valid JSON")
	}
	switch conditionType {
	case "organization":
		var value map[string]json.RawMessage
		if err := decodeStrictCondition(raw, &value); err != nil || len(value) != 0 {
			return fmt.Errorf("organization condition must be an empty object")
		}
		return nil
	case "calendar":
		var value calendarRuleCondition
		if err := decodeStrictCondition(raw, &value); err != nil {
			return fmt.Errorf("invalid calendar condition")
		}
		value.CalendarID = strings.TrimSpace(value.CalendarID)
		if value.CalendarID == "" || len([]rune(value.CalendarID)) > MaxCalendarIDRunes {
			return fmt.Errorf("calendarId is required and must be at most %d characters", MaxCalendarIDRunes)
		}
		return nil
	case "event":
		var value eventRuleCondition
		if err := decodeStrictCondition(raw, &value); err != nil {
			return fmt.Errorf("invalid event condition")
		}
		if value.BusyStatus != "busy" && value.BusyStatus != "free" {
			return fmt.Errorf("busyStatus must be busy or free")
		}
		return nil
	default:
		return fmt.Errorf("invalid condition type")
	}
}

func decodeStrictCondition(raw json.RawMessage, destination any) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return err
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return fmt.Errorf("condition must contain one JSON value")
	}
	return nil
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
