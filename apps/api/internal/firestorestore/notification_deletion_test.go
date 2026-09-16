package firestorestore

import (
	"errors"
	"testing"
	"time"

	"github.com/negotiable-calendar/negotiable-calendar/apps/api/internal/notification"
	coordinationrequest "github.com/negotiable-calendar/negotiable-calendar/apps/api/internal/request"
)

func TestNotificationFencesAllRequestParticipants(t *testing.T) {
	for _, deleting := range []string{"alice", "bob", "carol", "dave"} {
		for _, phase := range []string{"deleting", "complete"} {
			t.Run(deleting+"/"+phase, func(t *testing.T) {
				b, ctx := emulatorBackend(t)
				request := deletionRequest(time.Now().UTC())
				request.DelegatedUserID = "carol"
				request.Options = []coordinationrequest.Option{{ID: "historical", RequestID: request.ID, Type: coordinationrequest.OptionDelegate, DelegateUserID: "dave", CreatedAt: request.CreatedAt}}
				if err := b.Request().Create(ctx, request); err != nil {
					t.Fatal(err)
				}
				for _, userID := range []string{"alice", "bob", "carol", "dave"} {
					if err := b.Notification().Create(ctx, notification.Notification{ID: "before", UserID: userID, RequestID: request.ID}); err != nil {
						t.Fatal(err)
					}
				}
				putDocument(t, ctx, b.accountDeletionRef(deleting), accountDeletion{Phase: phase})
				for _, userID := range []string{"alice", "bob", "carol", "dave"} {
					if err := b.Notification().Create(ctx, notification.Notification{ID: "after", UserID: userID, RequestID: request.ID}); !errors.Is(err, errAccountDeleting) {
						t.Fatalf("participant fence bypass: %s %v", userID, err)
					}
					if _, err := b.Client.Collection("users").Doc(userID).Collection("notifications").Doc("after").Get(ctx); !firestoreNotFound(err) {
						t.Fatalf("rejected write persisted: %v", err)
					}
				}
			})
		}
	}
}

func TestNotificationRejectsUnrelatedOrMissingRequest(t *testing.T) {
	b, ctx := emulatorBackend(t)
	request := deletionRequest(time.Now().UTC())
	if err := b.Request().Create(ctx, request); err != nil {
		t.Fatal(err)
	}
	for _, value := range []notification.Notification{
		{ID: "unrelated", UserID: "outsider", RequestID: request.ID},
		{ID: "missing", UserID: "bob", RequestID: "missing"},
		{ID: "blank-request", UserID: "bob"},
		{ID: "blank-user", RequestID: request.ID},
		{UserID: "bob", RequestID: request.ID},
	} {
		if err := b.Notification().Create(ctx, value); err == nil {
			t.Fatalf("invalid notification accepted: %+v", value)
		}
	}
	if _, err := b.Client.Collection("coordinationRequests").Doc(request.ID).Delete(ctx); err != nil {
		t.Fatal(err)
	}
	if err := b.Notification().Create(ctx, notification.Notification{ID: "deleted-request", UserID: "bob", RequestID: request.ID}); err == nil {
		t.Fatal("deleted request accepted")
	}
}

func TestRequestParticipantsDeduplicatesDelegates(t *testing.T) {
	request := deletionRequest(time.Now().UTC())
	request.DelegatedUserID = "bob"
	request.Options = []coordinationrequest.Option{{DelegateUserID: "alice"}, {DelegateUserID: "carol"}, {DelegateUserID: "carol"}, {}}
	got := requestParticipants(request)
	if len(got) != 3 || got[0] != "alice" || got[1] != "bob" || got[2] != "carol" {
		t.Fatalf("participants: %v", got)
	}
}
