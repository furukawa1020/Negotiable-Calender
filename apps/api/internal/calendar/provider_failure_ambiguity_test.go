package calendar

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestAmbiguousProviderFailureCannotForceReconnect(t *testing.T) {
	for _, tc := range []struct {
		body  string
		token bool
	}{
		{`{"error":"private-secret","error":"invalid_grant"}`, true},
		{`{"error":"invalid_grant","ERROR":"invalid_client"}`, true},
		{`{"error":"invalid_grant","nested":{"a":1,"a":2}}`, true},
		{`{"error":{"errors":[{"reason":"private-secret","reason":"insufficientPermissions"}]}}`, false},
		{`{"error":{"errors":[{"reason":"insufficientPermissions","rea\u017fon":"rateLimitExceeded"}]}}`, false},
		{`{"error":{},"ERROR":{"errors":[{"reason":"insufficientPermissions"}]}}`, false},
		{`{"error":{"errors":[],"ERRORS":[{"reason":"insufficientPermissions"}]}}`, false},
		{`{"error":"invalid_grant","nested":` + strings.Repeat("[", providerJSONDepth) + `0` + strings.Repeat("]", providerJSONDepth) + `}`, true},
	} {
		for _, private := range []bool{false, true} {
			body := &countedCalendarBody{reader: strings.NewReader(tc.body)}
			status, want := 403, "provider_denied"
			if tc.token {
				status, want = 400, "temporary_failure"
			}
			p := NewGoogleProvider(GoogleConfig{}, &http.Client{Transport: limitTransport(func(r *http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: status, Body: body, Header: make(http.Header), Request: r}, nil
			})})
			var err error
			if tc.token {
				_, err = p.Refresh(context.Background(), "refresh")
			} else if private {
				_, err = p.ListPrivateEvents(context.Background(), "access", time.Now(), time.Now().Add(time.Hour))
			} else {
				_, err = p.ListChanges(context.Background(), "access", "", time.Now(), time.Now().Add(time.Hour))
			}
			if err == nil || errors.Is(err, ErrReconnectRequired) || failureCode(err) != want || !body.closed {
				t.Fatalf("ambiguous failure classified as authority: %v", err)
			}
			if strings.Contains(err.Error(), "private-secret") {
				t.Fatal("provider details leaked")
			}
		}
	}
}
