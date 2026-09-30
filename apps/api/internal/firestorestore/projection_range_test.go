package firestorestore

import (
	"reflect"
	"testing"
	"time"

	"github.com/negotiable-calendar/negotiable-calendar/apps/api/internal/projection"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestProjectionRangeReadsOnlyOverlapsAndPreservesFullExport(t *testing.T) {
	b, ctx := emulatorBackend(t)
	now := time.Now().UTC().Truncate(time.Second)
	from, to := now.Add(time.Hour), now.Add(2*time.Hour)
	rows := publicationFixtures(now, "outside", 600)
	for i := range rows {
		if i < 300 {
			rows[i].StartAt = from.Add(-time.Duration(i+1) * projection.BucketSize)
		} else {
			rows[i].StartAt = to.Add(time.Duration(i-300) * projection.BucketSize)
		}
		rows[i].EndAt = rows[i].StartAt.Add(projection.BucketSize)
	}
	inside := publicationFixtures(now, "inside", 4)
	inside[0].ID, inside[0].StartAt, inside[0].EndAt = "span", from.Add(-time.Hour), to.Add(time.Hour)
	// The index orders by end time; the public contract orders equal starts by ID.
	inside[1].ID, inside[1].StartAt, inside[1].EndAt = "z", from, from.Add(15*time.Minute)
	inside[2].ID, inside[2].StartAt, inside[2].EndAt = "a", from, from.Add(30*time.Minute)
	inside[3].ID, inside[3].StartAt, inside[3].EndAt = "expired", from, to
	inside[3].ExpiresAt = now.Add(-time.Second)
	rows = append(rows, inside...)
	if err := b.Projection().Replace(ctx, "alice", from.Add(-7*24*time.Hour), to.Add(7*24*time.Hour), rows); err != nil {
		t.Fatal(err)
	}
	trace := &planningReadTrace{}
	reader := planningReader(t, ctx, trace).Backend.Projection()
	values, err := reader.List(ctx, "alice", from, to)
	if err != nil || len(values) != 3 || trace.documents != 4 {
		t.Fatalf("range read: returned=%d fetched=%d error=%v", len(values), trace.documents, err)
	}
	ids := []string{values[0].ID, values[1].ID, values[2].ID}
	if !reflect.DeepEqual(ids, []string{"span", "a", "z"}) {
		t.Fatalf("unstable public ordering: %v", ids)
	}
	*trace = planningReadTrace{}
	view, err := reader.GetView(ctx, "alice", "Asia/Tokyo", from, to)
	if err != nil || len(view.Segments) != 3 || trace.documents != 4 {
		t.Fatalf("calendar view: segments=%d fetched=%d error=%v", len(view.Segments), trace.documents, err)
	}
	*trace = planningReadTrace{}
	all, err := reader.ListForUser(ctx, "alice")
	if err != nil || len(all) != 603 || trace.documents != 604 {
		t.Fatalf("full export was restricted: returned=%d fetched=%d error=%v", len(all), trace.documents, err)
	}
}

func TestProjectionRangeRejectsInvalidWindowBeforeReads(t *testing.T) {
	_, ctx := emulatorBackend(t)
	trace := &planningReadTrace{}
	reader := planningReader(t, ctx, trace).Backend.Projection()
	now := time.Now().UTC()
	for _, end := range []time.Time{now, now.Add(-time.Hour)} {
		if rows, err := reader.List(ctx, "alice", now, end); err == nil || rows != nil {
			t.Fatal("invalid interval accepted")
		}
	}
	if len(trace.queries) != 0 || trace.points != 0 {
		t.Fatal("invalid interval performed database reads")
	}
}

func TestProjectionRangeDiscardsChangedPublicationAndQueryFailure(t *testing.T) {
	for _, scenario := range []string{"new-publication", "dirty", "disconnect", "query-error"} {
		t.Run(scenario, func(t *testing.T) {
			b, ctx := emulatorBackend(t)
			from, to := seedPlanning(t, b, ctx)
			trace := &planningReadTrace{afterQuery: func(int) error {
				switch scenario {
				case "new-publication", "dirty":
					_, err := b.projectionPublicationRef("alice").Set(ctx, projectionPublication{ID: "new", Ready: true, Dirty: scenario == "dirty"})
					return err
				case "disconnect":
					_, err := b.projectionBlock("alice").Set(ctx, map[string]bool{"blocked": true})
					return err
				default:
					return status.Error(codes.FailedPrecondition, "synthetic query failure")
				}
			}}
			reader := planningReader(t, ctx, trace).Backend.Projection()
			rows, err := reader.List(ctx, "alice", from, to)
			if len(rows) != 0 || trace.documents != 1 {
				t.Fatal("returned partial or changed publication", err)
			}
			if scenario == "query-error" {
				if status.Code(err) != codes.FailedPrecondition {
					t.Fatal("query failure ignored", err)
				}
			} else if err != nil {
				t.Fatal(err)
			}
		})
	}
}
