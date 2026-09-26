package httpapi

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"

	coordinationrequest "github.com/negotiable-calendar/negotiable-calendar/apps/api/internal/request"
)

func (api *API) cancelConfirmedMeeting(response http.ResponseWriter, request *http.Request) {
	actor := request.Header.Get("X-Demo-User-ID")
	org := request.Header.Get("X-Organization-ID")
	if actor == "" || org == "" {
		writeJSON(response, http.StatusUnauthorized, map[string]string{"error": "request identity is required"})
		return
	}
	var input struct {
		OptionID string `json:"optionId"`
	}
	decoder := json.NewDecoder(http.MaxBytesReader(response, request.Body, 8<<10))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil || input.OptionID == "" || decoder.Decode(&struct{}{}) != io.EOF {
		writeJSON(response, http.StatusBadRequest, map[string]string{"error": "confirmed optionId is required"})
		return
	}
	store, ok := api.requests.(coordinationrequest.ScopedConfirmedLifecycleStore)
	if !ok {
		writeJSON(response, http.StatusServiceUnavailable, map[string]string{"error": "confirmed cancellation unavailable"})
		return
	}
	id := request.PathValue("requestId")
	err := store.CancelConfirmedInOrganization(request.Context(), id, actor, org, input.OptionID)
	switch {
	case err == nil, errors.Is(err, coordinationrequest.ErrAlreadyCancelled):
		// Effects are persisted by the store, never replayed by HTTP retries.
		writeJSON(response, http.StatusOK, map[string]string{"id": id, "status": string(coordinationrequest.Cancelled)})
	case errors.Is(err, coordinationrequest.ErrNotFound):
		writeJSON(response, http.StatusNotFound, map[string]string{"error": "coordination request not found"})
	case errors.Is(err, coordinationrequest.ErrCreationForbidden):
		writeJSON(response, http.StatusForbidden, map[string]string{"error": "current membership required", "code": "membership_required"})
	case errors.Is(err, coordinationrequest.ErrCancellationInvalid), errors.Is(err, coordinationrequest.ErrBookingConflict):
		writeJSON(response, http.StatusConflict, map[string]string{"error": "meeting cannot be cancelled; refresh and retry", "code": err.Error()})
	default:
		api.logger.Error("cancel confirmed meeting", "error", err)
		writeJSON(response, http.StatusInternalServerError, map[string]string{"error": "unable to cancel meeting"})
	}
}
