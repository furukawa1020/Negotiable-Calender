package firestorestore

import (
	"errors"
	"github.com/negotiable-calendar/negotiable-calendar/apps/api/internal/projection"
	coordinationrequest "github.com/negotiable-calendar/negotiable-calendar/apps/api/internal/request"
	"testing"
	"time"
)

func TestConfirmationUsesCommittedPublicationAndIsIdempotent(t *testing.T) {
	b, ctx := emulatorBackend(t)
	now := time.Now().UTC()
	value := confirmationRequest("r", "alice", "bob", now, now.Add(time.Hour))
	p := publicationFixtures(now, "available", 1)[0]
	p.UserID = "bob"
	p.EndAt = now.Add(4 * time.Hour)
	if err := b.Projection().Replace(ctx, "bob", now, p.EndAt, []projection.ScheduleProjection{p}); err != nil {
		t.Fatal(err)
	}
	if err := b.Request().Create(ctx, value); err != nil {
		t.Fatal(err)
	}
	gate := make(chan struct{})
	results := make(chan error, 2)
	for i := 0; i < 2; i++ {
		go func() {
			<-gate
			results <- b.Request().Respond(ctx, value.ID, "bob", coordinationrequest.Accepted, value.Options[0].ID)
		}()
	}
	close(gate)
	success, repeated := 0, 0
	for i := 0; i < 2; i++ {
		err := <-results
		if err == nil {
			success++
		} else if errors.Is(err, coordinationrequest.ErrAlreadyAccepted) {
			repeated++
		} else {
			t.Fatal(err)
		}
	}
	if success != 1 || repeated != 1 {
		t.Fatalf("success=%d repeated=%d", success, repeated)
	}
}
