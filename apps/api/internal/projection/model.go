package projection

import (
	"fmt"
	"strings"
	"time"
	_ "time/tzdata"

	"github.com/negotiable-calendar/negotiable-calendar/apps/api/internal/policy"
)

type ScheduleProjection struct {
	ID                     string
	UserID                 string
	StartAt                time.Time
	EndAt                  time.Time
	State                  policy.InteractionState
	ExpectedResponseBucket string
	GeneratedAt            time.Time
	ExpiresAt              time.Time
}

func (p ScheduleProjection) Validate() error {
	if strings.TrimSpace(p.ID) == "" {
		return fmt.Errorf("projection id is required")
	}
	if strings.TrimSpace(p.UserID) == "" {
		return fmt.Errorf("projection user id is required")
	}
	if err := validRange(p.StartAt, p.EndAt); err != nil {
		return err
	}
	if err := p.State.Validate(); err != nil {
		return err
	}
	if strings.TrimSpace(p.ExpectedResponseBucket) == "" {
		return fmt.Errorf("expected response bucket is required")
	}
	if err := validUTC("generated_at", p.GeneratedAt); err != nil {
		return err
	}
	if err := validUTC("expires_at", p.ExpiresAt); err != nil {
		return err
	}
	return nil
}

type Segment struct {
	StartAt                time.Time               `json:"startAt"`
	EndAt                  time.Time               `json:"endAt"`
	Availability           policy.Availability     `json:"availability"`
	Interruptibility       policy.Interruptibility `json:"interruptibility"`
	Requestability         policy.Requestability   `json:"requestability"`
	Reschedulability       policy.Reschedulability `json:"reschedulability"`
	ExpectedResponseBucket string                  `json:"expectedResponseBucket,omitempty"`
}

type View struct {
	UserID      string    `json:"userId"`
	Timezone    string    `json:"timezone"`
	Segments    []Segment `json:"segments"`
	GeneratedAt time.Time `json:"generatedAt"`
}

func NewView(userID, timezone string, projections []ScheduleProjection) (View, error) {
	if strings.TrimSpace(userID) == "" {
		return View{}, fmt.Errorf("user id is required")
	}
	if _, err := time.LoadLocation(timezone); err != nil {
		return View{}, fmt.Errorf("invalid timezone: %w", err)
	}
	view := View{UserID: userID, Timezone: timezone, Segments: make([]Segment, 0, len(projections))}
	for _, item := range projections {
		if err := item.Validate(); err != nil {
			return View{}, err
		}
		view.Segments = append(view.Segments, Segment{
			StartAt: item.StartAt, EndAt: item.EndAt,
			Availability:           item.State.Availability,
			Interruptibility:       item.State.Interruptibility,
			Requestability:         item.State.Requestability,
			Reschedulability:       item.State.Reschedulability,
			ExpectedResponseBucket: item.ExpectedResponseBucket,
		})
		if item.GeneratedAt.After(view.GeneratedAt) {
			view.GeneratedAt = item.GeneratedAt
		}
	}
	return view, nil
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
