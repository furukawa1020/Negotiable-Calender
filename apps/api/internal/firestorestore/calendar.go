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
	"github.com/negotiable-calendar/negotiable-calendar/apps/api/internal/projection"
)

var errClaimLost = errors.New("calendar connection claim lost")

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
	value.SyncLeaseID = ""
	value.SyncLeaseUntil = nil
	value.LastSyncedAt = nil
	next := value.ConnectedAt
	value.NextAttemptAt = &next
	return store.Client.RunTransaction(ctx, func(ctx context.Context, tx *firestore.Transaction) error {
		doc, err := tx.Get(store.projectionBlock(value.UserID))
		if err != nil && !firestoreNotFound(err) {
			return err
		}
		if err == nil {
			var control publicationControl
			if err := doc.DataTo(&control); err != nil {
				return err
			}
			if control.InProgress {
				return calendarintegration.ErrSyncBusy
			}
		}
		return tx.Set(store.Client.Collection("calendarConnections").Doc(value.UserID), value)
	})
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
	return store.MarkSyncSuccess(ctx, userID, "", now, now.Add(15*time.Minute))
}
func (store *Calendar) MarkReconnectRequired(ctx context.Context, userID string) error {
	return store.fencedWrite(ctx, userID, func(tx *firestore.Transaction) error {
		return tx.Update(store.Client.Collection("calendarConnections").Doc(userID), []firestore.Update{{Path: "ReconnectRequired", Value: true}})
	})
}
func (store *Calendar) DeleteConnection(ctx context.Context, userID string) error {
	id := calendarintegration.NewSyncLeaseID()
	err := store.Client.RunTransaction(ctx, func(ctx context.Context, tx *firestore.Transaction) error {
		doc, err := tx.Get(store.projectionBlock(userID))
		if err != nil && !firestoreNotFound(err) {
			return err
		}
		now := time.Now().UTC()
		if err == nil {
			var control publicationControl
			if err := doc.DataTo(&control); err != nil {
				return err
			}
			if control.InProgress && control.CleanupUntil != nil && control.CleanupUntil.After(now) {
				return calendarintegration.ErrSyncBusy
			}
		}
		until := now.Add(2 * time.Minute)
		if err := tx.Set(store.projectionBlock(userID), publicationControl{BlockedAt: now, CleanupID: id, CleanupUntil: &until, InProgress: true}); err != nil {
			return err
		}
		if err := tx.Set(store.privateInputsRef(userID), privateInputsControl{ID:id}); err != nil { return err }
		// Invalidates every old sync before any destructive cleanup begins.
		return tx.Delete(store.Client.Collection("calendarConnections").Doc(userID))
	})
	if err != nil {
		return err
	}
	ctx = context.WithValue(ctx, cleanupLeaseKey{}, cleanupLease{UserID: userID, ID: id})
	if err := store.Projection().DeleteForUser(ctx, userID); err != nil {
		return err
	}
	if err := deleteCollection(ctx, store.Client, store.Client.Collection("users").Doc(userID).Collection("privateEvents"), 200); err != nil {
		return err
	}
	return store.fencedWrite(ctx, userID, func(tx *firestore.Transaction) error {
		if err := tx.Set(store.privateInputsRef(userID), privateInputsControl{ID:id,Ready:true}); err != nil { return err }
		return tx.Update(store.projectionBlock(userID), []firestore.Update{{Path: "InProgress", Value: false}, {Path: "CleanupUntil", Value: nil}})
	})
}
func (store *Calendar) UserTimezone(ctx context.Context, userID string) (string, error) {
	return store.Organization().UserTimezone(ctx, userID)
}

