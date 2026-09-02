package organization

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/negotiable-calendar/negotiable-calendar/apps/api/internal/auth"
)

type InvitationHandlerConfig struct {
	WebOrigin     string
	InvitationTTL time.Duration
}

type InvitationHandler struct {
	next   http.Handler
	store  InvitationStore
	config InvitationHandlerConfig
	logger *slog.Logger
	now    func() time.Time
}

func NewInvitationHandler(next http.Handler, store InvitationStore, config InvitationHandlerConfig, logger *slog.Logger) http.Handler {
	if config.InvitationTTL == 0 {
		config.InvitationTTL = 72 * time.Hour
	}
	return &InvitationHandler{next: next, store: store, config: config, logger: logger, now: time.Now}
}

func (handler *InvitationHandler) ServeHTTP(response http.ResponseWriter, request *http.Request) {
	switch request.Method + " " + request.URL.Path {
	case http.MethodGet + " /api/v1/workspaces":
		handler.listWorkspaces(response, request)
		return
	case http.MethodPost + " /api/v1/workspaces/switch":
		handler.switchWorkspace(response, request)
		return
	case http.MethodPost + " /api/v1/invitations/preview":
		handler.previewInvitation(response, request)
		return
	case http.MethodPost + " /api/v1/invitations/accept":
		handler.acceptInvitation(response, request)
		return
	}
	if request.Method == http.MethodPost {
		if organizationID, ok := invitationOrganizationID(request.URL.Path); ok {
			handler.createInvitation(response, request, organizationID)
			return
		}
	}
	handler.next.ServeHTTP(response, request)
}

func (handler *InvitationHandler) trustedIdentity(response http.ResponseWriter, request *http.Request) (string, string, bool) {
	userID := request.Header.Get(auth.AuthenticatedUserHeader)
	organizationID := request.Header.Get(auth.AuthenticatedOrganizationHeader)
	if userID == "" || organizationID == "" {
		writeInvitationJSON(response, http.StatusUnauthorized, map[string]string{"error": "authenticated session required"})
		return "", "", false
	}
	return userID, organizationID, true
}

func (handler *InvitationHandler) createInvitation(response http.ResponseWriter, request *http.Request, organizationID string) {
	userID, activeOrganizationID, ok := handler.trustedIdentity(response, request)
	if !ok {
		return
	}
	if activeOrganizationID != organizationID {
		writeInvitationJSON(response, http.StatusForbidden, map[string]string{"error": "active workspace does not match"})
		return
	}
	var input struct {
		Role Role `json:"role"`
	}
	if err := decodeInvitationJSON(response, request, &input); err != nil {
		return
	}
	if !input.Role.Valid() || input.Role == Owner {
		writeInvitationJSON(response, http.StatusUnprocessableEntity, map[string]string{"error": "invalid invitation role"})
		return
	}
	token := secureToken(32)
	now := handler.now().UTC()
	value := Invitation{
		ID: newID("invitation"), OrganizationID: organizationID, InvitedBy: userID,
		Role: input.Role, TokenHash: invitationHash(token), CreatedAt: now,
		ExpiresAt: now.Add(handler.config.InvitationTTL),
	}
	if err := handler.store.CreateInvitation(request.Context(), value); errors.Is(err, ErrForbidden) {
		writeInvitationJSON(response, http.StatusForbidden, map[string]string{"error": "insufficient invitation permission"})
		return
	} else if err != nil {
		handler.logger.Error("create workspace invitation", "error", err)
		writeInvitationJSON(response, http.StatusInternalServerError, map[string]string{"error": "unable to create invitation"})
		return
	}
	writeInvitationJSON(response, http.StatusCreated, map[string]any{
		"invitationId": value.ID, "role": value.Role, "expiresAt": value.ExpiresAt,
		"token": token, "inviteUrl": strings.TrimRight(handler.config.WebOrigin, "/") + "/?invite=" + token,
	})
}

func (handler *InvitationHandler) previewInvitation(response http.ResponseWriter, request *http.Request) {
	if _, _, ok := handler.trustedIdentity(response, request); !ok {
		return
	}
	token, ok := decodeInvitationToken(response, request)
	if !ok {
		return
	}
	value, err := handler.store.PreviewInvitation(request.Context(), invitationHash(token), handler.now().UTC())
	if errors.Is(err, ErrInvitationNotFound) {
		writeInvitationJSON(response, http.StatusNotFound, map[string]string{"error": "invitation is invalid or expired"})
		return
	}
	if err != nil {
		handler.logger.Error("preview workspace invitation", "error", err)
		writeInvitationJSON(response, http.StatusInternalServerError, map[string]string{"error": "unable to preview invitation"})
		return
	}
	writeInvitationJSON(response, http.StatusOK, value)
}

