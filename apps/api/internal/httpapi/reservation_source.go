package httpapi

import (
	"context"
	coord "github.com/negotiable-calendar/negotiable-calendar/apps/api/internal/request"
)

func (api *API) candidateReservations(ctx context.Context, requester, target string) ([]coord.ReservedRange, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	store, ok := api.requests.(coord.ReservationStore)
	if !ok {
		return nil, coord.ErrReservationUnavailable
	}
	ctx, cancel := context.WithTimeout(ctx, coord.ReservationReadTimeout)
	defer cancel()
	values, err := store.LoadConfirmedRanges(ctx, requester, target)
	// A successful adapter response cannot extend the evidence budget.
	if interrupted := ctx.Err(); interrupted != nil {
		return nil, interrupted
	}
	return values, err
}
