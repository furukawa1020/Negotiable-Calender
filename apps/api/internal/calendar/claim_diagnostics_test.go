package calendar

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"testing"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type failingClaimStore struct {
	backgroundStubStore
	cause error
}

func (s *failingClaimStore) ClaimDueConnections(context.Context, time.Time, int, time.Duration) ([]Connection, error) {
	return nil, s.cause
}

func TestClaimFailureCategoriesNeverLogProviderText(t *testing.T) {
	secret := "secret-token private-user https://console.example/index?private=payload"
	for _, tc := range []struct {
		code codes.Code
		want string
	}{
		{codes.FailedPrecondition, "failed_precondition"}, {codes.PermissionDenied, "permission_denied"},
		{codes.Unauthenticated, "unauthenticated"}, {codes.ResourceExhausted, "resource_exhausted"},
		{codes.Unavailable, "unavailable"}, {codes.DeadlineExceeded, "deadline_exceeded"},
		{codes.Canceled, "canceled"}, {codes.NotFound, "not_found"}, {codes.InvalidArgument, "invalid_argument"},
		{codes.Aborted, "aborted"}, {codes.Internal, "internal"}, {codes.Unknown, "unknown"}, {codes.Code(999), "unknown"},
	} {
		t.Run(tc.want+tc.code.String(), func(t *testing.T) {
			cause := fmt.Errorf("query failed %s: %w", secret, status.Error(tc.code, secret))
			store := &failingClaimStore{cause: cause}
			syncer := &slowSyncer{}
			var logs bytes.Buffer
			worker := NewWorker(store, syncer, WorkerConfig{}, slog.New(slog.NewJSONHandler(&logs, nil)))
			result, err := worker.RunDue(context.Background())
			if !errors.Is(err, cause) || result.Attempted != 0 || syncer.calls != 0 {
				t.Fatal("claim failure started work or was hidden")
			}
			var event map[string]any
			if json.Unmarshal(logs.Bytes(), &event) != nil || event["failure_code"] != "claim_failed" || event["cause_code"] != tc.want {
				t.Fatalf("missing safe classification: %s", logs.String())
			}
			for _, value := range []string{secret, "secret-token", "private-user", "https://console.example", "query failed"} {
				if strings.Contains(logs.String(), value) {
					t.Fatal("provider data leaked")
				}
			}
		})
	}
}

func TestClaimFailureUnknownAndContextErrors(t *testing.T) {
	for _, tc := range []struct {
		err  error
		want string
	}{
		{nil, "unknown"}, {errors.New("secret-index-url"), "unknown"},
		{fmt.Errorf("secret: %w", context.Canceled), "canceled"},
		{fmt.Errorf("secret: %w", context.DeadlineExceeded), "deadline_exceeded"},
	} {
		if got := claimCauseCode(tc.err); got != tc.want {
			t.Fatalf("got %s, want %s", got, tc.want)
		}
	}
}
