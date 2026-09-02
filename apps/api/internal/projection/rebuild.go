package projection

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/negotiable-calendar/negotiable-calendar/apps/api/internal/policy"
	"github.com/negotiable-calendar/negotiable-calendar/apps/api/internal/privateevent"
)

type RebuildStore interface {
	UserTimezone(context.Context, string) (string, error)
	ListPrivateEvents(context.Context, string, time.Time, time.Time) ([]privateevent.PrivateEvent, error)
	Replace(context.Context, string, time.Time, time.Time, []ScheduleProjection) error
}

type Rebuilder struct {
	store    RebuildStore
	policies policy.Store
	engine   Engine
}

func NewRebuilder(store RebuildStore, policies policy.Store) *Rebuilder {
	return &Rebuilder{store: store, policies: policies, engine: NewEngine()}
}

func (rebuilder *Rebuilder) Rebuild(ctx context.Context, userID string, from, to, now time.Time) error {
	timezone, err := rebuilder.store.UserTimezone(ctx, userID)
	if err != nil {
		return fmt.Errorf("load projection timezone: %w", err)
	}
	value, err := rebuilder.policies.Get(ctx, userID)
	if errors.Is(err, policy.ErrNotFound) {
		value = defaultPolicy(userID, now)
		if err := rebuilder.policies.Upsert(ctx, value); err != nil {
			return fmt.Errorf("create default sharing policy: %w", err)
		}
	} else if err != nil {
		return fmt.Errorf("load sharing policy: %w", err)
	}
	events, err := rebuilder.store.ListPrivateEvents(ctx, userID, from, to)
	if err != nil {
		return fmt.Errorf("load private events: %w", err)
	}
	overrides, err := rebuilder.policies.ListActiveOverrides(ctx, userID, now)
	if err != nil {
		return fmt.Errorf("load manual overrides: %w", err)
	}
	values, err := rebuilder.engine.Generate(GenerateInput{
		UserID: userID, Timezone: timezone, From: from, To: to,
		Events: events, Policy: value, ManualOverrides: overrides, Now: now,
	})
	if err != nil {
		return fmt.Errorf("generate schedule projections: %w", err)
	}
	if err := rebuilder.store.Replace(ctx, userID, from, to, values); err != nil {
		return fmt.Errorf("publish schedule projections: %w", err)
	}
	return nil
}

func defaultPolicy(userID string, now time.Time) policy.SharingPolicy {
	windows := make([]policy.WorkingWindow, 0, 5)
	for weekday := time.Monday; weekday <= time.Friday; weekday++ {
		windows = append(windows, policy.WorkingWindow{Weekday: weekday, StartMinute: 9 * 60, EndMinute: 18 * 60})
	}
	return policy.SharingPolicy{
		ID: "default-policy-" + userID, UserID: userID,
		Default: policy.InteractionState{
			Availability: policy.Available, Interruptibility: policy.InterruptNormal,
			Requestability: policy.RequestOpen, Reschedulability: policy.RescheduleMedium,
		},
		WorkingHours: windows, CreatedAt: now, UpdatedAt: now,
	}
}

type PostgresRebuildStore struct {
	database    *sql.DB
	projections *PostgresStore
}

func NewPostgresRebuildStore(database *sql.DB) *PostgresRebuildStore {
	return &PostgresRebuildStore{database: database, projections: NewPostgresStore(database)}
}

func (store *PostgresRebuildStore) UserTimezone(ctx context.Context, userID string) (string, error) {
	var timezone string
	if err := store.database.QueryRowContext(ctx, `SELECT timezone FROM users WHERE id=$1`, userID).Scan(&timezone); err != nil {
		return "", fmt.Errorf("get user timezone: %w", err)
	}
	return timezone, nil
}

func (store *PostgresRebuildStore) ListPrivateEvents(ctx context.Context, userID string, from, to time.Time) ([]privateevent.PrivateEvent, error) {
	rows, err := store.database.QueryContext(ctx, `
SELECT provider_event_id,calendar_id,start_at,end_at,busy_status,visibility,created_at,updated_at
FROM private_events
WHERE user_id=$1 AND start_at<$3 AND end_at>$2
ORDER BY start_at,provider_event_id
`, userID, from, to)
	if err != nil {
		return nil, fmt.Errorf("query private events: %w", err)
	}
	defer rows.Close()
	var values []privateevent.PrivateEvent
	for rows.Next() {
		var value privateevent.PrivateEvent
		value.UserID = userID
		if err := rows.Scan(&value.ProviderEventID, &value.CalendarID, &value.StartAt, &value.EndAt,
			&value.BusyStatus, &value.Visibility, &value.CreatedAt, &value.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan private event: %w", err)
		}
		value.ID = userID + ":" + value.ProviderEventID
		value.StartAt, value.EndAt = value.StartAt.UTC(), value.EndAt.UTC()
		value.CreatedAt, value.UpdatedAt = value.CreatedAt.UTC(), value.UpdatedAt.UTC()
		values = append(values, value)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate private events: %w", err)
	}
	return values, nil
}

func (store *PostgresRebuildStore) Replace(ctx context.Context, userID string, from, to time.Time, values []ScheduleProjection) error {
	return store.projections.Replace(ctx, userID, from, to, values)
}
