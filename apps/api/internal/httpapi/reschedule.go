package httpapi

import (
	"encoding/json"
	"errors"
	coordinationrequest "github.com/negotiable-calendar/negotiable-calendar/apps/api/internal/request"
	"io"
	"net/http"
)

func (api *API) rescheduleMeeting(response http.ResponseWriter, request *http.Request) {
	response.Header().Set("Cache-Control", "no-store")
	actor := request.Header.Get("X-Demo-User-ID")
	org := request.Header.Get("X-Organization-ID")
	if actor == "" || org == "" {
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
	store, ok := api.requests.(coordinationrequest.ScopedRescheduleStore)
	if !ok {
		writeJSON(response, 503, map[string]string{"error": "rescheduling unavailable"})
		return
	}
	reader, ok := api.requests.(coordinationrequest.ScopedGetStore)
	if !ok {
		writeJSON(response, 503, map[string]string{"error": "rescheduling result verification unavailable"})
		return
	}
	id := request.PathValue("requestId")
	err := store.RescheduleInOrganization(request.Context(), id, actor, org, input)
	if err == nil || errors.Is(err, coordinationrequest.ErrRescheduleRepeated) {
		replayed := errors.Is(err, coordinationrequest.ErrRescheduleRepeated)
		value, err := reader.GetInOrganization(request.Context(), id, actor, org)
		if err != nil || value.ID != id || value.OrganizationID != org || (value.RequesterUserID != actor && value.TargetUserID != actor) {
			writeJSON(response, 503, map[string]string{"error": "refresh request to confirm result"})
			return
		}
		if replayed {
			response.Header().Set("Idempotency-Replayed", "true")
		}
		writeJSON(response, 200, value)
		return
	}
	if errors.Is(err, coordinationrequest.ErrNotFound) {
		writeJSON(response, 404, map[string]string{"error": "coordination request not found"})
		return
	}
	if errors.Is(err, coordinationrequest.ErrCreationForbidden) {
		writeJSON(response, http.StatusForbidden, map[string]string{"error": "current membership required", "code": "membership_required"})
		return
	}
	for _, conflict := range []error{coordinationrequest.ErrRescheduleInvalid, coordinationrequest.ErrCandidateExpired, coordinationrequest.ErrCandidateInvalid, coordinationrequest.ErrBookingConflict, coordinationrequest.ErrAvailabilityChanged} {
		if errors.Is(err, conflict) {
			writeJSON(response, 409, map[string]string{"error": "reschedule cannot be applied; refresh and retry", "code": conflict.Error()})
			return
		}
	}
	api.logger.Error("reschedule meeting", "failure_code", safeFailureCode(err))
	writeJSON(response, 500, map[string]string{"error": "unable to reschedule meeting"})
}
