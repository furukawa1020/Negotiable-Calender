package httpapi

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"time"
)

type databasePinger interface {
	PingContext(context.Context) error
}

type API struct {
	database  databasePinger
	webOrigin string
	logger    *slog.Logger
}

func New(database databasePinger, webOrigin string, logger *slog.Logger) http.Handler {
	api := &API{database: database, webOrigin: webOrigin, logger: logger}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", api.health)
	mux.HandleFunc("GET /readyz", api.ready)
	mux.HandleFunc("GET /api/v1/status", api.status)
	return api.middleware(mux)
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

func (api *API) middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		response.Header().Set("X-Content-Type-Options", "nosniff")
		response.Header().Set("Referrer-Policy", "no-referrer")
		response.Header().Set("Cache-Control", "no-store")

		if api.webOrigin != "" && request.Header.Get("Origin") == api.webOrigin {
			response.Header().Set("Access-Control-Allow-Origin", api.webOrigin)
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
