package httpapi

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/negotiable-calendar/negotiable-calendar/apps/api/internal/organization"
	"github.com/negotiable-calendar/negotiable-calendar/apps/api/internal/policy"
	"github.com/negotiable-calendar/negotiable-calendar/apps/api/internal/projection"
	coordinationrequest "github.com/negotiable-calendar/negotiable-calendar/apps/api/internal/request"
)

type databasePinger interface {
	PingContext(context.Context) error
}

type API struct {
	database      databasePinger
	policies      policy.Store
	projections   projection.Store
	organizations organization.Store
	requests      coordinationrequest.Store
	webOrigin     string
	logger        *slog.Logger
}

func New(database databasePinger, policies policy.Store, projections projection.Store, organizations organization.Store, requests coordinationrequest.Store, webOrigin string, logger *slog.Logger) http.Handler {
	api := &API{database: database, policies: policies, projections: projections, organizations: organizations, requests: requests, webOrigin: webOrigin, logger: logger}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", api.health)
	mux.HandleFunc("GET /readyz", api.ready)
	mux.HandleFunc("GET /api/v1/status", api.status)
	mux.HandleFunc("GET /api/v1/users/{userId}/sharing-policy", api.getSharingPolicy)
	mux.HandleFunc("PUT /api/v1/users/{userId}/sharing-policy", api.putSharingPolicy)
	mux.HandleFunc("GET /api/v1/users/{userId}/manual-overrides", api.listManualOverrides)
	mux.HandleFunc("POST /api/v1/users/{userId}/manual-overrides", api.createManualOverride)
	mux.HandleFunc("GET /api/v1/people/{userId}/projection", api.getPublicProjection)
	mux.HandleFunc("GET /api/v1/people", api.listPeople)
	mux.HandleFunc("GET /api/v1/requests", api.listCoordinationRequests)
	mux.HandleFunc("POST /api/v1/requests", api.createCoordinationRequest)
	mux.HandleFunc("POST /api/v1/requests/{requestId}/accept", api.acceptCoordinationRequest)
	mux.HandleFunc("POST /api/v1/requests/{requestId}/suggest", api.suggestCoordinationRequest)
	mux.HandleFunc("POST /api/v1/requests/{requestId}/delegate", api.delegateCoordinationRequest)
	mux.HandleFunc("POST /api/v1/requests/{requestId}/decline", api.declineCoordinationRequest)
	mux.HandleFunc("OPTIONS /api/v1/{path...}", api.options)
	return api.middleware(mux)
}

type delegateCoordinationRequestInput struct {
	DelegateUserID string `json:"delegateUserId"`
}

func (api *API) delegateCoordinationRequest(response http.ResponseWriter, request *http.Request) {
	targetUserID := request.Header.Get("X-Demo-User-ID")
	if targetUserID == "" {
		writeJSON(response, http.StatusUnauthorized, map[string]string{"error": "request identity is required"})
		return
	}
	var input delegateCoordinationRequestInput
	decoder := json.NewDecoder(http.MaxBytesReader(response, request.Body, 1<<20))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil || input.DelegateUserID == "" {
		writeJSON(response, http.StatusBadRequest, map[string]string{"error": "delegateUserId is required"})
		return
	}
	option := coordinationrequest.Option{
		ID: newID("option"), RequestID: request.PathValue("requestId"),
		Type: coordinationrequest.OptionDelegate, DelegateUserID: input.DelegateUserID,
		CreatedAt: time.Now().UTC(),
	}
	err := api.requests.Delegate(request.Context(), option.RequestID, targetUserID, option)
	if errors.Is(err, coordinationrequest.ErrNotFound) {
		writeJSON(response, http.StatusConflict, map[string]string{"error": "request cannot be delegated"})
		return
	}
	if err != nil {
		api.logger.Error("delegate coordination request", "error", err)
		writeJSON(response, http.StatusInternalServerError, map[string]string{"error": "unable to delegate request"})
		return
	}
	writeJSON(response, http.StatusOK, map[string]any{
		"id": option.RequestID, "status": coordinationrequest.Delegated,
		"delegatedUserId": option.DelegateUserID,
	})
}

type suggestCoordinationRequestInput struct {
	StartAt time.Time `json:"startAt"`
	EndAt   time.Time `json:"endAt"`
}

