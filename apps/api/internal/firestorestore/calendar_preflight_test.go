package firestorestore

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"testing"
	"time"

	"cloud.google.com/go/firestore"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestSyncQueryPreflightBoundedReadOnly(t *testing.T) {
	b, ctx := emulatorBackend(t)
	past := time.Now().UTC().Add(-time.Hour)
	before := map[string]*firestore.DocumentSnapshot{}
	for i := 0; i < 12; i++ {
		var next any = past
		if i < 6 {
			next = nil
		}
		id := fmt.Sprintf("preflight-%02d", i)
		ref := b.Client.Collection("calendarConnections").Doc(id)
		// Invalid connection bodies must not be fetched/decoded by the probe.
		putDocument(t, ctx, ref, map[string]any{"ReconnectRequired": false, "NextAttemptAt": next, "UserID": 42, "RefreshTokenCipher": "private-payload"})
		doc, err := ref.Get(ctx)
		if err != nil {
			t.Fatal(err)
		}
		before[id] = doc
	}
	trace := &planningReadTrace{}
	store := planningReader(t, ctx, trace).Backend.Calendar()
	if err := store.CheckSyncQueries(ctx); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(trace.limits, []int32{1, 1}) || trace.documents != 2 || trace.points != 0 {
		t.Fatalf("unexpected read counts: limits=%v documents=%d points=%d", trace.limits, trace.documents, trace.points)
	}
	if len(trace.budgets) != 2 {
		t.Fatal("missing query deadlines")
	}
	for i, query := range trace.queries {
		fields := query.GetSelect().GetFields()
		if len(fields) != 1 || fields[0].GetFieldPath() != "__name__" {
			t.Fatal("probe fetched private document fields")
		}
		if trace.budgets[i] <= 0 || trace.budgets[i] > 5*time.Second {
			t.Fatal("unbounded query deadline")
		}
	}
	for id, original := range before {
		after, err := b.Client.Collection("calendarConnections").Doc(id).Get(ctx)
		if err != nil || !original.UpdateTime.Equal(after.UpdateTime) || !reflect.DeepEqual(original.Data(), after.Data()) {
			t.Fatal("preflight modified connection")
		}
	}
}

func TestSyncQueryPreflightEmptyFailureAndCancellation(t *testing.T) {
	_, ctx := emulatorBackend(t)
	trace := &planningReadTrace{}
	store := planningReader(t, ctx, trace).Backend.Calendar()
	if err := store.CheckSyncQueries(ctx); err != nil || len(trace.limits) != 2 || trace.documents != 0 {
		t.Fatal("empty database is valid but both query shapes must execute")
	}
	trace.limits = nil
	trace.afterQuery = func(int) error { return status.Error(codes.FailedPrecondition, "synthetic missing-index") }
	if err := store.CheckSyncQueries(ctx); status.Code(err) != codes.FailedPrecondition || len(trace.limits) != 1 {
		t.Fatal("query failure ignored or probe continued after failure")
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if err := store.CheckSyncQueries(cancelled); !errors.Is(err, context.Canceled) && status.Code(err) != codes.Canceled {
		t.Fatalf("cancellation not respected: %v", err)
	}
}
