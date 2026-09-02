package auth

import (
	"context"
	"crypto/sha256"
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
	var userID, organizationID, authenticatedUserID string
	next := http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		userID, organizationID = request.Header.Get("X-Demo-User-ID"), request.Header.Get("X-Organization-ID")
		authenticatedUserID = request.Header.Get(AuthenticatedUserHeader)
		response.WriteHeader(http.StatusNoContent)
	})
	handler := testHandler(next, &stubStore{sessionErr: ErrNotFound}, &stubProvider{}, false)
	request := httptest.NewRequest(http.MethodGet, "/api/v1/requests", nil)
	request.Header.Set("X-Demo-User-ID", "attacker")
	request.Header.Set("X-Organization-ID", "attacker-org")
	request.Header.Set(AuthenticatedUserHeader, "attacker")
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if userID != "" || organizationID != "" || authenticatedUserID != "" {
		t.Fatalf("spoofed identity survived: user=%q org=%q trusted=%q", userID, organizationID, authenticatedUserID)
	}
}

func TestValidSessionOverridesSpoofedIdentity(t *testing.T) {
	t.Parallel()
	var userID, organizationID, authenticatedUserID string
	next := http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		userID, organizationID = request.Header.Get("X-Demo-User-ID"), request.Header.Get("X-Organization-ID")
		authenticatedUserID = request.Header.Get(AuthenticatedUserHeader)
		response.WriteHeader(http.StatusNoContent)
	})
	store := &stubStore{sessionIdentity: Identity{UserID: "user-1", OrganizationID: "org-1"}}
	handler := testHandler(next, store, &stubProvider{}, false)
	request := httptest.NewRequest(http.MethodGet, "/api/v1/requests", nil)
	request.Header.Set("X-Demo-User-ID", "attacker")
	request.AddCookie(&http.Cookie{Name: sessionCookieName, Value: "raw-session-token"})
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if userID != "user-1" || organizationID != "org-1" || authenticatedUserID != "user-1" {
		t.Fatalf("session identity missing: user=%q org=%q trusted=%q", userID, organizationID, authenticatedUserID)
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