func (api *API) suggestCoordinationRequest(response http.ResponseWriter, request *http.Request) {
	targetUserID := request.Header.Get("X-Demo-User-ID")
	if targetUserID == "" {
		writeJSON(response, http.StatusUnauthorized, map[string]string{"error": "request identity is required"})
		return
	}
	var input suggestCoordinationRequestInput
	decoder := json.NewDecoder(http.MaxBytesReader(response, request.Body, 1<<20))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil {
		writeJSON(response, http.StatusBadRequest, map[string]string{"error": "invalid JSON body"})
		return
	}
	now := time.Now().UTC()
	startAt, endAt := input.StartAt.UTC(), input.EndAt.UTC()
	option := coordinationrequest.Option{
		ID: newID("option"), RequestID: request.PathValue("requestId"),
		Type: coordinationrequest.OptionMeeting, StartAt: &startAt, EndAt: &endAt, CreatedAt: now,
	}
	if !startAt.After(now) {
		writeJSON(response, http.StatusUnprocessableEntity, map[string]string{"error": "suggested time must be in the future"})
		return
	}
	if err := option.Validate(); err != nil {
		writeJSON(response, http.StatusUnprocessableEntity, map[string]string{"error": fmt.Sprintf("invalid suggested option: %s", err)})
		return
	}
	err := api.requests.Suggest(request.Context(), option.RequestID, targetUserID, option)
	if errors.Is(err, coordinationrequest.ErrNotFound) {
		writeJSON(response, http.StatusConflict, map[string]string{"error": "request cannot be updated"})
		return
	}
	if err != nil {
		api.logger.Error("suggest coordination option", "error", err)
		writeJSON(response, http.StatusInternalServerError, map[string]string{"error": "unable to suggest option"})
		return
	}
	writeJSON(response, http.StatusCreated, option)
}

type acceptCoordinationRequestInput struct {
	OptionID string `json:"optionId"`
}

func (api *API) acceptCoordinationRequest(response http.ResponseWriter, request *http.Request) {
	targetUserID := request.Header.Get("X-Demo-User-ID")
	if targetUserID == "" {
		writeJSON(response, http.StatusUnauthorized, map[string]string{"error": "request identity is required"})
		return
	}
	var input acceptCoordinationRequestInput
	decoder := json.NewDecoder(http.MaxBytesReader(response, request.Body, 1<<20))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil || input.OptionID == "" {
		writeJSON(response, http.StatusBadRequest, map[string]string{"error": "optionId is required"})
		return
	}
	api.respondToCoordinationRequest(response, request, targetUserID, coordinationrequest.Accepted, input.OptionID)
}

func (api *API) declineCoordinationRequest(response http.ResponseWriter, request *http.Request) {
	targetUserID := request.Header.Get("X-Demo-User-ID")
	if targetUserID == "" {
		writeJSON(response, http.StatusUnauthorized, map[string]string{"error": "request identity is required"})
		return
	}
	api.respondToCoordinationRequest(response, request, targetUserID, coordinationrequest.Declined, "")
}

func (api *API) respondToCoordinationRequest(response http.ResponseWriter, request *http.Request, targetUserID string, status coordinationrequest.Status, optionID string) {
	err := api.requests.Respond(request.Context(), request.PathValue("requestId"), targetUserID, status, optionID)
	if errors.Is(err, coordinationrequest.ErrNotFound) {
		writeJSON(response, http.StatusConflict, map[string]string{"error": "request cannot be updated"})
		return
	}
	if err != nil {
		api.logger.Error("respond to coordination request", "error", err)
		writeJSON(response, http.StatusInternalServerError, map[string]string{"error": "unable to update request"})
		return
	}
	writeJSON(response, http.StatusOK, map[string]any{"id": request.PathValue("requestId"), "status": status, "acceptedOptionId": optionID})
}

func (api *API) listCoordinationRequests(response http.ResponseWriter, request *http.Request) {
	targetUserID := request.Header.Get("X-Demo-User-ID")
	if targetUserID == "" {
		writeJSON(response, http.StatusUnauthorized, map[string]string{"error": "request identity is required"})
		return
	}
	values, err := api.requests.ListForTarget(request.Context(), targetUserID)
	if err != nil {
		api.logger.Error("list coordination requests", "error", err)
		writeJSON(response, http.StatusInternalServerError, map[string]string{"error": "unable to load requests"})
		return
	}
	if values == nil {
		values = []coordinationrequest.CoordinationRequest{}
	}
	writeJSON(response, http.StatusOK, map[string]any{"requests": values})
}

