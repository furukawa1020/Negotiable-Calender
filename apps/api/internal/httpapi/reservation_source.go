package httpapi

import (
	"context"
	coord "github.com/negotiable-calendar/negotiable-calendar/apps/api/internal/request"
)

func (api *API) candidateReservations(ctx context.Context, requester, target string) ([]coord.ReservedRange, error) {
	store, ok := api.requests.(coord.ReservationStore)
	if !ok {
		return nil, coord.ErrReservationUnavailable
	}
	ctx, cancel := context.WithTimeout(ctx, coord.ReservationReadTimeout)
	defer cancel()
	return store.LoadConfirmedRanges(ctx, requester, target)
}
