package calendar

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/negotiable-calendar/negotiable-calendar/apps/api/internal/auth"
)

const (
	flowCookieName = "negotiable_calendar_flow"
)

type HandlerConfig struct {
	WebOrigin            string
	SecureCookies        bool
	FlowTTL              time.Duration
	SyncPast, SyncFuture time.Duration
}

type Handler struct {
	next      http.Handler
	store     Store
	provider  Provider
	cipher    *TokenCipher
	projector Projector
	config    HandlerConfig
	logger    *slog.Logger
	now       func() time.Time
}

type Projector interface {
	Rebuild(context.Context, string, time.Time, time.Time, time.Time) error
}

func NewHandler(next http.Handler, store Store, provider Provider, cipher *TokenCipher, projector Projector, config HandlerConfig, logger *slog.Logger) *Handler {
	if config.FlowTTL == 0 {
		config.FlowTTL = 10 * time.Minute
	}
	if config.SyncPast == 0 {
		config.SyncPast = 30 * 24 * time.Hour
	}
	if config.SyncFuture == 0 {
		config.SyncFuture = 90 * 24 * time.Hour
	}
	return &Handler{next: next, store: store, provider: provider, cipher: cipher, projector: projector, config: config, logger: logger, now: time.Now}
}

func (handler *Handler) ServeHTTP(response http.ResponseWriter, request *http.Request) {
	switch request.Method + " " + request.URL.Path {
	case http.MethodGet + " /api/v1/calendar/google/connect":
		handler.connect(response, request)
	case http.MethodGet + " /api/v1/calendar/google/callback":
		handler.callback(response, request)
	case http.MethodGet + " /api/v1/me/private-events":
		handler.privateEvents(response, request)
	case http.MethodGet + " /api/v1/calendar/connection":
		handler.status(response, request)
	case http.MethodPost + " /api/v1/calendar/sync":
		handler.sync(response, request)
	case http.MethodDelete + " /api/v1/calendar/connection":
		handler.disconnect(response, request)
	default:
		handler.next.ServeHTTP(response, request)
	}
}

func (handler *Handler) userID(response http.ResponseWriter, request *http.Request) (string, bool) {
	userID := request.Header.Get(auth.AuthenticatedUserHeader)
	if userID == "" {
		writeJSON(response, http.StatusUnauthorized, map[string]string{"error": "authenticated session required"})
		return "", false
	}
	return userID, true
}

func (handler *Handler) connect(response http.ResponseWriter, request *http.Request) {
	userID, ok := handler.userID(response, request)
	if !ok {
		return
	}
	if !handler.provider.Configured() || handler.cipher == nil {
		writeJSON(response, http.StatusServiceUnavailable, map[string]string{"error": "google calendar is not configured"})
		return
	}
	state, verifier := randomToken(32), randomToken(32)
	now := handler.now().UTC()
	flow := Flow{ID: newID("calendar-flow"), UserID: userID, StateHash: hashToken(state), CodeVerifier: verifier, CreatedAt: now, ExpiresAt: now.Add(handler.config.FlowTTL)}
	if err := handler.store.CreateFlow(request.Context(), flow); err != nil {
		handler.logger.Error("create calendar oauth flow", "error", err)
		writeJSON(response, 500, map[string]string{"error": "unable to connect calendar"})
		return
	}
	http.SetCookie(response, &http.Cookie{Name: flowCookieName, Value: flow.ID, Path: "/api/v1/calendar/google/callback", HttpOnly: true, Secure: handler.config.SecureCookies, SameSite: http.SameSiteLaxMode, MaxAge: int(handler.config.FlowTTL.Seconds())})
	sum := sha256.Sum256([]byte(verifier))
	http.Redirect(response, request, handler.provider.AuthorizationURL(state, base64.RawURLEncoding.EncodeToString(sum[:])), http.StatusFound)
}

