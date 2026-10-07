package firestorestore

import (
	"context"
	"time"

	"cloud.google.com/go/firestore"
	cal "github.com/negotiable-calendar/negotiable-calendar/apps/api/internal/calendar"
	coord "github.com/negotiable-calendar/negotiable-calendar/apps/api/internal/request"
)

// Reads belong to the same transaction as the booking and precede all writes.
// Input/connection controls fence partial sync, disconnect and source replacement.
func (store *Request) requesterCalendar(ctx context.Context, tx *firestore.Transaction, userID string, from, to time.Time) (cal.SourceState, error) {
	source, err := store.requesterCalendarSource(ctx, tx, userID)
	if err != nil {
		return source, err
	}
	if !source.PrivateReadable(from, to, time.Now().UTC()) {
		return source, coord.ErrAvailabilityChanged
	}
	if !source.Managed {
		return source, nil
	}
	intervals, err := store.requesterBusyIntervals(ctx, tx, userID)
	if err != nil {
		return source, err
	}
	if err := cal.ValidateBusyIntervals(source, intervals, from, to, time.Now().UTC()); err != nil {
		return source, coord.ErrAvailabilityChanged
	}
	return source, nil
}

func (store *Request) requesterCalendarSource(ctx context.Context, tx *firestore.Transaction, userID string) (cal.SourceState, error) {
	if _, err := tx.Get(store.projectionBlock(userID)); err == nil {
		return cal.SourceState{}, coord.ErrAvailabilityChanged
	} else if !firestoreNotFound(err) {
		return cal.SourceState{}, err
	}
	inputs, err := decodePrivateInputs(tx.Get(store.privateInputsRef(userID)))
	if err != nil || !inputs.Ready || inputs.LeaseUntil != nil {
		return cal.SourceState{}, coord.ErrAvailabilityChanged
	}
	return store.sourceState(tx.Get, userID, inputs)
}

func (store *Request) requesterBusyIntervals(ctx context.Context, tx *firestore.Transaction, userID string) ([]cal.BusyInterval, error) {
	// No public projection and no provider identifiers/details cross this boundary.
	query := store.Client.Collection("users").Doc(userID).Collection("privateEvents").Select("UserID", "StartAt", "EndAt", "BusyStatus").Limit(cal.BusyEvidenceLimit + 1)
	docs, err := tx.Documents(query).GetAll()
	if err != nil {
		return nil, err
	}
	intervals := make([]cal.BusyInterval, 0, len(docs))
	for _, doc := range docs {
		var row struct {
			UserID, BusyStatus string
			StartAt, EndAt     time.Time
		}
		if err := doc.DataTo(&row); err != nil || row.UserID != userID {
			return nil, coord.ErrAvailabilityChanged
		}
		intervals = append(intervals, cal.BusyInterval{StartAt: row.StartAt, EndAt: row.EndAt, Status: row.BusyStatus})
	}
	return intervals, nil
}
