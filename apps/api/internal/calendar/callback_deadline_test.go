package calendar

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/negotiable-calendar/negotiable-calendar/apps/api/internal/auth"
)

type deadlineConsentProvider struct {
	stubProvider
	exchange func(context.Context) (TokenSet, error)
}

func (p *deadlineConsentProvider) Exchange(ctx context.Context, _, _ string) (TokenSet, error) {
	return p.exchange(ctx)
}

func TestCalendarCallbackDeadlineAndSafeFailureLogging(t *testing.T) {
	for _, mode := range []string{"success", "deadline", "late-success", "provider-error"} {
		t.Run(mode, func(t *testing.T) {
			ctx := context.Background()
			if mode == "deadline" || mode == "late-success" {
				var cancel context.CancelFunc
				ctx, cancel = context.WithTimeout(ctx, 20*time.Millisecond)
				defer cancel()
			}
			store := &consentStore{flow: Flow{ID: "flow", UserID: "owner", StateHash: hashToken("secret-state"), ExpiresAt: time.Now().Add(time.Hour)}}
			store.connection = Connection{UserID: "owner", RefreshTokenCipher: []byte("existing")}
			provider := &deadlineConsentProvider{exchange: func(exchangeCtx context.Context) (TokenSet, error) {
				deadline, ok := exchangeCtx.Deadline()
				if !ok || time.Until(deadline) > 20*time.Second {
					t.Fatal("exchange has no bounded deadline")
				}
				if mode == "provider-error" {
					return TokenSet{}, errors.New("secret-code secret-token secret-provider-body")
				}
				if mode == "deadline" || mode == "late-success" {
					<-exchangeCtx.Done()
					if mode == "deadline" {
						return TokenSet{}, exchangeCtx.Err()
					}
				}
				return TokenSet{RefreshToken: "secret-token", Scopes: []string{CalendarOwnedEventsReadonlyScope}}, nil
			}}
			var logs bytes.Buffer
			handler := NewHandler(http.NotFoundHandler(), store, provider, testCipher(t), &stubProjector{}, HandlerConfig{WebOrigin: "https://app.example"}, slog.New(slog.NewTextHandler(&logs, nil)))
			request := httptest.NewRequest(http.MethodGet, "/api/v1/calendar/google/callback?state=secret-state&code=secret-code", nil).WithContext(ctx)
			request.Header.Set(auth.AuthenticatedUserHeader, "owner")
			request.AddCookie(&http.Cookie{Name: flowCookieName, Value: "flow"})
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if !store.consumed {
				t.Fatal("flow was not consumed")
			}
			if mode == "success" {
				if store.saves != 1 || response.Code != http.StatusFound {
					t.Fatalf("valid connection rejected: %d saves=%d", response.Code, store.saves)
				}
			} else {
				if store.saves != 0 || string(store.connection.RefreshTokenCipher) != "existing" || !strings.HasSuffix(response.Header().Get("Location"), "calendar=exchange_failed") {
					t.Fatal("failed or late exchange mutated connection")
				}
				if !strings.Contains(logs.String(), "failure_code=token_exchange_failed") {
					t.Fatal("missing safe diagnostic code")
				}
			}
			if strings.Contains(logs.String()+response.Body.String()+response.Header().Get("Location"), "secret-") {
				t.Fatal("OAuth secret leaked in diagnostics")
			}
		})
	}
}
