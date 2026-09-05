package firestorestore

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"sort"
	"time"

	"cloud.google.com/go/firestore"
	"google.golang.org/api/iterator"

	calendarintegration "github.com/negotiable-calendar/negotiable-calendar/apps/api/internal/calendar"
	"github.com/negotiable-calendar/negotiable-calendar/apps/api/internal/privateevent"
)

type privateEventRecord struct {
	ID, UserID, ProviderEventID, CalendarID string
	StartAt, EndAt                          time.Time
	BusyStatus                              privateevent.BusyStatus
	Visibility                              privateevent.Visibility
	CreatedAt, UpdatedAt                    time.Time
}

func (store *Calendar) CreateFlow(ctx context.Context, value calendarintegration.Flow) error {
	_, err := store.Client.Collection("calendarOAuthFlows").Doc(value.ID).Create(ctx, value)
	return err
}
func (store *Calendar) ConsumeFlow(ctx context.Context, id, userID string, state []byte, now time.Time) (calendarintegration.Flow, error) {
	ref := store.Client.Collection("calendarOAuthFlows").Doc(id)
	var value calendarintegration.Flow
	err := store.Client.RunTransaction(ctx, func(ctx context.Context, tx *firestore.Transaction) error {
		doc, err := tx.Get(ref)
		if err != nil {
			return err
		}
		if err := doc.DataTo(&value); err != nil {
			return err
		}
		if value.UserID != userID || !bytes.Equal(value.StateHash, state) || !value.ExpiresAt.After(now) {
			return calendarintegration.ErrNotFound
		}
		return tx.Delete(ref)
	})
	if firestoreNotFound(err) {
		return value, calendarintegration.ErrNotFound
	}
	return value, err
}
func (store *Calendar) SaveConnection(ctx context.Context, value calendarintegration.Connection) error {
	value.ReconnectRequired = false
	value.LastErrorCode = ""
	value.FailureCount = 0
	value.SyncToken = ""
	next := value.ConnectedAt
	value.NextAttemptAt = &next
	_, err := store.Client.Collection("calendarConnections").Doc(value.UserID).Set(ctx, value)
	return err
}
func (store *Calendar) GetConnection(ctx context.Context, userID string) (calendarintegration.Connection, error) {
	var value calendarintegration.Connection
	doc, err := store.Client.Collection("calendarConnections").Doc(userID).Get(ctx)
	if firestoreNotFound(err) {
		return value, calendarintegration.ErrNotFound
	}
	if err != nil {
		return value, err
	}
	err = doc.DataTo(&value)
	return value, err
}
func (store *Calendar) ReplaceBusySpans(ctx context.Context, userID string, spans []calendarintegration.BusySpan, from, to, now time.Time) error {
	changes := calendarintegration.ChangeSet{Full: true, Upserts: spans}
	return store.ApplyChanges(ctx, userID, changes, from, to, now)
}
func (store *Calendar) MarkSynced(ctx context.Context, userID string, now time.Time) error {
	_, err := store.Client.Collection("calendarConnections").Doc(userID).Update(ctx, []firestore.Update{{Path: "LastSyncedAt", Value: now}, {Path: "ReconnectRequired", Value: false}})
	return err
}
func (store *Calendar) MarkReconnectRequired(ctx context.Context, userID string) error {
	_, err := store.Client.Collection("calendarConnections").Doc(userID).Update(ctx, []firestore.Update{{Path: "ReconnectRequired", Value: true}})
	return err
}
func (store *Calendar) DeleteConnection(ctx context.Context, userID string) error {
	if err := deleteCollection(ctx, store.Client, store.Client.Collection("users").Doc(userID).Collection("privateEvents"), 200); err != nil {
		return err
	}
	_, err := store.Client.Collection("calendarConnections").Doc(userID).Delete(ctx)
	return err
}
func (store *Calendar) UserTimezone(ctx context.Context, userID string) (string, error) {
	return store.Organization().UserTimezone(ctx, userID)
}

