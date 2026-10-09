package calendar

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestGoogleFailureClassification(t *testing.T) {
	for _, tc := range []struct {
		name, body, code string
		status           int
		token, reconnect bool
	}{
		{"quota", `{"error":{"errors":[{"reason":"rateLimitExceeded"}],"message":"private-secret"}}`, "rate_limited", 403, false, false},
		{"user-quota", `{"error":{"errors":[{"reason":"userRateLimitExceeded"}]}}`, "rate_limited", 403, false, false},
		{"429", "private-secret", "rate_limited", 429, false, false},
		{"unknown", `{"error":{"errors":[{"reason":"private-secret"}]}}`, "provider_denied", 403, false, false},
		{"empty", "", "provider_denied", 403, false, false},
		{"mixed", `{"error":{"errors":[{"reason":"insufficientPermissions"},{"reason":"rateLimitExceeded"}]}}`, "provider_denied", 403, false, false},
		{"permission", `{"error":{"errors":[{"reason":"insufficientPermissions"}]}}`, "reconnect_required", 403, false, true},
		{"unauthorized", "", "reconnect_required", 401, false, true},
		{"server", "private-secret", "temporary_failure", 503, false, false},
		{"revoked", `{"error":"invalid_grant","error_description":"private-secret"}`, "reconnect_required", 400, true, true},
		{"client", `{"error":"invalid_client"}`, "provider_configuration", 401, true, false},
		{"unauthorized-client", `{"error":"unauthorized_client"}`, "provider_configuration", 400, true, false},
		{"unknown-token", `{"error":"private-secret"}`, "temporary_failure", 400, true, false},
		{"empty-token", "", "temporary_failure", 401, true, false},
		{"oversized", `{"error":"invalid_grant","padding":"` + strings.Repeat("x", 17*1024) + `"}`, "temporary_failure", 400, true, false},
		{"trailing", `{"error":"invalid_grant"} {}`, "temporary_failure", 400, true, false},
	} {
		for _, private := range []bool{false, true} {
			t.Run(tc.name+map[bool]string{true: "-private", false: "-sync"}[private], func(t *testing.T) {
				provider := NewGoogleProvider(GoogleConfig{}, &http.Client{Transport: limitTransport(func(r *http.Request) (*http.Response, error) {
					return &http.Response{StatusCode: tc.status, Body: io.NopCloser(strings.NewReader(tc.body)), Header: make(http.Header), Request: r}, nil
				})})
				var err error
				if tc.token {
					_, err = provider.Refresh(context.Background(), "refresh")
				} else if private {
					_, err = provider.ListPrivateEvents(context.Background(), "access", time.Now(), time.Now().Add(time.Hour))
				} else {
					_, err = provider.ListChanges(context.Background(), "access", "", time.Now(), time.Now().Add(time.Hour))
				}
				if err == nil || errors.Is(err, ErrReconnectRequired) != tc.reconnect || failureCode(err) != tc.code {
					t.Fatalf("classification: %v (%s), want %s reconnect=%v", err, failureCode(err), tc.code, tc.reconnect)
				}
				if strings.Contains(err.Error(), "private-secret") {
					t.Fatal("provider body leaked")
				}
			})
		}
	}
}
