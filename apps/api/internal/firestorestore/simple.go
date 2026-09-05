package firestorestore

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"

	"cloud.google.com/go/firestore"
	"google.golang.org/api/iterator"

	"github.com/negotiable-calendar/negotiable-calendar/apps/api/internal/audit"
	"github.com/negotiable-calendar/negotiable-calendar/apps/api/internal/notification"
	"github.com/negotiable-calendar/negotiable-calendar/apps/api/internal/policy"
	"github.com/negotiable-calendar/negotiable-calendar/apps/api/internal/projection"
)

func (store *Policy) Get(ctx context.Context, userID string) (policy.SharingPolicy, error) {
	var value policy.SharingPolicy
	snapshot, err := store.Client.Collection("sharingPolicies").Doc(userID).Get(ctx)
	if err != nil {
		if firestoreNotFound(err) {
			return value, policy.ErrNotFound
		}
		return value, fmt.Errorf("get sharing policy: %w", err)
	}
	if err := snapshot.DataTo(&value); err != nil {
		return value, fmt.Errorf("decode sharing policy: %w", err)
	}
	return value, nil
}

func (store *Policy) Upsert(ctx context.Context, value policy.SharingPolicy) error {
	if err := value.Validate(); err != nil {
		return err
	}
	_, err := store.Client.Collection("sharingPolicies").Doc(value.UserID).Set(ctx, value)
	if err != nil {
		return fmt.Errorf("upsert sharing policy: %w", err)
	}
	return nil
}

func (store *Policy) ListActiveOverrides(ctx context.Context, userID string, now time.Time) ([]policy.ManualOverride, error) {
	iter := store.Client.Collection("users").Doc(userID).Collection("manualOverrides").Documents(ctx)
	defer iter.Stop()
	values := []policy.ManualOverride{}
	for {
		doc, err := iter.Next()
		if errors.Is(err, iterator.Done) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("list manual overrides: %w", err)
		}
		var value policy.ManualOverride
		if err := doc.DataTo(&value); err != nil {
			return nil, fmt.Errorf("decode manual override: %w", err)
		}
		if value.EndAt.After(now) && value.ExpiresAt.After(now) {
			values = append(values, value)
		}
	}
	sort.Slice(values, func(i, j int) bool {
		if values[i].StartAt.Equal(values[j].StartAt) {
			return values[i].ID < values[j].ID
		}
		return values[i].StartAt.Before(values[j].StartAt)
	})
	return values, nil
}

func (store *Policy) CreateOverride(ctx context.Context, value policy.ManualOverride) error {
	if err := value.Validate(); err != nil {
		return err
	}
	_, err := store.Client.Collection("users").Doc(value.UserID).Collection("manualOverrides").Doc(value.ID).Create(ctx, value)
	if err != nil {
		return fmt.Errorf("create manual override: %w", err)
	}
	return nil
}

func (store *Projection) GetView(ctx context.Context, userID, timezone string, from, to time.Time) (projection.View, error) {
	values, err := store.List(ctx, userID, from, to)
	if err != nil {
		return projection.View{}, err
	}
	return projection.NewView(userID, timezone, values)
}

func (store *Projection) List(ctx context.Context, userID string, from, to time.Time) ([]projection.ScheduleProjection, error) {
	return store.list(ctx, userID, from, to, false)
}

func (store *Projection) ListForUser(ctx context.Context, userID string) ([]projection.ScheduleProjection, error) {
	return store.list(ctx, userID, time.Time{}, time.Time{}, true)
}

func (store *Projection) list(ctx context.Context, userID string, from, to time.Time, all bool) ([]projection.ScheduleProjection, error) {
	iter := store.Client.Collection("users").Doc(userID).Collection("scheduleProjections").Documents(ctx)
	defer iter.Stop()
	now := time.Now().UTC()
	values := []projection.ScheduleProjection{}
	for {
		doc, err := iter.Next()
		if errors.Is(err, iterator.Done) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("list schedule projections: %w", err)
		}
		var value projection.ScheduleProjection
		if err := doc.DataTo(&value); err != nil {
			return nil, fmt.Errorf("decode schedule projection: %w", err)
		}
		if value.ExpiresAt.After(now) && (all || (value.StartAt.Before(to) && value.EndAt.After(from))) {
			values = append(values, value)
		}
	}
	sort.Slice(values, func(i, j int) bool {
		if values[i].StartAt.Equal(values[j].StartAt) {
			return values[i].ID < values[j].ID
		}
		return values[i].StartAt.Before(values[j].StartAt)
	})
	return values, nil
}

func (store *Projection) Replace(ctx context.Context, userID string, from, to time.Time, values []projection.ScheduleProjection) error {
	for _, value := range values {
		if err := value.Validate(); err != nil {
			return fmt.Errorf("validate replacement projection: %w", err)
		}
		if value.UserID != userID {
			return fmt.Errorf("replacement projection user mismatch")
		}
	}
	collection := store.Client.Collection("users").Doc(userID).Collection("scheduleProjections")
	iter := collection.Documents(ctx)
	defer iter.Stop()
	batch := store.Client.Batch()
	for {
		doc, err := iter.Next()
		if errors.Is(err, iterator.Done) {
			break
		}
		if err != nil {
			return fmt.Errorf("list replaced projections: %w", err)
		}
		var existing projection.ScheduleProjection
		if err := doc.DataTo(&existing); err != nil {
			return fmt.Errorf("decode replaced projection: %w", err)
		}
		if existing.StartAt.Before(to) && existing.EndAt.After(from) {
			batch.Delete(doc.Ref)
		}
	}
	for _, value := range values {
		batch.Set(collection.Doc(value.ID), value)
	}
	if _, err := batch.Commit(ctx); err != nil {
		return fmt.Errorf("replace schedule projections: %w", err)
	}
	return nil
}

