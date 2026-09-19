package firestorestore

import (
	"cloud.google.com/go/firestore"
	"context"
	coordinationrequest "github.com/negotiable-calendar/negotiable-calendar/apps/api/internal/request"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"time"
)

func (store *Request) ResolveRequest(ctx context.Context, id, actor, organization string, command coordinationrequest.ResolutionCommand) (bool, error) {
	replayed := false
	ref := store.Client.Collection("coordinationRequests").Doc(id)
	err := store.Client.RunTransaction(ctx, func(ctx context.Context, tx *firestore.Transaction) error {
		replayed = false
		doc, err := tx.Get(ref)
		if err != nil {
			return err
		}
		var value coordinationrequest.CoordinationRequest
		if err := doc.DataTo(&value); err != nil {
			return err
		}
		if err := coordinationrequest.AuthorizeResolution(value, actor, organization, command); err != nil {
			return err
		}
		if err := store.guardCreation(ctx, tx, value); err != nil {
			return err
		}
		replayed, err = coordinationrequest.PrepareResolution(&value, command, time.Now().UTC())
		if err != nil {
			return err
		}
		if replayed {
			return nil
		}
		note, event := coordinationrequest.ResolutionEffects(value, actor)
		if err := tx.Set(ref, value); err != nil {
			return err
		}
		if err := tx.Create(store.Client.Collection("users").Doc(note.UserID).Collection("notifications").Doc(note.ID), note); err != nil {
			return err
		}
		return tx.Create(store.Client.Collection("organizations").Doc(event.OrganizationID).Collection("auditLogs").Doc(event.ID), event)
	})
	if firestoreNotFound(err) {
		return false, coordinationrequest.ErrNotFound
	}
	if status.Code(err) == codes.Aborted {
		return false, coordinationrequest.ErrResolutionConflict
	}
	if err != nil {
		return false, err
	}
	return replayed, nil
}

var _ coordinationrequest.ResolutionStore = (*Request)(nil)
