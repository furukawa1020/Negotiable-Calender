package httpapi

import (
	"errors"
	"net/http"

	coord "github.com/negotiable-calendar/negotiable-calendar/apps/api/internal/request"
)

func (api *API) proposeMeeting(response http.ResponseWriter, request *http.Request, input suggestCoordinationRequestInput) bool {
	store, ok := api.requests.(coord.CounterproposalStore)
	if !ok {
		return false
	}
	id, actor, org := request.PathValue("requestId"), request.Header.Get("X-Demo-User-ID"), request.Header.Get("X-Organization-ID")
	if actor == "" || org == "" {
		writeJSON(response, 401, map[string]string{"error": "request identity is required"})
		return true
	}
	if !coord.ValidHandoffID(id) {
		writeJSON(response, 400, map[string]string{"error": "invalid request id"})
		return true
	}
	option, replay, err := store.ProposeMeeting(request.Context(), id, actor, org, input.StartAt, input.EndAt)
	if err != nil {
		writeProposalError(response, err)
		return true
	}
	status := http.StatusCreated
	if replay {
		status = http.StatusOK
		response.Header().Set("Idempotency-Replayed", "true")
	}
	writeJSON(response, status, option)
	return true
}

func writeProposalError(response http.ResponseWriter, err error) {
	status, code := 503, "proposal_unavailable"
	switch {
	case errors.Is(err, coord.ErrNotFound):
		status, code = 404, "not_found"
	case errors.Is(err, coord.ErrCreationForbidden):
		status, code = 403, "membership_required"
	case errors.Is(err, coord.ErrCandidateInvalid):
		status, code = 422, coord.ErrCandidateInvalid.Error()
	case errors.Is(err, coord.ErrCandidateExpired):
		status, code = 409, coord.ErrCandidateExpired.Error()
	case errors.Is(err, coord.ErrProposalConflict):
		status, code = 409, coord.ErrProposalConflict.Error()
	case errors.Is(err, coord.ErrProposalLimit):
		status, code = 409, coord.ErrProposalLimit.Error()
	}
	writeJSON(response, status, map[string]string{"error": "unable to confirm proposal; retry the same times after an unknown result", "code": code})
}
