package firestorestore

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"reflect"
	"testing"
	"time"

	"cloud.google.com/go/firestore"
	"cloud.google.com/go/firestore/apiv1/firestorepb"
	request "github.com/negotiable-calendar/negotiable-calendar/apps/api/internal/request"
	"google.golang.org/api/option"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
)

// Observe actual SDK RPCs, not the lengths of post-query application slices.
type planningReadTrace struct {
	limits            []int32
	queries           []*firestorepb.StructuredQuery
	budgets           []time.Duration
	documents, points int
	afterQuery        func(int) error
}
type planningTraceStream struct {
	grpc.ClientStream
	trace *planningReadTrace
	query int
}

func (s *planningTraceStream) SendMsg(m any) error {
	switch v := m.(type) {
	case *firestorepb.RunQueryRequest:
		s.trace.limits = append(s.trace.limits, v.GetStructuredQuery().GetLimit().GetValue())
		s.trace.queries = append(s.trace.queries, v.GetStructuredQuery())
		if deadline, ok := s.Context().Deadline(); ok {
			s.trace.budgets = append(s.trace.budgets, time.Until(deadline))
		}
		s.query = len(s.trace.limits)
	case *firestorepb.BatchGetDocumentsRequest:
		s.trace.points += len(v.Documents)
	}
	return s.ClientStream.SendMsg(m)
}
func (s *planningTraceStream) RecvMsg(m any) error {
	err := s.ClientStream.RecvMsg(m)
	if r, ok := m.(*firestorepb.RunQueryResponse); ok && err == nil && r.Document != nil {
		s.trace.documents++
	}
	if errors.Is(err, io.EOF) && s.query > 0 && s.trace.afterQuery != nil {
		if hookErr := s.trace.afterQuery(s.query); hookErr != nil {
			return hookErr
		}
	}
	return err
}
func planningReader(t *testing.T, ctx context.Context, trace *planningReadTrace) *Request {
	t.Helper()
	intercept := func(ctx context.Context, desc *grpc.StreamDesc, cc *grpc.ClientConn, method string, streamer grpc.Streamer, opts ...grpc.CallOption) (grpc.ClientStream, error) {
		ctx = metadata.AppendToOutgoingContext(ctx, "authorization", "Bearer owner")
		stream, err := streamer(ctx, desc, cc, method, opts...)
		if err != nil {
			return nil, err
		}
		return &planningTraceStream{ClientStream: stream, trace: trace}, nil
	}
	// The SDK creates its own emulator connection and ignores dial options.
	// Pass the traced connection itself so assertions observe real RPCs.
	address := os.Getenv("FIRESTORE_EMULATOR_HOST")
	if address == "" {
		t.Fatal("emulator required; never trace production")
	}
	conn, err := grpc.NewClient("passthrough:///"+address, grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithStreamInterceptor(intercept))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	client, err := firestore.NewClient(ctx, "demo-nc-"+safeDigest(t.Name())[:16], option.WithGRPCConn(conn))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { client.Close() })
	return (&Backend{Client: client}).Request()
}
func seedPlanning(t *testing.T, b *Backend, ctx context.Context) (time.Time, time.Time) {
	t.Helper()
	now := time.Now().UTC().Truncate(time.Second)
	for _, user := range []string{"alice", "bob"} {
		putDocument(t, ctx, b.Client.Collection("users").Doc(user), userRecord{ID: user})
	}
	value := publicationFixtures(now, "p", 1)[0]
	value.StartAt = now.Add(time.Hour)
	value.EndAt = now.Add(2 * time.Hour)
	putDocument(t, ctx, b.Client.Collection("users").Doc("alice").Collection("scheduleProjections").Doc(value.ID), value)
	return value.StartAt, value.EndAt
}

func TestPlanningBoundedQueriesIgnoreLargeClosedHistory(t *testing.T) {
	b, ctx := emulatorBackend(t)
	from, to := seedPlanning(t, b, ctx)
	batch := b.Client.Batch()
	for i := 0; i < 200; i++ {
		value := confirmationRequest(fmt.Sprintf("history-%d", i), "bob", "alice", time.Now().UTC(), from)
		value.Status = request.Completed
		batch.Set(b.Client.Collection("coordinationRequests").Doc(value.ID), value)
	}
	// The new single range index skips historical projections server-side.
	for i := 0; i < 300; i++ {
		value := publicationFixtures(from.Add(-48*time.Hour), fmt.Sprintf("old-%d", i), 1)[0]
		batch.Set(b.Client.Collection("users").Doc("alice").Collection("scheduleProjections").Doc(value.ID), value)
	}
	if _, err := batch.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	booking := confirmationRequest("overlap", "alice", "elsewhere", time.Now().UTC(), from)
	booking.OrganizationID = "another-workspace"
	booking.Status = request.Accepted
	booking.AcceptedOptionID = booking.Options[0].ID
	putDocument(t, ctx, b.Client.Collection("coordinationRequests").Doc(booking.ID), booking)
	trace := &planningReadTrace{}
	reader := planningReader(t, ctx, trace)
	segments, bookings, err := reader.LoadPlanningSources(ctx, "alice", "bob", from, to)
	if err != nil || len(segments) != 1 || len(bookings) != 1 {
		t.Fatalf("segments=%d bookings=%d error=%v", len(segments), len(bookings), err)
	}
	if !request.ConflictsWithMeeting(booking.Options[0], bookings[0]) {
		t.Fatal("cross-workspace conflict omitted")
	}
	if !reflect.DeepEqual(trace.limits, []int32{257, 65, 65, 65, 65}) || trace.documents != 2 || trace.points > 26 {
		t.Fatalf("unbounded RPCs: %+v", trace)
	}
}

