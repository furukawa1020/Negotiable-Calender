package request

import (
	"errors"
	"sort"
	"time"

	"github.com/negotiable-calendar/negotiable-calendar/apps/api/internal/policy"
	"github.com/negotiable-calendar/negotiable-calendar/apps/api/internal/projection"
)

var (
	ErrCandidateInvalid    = errors.New("candidate_invalid")
	ErrCandidateExpired    = errors.New("candidate_expired")
	ErrAvailabilityChanged = errors.New("availability_changed")
	ErrBookingConflict     = errors.New("booking_conflict")
	ErrAlreadyAccepted     = errors.New("already_accepted")
)

func ConfirmableMeeting(value CoordinationRequest, optionID string, now time.Time) (Option, error) {
	for _, option := range value.Options {
		if option.ID != optionID {
			continue
		}
		if option.RequestID != value.ID || option.Type != OptionMeeting || option.Validate() != nil {
			return Option{}, ErrCandidateInvalid
		}
		if !value.DeadlineAt.After(now) || !option.StartAt.After(now) || option.EndAt.After(value.DeadlineAt) {
			return Option{}, ErrCandidateExpired
		}
		return option, nil
	}
	return Option{}, ErrCandidateInvalid
}

// Require complete, unexpired coverage. Unknown, gaps, or contradictory closed
// segments are not evidence that a meeting can be accepted.
func ValidateMeetingAvailability(userID string, option Option, values []projection.ScheduleProjection, now time.Time) error {
	if option.StartAt == nil || option.EndAt == nil {
		return ErrCandidateInvalid
	}
	ranges := []ReservedRange{}
	for _, value := range values {
		if !value.StartAt.Before(*option.EndAt) || !option.StartAt.Before(value.EndAt) {
			continue
		}
		if value.UserID != userID || value.Validate() != nil || !value.ExpiresAt.After(now) || value.GeneratedAt.After(now) || value.State.Requestability != policy.RequestOpen || (value.State.Availability != policy.Available && value.State.Availability != policy.Limited) {
			return ErrAvailabilityChanged
		}
		ranges = append(ranges, ReservedRange{StartAt: value.StartAt, EndAt: value.EndAt})
	}
	sort.Slice(ranges, func(i, j int) bool { return ranges[i].StartAt.Before(ranges[j].StartAt) })
	covered := *option.StartAt
	for _, value := range ranges {
		if value.StartAt.After(covered) {
			return ErrAvailabilityChanged
		}
		if value.EndAt.After(covered) {
			covered = value.EndAt
		}
	}
	if covered.Before(*option.EndAt) {
		return ErrAvailabilityChanged
	}
	return nil
}

func ConflictsWithMeeting(option Option, other CoordinationRequest) bool {
	if other.Status != Accepted {
		return false
	}
	for _, reserved := range other.Options {
		if reserved.ID != other.AcceptedOptionID {
			continue
		}
		if reserved.Type != OptionMeeting {
			return false
		}
		if reserved.StartAt == nil || reserved.EndAt == nil || !reserved.EndAt.After(*reserved.StartAt) {
			return true
		}
		return option.StartAt.Before(*reserved.EndAt) && reserved.StartAt.Before(*option.EndAt)
	}
	return true // Corrupt accepted records fail closed until repaired.
}
