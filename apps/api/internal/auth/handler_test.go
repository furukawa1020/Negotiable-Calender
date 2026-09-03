package auth

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

type stubStore struct {
	flow            Flow
	consumeErr      error
	identity        Identity
	session         Session
	sessionIdentity Identity
	sessionErr      error
	deletedHash     []byte
	deletedUser     string
	accountErr      error
}

func (store *stubStore) CreateFlow(_ context.Context, value Flow) error {
	store.flow = value
	return nil
}
func (store *stubStore) ConsumeFlow(_ context.Context, id string, stateHash []byte, now time.Time) (Flow, error) {
	if store.consumeErr != nil {
		return Flow{}, store.consumeErr
	}
	if id != store.flow.ID || !equalBytes(stateHash, store.flow.StateHash) || !store.flow.ExpiresAt.After(now) {
		return Flow{}, ErrNotFound
	}
	value := store.flow
	store.flow = Flow{}
	return value, nil
}
func (store *stubStore) UpsertGoogleIdentity(context.Context, Profile, time.Time) (Identity, error) {
	return store.identity, nil
}
func (store *stubStore) CreateSession(_ context.Context, value Session) error {
	store.session = value
	return nil
}
func (store *stubStore) GetSession(context.Context, []byte, time.Time) (Identity, error) {
	if store.sessionErr != nil {
		return Identity{}, store.sessionErr
	}
	return store.sessionIdentity, nil
}
func (store *stubStore) DeleteSession(_ context.Context, value []byte) error {
	store.deletedHash = value
	return nil
}
func (store *stubStore) DeleteAccount(_ context.Context, userID string) error {
	store.deletedUser = userID
	if store.accountErr == nil {
		store.sessionErr = ErrNotFound
	}
	return store.accountErr
}

type stubProvider struct {
	configured bool
	state      string
	challenge  string
	profile    Profile
	exchanges  int
}

func (provider *stubProvider) Configured() bool { return provider.configured }
func (provider *stubProvider) AuthorizationURL(state, challenge string) string {
	provider.state, provider.challenge = state, challenge
	return "https://accounts.example/auth?state=" + url.QueryEscape(state)
}
func (provider *stubProvider) Exchange(context.Context, string, string) (Profile, error) {
	provider.exchanges++
	return provider.profile, nil
}

type stubAccountRevoker struct {
	preparedUser string
	revoked      bool
	prepareErr   error
	revokeErr    error
}

func (value *stubAccountRevoker) PrepareForAccountDeletion(_ context.Context, userID string) (func(context.Context) error, error) {
	value.preparedUser = userID
	if value.prepareErr != nil {
		return nil, value.prepareErr
	}
	return func(context.Context) error {
		value.revoked = true
		return value.revokeErr
	}, nil
}

func TestLoginCreatesOneTimeFlowAndSecurePKCEChallenge(t *testing.T) {
	t.Parallel()
	store := &stubStore{}
	provider := &stubProvider{configured: true}
	handler := testHandler(http.NotFoundHandler(), store, provider, false)
	request := httptest.NewRequest(http.MethodGet, "/api/v1/auth/google/login", nil)
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusFound || provider.state == "" || provider.challenge == "" || store.flow.CodeVerifier == "" {
		t.Fatalf("oauth flow not created: status=%d flow=%#v", response.Code, store.flow)
	}
	if !equalBytes(store.flow.StateHash, hashToken(provider.state)) {
		t.Fatal("raw state was not represented by its hash")
	}
	challengeHash := sha256.Sum256([]byte(store.flow.CodeVerifier))
	if provider.challenge != encodeURL(challengeHash[:]) {
		t.Fatal("PKCE challenge does not match verifier")
	}
	cookies := response.Result().Cookies()
	if len(cookies) != 1 || !cookies[0].HttpOnly || cookies[0].SameSite != http.SameSiteLaxMode {
		t.Fatalf("unsafe oauth cookie: %#v", cookies)
	}
}