func (store *Projection) DeleteForUser(ctx context.Context, userID string) error {
	return deleteCollection(ctx, store.Client, store.Client.Collection("users").Doc(userID).Collection("scheduleProjections"), 200)
}

func (store *Notification) Create(ctx context.Context, value notification.Notification) error {
	_, err := store.Client.Collection("users").Doc(value.UserID).Collection("notifications").Doc(value.ID).Create(ctx, value)
	if err != nil {
		return fmt.Errorf("create notification: %w", err)
	}
	return nil
}

func (store *Notification) List(ctx context.Context, userID string) ([]notification.Notification, error) {
	iter := store.Client.Collection("users").Doc(userID).Collection("notifications").Documents(ctx)
	defer iter.Stop()
	values := []notification.Notification{}
	for {
		doc, err := iter.Next()
		if errors.Is(err, iterator.Done) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("list notifications: %w", err)
		}
		var value notification.Notification
		if err := doc.DataTo(&value); err != nil {
			return nil, fmt.Errorf("decode notification: %w", err)
		}
		values = append(values, value)
	}
	sort.Slice(values, func(i, j int) bool {
		if values[i].CreatedAt.Equal(values[j].CreatedAt) {
			return values[i].ID > values[j].ID
		}
		return values[i].CreatedAt.After(values[j].CreatedAt)
	})
	if len(values) > 100 {
		values = values[:100]
	}
	return values, nil
}

func (store *Notification) MarkRead(ctx context.Context, id, userID string, now time.Time) (bool, error) {
	ref := store.Client.Collection("users").Doc(userID).Collection("notifications").Doc(id)
	err := store.Client.RunTransaction(ctx, func(ctx context.Context, tx *firestore.Transaction) error {
		doc, err := tx.Get(ref)
		if err != nil {
			return err
		}
		var value notification.Notification
		if err := doc.DataTo(&value); err != nil {
			return err
		}
		if value.ReadAt == nil {
			value.ReadAt = &now
			return tx.Set(ref, value)
		}
		return nil
	})
	if firestoreNotFound(err) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("mark notification read: %w", err)
	}
	return true, nil
}

func (store *Audit) Create(ctx context.Context, value audit.Event) error {
	if value.OrganizationID == "" {
		var request struct{ OrganizationID string }
		doc, err := store.Client.Collection("coordinationRequests").Doc(value.ResourceID).Get(ctx)
		if err != nil {
			return fmt.Errorf("audit resource not found")
		}
		if err := doc.DataTo(&request); err != nil {
			return fmt.Errorf("decode audit resource: %w", err)
		}
		value.OrganizationID = request.OrganizationID
	}
	_, err := store.Client.Collection("organizations").Doc(value.OrganizationID).Collection("auditLogs").Doc(value.ID).Create(ctx, value)
	if err != nil {
		return fmt.Errorf("create audit event: %w", err)
	}
	return nil
}

func (store *Audit) List(ctx context.Context, organizationID string) ([]audit.Event, error) {
	iter := store.Client.Collection("organizations").Doc(organizationID).Collection("auditLogs").Documents(ctx)
	defer iter.Stop()
	values := []audit.Event{}
	for {
		doc, err := iter.Next()
		if errors.Is(err, iterator.Done) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("list audit events: %w", err)
		}
		var value audit.Event
		if err := doc.DataTo(&value); err != nil {
			return nil, fmt.Errorf("decode audit event: %w", err)
		}
		values = append(values, value)
	}
	sort.Slice(values, func(i, j int) bool {
		if values[i].CreatedAt.Equal(values[j].CreatedAt) {
			return values[i].ID > values[j].ID
		}
		return values[i].CreatedAt.After(values[j].CreatedAt)
	})
	if len(values) > 200 {
		values = values[:200]
	}
	return values, nil
}

func firestoreNotFound(err error) bool { return err != nil && stringsContains(err.Error(), "NotFound") }

func stringsContains(value, fragment string) bool {
	for index := 0; index+len(fragment) <= len(value); index++ {
		if value[index:index+len(fragment)] == fragment {
			return true
		}
	}
	return false
}

func deleteCollection(ctx context.Context, client *firestore.Client, collection *firestore.CollectionRef, batchSize int) error {
	for {
		docs, err := collection.Limit(batchSize).Documents(ctx).GetAll()
		if err != nil {
			return err
		}
		if len(docs) == 0 {
			return nil
		}
		batch := client.Batch()
		for _, doc := range docs {
			batch.Delete(doc.Ref)
		}
		if _, err := batch.Commit(ctx); err != nil {
			return err
		}
	}
}
