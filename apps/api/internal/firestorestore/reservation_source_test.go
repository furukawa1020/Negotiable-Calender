package firestorestore

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	coord "github.com/negotiable-calendar/negotiable-calendar/apps/api/internal/request"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func seedReservationParticipants(t *testing.T, b *Backend, ctx context.Context) {
	t.Helper()
	for _, user := range []string{"alice", "bob"} {
		putDocument(t, ctx, b.Client.Collection("users").Doc(user), userRecord{ID: user})
	}
}

func TestReservationSourceBoundedAndMinimalAcrossBothRoles(t *testing.T) {
	b, ctx := emulatorBackend(t)
	seedReservationParticipants(t, b, ctx)
	now := time.Now().UTC().Truncate(time.Second)
	seedReservationHistory(t, b, ctx, 5001, false, now, now.Add(-time.Hour))
	// One reservation appears in both participants' queries, but only once in output.
	for i, pair := range [][2]string{{"alice", "bob"}, {"elsewhere", "alice"}, {"bob", "elsewhere"}} {
		v := confirmationRequest("reserved-"+pair[0], pair[0], pair[1], now, now.Add(time.Duration(i+1)*time.Hour))
		v.OrganizationID, v.Status, v.AcceptedOptionID = "another-workspace", coord.Accepted, v.Options[0].ID
		putDocument(t, ctx, b.Client.Collection("coordinationRequests").Doc(v.ID), v)
	}
	trace := &planningReadTrace{}
	ranges, err := planningReader(t, ctx, trace).LoadConfirmedRanges(ctx, "alice", "bob")
	if err != nil || len(ranges) != 3 || trace.documents != 4 || !reflect.DeepEqual(trace.limits, []int32{5001, 5001, 5001, 5001}) {
		t.Fatalf("unbounded/incomplete read: ranges=%d documents=%d limits=%v err=%v", len(ranges), trace.documents, trace.limits, err)
	}
	for i, query := range trace.queries {
		fields := query.GetSelect().GetFields()
		if len(fields) != 6 {
			t.Fatal("missing data-minimal field selection")
		}
		for _, field := range fields {
			if field.GetFieldPath() == "Title" || field.GetFieldPath() == "AsyncMessage" {
				t.Fatal("private text fetched")
			}
		}
		if trace.budgets[i] <= 0 || trace.budgets[i] > coord.ReservationReadTimeout {
			t.Fatal("unbounded read deadline")
		}
	}
}

func TestReservationSourceRejectsDamagedRecordsAndAccounts(t *testing.T) {
	for _, scenario := range []string{"missing-option", "wrong-request", "duplicate-option", "invalid-time", "unknown-type", "wrong-id", "deleting", "missing-user", "query-failure", "cancelled"} {
		t.Run(scenario, func(t *testing.T) {
			b, ctx := emulatorBackend(t)
			seedReservationParticipants(t, b, ctx)
			now := time.Now().UTC()
			v := confirmationRequest("unsafe", "alice", "bob", now, now.Add(time.Hour))
			v.Status, v.AcceptedOptionID = coord.Accepted, v.Options[0].ID
			switch scenario {
			case "missing-option":
				v.Options = nil
			case "wrong-request":
				v.Options[0].RequestID = "another"
			case "duplicate-option":
				v.Options = append(v.Options, v.Options[0])
			case "invalid-time":
				v.Options[0].EndAt = v.Options[0].StartAt
			case "unknown-type":
				v.Options[0].Type = "unknown"
			case "wrong-id":
				v.ID = "wrong"
			case "deleting":
				putDocument(t, ctx, b.accountDeletionRef("bob"), accountDeletion{Phase: "deleting"})
			case "missing-user":
				if _, err := b.Client.Collection("users").Doc("bob").Delete(ctx); err != nil {
					t.Fatal(err)
				}
			}
			putDocument(t, ctx, b.Client.Collection("coordinationRequests").Doc("unsafe"), v)
			trace := &planningReadTrace{}
			if scenario == "query-failure" {
				trace.afterQuery = func(int) error { return status.Error(codes.FailedPrecondition, "synthetic failure") }
			}
			reader := planningReader(t, ctx, trace)
			if scenario == "cancelled" {
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			}
			if ranges, err := reader.LoadConfirmedRanges(ctx, "alice", "bob"); err == nil || ranges != nil {
				t.Fatal("unsafe or partial ranges returned", err)
			}
		})
	}
}

func TestReservationSourceOverflowDiscardsAllRanges(t *testing.T) {
	b, ctx := emulatorBackend(t)
	seedReservationParticipants(t, b, ctx)
	now := time.Now().UTC()
	seedReservationHistory(t, b, ctx, 5001, true, now, now.Add(time.Hour))
	if ranges, err := b.Request().LoadConfirmedRanges(ctx, "alice", "bob"); !errors.Is(err, coord.ErrReservationUnavailable) || ranges != nil {
		t.Fatal("overflow returned partial availability", err)
	}
}

func TestReservationSourceUsesOneReadOnlySnapshot(t *testing.T) {
	b, ctx := emulatorBackend(t)
	seedReservationParticipants(t, b, ctx)
	now := time.Now().UTC()
	v := confirmationRequest("first", "alice", "carol", now, now.Add(time.Hour))
	v.Status, v.AcceptedOptionID = coord.Accepted, v.Options[0].ID
	ref := b.Client.Collection("coordinationRequests").Doc(v.ID)
	putDocument(t, ctx, ref, v)
	second := confirmationRequest("second", "elsewhere", "bob", now, now.Add(2*time.Hour))
	second.Status, second.AcceptedOptionID = coord.Accepted, second.Options[0].ID
	secondRef := b.Client.Collection("coordinationRequests").Doc(second.ID)
	putDocument(t, ctx, secondRef, second)
	trace := &planningReadTrace{afterQuery: func(query int) error {
		if query != 1 {
			return nil
		}
		start, end := now.Add(3*time.Hour), now.Add(4*time.Hour)
		second.Options[0].StartAt, second.Options[0].EndAt = &start, &end
		_, err := secondRef.Set(ctx, second)
		return err
	}}
	ranges, err := planningReader(t, ctx, trace).LoadConfirmedRanges(ctx, "alice", "bob")
	if err != nil || len(ranges) != 2 {
		t.Fatal("incomplete reservation snapshot", ranges, err)
	}
	for _, value := range ranges {
		if value.StartAt.After(now.Add(2 * time.Hour)) {
			t.Fatal("mixed reservation snapshot", ranges)
		}
	}
}
