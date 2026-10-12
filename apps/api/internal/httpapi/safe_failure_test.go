package httpapi

import (
	"context"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"log/slog"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	coord "github.com/negotiable-calendar/negotiable-calendar/apps/api/internal/request"
)

func TestStorageFailuresNeverExposeRawErrors(t *testing.T) {
	for _, endpoint := range []string{"/api/v1/notifications", "/api/v1/notifications/n/read", "/api/v1/audit-logs", "/api/v1/requests/r", "/api/v1/users/alice/sharing-policy", "/api/v1/requests/r/cancel-confirmed", "/api/v1/requests/r/reschedule"} {
		for _, cause := range []error{errors.New("private-secret"), fmt.Errorf("private-secret: %w", context.DeadlineExceeded), fmt.Errorf("private-secret: %w", context.Canceled)} {
			t.Run(endpoint+"/"+safeFailureCode(cause), func(t *testing.T) {
				var logs strings.Builder
				var store coord.Store = &stubRequestStore{getErr: cause}
				method, body := "GET", ""
				if strings.HasSuffix(endpoint, "/read") {
					method = "POST"
				}
				if strings.HasSuffix(endpoint, "/cancel-confirmed") {
					store = &scopedCancellationStub{cancellationStore: cancellationStore{failure: cause}}
					method, body = "POST", `{"optionId":"selected"}`
				}
				if strings.HasSuffix(endpoint, "/reschedule") {
					store = &scopedRescheduleStub{rescheduleStore: rescheduleStore{failure: cause}}
					method, body = "POST", `{"action":"accept","proposalId":"proposal","expectedOptionId":"selected"}`
				}
				handler := NewWithStores(nil, &stubPolicyStore{err: cause}, nil, &stubOrganizationStore{}, store, &stubNotificationStore{err: cause}, &stubAuditStore{err: cause}, "https://app.test", slog.New(slog.NewTextHandler(&logs, nil)))
				r := httptest.NewRequest(method, endpoint, strings.NewReader(body))
				r.Header.Set("X-Demo-User-ID", "alice")
				r.Header.Set("X-Organization-ID", "org")
				r.Header.Set("Origin", "https://app.test")
				w := httptest.NewRecorder()
				handler.ServeHTTP(w, r)
				if w.Code != 500 || strings.Contains(w.Body.String(), "private-secret") || strings.Contains(logs.String(), "private-secret") || !strings.Contains(logs.String(), "failure_code="+safeFailureCode(cause)) {
					t.Fatalf("unsafe failure handling status=%d response=%s logs=%s", w.Code, w.Body.String(), logs.String())
				}
				if w.Header().Get("Cache-Control") != "no-store" || w.Header().Get("Access-Control-Allow-Origin") != "https://app.test" {
					t.Fatal("cache/CORS regression")
				}
			})
		}
	}
}

func TestWrappedCancellationConflictsUseFixedPublicCodes(t *testing.T) {
	for _, conflict := range []error{coord.ErrCancellationInvalid, coord.ErrBookingConflict} {
		var logs strings.Builder
		store := &scopedCancellationStub{cancellationStore: cancellationStore{failure: fmt.Errorf("private-secret: %w", conflict)}}
		handler := New(nil, nil, nil, nil, store, "", slog.New(slog.NewTextHandler(&logs, nil)))
		r := httptest.NewRequest("POST", "/api/v1/requests/r/cancel-confirmed", strings.NewReader(`{"optionId":"selected"}`))
		r.Header.Set("X-Demo-User-ID", "alice")
		r.Header.Set("X-Organization-ID", "org")
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		if w.Code != 409 || !strings.Contains(w.Body.String(), `"code":"`+conflict.Error()+`"`) || strings.Contains(w.Body.String()+logs.String(), "private-secret") {
			t.Fatal("wrapped domain error leaked")
		}
	}
}

func TestHTTPErrorLogsKeepStaticLabelsAndNeverRawErrorAttributes(t *testing.T) {
	files, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range files {
		if !strings.HasSuffix(file.Name(), ".go") || strings.HasSuffix(file.Name(), "_test.go") {
			continue
		}
		parsed, err := parser.ParseFile(token.NewFileSet(), file.Name(), nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		ast.Inspect(parsed, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok {
				return true
			}
			method, ok := call.Fun.(*ast.SelectorExpr)
			if !ok || method.Sel.Name != "Error" {
				return true
			}
			logger, ok := method.X.(*ast.SelectorExpr)
			if !ok || logger.Sel.Name != "logger" {
				return true
			}
			if len(call.Args) == 0 {
				t.Fatal("empty log")
			}
			if label, ok := call.Args[0].(*ast.BasicLit); !ok || label.Kind != token.STRING {
				t.Fatalf("non-static operation label in %s", file.Name())
			}
			for _, arg := range call.Args[1:] {
				if literal, ok := arg.(*ast.BasicLit); ok && literal.Value == `"error"` {
					t.Fatalf("raw error logging in %s", file.Name())
				}
				if name, ok := arg.(*ast.Ident); ok && name.Name == "err" {
					t.Fatalf("raw error argument in %s", file.Name())
				}
			}
			return true
		})
	}
}