func TestCallbackRejectsMismatchedAndReplayedState(t *testing.T) {
	t.Parallel()
	store := &stubStore{flow: Flow{ID: "flow-1", StateHash: hashToken("correct"), CodeVerifier: "verifier", ExpiresAt: time.Now().Add(time.Hour)}}
	provider := &stubProvider{configured: true, profile: Profile{EmailVerified: true}}
	handler := testHandler(http.NotFoundHandler(), store, provider, false)
	request := httptest.NewRequest(http.MethodGet, "/api/v1/auth/google/callback?state=wrong&code=code-1", nil)
	request.AddCookie(&http.Cookie{Name: flowCookieName, Value: "flow-1"})
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusBadRequest || provider.exchanges != 0 {
		t.Fatalf("mismatched state reached provider: status=%d exchanges=%d", response.Code, provider.exchanges)
	}

	store.flow = Flow{ID: "flow-1", StateHash: hashToken("correct"), CodeVerifier: "verifier", ExpiresAt: time.Now().Add(time.Hour)}
	store.identity = Identity{UserID: "user-1", OrganizationID: "org-1"}
	provider.profile = Profile{Subject: "sub-1", Email: "person@example.com", EmailVerified: true}
	first := httptest.NewRecorder()
	valid := httptest.NewRequest(http.MethodGet, "/api/v1/auth/google/callback?state=correct&code=code-1", nil)
	valid.AddCookie(&http.Cookie{Name: flowCookieName, Value: "flow-1"})
	handler.ServeHTTP(first, valid)
	if first.Code != http.StatusFound {
		t.Fatalf("valid callback failed: %d %s", first.Code, first.Body.String())
	}
	var sessionCookie *http.Cookie
	for _, cookie := range first.Result().Cookies() {
		if cookie.Name == sessionCookieName {
			sessionCookie = cookie
		}
	}
	if sessionCookie == nil || !equalBytes(store.session.TokenHash, hashToken(sessionCookie.Value)) || equalBytes(store.session.TokenHash, []byte(sessionCookie.Value)) {
		t.Fatalf("session token was not stored as a hash: cookie=%v hash=%x", sessionCookie, store.session.TokenHash)
	}
	replay := httptest.NewRecorder()
	handler.ServeHTTP(replay, valid)
	if replay.Code != http.StatusBadRequest || provider.exchanges != 1 {
		t.Fatalf("replayed state accepted: status=%d exchanges=%d", replay.Code, provider.exchanges)
	}
}

func TestProductionMiddlewareStripsSpoofedIdentityHeaders(t *testing.T) {
	t.Parallel()
	var userID, organizationID, authenticatedUserID, trustedOrganizationID, trustedRole, trustedSession string
	next := http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		userID, organizationID = request.Header.Get("X-Demo-User-ID"), request.Header.Get("X-Organization-ID")
		authenticatedUserID = request.Header.Get(AuthenticatedUserHeader)
		trustedOrganizationID = request.Header.Get(AuthenticatedOrganizationHeader)
		trustedRole = request.Header.Get(AuthenticatedRoleHeader)
		trustedSession = request.Header.Get(AuthenticatedSessionHeader)
		response.WriteHeader(http.StatusNoContent)
	})
	handler := testHandler(next, &stubStore{sessionErr: ErrNotFound}, &stubProvider{}, false)
	request := httptest.NewRequest(http.MethodGet, "/api/v1/requests", nil)
	request.Header.Set("X-Demo-User-ID", "attacker")
	request.Header.Set("X-Organization-ID", "attacker-org")
	request.Header.Set(AuthenticatedUserHeader, "attacker")
	request.Header.Set(AuthenticatedOrganizationHeader, "attacker-org")
	request.Header.Set(AuthenticatedRoleHeader, "OWNER")
	request.Header.Set(AuthenticatedSessionHeader, "attacker-session")
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if userID != "" || organizationID != "" || authenticatedUserID != "" || trustedOrganizationID != "" || trustedRole != "" || trustedSession != "" {
		t.Fatalf("spoofed identity survived: user=%q org=%q trusted=%q trustedOrg=%q role=%q session=%q", userID, organizationID, authenticatedUserID, trustedOrganizationID, trustedRole, trustedSession)
	}
}

