package firestorestore

import (
	"cloud.google.com/go/firestore"
	"context"
	cal "github.com/negotiable-calendar/negotiable-calendar/apps/api/internal/calendar"
	coord "github.com/negotiable-calendar/negotiable-calendar/apps/api/internal/request"
	"time"
)

func (store *Request) LoadRequesterCalendar(ctx context.Context, userID string) (cal.CandidateAvailability, error) {
	ctx, cancel := context.WithTimeout(ctx, coord.ReservationReadTimeout)
	defer cancel()
	zero := cal.CandidateAvailability{}
	if userID == "" {
		return zero, coord.ErrAvailabilityChanged
	}
	var snapshot cal.CandidateAvailability
	err := store.Client.RunTransaction(ctx, func(ctx context.Context, tx *firestore.Transaction) error {
		snapshot = zero
		if err := store.guardAccountActive(ctx, tx, userID); err != nil {
			return err
		}
		if _, err := tx.Get(store.Client.Collection("users").Doc(userID)); err != nil {
			return err
		}
		source, err := store.requesterCalendarSource(ctx, tx, userID)
		if err != nil {
			return err
		}
		if source.Managed && !source.Committed(time.Now().UTC()) {
			return coord.ErrAvailabilityChanged
		}
		var intervals []cal.BusyInterval
		if source.Managed {
			intervals, err = store.requesterBusyIntervals(ctx, tx, userID)
			if err != nil {
				return err
			}
		}
		snapshot, err = cal.NewCandidateAvailability(source, intervals, time.Now().UTC())
		return err
	}, firestore.ReadOnly)
	if err != nil {
		return zero, err
	}
	if err := snapshot.Validate(time.Now().UTC()); err != nil {
		return zero, err
	}
	return snapshot, nil
}

var _ coord.RequesterCalendarStore = (*Request)(nil)
