package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"
)

const (
	flowCookieName                  = "negotiable_oauth_flow"
	sessionCookieName               = "negotiable_session"
	AuthenticatedUserHeader         = "X-Negotiable-Authenticated-User-ID"
	AuthenticatedOrganizationHeader = "X-Negotiable-Authenticated-Organization-ID"
	AuthenticatedRoleHeader         = "X-Negotiable-Authenticated-Role"
	AuthenticatedSessionHeader      = "X-Negotiable-Authenticated-Session-Hash"
)

type HandlerConfig struct {
	WebOrigin     string
	DemoMode      bool
	SecureCookies bool
	FlowTTL       time.Duration
	SessionTTL    time.Duration
}

type AccountGrantRevoker interface {
	PrepareForAccountDeletion(context.Context, string) (func(context.Context) error, error)
}

type Handler struct {
	next     http.Handler
	store    Store
	provider Provider
	revoker  AccountGrantRevoker
	config   HandlerConfig
	logger   *slog.Logger
	now      func() time.Time
}

func NewHandler(next http.Handler, store Store, provider Provider, config HandlerConfig, logger *slog.Logger) http.Handler {
	return NewHandlerWithAccountRevoker(next, store, provider, nil, config, logger)
}

func NewHandlerWithAccountRevoker(next http.Handler, store Store, provider Provider, revoker AccountGrantRevoker, config HandlerConfig, logger *slog.Logger) http.Handler {
	if config.FlowTTL == 0 {
		config.FlowTTL = 10 * time.Minute
	}
	if config.SessionTTL == 0 {
		config.SessionTTL = 7 * 24 * time.Hour
	}
	return &Handler{next: next, store: store, provider: provider, revoker: revoker, config: config, logger: logger, now: time.Now}
}

func (handler *Handler) ServeHTTP(response http.ResponseWriter, request *http.Request) {
	if handler.config.WebOrigin != "" && request.Header.Get("Origin") == handler.config.WebOrigin {
		response.Header().Set("Access-Control-Allow-Origin", handler.config.WebOrigin)
		response.Header().Set("Access-Control-Allow-Credentials", "true")
		response.Header().Set("Vary", "Origin")
	}
	switch request.Method + " " + request.URL.Path {
	case http.MethodGet + " /api/v1/auth/google/login":
		handler.login(response, request)
		return
	case http.MethodGet + " /api/v1/auth/google/callback":
		handler.callback(response, request)
		return
	case http.MethodGet + " /api/v1/auth/session":
		handler.session(response, request)
		return
	case http.MethodPost + " /api/v1/auth/logout":
		handler.logout(response, request)
		return
	case http.MethodDelete + " /api/v1/me/account":
		handler.deleteAccount(response, request)
		return
	}

	identity, authenticated := handler.identity(request)
	if strings.HasPrefix(request.URL.Path, "/api/v1/") {
		cloned := request.Clone(request.Context())
		cloned.Header.Del(AuthenticatedUserHeader)
		cloned.Header.Del(AuthenticatedOrganizationHeader)
		cloned.Header.Del(AuthenticatedRoleHeader)
		cloned.Header.Del(AuthenticatedSessionHeader)
		if authenticated {
			cloned.Header.Set("X-Demo-User-ID", identity.UserID)
			cloned.Header.Set("X-Organization-ID", identity.OrganizationID)
			cloned.Header.Set(AuthenticatedUserHeader, identity.UserID)
			cloned.Header.Set(AuthenticatedOrganizationHeader, identity.OrganizationID)
			cloned.Header.Set(AuthenticatedRoleHeader, identity.Role)
			if cookie, err := request.Cookie(sessionCookieName); err == nil {
				cloned.Header.Set(AuthenticatedSessionHeader, base64.RawURLEncoding.EncodeToString(hashToken(cookie.Value)))
			}
		} else if !handler.config.DemoMode {
			cloned.Header.Del("X-Demo-User-ID")
			cloned.Header.Del("X-Organization-ID")
		}
		request = cloned
	}
	handler.next.ServeHTTP(response, request)
}

