package firestorestore

import (
 "context"
 "errors"
 "testing"
 "time"

 "github.com/negotiable-calendar/negotiable-calendar/apps/api/internal/policy"
 "github.com/negotiable-calendar/negotiable-calendar/apps/api/internal/privateevent"
 "github.com/negotiable-calendar/negotiable-calendar/apps/api/internal/projection"
)

func revisionPolicy(now time.Time) policy.SharingPolicy {
 return policy.SharingPolicy{ID:"policy", UserID:"alice", Default:publicationFixtures(now,"state",1)[0].State, CreatedAt:now, UpdatedAt:now}
}

func TestPolicyRevisionImmediatelyHidesAndRejectsStaleRebuild(t *testing.T) {
 b, ctx := emulatorBackend(t)
 now := time.Now().UTC().Truncate(time.Second)
 p := revisionPolicy(now)
 if err := b.Policy().Upsert(ctx,p); err != nil { t.Fatal(err) }
 if err := b.Projection().Replace(ctx,"alice",now,now.Add(24*time.Hour),publicationFixtures(now,"old",3)); err != nil { t.Fatal(err) }
 stale, err := b.Calendar().BeginRebuild(ctx,"alice")
 if err != nil { t.Fatal(err) }
 p.Default.Requestability = policy.RequestClosed
 if err := b.Policy().Upsert(ctx,p); err != nil { t.Fatal(err) }
 requirePublicationHidden(t,b,ctx)
 if err := b.Projection().Replace(stale,"alice",now,now.Add(24*time.Hour),publicationFixtures(now,"stale",3)); !errors.Is(err,errProjectionInputsChanged) { t.Fatalf("stale policy published: %v",err) }
 requirePublicationHidden(t,b,ctx)
 // Narrow regeneration must remove all old-policy rows, including outside it.
 fresh := publicationFixtures(now.Add(time.Hour),"fresh",1)
 fresh[0].State = p.Default
 if err := b.Projection().Replace(ctx,"alice",now.Add(time.Hour),now.Add(2*time.Hour),fresh); err != nil { t.Fatal(err) }
 got,err := b.Projection().ListForUser(ctx,"alice")
 if err != nil || len(got)!=1 || got[0].ID!=fresh[0].ID { t.Fatalf("old policy rows survived: %v %v",got,err) }
}

func TestPolicyRevisionFencesEveryBatchAndCompletion(t *testing.T) {
 b,ctx := emulatorBackend(t)
 now := time.Now().UTC().Truncate(time.Second)
 p := revisionPolicy(now)
 if err := b.Policy().Upsert(ctx,p); err != nil { t.Fatal(err) }
 writeCtx,err := b.beginProjectionWrite(ctx,"alice",now,now.Add(24*time.Hour),false)
 if err != nil { t.Fatal(err) }
 collection := b.Client.Collection("users").Doc("alice").Collection("scheduleProjections")
 batch := newChunkedBatch(b.Client,"alice")
 for _,v := range publicationFixtures(now,"partial",400) {
  if err := batch.Set(writeCtx,collection.Doc(v.ID),v); err != nil { t.Fatal(err) }
 }
 if err := b.Policy().Upsert(ctx,p); err != nil { t.Fatal(err) }
 late := publicationFixtures(now,"late",1)[0]
 if err := batch.Set(writeCtx,collection.Doc(late.ID),late); err != nil { t.Fatal(err) }
 if err := batch.Commit(writeCtx); !errors.Is(err,errProjectionInputsChanged) { t.Fatalf("late batch accepted: %v",err) }
 if err := b.finishProjectionWrite(writeCtx,"alice",true); !errors.Is(err,errProjectionInputsChanged) { t.Fatalf("stale completion accepted: %v",err) }
 requirePublicationHidden(t,b,ctx)
 b.abandonProjectionWrite(writeCtx,"alice")
 if err := b.Projection().Replace(ctx,"alice",now,now.Add(time.Hour),publicationFixtures(now,"repaired",1)); err != nil { t.Fatal(err) }
 got,err := b.Projection().ListForUser(ctx,"alice")
 if err != nil || len(got)!=1 || got[0].ID!="repaired-0000" { t.Fatalf("incomplete old-policy rows survived repair: %d %v",len(got),err) }
}

