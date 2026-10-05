package request

import (
	"context"
	"errors"
	"testing"
	"time"
)

type candidateStepContext struct {
	context.Context
	calls, stop int
}

func (ctx *candidateStepContext) Err() error {
	ctx.calls++
	if ctx.calls >= ctx.stop {
		return context.Canceled
	}
	return nil
}

func TestCandidateCancellationDiscardsEveryIntermediateResult(t *testing.T) {
	input := continuousCandidateInput(60)
	input.Reserved = []ReservedRange{{input.Now.Add(-time.Hour), input.Now}}
	trace := &candidateStepContext{Context: context.Background(), stop: 1000000}
	if got, err := generateCandidates(trace, input); err != nil || len(got) != 3 {
		t.Fatal("invalid fixture", err)
	}
	// Deterministic cancellation at every cooperative checkpoint, including after
	// accumulating candidate scores; never rely on timing/sleep to hit the race.
	for step := 1; step <= trace.calls; step++ {
		ctx := &candidateStepContext{Context: context.Background(), stop: step}
		if got, err := generateCandidates(ctx, input); !errors.Is(err, context.Canceled) || got != nil {
			t.Fatalf("partial result at step %d: %v", step, err)
		}
	}
}

func TestCandidateContextHonorsParentCancellationAndDeadline(t *testing.T) {
	for _, preference := range []SyncPreference{Either, AsyncPreferred} {
		input := continuousCandidateInput(30)
		input.Request.SyncPreference = preference
		cancelled, cancel := context.WithCancel(context.Background())
		cancel()
		if got, err := GenerateCandidatesContext(cancelled, input); !errors.Is(err, context.Canceled) || got != nil {
			t.Fatal("cancelled request generated options", err)
		}
		expired, release := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
		defer release()
		if got, err := GenerateCandidatesContext(expired, input); !errors.Is(err, context.DeadlineExceeded) || got != nil {
			t.Fatal("deadline ignored", err)
		}
	}
}