func TestPlanningReadBudgetBoundariesDiscardOverflow(t *testing.T) {
	for _, kind := range []string{"projections", "bookings"} {
		for _, overflow := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/%t", kind, overflow), func(t *testing.T) {
				b, ctx := emulatorBackend(t)
				from, to := seedPlanning(t, b, ctx)
				trace := &planningReadTrace{}
				reader := planningReader(t, ctx, trace)
				limit := planningProjectionLimit
				if kind == "bookings" {
					limit = planningBookingRoleLimit
				}
				count := limit
				if overflow {
					count++
				}
				batch := b.Client.Batch()
				if kind == "projections" {
					// Replace the seed ID so the total is exactly count.
					for i := 0; i < count; i++ {
						p := publicationFixtures(from, fmt.Sprintf("row-%d", i), 1)[0]
						if i == 0 {
							p.ID = "p-0000"
						}
						p.EndAt = to
						batch.Set(b.Client.Collection("users").Doc("alice").Collection("scheduleProjections").Doc(p.ID), p)
					}
				} else {
					for i := 0; i < count; i++ {
						r := confirmationRequest(fmt.Sprintf("booking-%d", i), "elsewhere", "alice", time.Now().UTC(), from)
						r.Status = request.Accepted
						r.AcceptedOptionID = r.Options[0].ID
						batch.Set(b.Client.Collection("coordinationRequests").Doc(r.ID), r)
					}
				}
				if _, err := batch.Commit(ctx); err != nil {
					t.Fatal(err)
				}
				segments, bookings, err := reader.LoadPlanningSources(ctx, "alice", "bob", from, to)
				if overflow {
					if !errors.Is(err, errPlanningLimit) || segments != nil || bookings != nil {
						t.Fatalf("partial success on overflow: %v", err)
					}
				} else if err != nil {
					t.Fatal(err)
				}
				if trace.documents > 517 || trace.points > 26 || len(trace.limits) > 5 {
					t.Fatalf("read budget exceeded: %+v", trace)
				}
				for _, limit := range trace.limits {
					if limit != 65 && limit != 257 {
						t.Fatalf("missing server limit %d", limit)
					}
				}
			})
		}
	}
}

func TestPlanningSourceFenceAndDeletedAccounts(t *testing.T) {
	for _, change := range []string{"target-deleted", "requester-deleted", "requester-missing", "dirty-publication", "disconnect"} {
		t.Run(change, func(t *testing.T) {
			b, ctx := emulatorBackend(t)
			from, to := seedPlanning(t, b, ctx)
			trace := &planningReadTrace{afterQuery: func(query int) error {
				if query != 5 {
					return nil
				}
				switch change {
				case "target-deleted":
					_, err := b.accountDeletionRef("alice").Set(ctx, accountDeletion{Phase: "deleting"})
					return err
				case "requester-deleted":
					_, err := b.accountDeletionRef("bob").Set(ctx, accountDeletion{Phase: "complete"})
					return err
				case "requester-missing":
					_, err := b.Client.Collection("users").Doc("bob").Delete(ctx)
					return err
				case "dirty-publication":
					_, err := b.projectionPublicationRef("alice").Set(ctx, projectionPublication{ID: "changed", Dirty: true})
					return err
				default:
					_, err := b.projectionBlock("alice").Set(ctx, map[string]bool{"blocked": true})
					return err
				}
			}}
			reader := planningReader(t, ctx, trace)
			segments, bookings, err := reader.LoadPlanningSources(ctx, "alice", "bob", from, to)
			if err == nil || segments != nil || bookings != nil {
				t.Fatal("changed source/account returned data")
			}
		})
	}
}

func TestPlanningPreservesCorruptBookingAndStaleProjectionEvidence(t *testing.T) {
	b, ctx := emulatorBackend(t)
	from, to := seedPlanning(t, b, ctx)
	p := publicationFixtures(time.Now().UTC(), "p", 1)[0]
	p.StartAt = from
	p.EndAt = to
	p.ExpiresAt = time.Now().UTC().Add(-time.Minute)
	putDocument(t, ctx, b.Client.Collection("users").Doc("alice").Collection("scheduleProjections").Doc(p.ID), p)
	putDocument(t, ctx, b.Client.Collection("coordinationRequests").Doc("corrupt"), request.CoordinationRequest{ID: "corrupt", TargetUserID: "alice", Status: request.Accepted})
	segments, bookings, err := b.Request().LoadPlanningSources(ctx, "alice", "bob", from, to)
	if err != nil || len(segments) != 1 || len(bookings) != 1 {
		t.Fatalf("evidence filtered out: %v", err)
	}
	meeting := confirmationRequest("candidate", "bob", "alice", time.Now().UTC(), from).Options[0]
	if request.ValidateMeetingAvailability("alice", meeting, segments, time.Now().UTC()) == nil || !request.ConflictsWithMeeting(meeting, bookings[0]) {
		t.Fatal("stale/corrupt data incorrectly became availability")
	}
}