type createCoordinationRequestInput struct {
	TargetUserID    string                             `json:"targetUserId"`
	Type            coordinationrequest.Type           `json:"type"`
	Title           string                             `json:"title"`
	DurationMinutes int                                `json:"durationMinutes"`
	DeadlineAt      time.Time                          `json:"deadlineAt"`
	SyncPreference  coordinationrequest.SyncPreference `json:"syncPreference"`
	Priority        coordinationrequest.Priority       `json:"priority"`
}

func (api *API) createCoordinationRequest(response http.ResponseWriter, request *http.Request) {
	requesterID := request.Header.Get("X-Demo-User-ID")
	organizationID := request.Header.Get("X-Organization-ID")
	if requesterID == "" || organizationID == "" {
		writeJSON(response, http.StatusUnauthorized, map[string]string{"error": "request identity is required"})
		return
	}
	var input createCoordinationRequestInput
	decoder := json.NewDecoder(http.MaxBytesReader(response, request.Body, 1<<20))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil {
		writeJSON(response, http.StatusBadRequest, map[string]string{"error": "invalid JSON body"})
		return
	}
	now := time.Now().UTC()
	value := coordinationrequest.CoordinationRequest{
		ID: newID("request"), OrganizationID: organizationID,
		RequesterUserID: requesterID, TargetUserID: input.TargetUserID,
		Type: input.Type, Title: input.Title, DurationMinutes: input.DurationMinutes,
		DeadlineAt: input.DeadlineAt.UTC(), SyncPreference: input.SyncPreference,
		Priority: input.Priority, Status: coordinationrequest.Pending,
		Options: []coordinationrequest.Option{}, CreatedAt: now, UpdatedAt: now,
	}
	if err := value.Validate(); err != nil {
		writeJSON(response, http.StatusUnprocessableEntity, map[string]string{"error": fmt.Sprintf("invalid coordination request: %s", err)})
		return
	}
	publicProjections, err := api.projections.List(request.Context(), value.TargetUserID, now, value.DeadlineAt)
	if err != nil {
		api.logger.Error("load projections for request candidates", "error", err)
		writeJSON(response, http.StatusInternalServerError, map[string]string{"error": "unable to generate request options"})
		return
	}
	options, err := coordinationrequest.GenerateCandidates(coordinationrequest.CandidateInput{
		Request: value, Projections: publicProjections, Now: now,
	})
	if err != nil {
		api.logger.Error("generate request candidates", "error", err)
		writeJSON(response, http.StatusInternalServerError, map[string]string{"error": "unable to generate request options"})
		return
	}
	value.Options = options
	value.Status = coordinationrequest.Suggested
	if err := api.requests.Create(request.Context(), value); err != nil {
		api.logger.Error("create coordination request", "error", err)
		writeJSON(response, http.StatusInternalServerError, map[string]string{"error": "unable to create request"})
		return
	}
	writeJSON(response, http.StatusCreated, value)
}

func (api *API) listPeople(response http.ResponseWriter, request *http.Request) {
	organizationID := request.URL.Query().Get("organizationId")
	if organizationID == "" {
		writeJSON(response, http.StatusBadRequest, map[string]string{"error": "organizationId is required"})
		return
	}
	people, err := api.organizations.ListPeople(request.Context(), organizationID)
	if err != nil {
		api.logger.Error("list organization people", "error", err)
		writeJSON(response, http.StatusInternalServerError, map[string]string{"error": "unable to load people"})
		return
	}
	writeJSON(response, http.StatusOK, map[string]any{"people": people})
}

