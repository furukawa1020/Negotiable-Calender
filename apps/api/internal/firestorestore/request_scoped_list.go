package firestorestore

import (
	"context"
	"errors"

	"cloud.google.com/go/firestore"
	coord "github.com/negotiable-calendar/negotiable-calendar/apps/api/internal/request"
)

func (store *Request) ListInOrganization(ctx context.Context, actor, org string, sent bool) ([]coord.CoordinationRequest, error) {
	if actor == "" || org == "" {
		return nil, coord.ErrCreationForbidden
	}
	field := "TargetUserID"
	if sent {
		field = "RequesterUserID"
	}
	var values []coord.CoordinationRequest
	err := store.Client.RunTransaction(ctx, func(ctx context.Context, tx *firestore.Transaction) error {
		values = []coord.CoordinationRequest{}
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
		// Equality-only filters use index merging; ordering is deterministic below.
		docs, err := tx.Documents(store.Client.Collection("coordinationRequests").Where("OrganizationID", "==", org).Where(field, "==", actor)).GetAll()
		if err != nil {
			return err
		}
		for _, doc := range docs {
			var value coord.CoordinationRequest
			if err := doc.DataTo(&value); err != nil {
				return err
			}
			if value.Options == nil {
				value.Options = []coord.Option{}
			}
			values = append(values, value)
		}
		return nil
	}, firestore.ReadOnly)
	if err != nil {
		return nil, err
	}
	sortRequests(values)
	return values, nil
}

var _ coord.ScopedListStore = (*Request)(nil)
