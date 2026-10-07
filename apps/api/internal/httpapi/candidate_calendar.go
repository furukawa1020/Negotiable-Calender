package httpapi

import (
	"context"
	"errors"
	cal "github.com/negotiable-calendar/negotiable-calendar/apps/api/internal/calendar"
	"github.com/negotiable-calendar/negotiable-calendar/apps/api/internal/projection"
	coord "github.com/negotiable-calendar/negotiable-calendar/apps/api/internal/request"
	"net/http"
	"time"
)

func (api *API) generateRequestCandidates(ctx context.Context, value coord.CoordinationRequest, projections []projection.ScheduleProjection, reserved []coord.ReservedRange, now time.Time) ([]coord.Option, error) {
	input := coord.CandidateInput{Request: value, Projections: projections, Reserved: reserved, Now: now}
	if value.SyncPreference != coord.AsyncPreferred {
		store, ok := api.requests.(coord.RequesterCalendarStore)
		if !ok {
			return nil, coord.ErrReservationUnavailable
		}
		readCtx, cancel := context.WithTimeout(ctx, coord.ReservationReadTimeout)
		snapshot, err := store.LoadRequesterCalendar(readCtx, value.RequesterUserID)
		cancel()
		if errors.Is(err, cal.ErrSourceUnavailable) {
			return nil, coord.ErrAvailabilityChanged
		}
		if err != nil {
			return nil, err
		}
		if snapshot.Validate(time.Now().UTC()) != nil {
			return nil, coord.ErrAvailabilityChanged
		}
		input.RequesterCalendar = &snapshot
	}
	options, err := coord.GenerateCandidatesContext(ctx, input)
	if err != nil {
		return nil, err
	}
	if input.RequesterCalendar != nil && input.RequesterCalendar.Validate(time.Now().UTC()) != nil {
		return nil, coord.ErrAvailabilityChanged
	}
	return options, nil
}

func writeCandidateCalendarError(response http.ResponseWriter, err error) {
	if errors.Is(err, coord.ErrAvailabilityChanged) || errors.Is(err, cal.ErrSourceUnavailable) {
		writeJSON(response, 409, map[string]string{"error": "unable to verify calendar availability; sync and retry", "code": "availability_changed"})
		return
	}
	writeJSON(response, 503, map[string]string{"error": "unable to generate request options"})
}