func (api *API) getPublicProjection(response http.ResponseWriter, request *http.Request) {
	timezone := request.URL.Query().Get("timezone")
	if timezone == "" {
		timezone = "Asia/Tokyo"
	}
	location, err := time.LoadLocation(timezone)
	if err != nil {
		writeJSON(response, http.StatusBadRequest, map[string]string{"error": "invalid timezone"})
		return
	}
	localNow := time.Now().In(location)
	localStart := time.Date(localNow.Year(), localNow.Month(), localNow.Day(), 0, 0, 0, 0, location)
	from := localStart.UTC()
	to := localStart.Add(24 * time.Hour).UTC()
	if raw := request.URL.Query().Get("from"); raw != "" {
		from, err = time.Parse(time.RFC3339, raw)
		if err != nil {
			writeJSON(response, http.StatusBadRequest, map[string]string{"error": "invalid from"})
			return
		}
		from = from.UTC()
	}
	if raw := request.URL.Query().Get("to"); raw != "" {
		to, err = time.Parse(time.RFC3339, raw)
		if err != nil {
			writeJSON(response, http.StatusBadRequest, map[string]string{"error": "invalid to"})
			return
		}
		to = to.UTC()
	}
	if !to.After(from) {
		writeJSON(response, http.StatusBadRequest, map[string]string{"error": "to must be after from"})
		return
	}
	view, err := api.projections.GetView(request.Context(), request.PathValue("userId"), timezone, from, to)
	if err != nil {
		api.logger.Error("get public projection", "error", err)
		writeJSON(response, http.StatusInternalServerError, map[string]string{"error": "unable to load projection"})
		return
	}
	writeJSON(response, http.StatusOK, view)
}

type createOverrideRequest struct {
	StartAt   time.Time               `json:"startAt"`
	EndAt     time.Time               `json:"endAt"`
	State     policy.InteractionState `json:"state"`
	ExpiresAt time.Time               `json:"expiresAt"`
}

func (api *API) listManualOverrides(response http.ResponseWriter, request *http.Request) {
	values, err := api.policies.ListActiveOverrides(request.Context(), request.PathValue("userId"), time.Now().UTC())
	if err != nil {
		api.logger.Error("list manual overrides", "error", err)
		writeJSON(response, http.StatusInternalServerError, map[string]string{"error": "unable to load manual overrides"})
		return
	}
	if values == nil {
		values = []policy.ManualOverride{}
	}
	writeJSON(response, http.StatusOK, map[string]any{"overrides": values})
}

func (api *API) createManualOverride(response http.ResponseWriter, request *http.Request) {
	var input createOverrideRequest
	decoder := json.NewDecoder(http.MaxBytesReader(response, request.Body, 1<<20))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil {
		writeJSON(response, http.StatusBadRequest, map[string]string{"error": "invalid JSON body"})
		return
	}
	now := time.Now().UTC()
	value := policy.ManualOverride{
		ID: newID("override"), UserID: request.PathValue("userId"),
		StartAt: input.StartAt, EndAt: input.EndAt, State: input.State,
		ExpiresAt: input.ExpiresAt, CreatedAt: now,
	}
	if err := value.Validate(); err != nil {
		writeJSON(response, http.StatusUnprocessableEntity, map[string]string{"error": fmt.Sprintf("invalid manual override: %s", err)})
		return
	}
	if err := api.policies.CreateOverride(request.Context(), value); err != nil {
		api.logger.Error("create manual override", "error", err)
		writeJSON(response, http.StatusInternalServerError, map[string]string{"error": "unable to create manual override"})
		return
	}
	writeJSON(response, http.StatusCreated, value)
}

func (api *API) health(response http.ResponseWriter, _ *http.Request) {
	writeJSON(response, http.StatusOK, map[string]string{"status": "ok"})
}

func (api *API) ready(response http.ResponseWriter, request *http.Request) {
	ctx, cancel := context.WithTimeout(request.Context(), 2*time.Second)
	defer cancel()
	if err := api.database.PingContext(ctx); err != nil {
		api.logger.Warn("database readiness check failed", "error", err)
		writeJSON(response, http.StatusServiceUnavailable, map[string]string{"status": "unavailable"})
		return
	}
	writeJSON(response, http.StatusOK, map[string]string{"status": "ready"})
}

func (api *API) status(response http.ResponseWriter, _ *http.Request) {
	writeJSON(response, http.StatusOK, map[string]string{
		"product":      "Negotiable Calendar",
		"privacy_mode": "fail_closed",
		"status":       "operational",
	})
}

func (api *API) options(response http.ResponseWriter, _ *http.Request) {
	response.WriteHeader(http.StatusNoContent)
}

