package calendar

import (
	"context"
	"io"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"google.golang.org/api/idtoken"
)

type lateBatchFunc func(context.Context) (BatchResult, error)

func (f lateBatchFunc) RunDue(ctx context.Context) (BatchResult, error) { return f(ctx) }

func TestSchedulerRejectsLateIdentity(t *testing.T) {
	for _, expired := range []bool{false, true} {
		runner := &batchStub{}
		h := schedulerFixture(runner, io.Discard)
		ctx, cancel := context.WithCancel(context.Background())
		if expired {
			cancel()
			ctx, cancel = context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
		}
		h.validate = func(context.Context, string, string) (*idtoken.Payload, error) {
			cancel()
			return validSchedulerPayload(h.config), nil
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, schedulerRequest().WithContext(ctx))
		cancel()
		if w.Code != 401 || runner.calls != 0 || w.Header().Get("Cache-Control") != "no-store" {
			t.Fatal("late authorization admitted", w.Code, runner.calls)
		}
	}
}

func TestSchedulerLateBatchIsNotCompletedAndReleasesLock(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	calls := 0
	h := schedulerFixture(lateBatchFunc(func(context.Context) (BatchResult, error) {
		calls++
		if calls == 1 {
			cancel()
		}
		return BatchResult{Claimed: 1, Attempted: 1, Succeeded: 1}, nil
	}), io.Discard)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, schedulerRequest().WithContext(ctx))
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"status":"budget_exhausted"`) {
		t.Fatal("late batch claimed completion", w.Code, w.Body)
	}
	w = httptest.NewRecorder()
	h.ServeHTTP(w, schedulerRequest())
	if w.Code != 200 || calls != 2 || !strings.Contains(w.Body.String(), `"status":"completed"`) {
		t.Fatal("batch lock leaked", w.Code, w.Body)
	}
}
