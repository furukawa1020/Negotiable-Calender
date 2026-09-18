package httpapi

import (
	"encoding/json"
	"errors"
	coordinationrequest "github.com/negotiable-calendar/negotiable-calendar/apps/api/internal/request"
	"io"
	"net/http"
)

func (api *API) rescheduleMeeting(response http.ResponseWriter, request *http.Request) {
	actor := request.Header.Get("X-Demo-User-ID")
	if actor == "" {
		writeJSON(response, 401, map[string]string{"error": "request identity is required"})
		return
	}
	var input coordinationrequest.RescheduleCommand
	decoder := json.NewDecoder(http.MaxBytesReader(response, request.Body, 8<<10))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil || input.ProposalID == "" || input.ExpectedOptionID == "" || decoder.Decode(&struct{}{}) != io.EOF {
		writeJSON(response, 400, map[string]string{"error": "valid proposal and expected option are required"})
		return
	}
	if input.Action != "propose" && input.Action != "accept" && input.Action != "decline" && input.Action != "withdraw" {
		writeJSON(response, 400, map[string]string{"error": "invalid reschedule action"})
		return
	}
	store, ok := api.requests.(coordinationrequest.RescheduleStore)
	if !ok {
		writeJSON(response, 503, map[string]string{"error": "rescheduling unavailable"})
		return
	}
	id := request.PathValue("requestId")
	err := store.Reschedule(request.Context(), id, actor, input)
	if err == nil || errors.Is(err, coordinationrequest.ErrRescheduleRepeated) {
		value, err := api.requests.GetForUser(request.Context(), id, actor)
		if err != nil {
			writeJSON(response, 503, map[string]string{"error": "refresh request to confirm result"})
			return
		}
		writeJSON(response, 200, value)
		return
	}
	if errors.Is(err, coordinationrequest.ErrNotFound) {
		writeJSON(response, 404, map[string]string{"error": "coordination request not found"})
		return
	}
	for _, conflict := range []error{coordinationrequest.ErrRescheduleInvalid, coordinationrequest.ErrCandidateExpired, coordinationrequest.ErrCandidateInvalid, coordinationrequest.ErrBookingConflict, coordinationrequest.ErrAvailabilityChanged} {
		if errors.Is(err, conflict) {
			writeJSON(response, 409, map[string]string{"error": "reschedule cannot be applied; refresh and retry", "code": conflict.Error()})
			return
		}
	}
	api.logger.Error("reschedule meeting", "error", err)
	writeJSON(response, 500, map[string]string{"error": "unable to reschedule meeting"})
}
