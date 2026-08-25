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

	"github.com/negotiable-calendar/negotiable-calendar/apps/api/internal/policy"
)

type databasePinger interface {
	PingContext(context.Context) error
}

type API struct {
	database  databasePinger
	policies  policy.Store
	webOrigin string
	logger    *slog.Logger
}

func New(database databasePinger, policies policy.Store, webOrigin string, logger *slog.Logger) http.Handler {
	api := &API{database: database, policies: policies, webOrigin: webOrigin, logger: logger}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", api.health)
	mux.HandleFunc("GET /readyz", api.ready)
	mux.HandleFunc("GET /api/v1/status", api.status)
	mux.HandleFunc("GET /api/v1/users/{userId}/sharing-policy", api.getSharingPolicy)
	mux.HandleFunc("PUT /api/v1/users/{userId}/sharing-policy", api.putSharingPolicy)
	mux.HandleFunc("GET /api/v1/users/{userId}/manual-overrides", api.listManualOverrides)
	mux.HandleFunc("POST /api/v1/users/{userId}/manual-overrides", api.createManualOverride)
	mux.HandleFunc("OPTIONS /api/v1/{path...}", api.options)
	return api.middleware(mux)
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
			response.Header().Set("Access-Control-Allow-Headers", "Content-Type")
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
