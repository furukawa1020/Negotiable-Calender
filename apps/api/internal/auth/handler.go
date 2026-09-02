package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"
)

const (
	flowCookieName          = "negotiable_oauth_flow"
	sessionCookieName       = "negotiable_session"
	AuthenticatedUserHeader = "X-Negotiable-Authenticated-User-ID"
)

type HandlerConfig struct {
	WebOrigin     string
	DemoMode      bool
	SecureCookies bool
	FlowTTL       time.Duration
	SessionTTL    time.Duration
}

type Handler struct {
	next     http.Handler
	store    Store
	provider Provider
	config   HandlerConfig
	logger   *slog.Logger
	now      func() time.Time
}

func NewHandler(next http.Handler, store Store, provider Provider, config HandlerConfig, logger *slog.Logger) http.Handler {
	if config.FlowTTL == 0 {
		config.FlowTTL = 10 * time.Minute
	}
	if config.SessionTTL == 0 {
		config.SessionTTL = 7 * 24 * time.Hour
	}
	return &Handler{next: next, store: store, provider: provider, config: config, logger: logger, now: time.Now}
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
	}

	identity, authenticated := handler.identity(request)
	if strings.HasPrefix(request.URL.Path, "/api/v1/") {
		cloned := request.Clone(request.Context())
		cloned.Header.Del(AuthenticatedUserHeader)
		if authenticated {
			cloned.Header.Set("X-Demo-User-ID", identity.UserID)
			cloned.Header.Set("X-Organization-ID", identity.OrganizationID)
			cloned.Header.Set(AuthenticatedUserHeader, identity.UserID)
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
