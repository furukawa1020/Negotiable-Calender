package request

import (
	"context"
	"errors"
	"time"
)

const ReservationRoleLimit = 5000
const ReservationReadTimeout = 10 * time.Second

var ErrReservationUnavailable = errors.New("reservation source unavailable")

// ReservationStore returns only occupied intervals for candidate generation.
// Implementations read both participants/roles from one snapshot, include all
// workspaces and malformed accepted records, and discard all data on overflow.
// This read is not a reservation; final approval still checks atomically.
type ReservationStore interface {
	LoadConfirmedRanges(context.Context, string, string) ([]ReservedRange, error)
}

// ReservationRanges validates selected-option identity before reducing stored
// records to time ranges. It must not turn a damaged accepted record into free time.
func ReservationRanges(values []CoordinationRequest) ([]ReservedRange, error) {
	for _, value := range values {
		if value.ID == "" || value.Status != Accepted || value.AcceptedOptionID == "" {
			return nil, ErrAvailabilityChanged
		}
		matches := 0
		for _, option := range value.Options {
			if option.ID != value.AcceptedOptionID {
				continue
			}
			matches++
			if option.RequestID != value.ID || !option.Type.Valid() {
				return nil, ErrAvailabilityChanged
			}
		}
		if matches != 1 {
			return nil, ErrAvailabilityChanged
		}
	}
	return ConfirmedRanges(values)
}
