package firestorestore

import (
	"cloud.google.com/go/firestore"
	"context"
	"errors"
	coord "github.com/negotiable-calendar/negotiable-calendar/apps/api/internal/request"
)

func (store *Request) GetInOrganization(ctx context.Context, id, actor, org string) (coord.CoordinationRequest, error) {
	if actor == "" || org == "" {
		return coord.CoordinationRequest{}, coord.ErrCreationForbidden
	}
	if !coord.ValidHandoffID(id) {
		return coord.CoordinationRequest{}, coord.ErrNotFound
	}
	var value coord.CoordinationRequest
	err := store.Client.RunTransaction(ctx, func(ctx context.Context, tx *firestore.Transaction) error {
		value = coord.CoordinationRequest{}
		if err := store.guardAccountActive(ctx, tx, actor); errors.Is(err, errAccountDeleting) {
			return coord.ErrCreationForbidden
		} else if err != nil {
			return err
		}
		if _, err := tx.Get(store.Client.Collection("organizations").Doc(org).Collection("members").Doc(actor)); firestoreNotFound(err) {
			return coord.ErrCreationForbidden
		} else if err != nil {
			return err
		}
		doc, err := tx.Get(store.Client.Collection("coordinationRequests").Doc(id))
		if firestoreNotFound(err) {
			return coord.ErrNotFound
		}
		if err != nil {
			return err
		}
		if err := doc.DataTo(&value); err != nil {
			return err
		}
		if value.ID != id || value.OrganizationID != org || (value.RequesterUserID != actor && value.TargetUserID != actor) {
			return coord.ErrNotFound
		}
		if value.Options == nil {
			value.Options = []coord.Option{}
		}
		return nil
	}, firestore.ReadOnly)
	if err != nil {
		return coord.CoordinationRequest{}, err
	}
	return value, nil
}

var _ coord.ScopedGetStore = (*Request)(nil)
