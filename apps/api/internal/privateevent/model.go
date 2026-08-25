package privateevent

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

var ErrSerializationForbidden = errors.New("private events cannot be serialized")

type BusyStatus string
type Visibility string

const (
	Busy        BusyStatus = "busy"
	Free        BusyStatus = "free"
	Tentative   BusyStatus = "tentative"
	BusyUnknown BusyStatus = "unknown"

	VisibilityDefault      Visibility = "default"
	VisibilityPublic       Visibility = "public"
	VisibilityPrivate      Visibility = "private"
	VisibilityConfidential Visibility = "confidential"
)

func (v BusyStatus) Valid() bool {
	switch v {
	case Busy, Free, Tentative, BusyUnknown:
		return true
	default:
		return false
	}
}

func (v Visibility) Valid() bool {
	switch v {
	case VisibilityDefault, VisibilityPublic, VisibilityPrivate, VisibilityConfidential:
		return true
	default:
		return false
	}
}

type PrivateEvent struct {
	ID                   string
	UserID               string
	ProviderEventID      string
	StartAt              time.Time
	EndAt                time.Time
	TitleEncrypted       []byte
	DescriptionEncrypted []byte
	LocationEncrypted    []byte
	AttendeesEncrypted   []byte
	CalendarID           string
	BusyStatus           BusyStatus
	Visibility           Visibility
	CreatedAt            time.Time
	UpdatedAt            time.Time
	DeletedAt            *time.Time
}

func (e PrivateEvent) Validate() error {
	if strings.TrimSpace(e.ID) == "" {
		return fmt.Errorf("private event id is required")
	}
	if strings.TrimSpace(e.UserID) == "" {
		return fmt.Errorf("private event user id is required")
	}
	if strings.TrimSpace(e.ProviderEventID) == "" {
		return fmt.Errorf("provider event id is required")
	}
	if strings.TrimSpace(e.CalendarID) == "" {
		return fmt.Errorf("calendar id is required")
	}
	if err := validRange(e.StartAt, e.EndAt); err != nil {
		return err
	}
	if !e.BusyStatus.Valid() {
		return fmt.Errorf("invalid busy status")
	}
	if !e.Visibility.Valid() {
		return fmt.Errorf("invalid visibility")
	}
	if err := validUTC("created_at", e.CreatedAt); err != nil {
		return err
	}
	if err := validUTC("updated_at", e.UpdatedAt); err != nil {
		return err
	}
	if e.DeletedAt != nil {
		if err := validUTC("deleted_at", *e.DeletedAt); err != nil {
			return err
		}
	}
	return nil
}

func (PrivateEvent) MarshalJSON() ([]byte, error) {
	return nil, ErrSerializationForbidden
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
