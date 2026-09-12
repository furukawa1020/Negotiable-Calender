package firestorestore

import (
	"context"
	"time"

	"cloud.google.com/go/firestore"
	calendarintegration "github.com/negotiable-calendar/negotiable-calendar/apps/api/internal/calendar"
)

type cleanupLeaseKey struct{}
type cleanupLease struct{ UserID, ID string }
type publicationControl struct {
	BlockedAt    time.Time
	CleanupID    string
	CleanupUntil *time.Time
	InProgress   bool
}

func (store *Calendar) AcquireSync(ctx context.Context, userID string, now time.Time, duration time.Duration) (calendarintegration.Connection, error) {
	if duration <= 0 {
		duration = 2 * time.Minute
	}
	id := calendarintegration.NewSyncLeaseID()
	var value calendarintegration.Connection
	ref := store.Client.Collection("calendarConnections").Doc(userID)
	err := store.Client.RunTransaction(ctx, func(ctx context.Context, tx *firestore.Transaction) error {
		doc, err := tx.Get(ref)
		if firestoreNotFound(err) {
			return calendarintegration.ErrNotFound
		}
		if err != nil {
			return err
		}
		if err := doc.DataTo(&value); err != nil {
			return err
		}
		if value.ReconnectRequired {
			return calendarintegration.ErrReconnectRequired
		}
		if value.SyncLeaseID != "" && value.SyncLeaseUntil != nil && value.SyncLeaseUntil.After(now) {
			return calendarintegration.ErrSyncBusy
		}
		until := now.Add(duration)
		value.SyncLeaseID, value.SyncLeaseUntil = id, &until
		value.LastAttemptAt, value.NextAttemptAt = &now, &until
		return tx.Set(ref, value)
	})
	if err != nil {
		return calendarintegration.Connection{}, err
	}
	return value, nil
}

func (backend *Backend) guardCalendarWrite(ctx context.Context, tx *firestore.Transaction, userID string) error {
	if lease, ok := ctx.Value(cleanupLeaseKey{}).(cleanupLease); ok {
		if lease.UserID != userID {
			return calendarintegration.ErrSyncLeaseLost
		}
		doc, err := tx.Get(backend.projectionBlock(userID))
		if firestoreNotFound(err) {
			return calendarintegration.ErrSyncLeaseLost
		}
		if err != nil {
			return err
		}
		var control publicationControl
		if err := doc.DataTo(&control); err != nil {
			return err
		}
		if !control.InProgress || control.CleanupID != lease.ID || control.CleanupUntil == nil || !control.CleanupUntil.After(time.Now().UTC()) {
			return calendarintegration.ErrSyncLeaseLost
		}
		return nil
	}
	lease, ok := calendarintegration.SyncLeaseFromContext(ctx)
	if !ok {
		return nil
	}
	if lease.UserID != userID || lease.ID == "" {
		return calendarintegration.ErrSyncLeaseLost
	}
	doc, err := tx.Get(backend.Client.Collection("calendarConnections").Doc(userID))
	if firestoreNotFound(err) {
		return calendarintegration.ErrSyncLeaseLost
	}
	if err != nil {
		return err
	}
	var connection calendarintegration.Connection
	if err := doc.DataTo(&connection); err != nil {
		return err
	}
	if connection.SyncLeaseID != lease.ID || connection.SyncLeaseUntil == nil || !connection.SyncLeaseUntil.After(time.Now().UTC()) {
		return calendarintegration.ErrSyncLeaseLost
	}
	return nil
}

func (backend *Backend) fencedWrite(ctx context.Context, userID string, write func(*firestore.Transaction) error) error {
	return backend.Client.RunTransaction(ctx, func(txctx context.Context, tx *firestore.Transaction) error {
		if err := backend.guardCalendarWrite(txctx, tx, userID); err != nil {
			return err
		}
		if err := backend.guardProjectionWrite(txctx, tx, userID); err != nil {
			return err
		}
		if err := backend.guardProjectionInputs(txctx, tx, userID); err != nil { return err }
		return write(tx)
	})
}

func fenceUser(ctx context.Context) (string, bool) {
	if lease, ok := ctx.Value(projectionLeaseKey{}).(projectionLease); ok {
		return lease.UserID, true
	}
	if lease, ok := ctx.Value(cleanupLeaseKey{}).(cleanupLease); ok {
		return lease.UserID, true
	}
	if lease, ok := calendarintegration.SyncLeaseFromContext(ctx); ok {
		return lease.UserID, true
	}
	return "", false
}