func (store *Calendar) ApplyChanges(ctx context.Context, userID string, changes calendarintegration.ChangeSet, from, to, now time.Time) error {
	if !from.Before(to) { return fmt.Errorf("invalid calendar sync range") }
 for _, span := range changes.Upserts {
  if span.ProviderEventID == "" || span.CalendarID == "" || !span.StartAt.Before(span.EndAt) { return fmt.Errorf("invalid calendar change") }
 }
 ctx, err := store.beginPrivateInputs(ctx,userID,changes.Full)
 if err != nil { return err }
 completed := false
 defer func(){ if !completed { store.abandonPrivateInputs(ctx,userID) } }()
 collection := store.Client.Collection("users").Doc(userID).Collection("privateEvents")
 // Full responses replace the whole cache, so shifted-window recovery cannot
 // retain partial rows from an earlier failed attempt.
 if changes.Full {
  if err := deleteCollection(ctx,store.Client,collection,200); err != nil { return err }
 }
 writes := newChunkedBatch(store.Client,userID)
	for _, id := range changes.DeletedProviderEventIDs {
		if id != "" {
			if err := writes.Delete(ctx, collection.Doc(safeDigest(id))); err != nil {
				return fmt.Errorf("delete private event changes: %w", err)
			}
		}
	}
	for _, span := range changes.Upserts {
		status := privateevent.Busy
		if !span.Busy {
			status = privateevent.Free
		}
		value := privateEventRecord{ID: userID + ":" + span.ProviderEventID, UserID: userID, ProviderEventID: span.ProviderEventID, CalendarID: span.CalendarID, StartAt: span.StartAt.UTC(), EndAt: span.EndAt.UTC(), BusyStatus: status, Visibility: privateevent.VisibilityDefault, CreatedAt: now.UTC(), UpdatedAt: now.UTC()}
		if err := writes.Set(ctx, collection.Doc(safeDigest(span.ProviderEventID)), value); err != nil {
			return fmt.Errorf("write private event changes: %w", err)
		}
	}
	if err := writes.Commit(ctx); err != nil {
		return fmt.Errorf("commit private event changes: %w", err)
	}
	if err := store.finishPrivateInputs(ctx,userID); err != nil { return err }
 completed = true
 return nil
}
func (store *Calendar) ListPrivateEvents(ctx context.Context, userID string, from, to time.Time) ([]privateevent.PrivateEvent, error) {
 revision, err := store.privateInputRevision(ctx,userID)
 if err != nil { return nil,err }
 if inputs,ok := ctx.Value(projectionInputsKey{}).(projectionInputs); ok && (inputs.UserID != userID || inputs.PrivateRevision != revision) { return nil,errProjectionInputsChanged }
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
	current,err := store.privateInputRevision(ctx,userID)
 if err != nil { return nil,err }
 if revision != current { return nil,errProjectionInputsChanged }
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
	if limit <= 0 {
		limit = 10
	}
	if lease <= 0 {
		lease = 2 * time.Minute
	}
	iter := store.Client.Collection("calendarConnections").Documents(ctx)
	defer iter.Stop()
	values := []calendarintegration.Connection{}
	for {
		doc, err := iter.Next()
		if errors.Is(err, iterator.Done) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("list due calendar connections: %w", err)
		}
		var value calendarintegration.Connection
		if err := doc.DataTo(&value); err != nil {
			return nil, fmt.Errorf("decode calendar connection: %w", err)
		}
		if !value.ReconnectRequired && (value.SyncLeaseUntil == nil || !value.SyncLeaseUntil.After(now)) && (value.NextAttemptAt == nil || !value.NextAttemptAt.After(now)) {
			values = append(values, value)
		}
	}
	sort.Slice(values, func(i, j int) bool {
		if values[i].NextAttemptAt == nil {
			return values[j].NextAttemptAt != nil || values[i].UserID < values[j].UserID
		}
		if values[j].NextAttemptAt == nil {
			return false
		}
		if values[i].NextAttemptAt.Equal(*values[j].NextAttemptAt) {
			return values[i].UserID < values[j].UserID
		}
		return values[i].NextAttemptAt.Before(*values[j].NextAttemptAt)
	})
	if len(values) > limit {
		values = values[:limit]
	}
	claimed := make([]calendarintegration.Connection, 0, len(values))
	for _, candidate := range values {
		ref := store.Client.Collection("calendarConnections").Doc(candidate.UserID)
		var current calendarintegration.Connection
		err := store.Client.RunTransaction(ctx, func(ctx context.Context, tx *firestore.Transaction) error {
			doc, err := tx.Get(ref)
			if err != nil {
				return err
			}
			if err := doc.DataTo(&current); err != nil {
				return err
			}
			if current.ReconnectRequired || (current.SyncLeaseUntil != nil && current.SyncLeaseUntil.After(now)) || (current.NextAttemptAt != nil && current.NextAttemptAt.After(now)) {
				return errClaimLost
			}
			next := now.Add(lease)
			current.LastAttemptAt = &now
			current.NextAttemptAt = &next
			return tx.Set(ref, current)
		})
		if errors.Is(err, errClaimLost) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("claim calendar connection: %w", err)
		}
		claimed = append(claimed, current)
	}
	return claimed, nil
}
func (store *Calendar) MarkSyncSuccess(ctx context.Context, userID, syncToken string, now, next time.Time) error {
	return store.fencedWrite(ctx, userID, func(tx *firestore.Transaction) error {
		if err := tx.Update(store.Client.Collection("calendarConnections").Doc(userID), []firestore.Update{{Path: "SyncToken", Value: syncToken}, {Path: "LastSyncedAt", Value: now}, {Path: "LastAttemptAt", Value: now}, {Path: "NextAttemptAt", Value: next}, {Path: "LastErrorCode", Value: ""}, {Path: "FailureCount", Value: 0}, {Path: "ReconnectRequired", Value: false}, {Path: "SyncLeaseID", Value: ""}, {Path: "SyncLeaseUntil", Value: nil}}); err != nil {
			return err
		}
		return tx.Delete(store.projectionBlock(userID))
	})
}
func (store *Calendar) MarkSyncFailure(ctx context.Context, userID, code string, next time.Time, reconnect bool) error {
	if code == "" {
		code = "temporary_failure"
	}
	return store.fencedWrite(ctx, userID, func(tx *firestore.Transaction) error {
		return tx.Update(store.Client.Collection("calendarConnections").Doc(userID), []firestore.Update{{Path: "NextAttemptAt", Value: next}, {Path: "LastErrorCode", Value: code}, {Path: "FailureCount", Value: firestore.Increment(1)}, {Path: "ReconnectRequired", Value: reconnect}, {Path: "SyncLeaseID", Value: ""}, {Path: "SyncLeaseUntil", Value: nil}})
	})
}
