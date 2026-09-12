package firestorestore

import (
	"context"
	"errors"
	"time"

	"cloud.google.com/go/firestore"
	calendarintegration "github.com/negotiable-calendar/negotiable-calendar/apps/api/internal/calendar"
)

var errPrivateInputsIncomplete = errors.New("private calendar inputs are incomplete; full sync required")

type privateInputsLeaseKey struct{}
type privateInputsLease struct{ UserID, ID string }
type privateInputsControl struct {
	ID         string
	Ready      bool
	LeaseUntil *time.Time
}

func (b *Backend) privateInputsRef(userID string) *firestore.DocumentRef {
	return b.Client.Collection("users").Doc(userID).Collection("projectionControls").Doc("privateInputs")
}

func decodePrivateInputs(doc *firestore.DocumentSnapshot, err error) (privateInputsControl, error) {
	if firestoreNotFound(err) {
		return privateInputsControl{Ready: true}, nil
	}
	if err != nil {
		return privateInputsControl{}, err
	}
	var value privateInputsControl
	if err := doc.DataTo(&value); err != nil {
		return value, err
	}
	if value.ID == "" {
		return value, errPrivateInputsIncomplete
	}
	return value, nil
}

func (b *Backend) privateInputRevision(ctx context.Context, userID string) (string, error) {
	if deleting, err := b.accountIsDeleting(ctx, userID); err != nil {
		return "", err
	} else if deleting {
		return "", errAccountDeleting
	}
	value, err := decodePrivateInputs(b.privateInputsRef(userID).Get(ctx))
	if err != nil {
		return "", err
	}
	if !value.Ready || value.LeaseUntil != nil {
		return "", errPrivateInputsIncomplete
	}
	return value.ID, nil
}

func (b *Backend) beginPrivateInputs(ctx context.Context, userID string, full bool) (context.Context, error) {
	id := calendarintegration.NewSyncLeaseID()
	err := b.fencedWrite(ctx, userID, func(tx *firestore.Transaction) error {
		block, err := tx.Get(b.projectionBlock(userID))
		if err == nil {
			var control publicationControl
			if err := block.DataTo(&control); err != nil {
				return err
			}
			if control.InProgress {
				return calendarintegration.ErrSyncBusy
			}
		} else if !firestoreNotFound(err) {
			return err
		}
		previous, err := decodePrivateInputs(tx.Get(b.privateInputsRef(userID)))
		if err != nil {
			return err
		}
		now := time.Now().UTC()
		if previous.LeaseUntil != nil && previous.LeaseUntil.After(now) {
			return calendarintegration.ErrSyncBusy
		}
		if !previous.Ready && !full {
			return errPrivateInputsIncomplete
		}
		until := now.Add(2 * time.Minute)
		return tx.Set(b.privateInputsRef(userID), privateInputsControl{ID: id, LeaseUntil: &until})
	})
	if err != nil {
		return nil, err
	}
	return context.WithValue(ctx, privateInputsLeaseKey{}, privateInputsLease{UserID: userID, ID: id}), nil
}

func (b *Backend) guardPrivateInputs(ctx context.Context, tx *firestore.Transaction, userID string) error {
	lease, ok := ctx.Value(privateInputsLeaseKey{}).(privateInputsLease)
	if !ok {
		return nil
	}
	if lease.UserID != userID {
		return calendarintegration.ErrSyncLeaseLost
	}
	value, err := decodePrivateInputs(tx.Get(b.privateInputsRef(userID)))
	if err != nil {
		return err
	}
	if value.ID != lease.ID || value.LeaseUntil == nil || !value.LeaseUntil.After(time.Now().UTC()) {
		return calendarintegration.ErrSyncLeaseLost
	}
	return nil
}

func (b *Backend) finishPrivateInputs(ctx context.Context, userID string) error {
	lease, _ := ctx.Value(privateInputsLeaseKey{}).(privateInputsLease)
	return b.fencedWrite(ctx, userID, func(tx *firestore.Transaction) error {
		return tx.Set(b.privateInputsRef(userID), privateInputsControl{ID: lease.ID, Ready: true})
	})
}

func (b *Backend) abandonPrivateInputs(ctx context.Context, userID string) {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	_ = b.fencedWrite(ctx, userID, func(tx *firestore.Transaction) error {
		return tx.Update(b.privateInputsRef(userID), []firestore.Update{{Path: "LeaseUntil", Value: nil}})
	})
}
