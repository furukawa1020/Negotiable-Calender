package firestorestore

import (
	"cloud.google.com/go/firestore"
	"context"
	coordinationrequest "github.com/negotiable-calendar/negotiable-calendar/apps/api/internal/request"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"time"
)

func (store *Request) Reschedule(ctx context.Context, id, actor string, command coordinationrequest.RescheduleCommand) error {
	return store.reschedule(ctx, id, actor, "", command)
}

func (store *Request) RescheduleInOrganization(ctx context.Context, id, actor, org string, command coordinationrequest.RescheduleCommand) error {
	if actor == "" || org == "" {
		return coordinationrequest.ErrNotFound
	}
	return store.reschedule(ctx, id, actor, org, command)
}

func (store *Request) reschedule(ctx context.Context, id, actor, org string, command coordinationrequest.RescheduleCommand) error {
	ref := store.Client.Collection("coordinationRequests").Doc(id)
	err := store.Client.RunTransaction(ctx, func(ctx context.Context, tx *firestore.Transaction) error {
		doc, err := tx.Get(ref)
		if err != nil {
			return err
		}
		var value coordinationrequest.CoordinationRequest
		if err := doc.DataTo(&value); err != nil {
			return err
		}
		if (actor != value.RequesterUserID && actor != value.TargetUserID) || (org != "" && org != value.OrganizationID) {
			return coordinationrequest.ErrNotFound
		}
		if err := store.guardRequestAccounts(ctx, tx, value); err != nil {
			return err
		}
		if org != "" {
			if err := store.guardCreation(ctx, tx, value); err != nil {
				return err
			}
		}
		now := time.Now().UTC()
		if err := coordinationrequest.ApplyReschedule(&value, actor, command, now); err != nil {
			return err
		}
		if command.Action == "accept" {
			selected, err := coordinationrequest.ConfirmableMeeting(value, command.ProposalID, now)
			if err != nil {
				return err
			}
			if err := store.checkMeetingSlot(ctx, tx, value, selected); err != nil {
				return err
			}
			// Recheck the original start after potentially slow reservation reads.
			old := value
			old.AcceptedOptionID = command.ExpectedOptionID
			if err := coordinationrequest.ValidateConfirmedCancellation(old, actor, command.ExpectedOptionID, time.Now().UTC()); err != nil {
				return coordinationrequest.ErrRescheduleInvalid
			}
		}
		note, event := coordinationrequest.RescheduleEffects(value, actor, command.Action, now)
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

var _ coordinationrequest.ScopedRescheduleStore = (*Request)(nil)
