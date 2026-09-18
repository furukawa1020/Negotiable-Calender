package firestorestore

import (
	"context"
	"time"

	"cloud.google.com/go/firestore"
	coordinationrequest "github.com/negotiable-calendar/negotiable-calendar/apps/api/internal/request"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (store *Request) CancelConfirmed(ctx context.Context, requestID, actor, optionID string) error {
	ref := store.Client.Collection("coordinationRequests").Doc(requestID)
	err := store.Client.RunTransaction(ctx, func(ctx context.Context, tx *firestore.Transaction) error {
		doc, err := tx.Get(ref)
		if err != nil {
			return err
		}
		var value coordinationrequest.CoordinationRequest
		if err := doc.DataTo(&value); err != nil {
			return err
		}
		if actor == "" || (actor != value.RequesterUserID && actor != value.TargetUserID) {
			return coordinationrequest.ErrNotFound
		}
		if err := store.guardRequestAccounts(ctx, tx, value); err != nil {
			return err
		}
		if err := coordinationrequest.ValidateConfirmedCancellation(value, actor, optionID, time.Now().UTC()); err != nil {
			return err
		}
		locks := []*firestore.DocumentRef{}
		for _, participant := range []string{value.RequesterUserID, value.TargetUserID} {
			lock := store.Client.Collection("users").Doc(participant).Collection("projectionControls").Doc("coordinationConfirmation")
			if _, err := tx.Get(lock); err != nil && !firestoreNotFound(err) {
				return err
			}
			locks = append(locks, lock)
		}
		now := time.Now().UTC()
		if err := coordinationrequest.ValidateConfirmedCancellation(value, actor, optionID, now); err != nil {
			return err
		}
		note, event := coordinationrequest.ConfirmedCancellationEffects(value, actor, now)
		// All reads precede writes. Same locks as acceptance serialize reservation release.
		for _, lock := range locks {
			if err := tx.Set(lock, map[string]any{"Revision": randomID("cancellation")}); err != nil {
				return err
			}
		}
		value.Status, value.UpdatedAt = coordinationrequest.Cancelled, now
		if err := tx.Set(ref, value); err != nil {
			return err
		}
		if err := tx.Create(store.Client.Collection("users").Doc(note.UserID).Collection("notifications").Doc(note.ID), note); err != nil {
			return err
		}
		return tx.Create(store.Client.Collection("organizations").Doc(event.OrganizationID).Collection("auditLogs").Doc(event.ID), event)
	})
	if firestoreNotFound(err) {
		return coordinationrequest.ErrNotFound
	}
	if status.Code(err) == codes.Aborted {
		return coordinationrequest.ErrBookingConflict
	}
	return err
}
