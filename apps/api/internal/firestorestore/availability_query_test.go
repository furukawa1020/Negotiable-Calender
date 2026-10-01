package firestorestore

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"reflect"
	"testing"
	"time"

	coord "github.com/negotiable-calendar/negotiable-calendar/apps/api/internal/request"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestAvailabilityQueryPreflightIsBoundedReadOnly(t *testing.T) {
	b, ctx := emulatorBackend(t)
	ref := b.Client.Collection("users").Doc("_availability-index-probe").Collection("scheduleProjections").Doc("synthetic")
	from := time.Unix(0, 0).UTC()
	putDocument(t, ctx, ref, map[string]any{"StartAt": from, "EndAt": from.Add(time.Hour), "UserID": 42, "PrivatePayload": "never-fetch"})
	before, err := ref.Get(ctx)
	if err != nil {
		t.Fatal(err)
	}
	bookingRef := b.Client.Collection("coordinationRequests").Doc("synthetic-booking")
	putDocument(t, ctx, bookingRef, map[string]any{"RequesterUserID": "_availability-index-probe", "TargetUserID": "_availability-index-probe", "Status": coord.Accepted, "Options": "invalid-private-payload"})
	bookingBefore, err := bookingRef.Get(ctx)
	if err != nil {
		t.Fatal(err)
	}
	trace := &planningReadTrace{}
	if err := planningReader(t, ctx, trace).CheckAvailabilityQueries(ctx); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(trace.limits, []int32{1, 1, 1}) || trace.documents != 3 || trace.points != 0 || len(trace.budgets) != 3 {
		t.Fatalf("unbounded preflight: %+v", trace)
	}
	for i, query := range trace.queries {
		fields := query.GetSelect().GetFields()
		if len(fields) != 1 || fields[0].GetFieldPath() != "__name__" {
			t.Fatal("preflight fetched private fields")
		}
		if trace.budgets[i] <= 0 || trace.budgets[i] > 5*time.Second {
			t.Fatal("unbounded preflight deadline")
		}
	}
	filters := trace.queries[0].GetWhere().GetCompositeFilter().GetFilters()
	if len(filters) != 2 || filters[0].GetFieldFilter().GetField().GetFieldPath() != "StartAt" || filters[1].GetFieldFilter().GetField().GetFieldPath() != "EndAt" {
		t.Fatal("preflight did not exercise both range predicates")
	}
	for i, role := range []string{"RequesterUserID", "TargetUserID"} {
		filters := trace.queries[i+1].GetWhere().GetCompositeFilter().GetFilters()
		if len(filters) != 2 || filters[0].GetFieldFilter().GetField().GetFieldPath() != role || filters[1].GetFieldFilter().GetField().GetFieldPath() != "Status" || filters[1].GetFieldFilter().GetValue().GetStringValue() != string(coord.Accepted) {
			t.Fatal("preflight did not exercise participant + accepted-status predicates")
		}
	}
	after, err := ref.Get(ctx)
	if err != nil || !before.UpdateTime.Equal(after.UpdateTime) || !reflect.DeepEqual(before.Data(), after.Data()) {
		t.Fatal("preflight mutated records")
	}
	bookingAfter, err := bookingRef.Get(ctx)
	if err != nil || !bookingBefore.UpdateTime.Equal(bookingAfter.UpdateTime) || !reflect.DeepEqual(bookingBefore.Data(), bookingAfter.Data()) {
		t.Fatal("preflight mutated bookings")
	}
}

func TestAvailabilityQueryPreflightPropagatesFailure(t *testing.T) {
	_, ctx := emulatorBackend(t)
	trace := &planningReadTrace{}
	reader := planningReader(t, ctx, trace)
	if err := reader.CheckAvailabilityQueries(ctx); err != nil || trace.documents != 0 || len(trace.queries) != 3 {
		t.Fatal("empty namespace still must execute the indexed query", err)
	}
	for failAt := 1; failAt <= 3; failAt++ {
		*trace = planningReadTrace{afterQuery: func(query int) error {
			if query == failAt {
				return status.Error(codes.FailedPrecondition, "synthetic query failure")
			}
			return nil
		}}
		if err := reader.CheckAvailabilityQueries(ctx); status.Code(err) != codes.FailedPrecondition || len(trace.queries) != failAt {
			t.Fatal("query failure ignored or continued after failure", err)
		}
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if err := reader.CheckAvailabilityQueries(cancelled); !errors.Is(err, context.Canceled) && status.Code(err) != codes.Canceled {
		t.Fatal("cancellation ignored", err)
	}
}

func TestAvailabilityCompositeIndexIsCheckedIn(t *testing.T) {
	data, err := os.ReadFile("../../../../firestore.indexes.json")
	if err != nil {
		t.Fatal(err)
	}
	type field struct{ FieldPath, Order string }
	var config struct {
		Indexes []struct {
			CollectionGroup, QueryScope string
			Fields                      []field
		}
	}
	if err := json.Unmarshal(data, &config); err != nil {
		t.Fatal(err)
	}
	for _, index := range config.Indexes {
		if index.CollectionGroup == "scheduleProjections" && index.QueryScope == "COLLECTION" && reflect.DeepEqual(index.Fields, []field{{"StartAt", "ASCENDING"}, {"EndAt", "ASCENDING"}}) {
			return
		}
	}
	t.Fatal("availability overlap query index is missing")
}
