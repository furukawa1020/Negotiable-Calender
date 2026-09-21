package firestorestore

import (
	"context"
	"time"

	"cloud.google.com/go/firestore"
	coord "github.com/negotiable-calendar/negotiable-calendar/apps/api/internal/request"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (store *Request) ProposeMeeting(ctx context.Context, id, actor, org string, start, end time.Time) (coord.Option, bool, error) {
	var option coord.Option
	replay := false
	ref := store.Client.Collection("coordinationRequests").Doc(id)
	err := store.Client.RunTransaction(ctx, func(ctx context.Context, tx *firestore.Transaction) error {
		option, replay = coord.Option{}, false
		doc, err := tx.Get(ref)
		if err != nil {
			return err
		}
		var value coord.CoordinationRequest
		if err := doc.DataTo(&value); err != nil {
			return err
		}
		if actor == "" || actor != value.TargetUserID || org == "" || org != value.OrganizationID {
			return coord.ErrNotFound
		}
		if err := store.guardCreation(ctx, tx, value); err != nil {
			return err
		}
		option, replay, err = coord.PrepareProposal(&value, actor, org, start, end, time.Now().UTC())
		if err != nil {
			return err
		}
		if replay {
			return nil
		}
		note, event := coord.ProposalEffects(value, option)
		if err := tx.Set(ref, value); err != nil {
			return err
		}
		if err := tx.Create(store.Client.Collection("users").Doc(note.UserID).Collection("notifications").Doc(note.ID), note); err != nil {
			return err
		}
		return tx.Create(store.Client.Collection("organizations").Doc(org).Collection("auditLogs").Doc(event.ID), event)
	})
	if firestoreNotFound(err) {
		err = coord.ErrNotFound
	}
	if status.Code(err) == codes.Aborted {
		err = coord.ErrProposalConflict
	}
	if err != nil {
		return coord.Option{}, false, err
	}
	return option, replay, nil
}
func (store *Request) ConfirmMeeting(ctx context.Context, id, actor, org, option string) error {
	if org == "" {
		return coord.ErrNotFound
	}
	return store.confirmMeeting(ctx, id, actor, org, option)
}

var _ coord.CounterproposalStore = (*Request)(nil)
