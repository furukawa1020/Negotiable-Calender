package httpapi

import (
	"context"
	"errors"
	coord "github.com/negotiable-calendar/negotiable-calendar/apps/api/internal/request"
	"net/http"
)

func (api *API) loadWorkspaceRequest(response http.ResponseWriter, request *http.Request) (coord.CoordinationRequest, bool) {
	response.Header().Set("Cache-Control", "no-store")
	id, actor, org := request.PathValue("requestId"), request.Header.Get("X-Demo-User-ID"), request.Header.Get("X-Organization-ID")
	fail := func(status int, message string) (coord.CoordinationRequest, bool) {
		writeJSON(response, status, map[string]string{"error": message})
		return coord.CoordinationRequest{}, false
	}
	if actor == "" || org == "" {
		return fail(401, "request identity is required")
	}
	if !coord.ValidHandoffID(id) {
		return fail(404, "request not found")
	}
	store, ok := api.requests.(coord.ScopedGetStore)
	if !ok {
		return fail(503, "workspace request reading unavailable")
	}
	value, err := readWorkspace(request.Context(), func(ctx context.Context) (coord.CoordinationRequest, error) {
		return store.GetInOrganization(ctx, id, actor, org)
	})
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		api.logger.Error("load workspace request interrupted", "failure_code", safeFailureCode(err))
		return fail(503, "workspace request read interrupted")
	}
	if errors.Is(err, coord.ErrCreationForbidden) {
		return fail(403, "current workspace membership required")
	}
	if errors.Is(err, coord.ErrNotFound) {
		return fail(404, "request not found")
	}
	if err != nil {
		api.logger.Error("load workspace request", "failure_code", safeFailureCode(err))
		return fail(500, "unable to load request")
	}
	if value.ID != id || value.OrganizationID != org || (value.RequesterUserID != actor && value.TargetUserID != actor) {
		return fail(404, "request not found")
	}
	return value, true
}
