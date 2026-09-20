package httpapi

import (
	"errors"
	coord "github.com/negotiable-calendar/negotiable-calendar/apps/api/internal/request"
	"net/http"
	"time"
)

func (api *API) handoffRequest(response http.ResponseWriter, request *http.Request, recipient string) bool {
	store, ok := api.requests.(coord.HandoffStore)
	if !ok {
		return false
	}
	id, actor, org := request.PathValue("requestId"), request.Header.Get("X-Demo-User-ID"), request.Header.Get("X-Organization-ID")
	if actor == "" || org == "" {
		writeJSON(response, 401, map[string]string{"error": "request identity is required"})
		return true
	}
	if !coord.ValidHandoffID(id) || !coord.ValidHandoffID(recipient) {
		writeJSON(response, 400, map[string]string{"error": "invalid request or recipient"})
		return true
	}
	value, replay, err := store.InspectHandoff(request.Context(), id, actor, org, recipient)
	if err != nil {
		writeHandoffError(response, err)
		return true
	}
	if !replay {
		now := time.Now().UTC()
		value.TargetUserID = recipient
		value.Options = nil
		public, err := api.projections.List(request.Context(), recipient, now, value.DeadlineAt)
		if err != nil {
			writeHandoffError(response, err)
			return true
		}
		var reserved []coord.ReservedRange
		for _, user := range []string{value.RequesterUserID, recipient} {
			requests, err := api.requests.ListForUser(request.Context(), user)
			if err != nil {
				writeHandoffError(response, err)
				return true
			}
			ranges, err := coord.ConfirmedRanges(requests)
			if err != nil {
				writeHandoffError(response, err)
				return true
			}
			reserved = append(reserved, ranges...)
		}
		options, err := coord.GenerateCandidates(coord.CandidateInput{Request: value, Projections: public, Reserved: reserved, Now: now})
		if err != nil {
			writeHandoffError(response, err)
			return true
		}
		replay, err = store.Handoff(request.Context(), id, actor, org, recipient, options)
		if err != nil {
			writeHandoffError(response, err)
			return true
		}
	}
	if replay {
		response.Header().Set("Idempotency-Replayed", "true")
	}
	// This is an acknowledgement, not disclosure of the new recipient's state.
	writeJSON(response, 200, map[string]any{"id": id, "status": "delegated", "delegatedUserId": recipient, "handedOff": true})
	return true
}

func writeHandoffError(response http.ResponseWriter, err error) {
	status, message, code := 503, "unable to confirm handoff; retry the same recipient", "handoff_unavailable"
	switch {
	case errors.Is(err, coord.ErrNotFound):
		status, message, code = 404, "coordination request not found", "not_found"
	case errors.Is(err, coord.ErrCreationForbidden):
		status, message, code = 403, "current membership required", "membership_required"
	case errors.Is(err, coord.ErrHandoffConflict):
		status, message, code = 409, "request cannot be handed off to this recipient", coord.ErrHandoffConflict.Error()
	case errors.Is(err, coord.ErrHandoffExpired):
		status, message, code = 409, "request deadline has passed", coord.ErrHandoffExpired.Error()
	}
	writeJSON(response, status, map[string]string{"error": message, "code": code})
}
