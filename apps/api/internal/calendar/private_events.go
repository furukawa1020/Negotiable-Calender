package calendar

import (
	"context"
	"errors"
	"net/http"
	"time"
)

const maxPrivateEventRange = 45 * 24 * time.Hour

type privateEventProvider interface {
	ListPrivateEvents(context.Context, string, time.Time, time.Time) ([]PrivateEventView, error)
}

type userTimezoneStore interface {
	UserTimezone(context.Context, string) (string, error)
}

func (handler *Handler) privateEvents(response http.ResponseWriter, request *http.Request) {
	userID, ok := handler.userID(response, request)
	if !ok {
		return
	}
	from, to, ok := parsePrivateEventRange(response, request)
	if !ok {
		return
	}
	provider, ok := handler.provider.(privateEventProvider)
	if !ok || !handler.provider.Configured() || handler.cipher == nil {
		writeJSON(response, http.StatusServiceUnavailable, map[string]string{"error": "private calendar is not configured"})
		return
	}
	connection, err := handler.store.GetConnection(request.Context(), userID)
	if errors.Is(err, ErrNotFound) {
		writeJSON(response, http.StatusConflict, map[string]string{"error": "calendar connection required"})
		return
	}
	if err != nil {
		handler.logger.Error("load self calendar connection", "failure_code", "connection_load_failed")
		writeJSON(response, http.StatusInternalServerError, map[string]string{"error": "unable to load private calendar"})
		return
	}
	refreshToken, err := handler.cipher.Decrypt(connection.RefreshTokenCipher)
	if err != nil {
		_ = handler.store.MarkReconnectRequired(request.Context(), userID)
		handler.logger.Warn("decrypt self calendar grant", "failure_code", "grant_decrypt_failed")
		writeJSON(response, http.StatusConflict, map[string]string{"error": "calendar reconnect required"})
		return
	}
	ctx, cancel := context.WithTimeout(request.Context(), 20*time.Second)
	defer cancel()
	tokens, err := handler.provider.Refresh(ctx, refreshToken)
	if err != nil {
		if errors.Is(err, ErrReconnectRequired) {
			_ = handler.store.MarkReconnectRequired(request.Context(), userID)
			writeJSON(response, http.StatusConflict, map[string]string{"error": "calendar reconnect required"})
			return
		}
		handler.logger.Warn("refresh self calendar access", "failure_code", "token_refresh_failed")
		writeJSON(response, http.StatusBadGateway, map[string]string{"error": "unable to read private calendar"})
		return
	}
	events, err := provider.ListPrivateEvents(ctx, tokens.AccessToken, from, to)
	if err != nil {
		if errors.Is(err, ErrReconnectRequired) {
			_ = handler.store.MarkReconnectRequired(request.Context(), userID)
			writeJSON(response, http.StatusConflict, map[string]string{"error": "calendar reconnect required"})
			return
		}
		handler.logger.Warn("read self calendar", "failure_code", "provider_read_failed")
		writeJSON(response, http.StatusBadGateway, map[string]string{"error": "unable to read private calendar"})
		return
	}
	timezone := "UTC"
	if store, supported := handler.store.(userTimezoneStore); supported {
		if value, timezoneErr := store.UserTimezone(request.Context(), userID); timezoneErr == nil && value != "" {
			timezone = value
		}
	}
	response.Header().Set("Cache-Control", "no-store")
	writeJSON(response, http.StatusOK, map[string]any{
		"userId": userID, "timezone": timezone, "from": from, "to": to, "events": events,
	})
}

func parsePrivateEventRange(response http.ResponseWriter, request *http.Request) (time.Time, time.Time, bool) {
	rawFrom, rawTo := request.URL.Query().Get("from"), request.URL.Query().Get("to")
	if rawFrom == "" || rawTo == "" {
		writeJSON(response, http.StatusBadRequest, map[string]string{"error": "from and to are required"})
		return time.Time{}, time.Time{}, false
	}
	from, fromErr := time.Parse(time.RFC3339, rawFrom)
	to, toErr := time.Parse(time.RFC3339, rawTo)
	if fromErr != nil || toErr != nil {
		writeJSON(response, http.StatusBadRequest, map[string]string{"error": "from and to must be RFC3339 timestamps"})
		return time.Time{}, time.Time{}, false
	}
	from, to = from.UTC(), to.UTC()
	if !to.After(from) {
		writeJSON(response, http.StatusUnprocessableEntity, map[string]string{"error": "to must be after from"})
		return time.Time{}, time.Time{}, false
	}
	if to.Sub(from) > maxPrivateEventRange {
		writeJSON(response, http.StatusUnprocessableEntity, map[string]string{"error": "private calendar range cannot exceed 45 days"})
		return time.Time{}, time.Time{}, false
	}
	return from, to, true
}

func (store *PostgresStore) UserTimezone(ctx context.Context, userID string) (string, error) {
	var timezone string
	if err := store.database.QueryRowContext(ctx, "SELECT timezone FROM users WHERE id=$1", userID).Scan(&timezone); err != nil {
		return "", err
	}
	return timezone, nil
}