func TestValidSessionOverridesSpoofedIdentity(t *testing.T) {
	t.Parallel()
	var userID, organizationID, authenticatedUserID, trustedOrganizationID, trustedRole, trustedSession string
	next := http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		userID, organizationID = request.Header.Get("X-Demo-User-ID"), request.Header.Get("X-Organization-ID")
		authenticatedUserID = request.Header.Get(AuthenticatedUserHeader)
		trustedOrganizationID = request.Header.Get(AuthenticatedOrganizationHeader)
		trustedRole = request.Header.Get(AuthenticatedRoleHeader)
		trustedSession = request.Header.Get(AuthenticatedSessionHeader)
		response.WriteHeader(http.StatusNoContent)
	})
	store := &stubStore{sessionIdentity: Identity{UserID: "user-1", OrganizationID: "org-1", Role: "ADMIN"}}
	handler := testHandler(next, store, &stubProvider{}, false)
	request := httptest.NewRequest(http.MethodGet, "/api/v1/requests", nil)
	request.Header.Set("X-Demo-User-ID", "attacker")
	request.AddCookie(&http.Cookie{Name: sessionCookieName, Value: "raw-session-token"})
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	expectedSession := base64.RawURLEncoding.EncodeToString(hashToken("raw-session-token"))
	if userID != "user-1" || organizationID != "org-1" || authenticatedUserID != "user-1" || trustedOrganizationID != "org-1" || trustedRole != "ADMIN" || trustedSession != expectedSession {
		t.Fatalf("session identity missing: user=%q org=%q trusted=%q trustedOrg=%q role=%q session=%q", userID, organizationID, authenticatedUserID, trustedOrganizationID, trustedRole, trustedSession)
	}
}

func TestLogoutRevokesHashedSessionAndExpiresCookie(t *testing.T) {
	t.Parallel()
	store := &stubStore{}
	handler := testHandler(http.NotFoundHandler(), store, &stubProvider{}, false)
	request := httptest.NewRequest(http.MethodPost, "/api/v1/auth/logout", strings.NewReader(""))
	request.AddCookie(&http.Cookie{Name: sessionCookieName, Value: "raw-session-token"})
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusNoContent || !equalBytes(store.deletedHash, hashToken("raw-session-token")) {
		t.Fatalf("session was not revoked: status=%d hash=%x", response.Code, store.deletedHash)
	}
	cookies := response.Result().Cookies()
	if len(cookies) != 1 || cookies[0].MaxAge != -1 {
		t.Fatalf("session cookie was not expired: %#v", cookies)
	}
}

func TestAccountDeletionRequiresRealSessionAndExactConfirmation(t *testing.T) {
	t.Parallel()
	store := &stubStore{sessionIdentity: Identity{UserID: "user-1", OrganizationID: "org-1"}}
	handler := testHandler(http.NotFoundHandler(), store, &stubProvider{}, false)

	unauthenticated := httptest.NewRecorder()
	handler.ServeHTTP(unauthenticated, httptest.NewRequest(http.MethodDelete, "/api/v1/me/account", strings.NewReader(`{"confirmation":"DELETE"}`)))
	if unauthenticated.Code != http.StatusUnauthorized || store.deletedUser != "" {
		t.Fatalf("unauthenticated deletion reached store: status=%d user=%q", unauthenticated.Code, store.deletedUser)
	}

	for name, body := range map[string]string{
		"wrong":    `{"confirmation":"delete"}`,
		"unknown":  `{"confirmation":"DELETE","userId":"victim"}`,
		"trailing": `{"confirmation":"DELETE"}{"confirmation":"DELETE"}`,
	} {
		t.Run(name, func(t *testing.T) {
			response := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodDelete, "/api/v1/me/account", strings.NewReader(body))
			request.AddCookie(&http.Cookie{Name: sessionCookieName, Value: "session"})
			handler.ServeHTTP(response, request)
			if response.Code != http.StatusBadRequest && response.Code != http.StatusUnprocessableEntity {
				t.Fatalf("invalid confirmation accepted: status=%d body=%s", response.Code, response.Body.String())
			}
		})
	}
	if store.deletedUser != "" {
		t.Fatalf("invalid confirmation deleted user %q", store.deletedUser)
	}
}

