package firestorestore

import (
	"cloud.google.com/go/firestore"
	"context"
	coord "github.com/negotiable-calendar/negotiable-calendar/apps/api/internal/request"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"time"
)

func (store *Request) InspectHandoff(ctx context.Context, id, actor, org, recipient string) (coord.CoordinationRequest, bool, error) {
	return store.handoff(ctx, id, actor, org, recipient, nil, false)
}
func (store *Request) Handoff(ctx context.Context, id, actor, org, recipient string, options []coord.Option) (bool, error) {
	_, replay, err := store.handoff(ctx, id, actor, org, recipient, options, true)
	return replay, err
}
func (store *Request) handoff(ctx context.Context, id, actor, org, recipient string, options []coord.Option, commit bool) (coord.CoordinationRequest, bool, error) {
	var result coord.CoordinationRequest
	replay := false
	ref := store.Client.Collection("coordinationRequests").Doc(id)
	err := store.Client.RunTransaction(ctx, func(ctx context.Context, tx *firestore.Transaction) error {
		result = coord.CoordinationRequest{}
		replay = false
		doc, err := tx.Get(ref)
		if err != nil {
			return err
		}
		var value coord.CoordinationRequest
		if err := doc.DataTo(&value); err != nil {
			return err
		}
		replay, err = coord.ValidateHandoff(value, actor, org, recipient, time.Now().UTC())
		if err != nil {
			return err
		}
		if err := store.guardRequestAccounts(ctx, tx, value); err != nil {
			return err
		}
		if err := store.guardAccountActive(ctx, tx, recipient); err != nil {
			return err
		}
		for _, user := range []string{value.RequesterUserID, actor, recipient} {
			if _, err := tx.Get(store.Client.Collection("organizations").Doc(org).Collection("members").Doc(user)); firestoreNotFound(err) {
				return coord.ErrCreationForbidden
			} else if err != nil {
				return err
			}
		}
		if replay {
			return nil
		}
		if !commit {
			result = value
			return nil
		}
		if err := coord.ApplyHandoff(&value, actor, recipient, options, time.Now().UTC()); err != nil {
			return err
		}
		notes, event := coord.HandoffEffects(value, actor)
		if err := tx.Set(ref, value); err != nil {
			return err
		}
		for _, note := range notes {
			if err := tx.Create(store.Client.Collection("users").Doc(note.UserID).Collection("notifications").Doc(note.ID), note); err != nil {
				return err
			}
		}
		return tx.Create(store.Client.Collection("organizations").Doc(org).Collection("auditLogs").Doc(event.ID), event)
	})
	if firestoreNotFound(err) {
		return coord.CoordinationRequest{}, false, coord.ErrNotFound
	}
	if status.Code(err) == codes.Aborted {
		return coord.CoordinationRequest{}, false, coord.ErrHandoffConflict
	}
	if err != nil {
		return coord.CoordinationRequest{}, false, err
	}
	return result, replay, nil
}

var _ coord.HandoffStore = (*Request)(nil)
