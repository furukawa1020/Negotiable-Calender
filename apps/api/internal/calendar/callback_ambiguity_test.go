package calendar

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/negotiable-calendar/negotiable-calendar/apps/api/internal/auth"
)

func TestCallbackRejectsAmbiguityBeforeConsumingFlow(t *testing.T) {
	for _, query := range []string{
		"state=secret-state&state=secret-state&code=secret-code",
		"state=secret-state&%73tate=other&code=secret-code",
		"state=secret-state&code=secret-code&code=other",
		"state=secret-state&error=access_denied&error=access_denied",
		"state=secret-state&code=secret-code&error=",
		"state=secret-state&error=access_denied&code=",
		"state=secret-state&code=secret-code&bad=%zz",
		"state=secret-state&code=secret-code&bad=x;y",
	} {
		t.Run(query, func(t *testing.T) { checkAmbiguousCallback(t, query, false) })
	}
	checkAmbiguousCallback(t, "state=secret-state&code=secret-code", true)
}

func checkAmbiguousCallback(t *testing.T, query string, duplicateCookie bool) {
	t.Helper()
	s := &consentStore{flow: Flow{ID: "flow-id", UserID: "owner", StateHash: hashToken("secret-state"), ExpiresAt: time.Now().Add(time.Hour)}}
	p := &consentProvider{tokens: TokenSet{RefreshToken: "secret-refresh", Scopes: []string{CalendarReadonlyScope}}}
	h := NewHandler(http.NotFoundHandler(), s, p, testCipher(t), &stubProjector{}, HandlerConfig{}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	r := httptest.NewRequest("GET", "/api/v1/calendar/google/callback?"+query, nil)
	r.Header.Set(auth.AuthenticatedUserHeader, "owner")
	r.AddCookie(&http.Cookie{Name: flowCookieName, Value: "flow-id"})
	if duplicateCookie {
		r.AddCookie(&http.Cookie{Name: flowCookieName, Value: "flow-id"})
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 400 || s.consumed || s.saves != 0 || p.exchanges != 0 {
		t.Fatal("ambiguous callback had effects", w.Code)
	}
	if strings.Contains(w.Body.String(), "secret-") || w.Header().Get("Cache-Control") != "no-store" || w.Header().Get("Referrer-Policy") != "no-referrer" {
		t.Fatal("unsafe callback response")
	}
}
