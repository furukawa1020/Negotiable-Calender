package firestorestore

import (
	"context"
	"time"

	"cloud.google.com/go/firestore"
	"github.com/negotiable-calendar/negotiable-calendar/apps/api/internal/projection"
	coordinationrequest "github.com/negotiable-calendar/negotiable-calendar/apps/api/internal/request"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (store *Request) acceptMeeting(ctx context.Context, requestID, userID, optionID string) error {
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
		if value.TargetUserID != userID {
			return coordinationrequest.ErrNotFound
		}
		if err := store.guardRequestAccounts(ctx, tx, value); err != nil {
			return err
		}
		if value.Status == coordinationrequest.Accepted && value.AcceptedOptionID == optionID {
			return coordinationrequest.ErrAlreadyAccepted
		}
		if value.Status != coordinationrequest.Suggested {
			return coordinationrequest.ErrNotFound
		}
		selected, err := coordinationrequest.ConfirmableMeeting(value, optionID, time.Now().UTC())
		if err != nil {
			return err
		}
		if err := store.checkMeetingSlot(ctx, tx, value, selected); err != nil {
			return err
		}
		value.Status, value.AcceptedOptionID, value.UpdatedAt = coordinationrequest.Accepted, optionID, time.Now().UTC()
		return tx.Set(ref, value)
	})
	if firestoreNotFound(err) {
		return coordinationrequest.ErrNotFound
	}
	if status.Code(err) == codes.Aborted {
		return coordinationrequest.ErrBookingConflict
	}
	return err
}

// Reads the conflict/publication snapshot, then writes only the shared participant locks.
// Callers must finish all other transaction reads before calling this helper.
func (store *Request) checkMeetingSlot(ctx context.Context, tx *firestore.Transaction, value coordinationrequest.CoordinationRequest, selected coordinationrequest.Option) error {
	// Both roles share the same per-person serialization document across all orgs.
	locks := []*firestore.DocumentRef{}
	for _, participant := range []string{value.RequesterUserID, value.TargetUserID} {
		lock := store.Client.Collection("users").Doc(participant).Collection("projectionControls").Doc("coordinationConfirmation")
		if _, err := tx.Get(lock); err != nil && !firestoreNotFound(err) {
			return err
		}
		locks = append(locks, lock)
		for _, field := range []string{"RequesterUserID", "TargetUserID"} {
			docs, err := tx.Documents(store.Client.Collection("coordinationRequests").Where(field, "==", participant).Limit(5001)).GetAll()
			if err != nil {
				return err
			}
			if len(docs) > 5000 {
				return coordinationrequest.ErrAvailabilityChanged
			}
			for _, doc := range docs {
				if doc.Ref.ID == value.ID {
					continue
				}
				var other coordinationrequest.CoordinationRequest
				if err := doc.DataTo(&other); err != nil {
					return err
				}
				if coordinationrequest.ConflictsWithMeeting(selected, other) {
					return coordinationrequest.ErrBookingConflict
				}
			}
		}
	}
	values, err := store.confirmationProjections(ctx, tx, value.TargetUserID)
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	if _, err := coordinationrequest.ConfirmableMeeting(value, selected.ID, now); err != nil {
		return err
	}
	if err := coordinationrequest.ValidateMeetingAvailability(value.TargetUserID, selected, values, now); err != nil {
		return err
	}
	// All reads precede writes. The marker lives in an already-cleaned user collection.
	for _, lock := range locks {
		if err := tx.Set(lock, map[string]any{"Revision": randomID("confirmation")}); err != nil {
			return err
		}
	}
	return nil
}

func (store *Request) confirmationProjections(ctx context.Context, tx *firestore.Transaction, userID string) ([]projection.ScheduleProjection, error) {
	if _, err := tx.Get(store.projectionBlock(userID)); err == nil {
		return nil, coordinationrequest.ErrAvailabilityChanged
	} else if !firestoreNotFound(err) {
		return nil, err
	}
	policyRevision, err := decodePolicyRevision(tx.Get(store.policyRevisionRef(userID)))
	if err != nil {
		return nil, err
	}
	inputs, err := decodePrivateInputs(tx.Get(store.privateInputsRef(userID)))
	if err != nil || !inputs.Ready || inputs.LeaseUntil != nil {
		return nil, coordinationrequest.ErrAvailabilityChanged
	}
	doc, err := tx.Get(store.projectionPublicationRef(userID))
	if firestoreNotFound(err) {
		if policyRevision != "" || inputs.ID != "" {
			return nil, coordinationrequest.ErrAvailabilityChanged
		}
	} else {
		if err != nil {
			return nil, err
		}
		var publication projectionPublication
		if err := doc.DataTo(&publication); err != nil {
			return nil, err
		}
		if !publication.Ready || publication.Dirty || publication.LeaseUntil != nil || publication.PolicyRevision != policyRevision || publication.PrivateRevision != inputs.ID {
			return nil, coordinationrequest.ErrAvailabilityChanged
		}
	}
	docs, err := tx.Documents(store.Client.Collection("users").Doc(userID).Collection("scheduleProjections").Limit(10001)).GetAll()
	if err != nil {
		return nil, err
	}
	if len(docs) > 10000 {
		return nil, coordinationrequest.ErrAvailabilityChanged
	}
	values := make([]projection.ScheduleProjection, 0, len(docs))
	for _, doc := range docs {
		var value projection.ScheduleProjection
		if err := doc.DataTo(&value); err != nil {
			return nil, err
		}
		values = append(values, value)
	}
	return values, nil
}
