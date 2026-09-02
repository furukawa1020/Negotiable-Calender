package organization

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/negotiable-calendar/negotiable-calendar/apps/api/internal/auth"
)

type invitationStoreStub struct {
	created       Invitation
	lastTokenHash []byte
	workspaces    []Workspace
	switched      string
	sessionHash   []byte
	preview       InvitationPreview
}

func (store *invitationStoreStub) CreateInvitation(_ context.Context, value Invitation) error {
	store.created = value
	return nil
}
func (store *invitationStoreStub) PreviewInvitation(_ context.Context, hash []byte, _ time.Time) (InvitationPreview, error) {
	store.lastTokenHash = hash
	return store.preview, nil
}
func (store *invitationStoreStub) AcceptInvitation(_ context.Context, hash []byte, _ string, _ time.Time) (Workspace, error) {
	store.lastTokenHash = hash
	return Workspace{ID: "org-2", Name: "Shared", Role: Member}, nil
}
func (store *invitationStoreStub) ListWorkspaces(context.Context, string) ([]Workspace, error) {
	return store.workspaces, nil
}
func (store *invitationStoreStub) SwitchWorkspace(_ context.Context, hash []byte, _ string, organizationID string, _ time.Time) (Workspace, error) {
	store.sessionHash, store.switched = hash, organizationID
	return Workspace{ID: organizationID, Name: "Shared", Role: Member}, nil
}

func TestInvitationCreationReturnsRawTokenOnceAndStoresOnlyHash(t *testing.T) {
	store := &invitationStoreStub{}
	handler := testInvitationHandler(store)
	request := httptest.NewRequest(http.MethodPost, "/api/v1/workspaces/org-1/invitations", bytes.NewBufferString(`{"role":"MEMBER"}`))
	trustedInvitationHeaders(request, "user-1", "org-1")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusCreated {
		t.Fatalf("create failed: %d %s", response.Code, response.Body.String())
	}
	var body struct {
		Token, InviteURL string
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	expected := sha256.Sum256([]byte(body.Token))
	if body.Token == "" || body.InviteURL == "" || !bytes.Equal(store.created.TokenHash, expected[:]) {
		t.Fatalf("raw token/hash contract failed: %#v", body)
	}
	if bytes.Contains(store.created.TokenHash, []byte(body.Token)) {
		t.Fatal("raw invitation token reached persistence")
	}
}

func TestInvitationEndpointsRejectDemoIdentity(t *testing.T) {
	handler := testInvitationHandler(&invitationStoreStub{})
	request := httptest.NewRequest(http.MethodGet, "/api/v1/workspaces", nil)
	request.Header.Set("X-Demo-User-ID", "demo-manager")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("demo identity accessed workspaces: %d", response.Code)
	}
}

func TestInvitationAcceptanceHashesBodyToken(t *testing.T) {
	store := &invitationStoreStub{}
	handler := testInvitationHandler(store)
	token := "abcdefghijklmnopqrstuvwxyz-0123456789"
	request := httptest.NewRequest(http.MethodPost, "/api/v1/invitations/accept", bytes.NewBufferString(`{"token":"`+token+`"}`))
	trustedInvitationHeaders(request, "user-2", "personal-org")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	expected := sha256.Sum256([]byte(token))
	if response.Code != http.StatusOK || !bytes.Equal(store.lastTokenHash, expected[:]) {
		t.Fatalf("accept did not hash token: status=%d hash=%x", response.Code, store.lastTokenHash)
	}
}

func TestWorkspaceSwitchUsesTrustedSessionHash(t *testing.T) {
	store := &invitationStoreStub{}
	handler := testInvitationHandler(store)
	hash := sha256.Sum256([]byte("session-token"))
	request := httptest.NewRequest(http.MethodPost, "/api/v1/workspaces/switch", bytes.NewBufferString(`{"organizationId":"org-2"}`))
	trustedInvitationHeaders(request, "user-2", "personal-org")
	request.Header.Set(auth.AuthenticatedSessionHeader, base64.RawURLEncoding.EncodeToString(hash[:]))
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK || store.switched != "org-2" || !bytes.Equal(store.sessionHash, hash[:]) {
		t.Fatalf("workspace switch failed: status=%d org=%q", response.Code, store.switched)
	}
}

func testInvitationHandler(store InvitationStore) http.Handler {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	return NewInvitationHandler(http.NotFoundHandler(), store, InvitationHandlerConfig{WebOrigin: "https://calendar.example"}, logger)
}

func trustedInvitationHeaders(request *http.Request, userID, organizationID string) {
	request.Header.Set(auth.AuthenticatedUserHeader, userID)
	request.Header.Set(auth.AuthenticatedOrganizationHeader, organizationID)
}
