package firestorestore

import (
	"context"
	"errors"
	"time"

	"cloud.google.com/go/firestore"
	calendarintegration "github.com/negotiable-calendar/negotiable-calendar/apps/api/internal/calendar"
)

var errProjectionRepairRequired = errors.New("incomplete projection update requires replacement of the entire affected range or deletion")

type projectionLeaseKey struct{}
type projectionClearAllKey struct{}
type projectionLease struct{ UserID, ID string }

// A durable gate covers every batch. Failed updates stay hidden until their
// entire affected range is repaired. Lease expiry never reopens publication.
type projectionPublication struct {
	PrivateRevision string
	PolicyRevision  string
	ID              string
	Ready           bool
	Dirty           bool
	ClearRequired   bool
	From, To        time.Time
	LeaseUntil      *time.Time
}

func (b *Backend) projectionPublicationRef(userID string) *firestore.DocumentRef {
	return b.Client.Collection("users").Doc(userID).Collection("projectionControls").Doc("publication")
}

func (b *Backend) beginProjectionWrite(ctx context.Context, userID string, from, to time.Time, deleting bool) (context.Context, error) {
	if deleting {
		ctx = context.WithValue(ctx, projectionInputsKey{}, nil)
	} else if _, ok := ctx.Value(projectionInputsKey{}).(projectionInputs); !ok {
		var err error
		ctx, err = b.Calendar().BeginRebuild(ctx, userID)
		if err != nil {
			return nil, err
		}
	}
	clearAll := false
	inputs, _ := ctx.Value(projectionInputsKey{}).(projectionInputs)
	id := calendarintegration.NewSyncLeaseID()
	err := b.fencedWrite(ctx, userID, func(tx *firestore.Transaction) error {
		doc, err := tx.Get(b.projectionPublicationRef(userID))
		var previous projectionPublication
		if err == nil {
			if err := doc.DataTo(&previous); err != nil {
				return err
			}
		} else if !firestoreNotFound(err) {
			return err
		}
		clearAll = previous.PolicyRevision != inputs.Revision || previous.PrivateRevision != inputs.PrivateRevision
		now := time.Now().UTC()
		// Deletion revokes an in-flight replacement before removing any rows.
		if !deleting {
			if previous.LeaseUntil != nil && previous.LeaseUntil.After(now) {
				return calendarintegration.ErrSyncBusy
			}
			if previous.Dirty && !clearAll && (previous.ClearRequired || from.After(previous.From) || to.Before(previous.To)) {
				return errProjectionRepairRequired
			}
		}
		until := now.Add(2 * time.Minute)
		return tx.Set(b.projectionPublicationRef(userID), projectionPublication{ID: id, Dirty: true, ClearRequired: deleting, From: from, To: to, LeaseUntil: &until})
	})
	if err != nil {
		return nil, err
	}
	ctx = context.WithValue(ctx, projectionClearAllKey{}, clearAll)
	return context.WithValue(ctx, projectionLeaseKey{}, projectionLease{UserID: userID, ID: id}), nil
}

func (b *Backend) guardProjectionWrite(ctx context.Context, tx *firestore.Transaction, userID string) error {
	lease, ok := ctx.Value(projectionLeaseKey{}).(projectionLease)
	if !ok {
		return nil
	}
	if lease.UserID != userID {
		return calendarintegration.ErrSyncLeaseLost
	}
	doc, err := tx.Get(b.projectionPublicationRef(userID))
	if firestoreNotFound(err) {
		return calendarintegration.ErrSyncLeaseLost
	}
	if err != nil {
		return err
	}
	var value projectionPublication
	if err := doc.DataTo(&value); err != nil {
		return err
	}
	if value.ID != lease.ID || value.LeaseUntil == nil || !value.LeaseUntil.After(time.Now().UTC()) {
		return calendarintegration.ErrSyncLeaseLost
	}
	return nil
}

func (b *Backend) finishProjectionWrite(ctx context.Context, userID string, ready bool) error {
	lease, _ := ctx.Value(projectionLeaseKey{}).(projectionLease)
	inputs, _ := ctx.Value(projectionInputsKey{}).(projectionInputs)
	return b.fencedWrite(ctx, userID, func(tx *firestore.Transaction) error {
		return tx.Set(b.projectionPublicationRef(userID), projectionPublication{ID: lease.ID, Ready: ready, PolicyRevision: inputs.Revision, PrivateRevision: inputs.PrivateRevision})
	})
}

func (b *Backend) abandonProjectionWrite(ctx context.Context, userID string) {
	ctx = context.WithValue(ctx, projectionInputsKey{}, nil)
	// Cancellation must not reopen publication. Release only this writer's lease;
	// process crashes remain recoverable after lease expiry.
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	_ = b.fencedWrite(ctx, userID, func(tx *firestore.Transaction) error {
		return tx.Update(b.projectionPublicationRef(userID), []firestore.Update{{Path: "LeaseUntil", Value: nil}})
	})
}

// Readers validate before and after reading the collection. An operation ID
// prevents ABA (ready -> busy -> ready) from accepting a mixed read.
// Legacy data has an empty revision; controls must not be deleted independently.
func (b *Backend) projectionReadRevision(ctx context.Context, userID string) (string, bool, error) {
	if deleting, err := b.accountIsDeleting(ctx, userID); err != nil {
		return "", false, err
	} else if deleting {
		return "", false, nil
	}
	if _, err := b.projectionBlock(userID).Get(ctx); err == nil {
		return "", false, nil
	} else if !firestoreNotFound(err) {
		return "", false, err
	}
	doc, err := b.projectionPublicationRef(userID).Get(ctx)
	legacy := firestoreNotFound(err)
	if err != nil && !legacy {
		return "", false, err
	}
	var value projectionPublication
	if !legacy {
		if err := doc.DataTo(&value); err != nil {
			return "", false, err
		}
	}
	revision, err := decodePolicyRevision(b.policyRevisionRef(userID).Get(ctx))
	if err != nil {
		return "", false, err
	}
	privateRevision, err := b.privateInputRevision(ctx, userID)
	if errors.Is(err, errPrivateInputsIncomplete) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	if legacy {
		return "", revision == "" && privateRevision == "", nil
	}
	return value.ID, value.PrivateRevision == privateRevision && value.PolicyRevision == revision && value.Ready && !value.Dirty && value.LeaseUntil == nil, nil
}