func (handler *InvitationHandler) acceptInvitation(response http.ResponseWriter, request *http.Request) {
	userID, _, ok := handler.trustedIdentity(response, request)
	if !ok {
		return
	}
	token, ok := decodeInvitationToken(response, request)
	if !ok {
		return
	}
	value, err := handler.store.AcceptInvitation(request.Context(), invitationHash(token), userID, handler.now().UTC())
	if errors.Is(err, ErrInvitationNotFound) {
		writeInvitationJSON(response, http.StatusNotFound, map[string]string{"error": "invitation is invalid, expired, or already used"})
		return
	}
	if err != nil {
		handler.logger.Error("accept workspace invitation", "error", err)
		writeInvitationJSON(response, http.StatusInternalServerError, map[string]string{"error": "unable to accept invitation"})
		return
	}
	writeInvitationJSON(response, http.StatusOK, map[string]any{"accepted": true, "workspace": value})
}

func (handler *InvitationHandler) listWorkspaces(response http.ResponseWriter, request *http.Request) {
	userID, activeOrganizationID, ok := handler.trustedIdentity(response, request)
	if !ok {
		return
	}
	values, err := handler.store.ListWorkspaces(request.Context(), userID)
	if err != nil {
		handler.logger.Error("list user workspaces", "error", err)
		writeInvitationJSON(response, http.StatusInternalServerError, map[string]string{"error": "unable to list workspaces"})
		return
	}
	writeInvitationJSON(response, http.StatusOK, map[string]any{"activeWorkspaceId": activeOrganizationID, "workspaces": values})
}

func (handler *InvitationHandler) switchWorkspace(response http.ResponseWriter, request *http.Request) {
	userID, _, ok := handler.trustedIdentity(response, request)
	if !ok {
		return
	}
	var input struct {
		OrganizationID string `json:"organizationId"`
	}
	if err := decodeInvitationJSON(response, request, &input); err != nil || strings.TrimSpace(input.OrganizationID) == "" {
		if err == nil {
			writeInvitationJSON(response, http.StatusBadRequest, map[string]string{"error": "organizationId is required"})
		}
		return
	}
	sessionHash, err := base64.RawURLEncoding.DecodeString(request.Header.Get(auth.AuthenticatedSessionHeader))
	if err != nil || len(sessionHash) != sha256.Size {
		writeInvitationJSON(response, http.StatusUnauthorized, map[string]string{"error": "valid server session required"})
		return
	}
	value, err := handler.store.SwitchWorkspace(request.Context(), sessionHash, userID, input.OrganizationID, handler.now().UTC())
	if errors.Is(err, ErrForbidden) {
		writeInvitationJSON(response, http.StatusForbidden, map[string]string{"error": "workspace membership required"})
		return
	}
	if err != nil {
		handler.logger.Error("switch active workspace", "error", err)
		writeInvitationJSON(response, http.StatusInternalServerError, map[string]string{"error": "unable to switch workspace"})
		return
	}
	writeInvitationJSON(response, http.StatusOK, map[string]any{"activeWorkspace": value})
}

func invitationOrganizationID(path string) (string, bool) {
	const prefix, suffix = "/api/v1/workspaces/", "/invitations"
	if !strings.HasPrefix(path, prefix) || !strings.HasSuffix(path, suffix) {
		return "", false
	}
	value := strings.TrimSuffix(strings.TrimPrefix(path, prefix), suffix)
	return value, value != "" && !strings.Contains(value, "/")
}

func decodeInvitationToken(response http.ResponseWriter, request *http.Request) (string, bool) {
	var input struct {
		Token string `json:"token"`
	}
	if err := decodeInvitationJSON(response, request, &input); err != nil {
		return "", false
	}
	if len(input.Token) < 32 {
		writeInvitationJSON(response, http.StatusBadRequest, map[string]string{"error": "invitation token is required"})
		return "", false
	}
	return input.Token, true
}

func decodeInvitationJSON(response http.ResponseWriter, request *http.Request, target any) error {
	decoder := json.NewDecoder(http.MaxBytesReader(response, request.Body, 16<<10))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		writeInvitationJSON(response, http.StatusBadRequest, map[string]string{"error": "invalid JSON body"})
		return err
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		writeInvitationJSON(response, http.StatusBadRequest, map[string]string{"error": "request body must contain one JSON value"})
		return fmt.Errorf("trailing JSON")
	}
	return nil
}

func secureToken(size int) string {
	value := make([]byte, size)
	if _, err := rand.Read(value); err != nil {
		panic("secure random source unavailable")
	}
	return base64.RawURLEncoding.EncodeToString(value)
}

func invitationHash(value string) []byte {
	hash := sha256.Sum256([]byte(value))
	return hash[:]
}

func newID(prefix string) string { return prefix + "-" + secureToken(16) }

func writeInvitationJSON(response http.ResponseWriter, status int, body any) {
	response.Header().Set("Content-Type", "application/json; charset=utf-8")
	response.WriteHeader(status)
	_ = json.NewEncoder(response).Encode(body)
}
