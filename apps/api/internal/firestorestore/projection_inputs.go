package firestorestore

import (
	"context"
	"errors"

	"cloud.google.com/go/firestore"
	calendarintegration "github.com/negotiable-calendar/negotiable-calendar/apps/api/internal/calendar"
)

var errProjectionInputsChanged = errors.New("projection policy inputs changed; rebuild required")

type projectionInputsKey struct{}
type projectionInputs struct{ UserID, Revision, PrivateRevision string }
type policyRevision struct{ ID string }

func (b *Backend) policyRevisionRef(userID string) *firestore.DocumentRef {
	return b.Client.Collection("users").Doc(userID).Collection("projectionControls").Doc("policyRevision")
}

func decodePolicyRevision(doc *firestore.DocumentSnapshot, err error) (string, error) {
	if firestoreNotFound(err) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	var value policyRevision
	if err := doc.DataTo(&value); err != nil {
		return "", err
	}
	if value.ID == "" {
		return "", errProjectionInputsChanged
	}
	return value.ID, nil
}

// Capture before reading policy and overrides. Every publication batch checks
// this revision; the completed gate retains it for public read invalidation.
func (store *Calendar) BeginRebuild(ctx context.Context, userID string) (context.Context, error) {
	revision, err := decodePolicyRevision(store.policyRevisionRef(userID).Get(ctx))
	if err != nil {
		return nil, err
	}
	privateRevision, err := store.privateInputRevision(ctx, userID)
	if err != nil {
		return nil, err
	}
	return context.WithValue(ctx, projectionInputsKey{}, projectionInputs{UserID: userID, Revision: revision, PrivateRevision: privateRevision}), nil
}

func (b *Backend) guardProjectionInputs(ctx context.Context, tx *firestore.Transaction, userID string) error {
	inputs, ok := ctx.Value(projectionInputsKey{}).(projectionInputs)
	if !ok {
		return nil
	}
	if inputs.UserID != userID {
		return errProjectionInputsChanged
	}
	revision, err := decodePolicyRevision(tx.Get(b.policyRevisionRef(userID)))
	if err != nil {
		return err
	}
	private, err := decodePrivateInputs(tx.Get(b.privateInputsRef(userID)))
	if err != nil {
		return err
	}
	if !private.Ready || private.LeaseUntil != nil {
		return errPrivateInputsIncomplete
	}
	if private.ID != inputs.PrivateRevision {
		return errProjectionInputsChanged
	}
	if revision != inputs.Revision {
		return errProjectionInputsChanged
	}
	return nil
}

func (b *Backend) invalidatePolicyProjections(tx *firestore.Transaction, userID string) error {
	return tx.Set(b.policyRevisionRef(userID), policyRevision{ID: calendarintegration.NewSyncLeaseID()})
}