func (store *Calendar) ApplyChanges(ctx context.Context, userID string, changes calendarintegration.ChangeSet, from, to, now time.Time) error {
	collection := store.Client.Collection("users").Doc(userID).Collection("privateEvents")
	batch := store.Client.Batch()
	if changes.Full {
		iter := collection.Documents(ctx)
		defer iter.Stop()
		for {
			doc, err := iter.Next()
			if errors.Is(err, iterator.Done) {
				break
			}
			if err != nil {
				return err
			}
			var value privateEventRecord
			if err := doc.DataTo(&value); err != nil {
				return err
			}
			if value.StartAt.Before(to) && value.EndAt.After(from) {
				batch.Delete(doc.Ref)
			}
		}
	}
	for _, id := range changes.DeletedProviderEventIDs {
		if id != "" {
			batch.Delete(collection.Doc(safeDigest(id)))
		}
	}
	for _, span := range changes.Upserts {
		status := privateevent.Busy
		if !span.Busy {
			status = privateevent.Free
		}
		value := privateEventRecord{ID: userID + ":" + span.ProviderEventID, UserID: userID, ProviderEventID: span.ProviderEventID, CalendarID: span.CalendarID, StartAt: span.StartAt.UTC(), EndAt: span.EndAt.UTC(), BusyStatus: status, Visibility: privateevent.VisibilityDefault, CreatedAt: now.UTC(), UpdatedAt: now.UTC()}
		batch.Set(collection.Doc(safeDigest(span.ProviderEventID)), value)
	}
	_, err := batch.Commit(ctx)
	return err
}
func (store *Calendar) ListPrivateEvents(ctx context.Context, userID string, from, to time.Time) ([]privateevent.PrivateEvent, error) {
	iter := store.Client.Collection("users").Doc(userID).Collection("privateEvents").Documents(ctx)
	defer iter.Stop()
	values := []privateevent.PrivateEvent{}
	for {
		doc, err := iter.Next()
		if errors.Is(err, iterator.Done) {
			break
		}
		if err != nil {
			return nil, err
		}
		var value privateEventRecord
		if err := doc.DataTo(&value); err != nil {
			return nil, err
		}
		if value.StartAt.Before(to) && value.EndAt.After(from) {
			values = append(values, privateevent.PrivateEvent{ID: value.ID, UserID: value.UserID, ProviderEventID: value.ProviderEventID, CalendarID: value.CalendarID, StartAt: value.StartAt, EndAt: value.EndAt, BusyStatus: value.BusyStatus, Visibility: value.Visibility, CreatedAt: value.CreatedAt, UpdatedAt: value.UpdatedAt})
		}
	}
	sort.Slice(values, func(i, j int) bool {
		if values[i].StartAt.Equal(values[j].StartAt) {
			return values[i].ProviderEventID < values[j].ProviderEventID
		}
		return values[i].StartAt.Before(values[j].StartAt)
	})
	return values, nil
}
func (store *Calendar) Replace(ctx context.Context, userID string, from, to time.Time, values []projection.ScheduleProjection) error {
	return store.Projection().Replace(ctx, userID, from, to, values)
}

func (store *Calendar) ClaimDueConnections(ctx context.Context, now time.Time, limit int, lease time.Duration) ([]calendarintegration.Connection, error) {
	return []calendarintegration.Connection{}, nil
}
func (store *Calendar) MarkSyncSuccess(ctx context.Context, userID, syncToken string, now, next time.Time) error {
	_, err := store.Client.Collection("calendarConnections").Doc(userID).Update(ctx, []firestore.Update{{Path: "SyncToken", Value: syncToken}, {Path: "LastSyncedAt", Value: now}, {Path: "LastAttemptAt", Value: now}, {Path: "NextAttemptAt", Value: next}, {Path: "LastErrorCode", Value: ""}, {Path: "FailureCount", Value: 0}, {Path: "ReconnectRequired", Value: false}})
	return err
}
func (store *Calendar) MarkSyncFailure(ctx context.Context, userID, code string, next time.Time, reconnect bool) error {
	if code == "" {
		code = "temporary_failure"
	}
	_, err := store.Client.Collection("calendarConnections").Doc(userID).Update(ctx, []firestore.Update{{Path: "NextAttemptAt", Value: next}, {Path: "LastErrorCode", Value: code}, {Path: "FailureCount", Value: firestore.Increment(1)}, {Path: "ReconnectRequired", Value: reconnect}})
	return err
}
