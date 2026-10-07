package request

import (
	"context"
	cal "github.com/negotiable-calendar/negotiable-calendar/apps/api/internal/calendar"
)

// This private capability is mandatory for meeting candidate HTTP routes.
// Implementations read a bounded coherent source snapshot, never a public DTO.
type RequesterCalendarStore interface {
	LoadRequesterCalendar(context.Context, string) (cal.CandidateAvailability, error)
}
