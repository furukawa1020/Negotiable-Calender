package firestorestore

import (
	"cloud.google.com/go/firestore"
	"context"
	calendarintegration "github.com/negotiable-calendar/negotiable-calendar/apps/api/internal/calendar"
)

func (b *Backend) sourceState(get func(*firestore.DocumentRef) (*firestore.DocumentSnapshot, error), userID string, inputs privateInputsControl) (calendarintegration.SourceState, error) {
	state := calendarintegration.SourceState{Managed: inputs.Source != nil}
	if inputs.Source != nil && inputs.Ready && inputs.LeaseUntil == nil && inputs.Source.Revision == inputs.ID {
		state.Snapshot = *inputs.Source
	}
	doc, err := get(b.Client.Collection("calendarConnections").Doc(userID))
	if err == nil {
		var c calendarintegration.Connection
		if err := doc.DataTo(&c); err != nil {
			return state, err
		}
		if c.UserID != userID {
			return state, calendarintegration.ErrSourceUnavailable
		}
		state.Managed, state.Connection = true, &c
	} else if !firestoreNotFound(err) {
		return state, err
	}
	if _, err := get(b.projectionBlock(userID)); err == nil {
		state.Managed = true
	} else if !firestoreNotFound(err) {
		return state, err
	}
	return state, nil
}

func (store *Calendar) LoadSourceSnapshot(ctx context.Context, userID string) (calendarintegration.SourceSnapshot, error) {
	inputs, err := decodePrivateInputs(store.privateInputsRef(userID).Get(ctx))
	if err != nil {
		return calendarintegration.SourceSnapshot{}, err
	}
	if !inputs.Ready || inputs.LeaseUntil != nil || inputs.Source == nil || inputs.Source.Revision != inputs.ID {
		return calendarintegration.SourceSnapshot{}, nil
	}
	return *inputs.Source, nil
}