type updatePolicyRequest struct {
	Default      policy.InteractionState `json:"default"`
	WorkingHours []policy.WorkingWindow  `json:"workingHours"`
	Rules        []updateRuleRequest     `json:"rules"`
}

type updateRuleRequest struct {
	ConditionType string                  `json:"conditionType"`
	Condition     json.RawMessage         `json:"condition"`
	State         policy.InteractionState `json:"state"`
	Priority      int                     `json:"priority"`
	Enabled       bool                    `json:"enabled"`
}

func (api *API) getSharingPolicy(response http.ResponseWriter, request *http.Request) {
	value, err := api.policies.Get(request.Context(), request.PathValue("userId"))
	if errors.Is(err, policy.ErrNotFound) {
		writeJSON(response, http.StatusNotFound, map[string]string{"error": "sharing policy not found"})
		return
	}
	if err != nil {
		api.logger.Error("get sharing policy", "error", err)
		writeJSON(response, http.StatusInternalServerError, map[string]string{"error": "unable to load sharing policy"})
		return
	}
	writeJSON(response, http.StatusOK, value)
}

func (api *API) putSharingPolicy(response http.ResponseWriter, request *http.Request) {
	var input updatePolicyRequest
	decoder := json.NewDecoder(http.MaxBytesReader(response, request.Body, 1<<20))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil {
		writeJSON(response, http.StatusBadRequest, map[string]string{"error": "invalid JSON body"})
		return
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		writeJSON(response, http.StatusBadRequest, map[string]string{"error": "request body must contain one JSON value"})
		return
	}

	userID := request.PathValue("userId")
	now := time.Now().UTC()
	value, err := api.policies.Get(request.Context(), userID)
	if errors.Is(err, policy.ErrNotFound) {
		value = policy.SharingPolicy{ID: newID("policy"), UserID: userID, CreatedAt: now}
	} else if err != nil {
		api.logger.Error("load sharing policy before update", "error", err)
		writeJSON(response, http.StatusInternalServerError, map[string]string{"error": "unable to update sharing policy"})
		return
	}
	value.Default = input.Default
	value.WorkingHours = input.WorkingHours
	value.Rules = make([]policy.Rule, 0, len(input.Rules))
	value.UpdatedAt = now
	for _, inputRule := range input.Rules {
		value.Rules = append(value.Rules, policy.Rule{
			ID: newID("rule"), PolicyID: value.ID,
			ConditionType: inputRule.ConditionType, Condition: inputRule.Condition,
			State: inputRule.State, Priority: inputRule.Priority, Enabled: inputRule.Enabled,
		})
	}
	if err := value.Validate(); err != nil {
		writeJSON(response, http.StatusUnprocessableEntity, map[string]string{"error": fmt.Sprintf("invalid sharing policy: %s", err)})
		return
	}
	if err := api.policies.Upsert(request.Context(), value); err != nil {
		api.logger.Error("update sharing policy", "error", err)
		writeJSON(response, http.StatusInternalServerError, map[string]string{"error": "unable to update sharing policy"})
		return
	}
	writeJSON(response, http.StatusOK, value)
}

func newID(prefix string) string {
	bytes := make([]byte, 16)
	if _, err := rand.Read(bytes); err != nil {
		panic("secure random source unavailable")
	}
	return fmt.Sprintf("%s-%x", prefix, bytes)
}

func (api *API) middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		response.Header().Set("X-Content-Type-Options", "nosniff")
		response.Header().Set("Referrer-Policy", "no-referrer")
		response.Header().Set("Cache-Control", "no-store")

		if api.webOrigin != "" && request.Header.Get("Origin") == api.webOrigin {
			response.Header().Set("Access-Control-Allow-Origin", api.webOrigin)
			response.Header().Set("Access-Control-Allow-Headers", "Content-Type, X-Demo-User-ID, X-Organization-ID")
			response.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, OPTIONS")
			response.Header().Set("Vary", "Origin")
		}

		next.ServeHTTP(response, request)
	})
}

func writeJSON(response http.ResponseWriter, status int, body any) {
	response.Header().Set("Content-Type", "application/json; charset=utf-8")
	response.WriteHeader(status)
	_ = json.NewEncoder(response).Encode(body)
}