func (handler *Handler) callback(response http.ResponseWriter, request *http.Request) {
	userID, ok := handler.userID(response, request)
	if !ok {
		return
	}
	if !handler.provider.Configured() || handler.cipher == nil {
		writeJSON(response, http.StatusServiceUnavailable, map[string]string{"error": "google calendar is not configured"})
		return
	}
	cookie, err := request.Cookie(flowCookieName)
	state, code := request.URL.Query().Get("state"), request.URL.Query().Get("code")
	if err != nil || state == "" || code == "" || request.URL.Query().Get("error") != "" {
		writeJSON(response, 400, map[string]string{"error": "invalid calendar oauth callback"})
		return
	}
	flow, err := handler.store.ConsumeFlow(request.Context(), cookie.Value, userID, hashToken(state), handler.now().UTC())
	if errors.Is(err, ErrNotFound) {
		writeJSON(response, 400, map[string]string{"error": "calendar oauth state is invalid or expired"})
		return
	}
	if err != nil {
		handler.logger.Error("consume calendar oauth flow", "error", err)
		writeJSON(response, 500, map[string]string{"error": "unable to connect calendar"})
		return
	}
	tokens, err := handler.provider.Exchange(request.Context(), code, flow.CodeVerifier)
	if err != nil {
		handler.logger.Warn("calendar oauth exchange rejected", "error", err)
		writeJSON(response, 502, map[string]string{"error": "calendar permission could not be verified"})
		return
	}
	if tokens.RefreshToken == "" || !contains(tokens.Scopes, CalendarReadonlyScope) {
		writeJSON(response, 403, map[string]string{"error": "calendar readonly permission and offline access are required"})
		return
	}
	encrypted, err := handler.cipher.Encrypt(tokens.RefreshToken)
	if err != nil {
		handler.logger.Error("encrypt calendar token", "error", err)
		writeJSON(response, 500, map[string]string{"error": "unable to store calendar permission"})
		return
	}
	connection := Connection{UserID: userID, RefreshTokenCipher: encrypted, GrantedScopes: tokens.Scopes, ConnectedAt: handler.now().UTC()}
	if err := handler.store.SaveConnection(request.Context(), connection); err != nil {
		handler.logger.Error("save calendar connection", "error", err)
		writeJSON(response, 500, map[string]string{"error": "unable to store calendar permission"})
		return
	}
	expireCookie(response, flowCookieName, "/api/v1/calendar/google/callback", handler.config.SecureCookies)
	http.Redirect(response, request, strings.TrimRight(handler.config.WebOrigin, "/")+"/?calendar=connected", http.StatusFound)
}

func (handler *Handler) status(response http.ResponseWriter, request *http.Request) {
	userID, ok := handler.userID(response, request)
	if !ok {
		return
	}
	value, err := handler.store.GetConnection(request.Context(), userID)
	if errors.Is(err, ErrNotFound) {
		writeJSON(response, 200, map[string]any{"connected": false})
		return
	}
	if err != nil {
		handler.logger.Error("get calendar connection", "error", err)
		writeJSON(response, 500, map[string]string{"error": "unable to load calendar connection"})
		return
	}
	writeJSON(response, 200, map[string]any{"connected": true, "connection": value})
}

func (handler *Handler) sync(response http.ResponseWriter, request *http.Request) {
	userID, ok := handler.userID(response, request)
	if !ok {
		return
	}
	result, err := handler.SyncUser(request.Context(), userID)
	if err != nil {
		var failure *SyncFailure
		if errors.As(err, &failure) {
			handler.logger.Warn("manual calendar sync incomplete", "failure_code", failure.Code)
			writeJSON(response, failure.Status, map[string]string{"error": failure.PublicText})
			return
		}
		handler.logger.Error("manual calendar sync failed", "failure_code", "unexpected")
		writeJSON(response, http.StatusInternalServerError, map[string]string{"error": "unable to sync calendar"})
		return
	}
	writeJSON(response, http.StatusOK, map[string]any{
		"synced": true, "busySpanCount": result.BusySpanCount, "lastSyncedAt": result.LastSyncedAt,
	})
}

func (handler *Handler) disconnect(response http.ResponseWriter, request *http.Request) {
	userID, ok := handler.userID(response, request)
	if !ok {
		return
	}
	if err := handler.store.DeleteConnection(request.Context(), userID); err != nil {
		handler.logger.Error("delete calendar connection", "error", err)
		writeJSON(response, 500, map[string]string{"error": "unable to disconnect calendar"})
		return
	}
	response.WriteHeader(http.StatusNoContent)
}

func randomToken(size int) string {
	value := make([]byte, size)
	if _, err := rand.Read(value); err != nil {
		panic("secure random source unavailable")
	}
	return base64.RawURLEncoding.EncodeToString(value)
}
func hashToken(value string) []byte { sum := sha256.Sum256([]byte(value)); return sum[:] }
func newID(prefix string) string    { return prefix + "-" + randomToken(16) }
func contains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
func expireCookie(response http.ResponseWriter, name, path string, secure bool) {
	http.SetCookie(response, &http.Cookie{Name: name, Value: "", Path: path, HttpOnly: true, Secure: secure, SameSite: http.SameSiteLaxMode, MaxAge: -1, Expires: time.Unix(1, 0)})
}
func writeJSON(response http.ResponseWriter, status int, body any) {
	response.Header().Set("Content-Type", "application/json; charset=utf-8")
	response.WriteHeader(status)
	_ = json.NewEncoder(response).Encode(body)
}
