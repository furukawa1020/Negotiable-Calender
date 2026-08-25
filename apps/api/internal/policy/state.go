package policy

import "fmt"

type Availability string
type Interruptibility string
type Requestability string
type Reschedulability string

const (
	Available   Availability = "available"
	Limited     Availability = "limited"
	Unavailable Availability = "unavailable"
	Unknown     Availability = "unknown"

	InterruptOpen   Interruptibility = "open"
	InterruptNormal Interruptibility = "normal"
	UrgentOnly      Interruptibility = "urgent_only"
	DoNotInterrupt  Interruptibility = "do_not_interrupt"

	RequestOpen   Requestability = "open"
	AsyncOnly     Requestability = "async_only"
	RequestLater  Requestability = "later"
	RequestClosed Requestability = "closed"

	RescheduleHigh   Reschedulability = "high"
	RescheduleMedium Reschedulability = "medium"
	RescheduleLow    Reschedulability = "low"
	RescheduleFixed  Reschedulability = "fixed"
)

func (v Availability) Valid() bool {
	switch v {
	case Available, Limited, Unavailable, Unknown:
		return true
	default:
		return false
	}
}

func (v Interruptibility) Valid() bool {
	switch v {
	case InterruptOpen, InterruptNormal, UrgentOnly, DoNotInterrupt:
		return true
	default:
		return false
	}
}

func (v Requestability) Valid() bool {
	switch v {
	case RequestOpen, AsyncOnly, RequestLater, RequestClosed:
		return true
	default:
		return false
	}
}

func (v Reschedulability) Valid() bool {
	switch v {
	case RescheduleHigh, RescheduleMedium, RescheduleLow, RescheduleFixed:
		return true
	default:
		return false
	}
}

type InteractionState struct {
	Availability     Availability     `json:"availability"`
	Interruptibility Interruptibility `json:"interruptibility"`
	Requestability   Requestability   `json:"requestability"`
	Reschedulability Reschedulability `json:"reschedulability"`
}

func (s InteractionState) Validate() error {
	if !s.Availability.Valid() {
		return fmt.Errorf("invalid availability")
	}
	if !s.Interruptibility.Valid() {
		return fmt.Errorf("invalid interruptibility")
	}
	if !s.Requestability.Valid() {
		return fmt.Errorf("invalid requestability")
	}
	if !s.Reschedulability.Valid() {
		return fmt.Errorf("invalid reschedulability")
	}
	return nil
}
