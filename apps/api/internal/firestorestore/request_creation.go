package firestorestore

import (
	"context"

	"cloud.google.com/go/firestore"
	coordinationrequest "github.com/negotiable-calendar/negotiable-calendar/apps/api/internal/request"
)

func (store *Request) guardCreation(ctx context.Context, tx *firestore.Transaction, value coordinationrequest.CoordinationRequest) error {
	if err := store.guardRequestAccounts(ctx, tx, value); err != nil {
		return err
	}
	for _, user := range []string{value.RequesterUserID, value.TargetUserID} {
		if _, err := tx.Get(store.Client.Collection("organizations").Doc(value.OrganizationID).Collection("members").Doc(user)); firestoreNotFound(err) {
			return coordinationrequest.ErrCreationForbidden
		} else if err != nil {
			return err
		}
	}
	return nil
}

func (store *Request) LookupCreation(ctx context.Context, command coordinationrequest.CoordinationRequest) (coordinationrequest.CoordinationRequest, error) {
	var value coordinationrequest.CoordinationRequest
	err := store.Client.RunTransaction(ctx, func(ctx context.Context, tx *firestore.Transaction) error {
		if err := store.guardCreation(ctx, tx, command); err != nil {
			return err
		}
		doc, err := tx.Get(store.Client.Collection("coordinationRequests").Doc(command.ID))
		if firestoreNotFound(err) {
			return coordinationrequest.ErrNotFound
		}
		if err != nil {
			return err
		}
		if err := doc.DataTo(&value); err != nil {
			return err
		}
		if !coordinationrequest.SameCreation(value, command) {
			return coordinationrequest.ErrCreationConflict
		}
		return nil
	})
	return value, err
}

func (store *Request) CreateOnce(ctx context.Context, value coordinationrequest.CoordinationRequest) (bool, error) {
	created := false
	err := store.Client.RunTransaction(ctx, func(ctx context.Context, tx *firestore.Transaction) error {
		created = false // Firestore may retry the callback.
		if err := store.guardCreation(ctx, tx, value); err != nil {
			return err
		}
		ref := store.Client.Collection("coordinationRequests").Doc(value.ID)
		doc, err := tx.Get(ref)
		if err == nil {
			var existing coordinationrequest.CoordinationRequest
			if err := doc.DataTo(&existing); err != nil {
				return err
			}
			if !coordinationrequest.SameCreation(existing, value) {
				return coordinationrequest.ErrCreationConflict
			}
			return nil
		}
		if !firestoreNotFound(err) {
			return err
		}
		if err := value.Validate(); err != nil {
			return err
		}
		note, event := coordinationrequest.CreationEffects(value)
		if err := tx.Create(ref, value); err != nil {
			return err
		}
		if err := tx.Create(store.Client.Collection("users").Doc(note.UserID).Collection("notifications").Doc(note.ID), note); err != nil {
			return err
		}
		if err := tx.Create(store.Client.Collection("organizations").Doc(event.OrganizationID).Collection("auditLogs").Doc(event.ID), event); err != nil {
			return err
		}
		created = true
		return nil
	})
	if err != nil {
		return false, err
	}
	return created, nil
}

var _ coordinationrequest.CreationStore = (*Request)(nil)
