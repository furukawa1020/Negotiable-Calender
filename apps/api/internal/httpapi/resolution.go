package httpapi

import (
	"encoding/json"
	"errors"
	coordinationrequest "github.com/negotiable-calendar/negotiable-calendar/apps/api/internal/request"
	"io"
	"net/http"
	"strings"
)

func (api *API) resolveRequest(response http.ResponseWriter, request *http.Request, command coordinationrequest.ResolutionCommand) bool {
	store, ok := api.requests.(coordinationrequest.ResolutionStore)
	if !ok {
		return false
	} // Legacy test/custom stores retain their old contract.
	actor, organization := request.Header.Get("X-Demo-User-ID"), request.Header.Get("X-Organization-ID")
	if actor == "" || organization == "" {
		writeJSON(response, http.StatusUnauthorized, map[string]string{"error": "request identity is required"})
		return true
	}
	id := request.PathValue("requestId")
	if id == "" || len(id) > 256 || strings.ContainsAny(id, "/\\\r\n") {
		writeJSON(response, http.StatusNotFound, map[string]string{"error": "coordination request not found"})
		return true
	}
	replayed, err := store.ResolveRequest(request.Context(), id, actor, organization, command)
	if err != nil {
		status, message, code := http.StatusServiceUnavailable, "unable to confirm outcome; retry the same operation and message", "resolution_unavailable"
		switch {
		case errors.Is(err, coordinationrequest.ErrNotFound):
			status, message, code = http.StatusNotFound, "coordination request not found", "not_found"
		case errors.Is(err, coordinationrequest.ErrCreationForbidden):
			status, message, code = http.StatusForbidden, "current membership required", "membership_required"
		case errors.Is(err, coordinationrequest.ErrResolutionExpired):
			status, message, code = http.StatusConflict, "request deadline has passed", coordinationrequest.ErrResolutionExpired.Error()
		case errors.Is(err, coordinationrequest.ErrResolutionConflict):
			status, message, code = http.StatusConflict, "request state or response differs; refresh before continuing", coordinationrequest.ErrResolutionConflict.Error()
		}
		writeJSON(response, status, map[string]string{"error": message, "code": code})
		return true
	}
	if replayed {
		response.Header().Set("Idempotency-Replayed", "true")
	}
	writeJSON(response, http.StatusOK, map[string]any{"id": id, "status": command.Status, "asyncMessage": command.Message, "acceptedOptionId": command.ExpectedOptionID})
	return true
}

func (api *API) completeConfirmedMeeting(response http.ResponseWriter, request *http.Request) {
	response.Header().Set("Cache-Control", "no-store")
	if request.Header.Get("X-Demo-User-ID") == "" || request.Header.Get("X-Organization-ID") == "" {
		writeJSON(response, http.StatusUnauthorized, map[string]string{"error": "request identity is required"})
		return
	}
	var input struct {
		OptionID string `json:"optionId"`
	}
	decoder := json.NewDecoder(http.MaxBytesReader(response, request.Body, 8<<10))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil || strings.TrimSpace(input.OptionID) == "" || len(input.OptionID) > 256 || decoder.Decode(&struct{}{}) != io.EOF {
		writeJSON(response, http.StatusBadRequest, map[string]string{"error": "confirmed optionId is required"})
		return
	}
	if !api.resolveRequest(response, request, coordinationrequest.ResolutionCommand{Status: coordinationrequest.Completed, ExpectedOptionID: input.OptionID}) {
		writeJSON(response, http.StatusServiceUnavailable, map[string]string{"error": "meeting completion unavailable"})
	}
}