func TestOverrideRevisionIsAtomicAndIsolated(t *testing.T) {
 b,ctx := emulatorBackend(t)
 now := time.Now().UTC().Truncate(time.Second)
 if err := b.Projection().Replace(ctx,"alice",now,now.Add(24*time.Hour),publicationFixtures(now,"legacy",1)); err != nil { t.Fatal(err) }
 captured,err := b.Calendar().BeginRebuild(ctx,"alice")
 if err != nil { t.Fatal(err) }
 override := policy.ManualOverride{ID:"override",UserID:"alice",StartAt:now,EndAt:now.Add(time.Hour),State:revisionPolicy(now).Default,ExpiresAt:now.Add(time.Hour),CreatedAt:now}
 if err := b.Policy().CreateOverride(ctx,override); err != nil { t.Fatal(err) }
 requirePublicationHidden(t,b,ctx)
 if err := b.Projection().Replace(captured,"alice",now,now.Add(24*time.Hour),nil); !errors.Is(err,errProjectionInputsChanged) { t.Fatalf("override did not fence old input: %v",err) }
 before,err := decodePolicyRevision(b.policyRevisionRef("alice").Get(ctx))
 if err != nil { t.Fatal(err) }
 if err := b.Policy().CreateOverride(ctx,override); err == nil { t.Fatal("duplicate override accepted") }
 after,err := decodePolicyRevision(b.policyRevisionRef("alice").Get(ctx))
 if err != nil || before!=after { t.Fatalf("failed create advanced revision: %v",err) }
 bob := revisionPolicy(now); bob.UserID="bob"
 if err := b.Policy().Upsert(ctx,bob); err != nil { t.Fatal(err) }
 after,err = decodePolicyRevision(b.policyRevisionRef("alice").Get(ctx))
 if err != nil || before!=after { t.Fatal("other user's policy changed revision") }
}

type pausedRebuildInputs struct {
 *Calendar
 entered chan struct{}
 resume chan struct{}
}
func (s *pausedRebuildInputs) ListPrivateEvents(ctx context.Context, userID string, from,to time.Time) ([]privateevent.PrivateEvent,error) {
 close(s.entered)
 select { case <-s.resume: case <-ctx.Done(): return nil,ctx.Err() }
 return s.Calendar.ListPrivateEvents(ctx,userID,from,to)
}

func TestRealRebuilderRejectsConcurrentPolicyChange(t *testing.T) {
 b,ctx := emulatorBackend(t)
 now := time.Now().UTC().Truncate(time.Second)
 putDocument(t,ctx,b.Client.Collection("users").Doc("alice"),userRecord{ID:"alice",Timezone:"UTC"})
 p := revisionPolicy(now)
 if err := b.Policy().Upsert(ctx,p); err != nil { t.Fatal(err) }
 paused := &pausedRebuildInputs{Calendar:b.Calendar(),entered:make(chan struct{}),resume:make(chan struct{})}
 result := make(chan error,1)
 go func(){ result <- projection.NewRebuilder(paused,b.Policy()).Rebuild(ctx,"alice",now,now.Add(time.Hour),now) }()
 select { case <-paused.entered: case <-ctx.Done(): t.Fatal(ctx.Err()) }
 p.Default.Requestability=policy.RequestClosed
 changeErr := b.Policy().Upsert(ctx,p)
 close(paused.resume)
 rebuildErr := <-result
 if changeErr != nil { t.Fatal(changeErr) }
 if !errors.Is(rebuildErr,errProjectionInputsChanged) { t.Fatalf("real rebuild published old input: %v",rebuildErr) }
 requirePublicationHidden(t,b,ctx)
 if err := projection.NewRebuilder(b.Calendar(),b.Policy()).Rebuild(ctx,"alice",now,now.Add(time.Hour),now); err != nil { t.Fatal(err) }
 got,err := b.Projection().ListForUser(ctx,"alice")
 if err != nil || len(got)==0 { t.Fatalf("fresh rebuild failed: %v",err) }
 for _,v := range got { if v.State.Requestability!=policy.RequestClosed { t.Fatal("fresh rebuild used old policy") } }
}