func TestAccountDeletionInvalidatesSessionsAndRevokesPreparedGrant(t *testing.T) {
	t.Parallel()
	store := &stubStore{sessionIdentity: Identity{UserID: "user-1", OrganizationID: "org-1"}}
	revoker := &stubAccountRevoker{}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	handler := NewHandlerWithAccountRevoker(http.NotFoundHandler(), store, &stubProvider{}, revoker, HandlerConfig{
		WebOrigin: "https://app.example", SecureCookies: true,
	}, logger)
	request := httptest.NewRequest(http.MethodDelete, "/api/v1/me/account", strings.NewReader(`{"confirmation":"DELETE"}`))
	request.AddCookie(&http.Cookie{Name: sessionCookieName, Value: "raw-session"})
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusNoContent || store.deletedUser != "user-1" {
		t.Fatalf("account was not deleted: status=%d store=%#v", response.Code, store)
	}
	if revoker.preparedUser != "user-1" || !revoker.revoked {
		t.Fatalf("calendar grant was not revoked after deletion: %#v", revoker)
	}
	cookies := response.Result().Cookies()
	if len(cookies) != 2 {
		t.Fatalf("expected session and flow cookies to expire: %#v", cookies)
	}
	for _, cookie := range cookies {
		if cookie.MaxAge != -1 || !cookie.HttpOnly || !cookie.Secure {
			t.Fatalf("unsafe expired cookie: %#v", cookie)
		}
	}
	replay := httptest.NewRecorder()
	handler.ServeHTTP(replay, request)
	if replay.Code != http.StatusUnauthorized {
		t.Fatalf("deleted session was reusable: %d", replay.Code)
	}
}

func TestAccountDeletionDoesNotRevokeGrantWhenOwnerGuardRejects(t *testing.T) {
	t.Parallel()
	store := &stubStore{
		sessionIdentity: Identity{UserID: "owner-1", OrganizationID: "shared-org"},
		accountErr: ErrLastOrganizationOwner,
	}
	revoker := &stubAccountRevoker{}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	handler := NewHandlerWithAccountRevoker(http.NotFoundHandler(), store, &stubProvider{}, revoker, HandlerConfig{}, logger)
	request := httptest.NewRequest(http.MethodDelete, "/api/v1/me/account", strings.NewReader(`{"confirmation":"DELETE"}`))
	request.AddCookie(&http.Cookie{Name: sessionCookieName, Value: "raw-session"})
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusConflict || revoker.revoked {
		t.Fatalf("owner guard failed: status=%d revoked=%t", response.Code, revoker.revoked)
	}
	if len(response.Result().Cookies()) != 0 {
		t.Fatal("active session cookie expired even though deletion was rejected")
	}
}

func testHandler(next http.Handler, store Store, provider Provider, demoMode bool) http.Handler {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	return NewHandler(next, store, provider, HandlerConfig{WebOrigin: "http://localhost:3000", DemoMode: demoMode, FlowTTL: 10 * time.Minute, SessionTTL: time.Hour}, logger)
}

func equalBytes(left, right []byte) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func encodeURL(value []byte) string {
	return strings.TrimRight(base64URLEncode(value), "=")
}

func base64URLEncode(value []byte) string {
	const alphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-_"
	var result strings.Builder
	for index := 0; index < len(value); index += 3 {
		remaining := len(value) - index
		chunk := uint32(value[index]) << 16
		if remaining > 1 {
			chunk |= uint32(value[index+1]) << 8
		}
		if remaining > 2 {
			chunk |= uint32(value[index+2])
		}
		result.WriteByte(alphabet[(chunk>>18)&63])
		result.WriteByte(alphabet[(chunk>>12)&63])
		if remaining > 1 {
			result.WriteByte(alphabet[(chunk>>6)&63])
		}
		if remaining > 2 {
			result.WriteByte(alphabet[chunk&63])
		}
	}
	return result.String()
}