func (handler *Handler) login(response http.ResponseWriter, request *http.Request) {
	if !handler.provider.Configured() {
		writeJSON(response, http.StatusServiceUnavailable, map[string]string{"error": "google login is not configured"})
		return
	}
	state := randomToken(32)
	verifier := randomToken(32)
	flow := Flow{
		ID: newID("oauth-flow"), StateHash: hashToken(state), CodeVerifier: verifier,
		CreatedAt: handler.now().UTC(), ExpiresAt: handler.now().UTC().Add(handler.config.FlowTTL),
	}
	if err := handler.store.CreateFlow(request.Context(), flow); err != nil {
		handler.logger.Error("create google oauth flow", "error", err)
		writeJSON(response, http.StatusInternalServerError, map[string]string{"error": "unable to start google login"})
		return
	}
	http.SetCookie(response, &http.Cookie{
		Name: flowCookieName, Value: flow.ID, Path: "/api/v1/auth/google/callback",
		HttpOnly: true, Secure: handler.config.SecureCookies, SameSite: http.SameSiteLaxMode,
		MaxAge: int(handler.config.FlowTTL.Seconds()),
	})
	challengeHash := sha256.Sum256([]byte(verifier))
	challenge := base64.RawURLEncoding.EncodeToString(challengeHash[:])
	http.Redirect(response, request, handler.provider.AuthorizationURL(state, challenge), http.StatusFound)
}

func (handler *Handler) callback(response http.ResponseWriter, request *http.Request) {
	flowCookie, err := request.Cookie(flowCookieName)
	state := request.URL.Query().Get("state")
	code := request.URL.Query().Get("code")
	if err != nil || state == "" || code == "" || request.URL.Query().Get("error") != "" {
		writeJSON(response, http.StatusBadRequest, map[string]string{"error": "invalid oauth callback"})
		return
	}
	flow, err := handler.store.ConsumeFlow(request.Context(), flowCookie.Value, hashToken(state), handler.now().UTC())
	if errors.Is(err, ErrNotFound) {
		writeJSON(response, http.StatusBadRequest, map[string]string{"error": "oauth state is invalid or expired"})
		return
	}
	if err != nil {
		handler.logger.Error("consume google oauth flow", "error", err)
		writeJSON(response, http.StatusInternalServerError, map[string]string{"error": "unable to complete google login"})
		return
	}
	profile, err := handler.provider.Exchange(request.Context(), code, flow.CodeVerifier)
	if err != nil {
		handler.logger.Warn("google oauth exchange rejected", "error", err)
		writeJSON(response, http.StatusBadGateway, map[string]string{"error": "google login could not be verified"})
		return
	}
	if !profile.EmailVerified {
		writeJSON(response, http.StatusForbidden, map[string]string{"error": "verified google email is required"})
		return
	}
	identity, err := handler.store.UpsertGoogleIdentity(request.Context(), profile, handler.now().UTC())
	if err != nil {
		handler.logger.Error("persist google identity", "error", err)
		writeJSON(response, http.StatusInternalServerError, map[string]string{"error": "unable to create user session"})
		return
	}
	token := randomToken(32)
	now := handler.now().UTC()
	if err := handler.store.CreateSession(request.Context(), Session{
		TokenHash: hashToken(token), UserID: identity.UserID, OrganizationID: identity.OrganizationID,
		CreatedAt: now, ExpiresAt: now.Add(handler.config.SessionTTL),
	}); err != nil {
		handler.logger.Error("create user session", "error", err)
		writeJSON(response, http.StatusInternalServerError, map[string]string{"error": "unable to create user session"})
		return
	}
	http.SetCookie(response, &http.Cookie{
		Name: sessionCookieName, Value: token, Path: "/", HttpOnly: true,
		Secure: handler.config.SecureCookies, SameSite: http.SameSiteLaxMode,
		MaxAge: int(handler.config.SessionTTL.Seconds()),
	})
	expireCookie(response, flowCookieName, "/api/v1/auth/google/callback", handler.config.SecureCookies)
	http.Redirect(response, request, strings.TrimRight(handler.config.WebOrigin, "/")+"/?auth=success", http.StatusFound)
}

func (handler *Handler) session(response http.ResponseWriter, request *http.Request) {
	identity, authenticated := handler.identity(request)
	if !authenticated {
		if handler.config.DemoMode {
			writeJSON(response, http.StatusOK, map[string]any{"authenticated": false, "demoMode": true})
			return
		}
		writeJSON(response, http.StatusUnauthorized, map[string]any{"authenticated": false})
		return
	}
	writeJSON(response, http.StatusOK, map[string]any{"authenticated": true, "demoMode": handler.config.DemoMode, "user": identity})
}

func (handler *Handler) logout(response http.ResponseWriter, request *http.Request) {
	if cookie, err := request.Cookie(sessionCookieName); err == nil {
		if err := handler.store.DeleteSession(request.Context(), hashToken(cookie.Value)); err != nil {
			handler.logger.Error("delete user session", "error", err)
			writeJSON(response, http.StatusInternalServerError, map[string]string{"error": "unable to log out"})
			return
		}
	}
	expireCookie(response, sessionCookieName, "/", handler.config.SecureCookies)
	response.WriteHeader(http.StatusNoContent)
}

type deleteAccountInput struct {
	Confirmation string `json:"confirmation"`
}

func (handler *Handler) deleteAccount(response http.ResponseWriter, request *http.Request) {
	identity, authenticated := handler.identity(request)
	if !authenticated {
		writeJSON(response, http.StatusUnauthorized, map[string]string{"error": "authentication required"})
		return
	}
	var input deleteAccountInput
	decoder := json.NewDecoder(http.MaxBytesReader(response, request.Body, 1<<10))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil {
		writeJSON(response, http.StatusBadRequest, map[string]string{"error": "invalid confirmation"})
		return
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		writeJSON(response, http.StatusBadRequest, map[string]string{"error": "invalid confirmation"})
		return
	}
	if input.Confirmation != "DELETE" {
		writeJSON(response, http.StatusUnprocessableEntity, map[string]string{"error": "confirmation must equal DELETE"})
		return
	}
	var revokeGrant func(context.Context) error
	if handler.revoker != nil {
		prepared, err := handler.revoker.PrepareForAccountDeletion(request.Context(), identity.UserID)
		if err != nil {
			handler.logger.Warn("calendar grant preparation failed during account deletion")
		} else {
			revokeGrant = prepared
		}
	}
	if err := handler.store.DeleteAccount(request.Context(), identity.UserID); errors.Is(err, ErrLastOrganizationOwner) {
		writeJSON(response, http.StatusConflict, map[string]string{"error": "transfer workspace ownership before deleting your account"})
		return
	} else if errors.Is(err, ErrNotFound) {
		expireCookie(response, sessionCookieName, "/", handler.config.SecureCookies)
		writeJSON(response, http.StatusUnauthorized, map[string]string{"error": "authentication required"})
		return
	} else if err != nil {
		handler.logger.Error("delete account")
		writeJSON(response, http.StatusInternalServerError, map[string]string{"error": "unable to delete account"})
		return
	}
	if revokeGrant != nil {
		revocationContext, cancel := context.WithTimeout(context.WithoutCancel(request.Context()), 10*time.Second)
		defer cancel()
		if err := revokeGrant(revocationContext); err != nil {
			handler.logger.Warn("calendar grant revocation failed after account deletion")
		}
	}
	expireCookie(response, sessionCookieName, "/", handler.config.SecureCookies)
	expireCookie(response, flowCookieName, "/api/v1/auth/google/callback", handler.config.SecureCookies)
	response.WriteHeader(http.StatusNoContent)
}

func (handler *Handler) identity(request *http.Request) (Identity, bool) {
	cookie, err := request.Cookie(sessionCookieName)
	if err != nil || cookie.Value == "" {
		return Identity{}, false
	}
	value, err := handler.store.GetSession(request.Context(), hashToken(cookie.Value), handler.now().UTC())
	return value, err == nil
}

func randomToken(size int) string {
	value := make([]byte, size)
	if _, err := rand.Read(value); err != nil {
		panic("secure random source unavailable")
	}
	return base64.RawURLEncoding.EncodeToString(value)
}

func hashToken(value string) []byte {
	hash := sha256.Sum256([]byte(value))
	return hash[:]
}

func expireCookie(response http.ResponseWriter, name, path string, secure bool) {
	http.SetCookie(response, &http.Cookie{Name: name, Value: "", Path: path, HttpOnly: true, Secure: secure, SameSite: http.SameSiteLaxMode, MaxAge: -1, Expires: time.Unix(1, 0)})
}

func writeJSON(response http.ResponseWriter, status int, body any) {
	response.Header().Set("Content-Type", "application/json; charset=utf-8")
	response.WriteHeader(status)
	_ = json.NewEncoder(response).Encode(body)
}
