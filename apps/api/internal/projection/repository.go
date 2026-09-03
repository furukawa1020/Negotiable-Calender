package projection

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/negotiable-calendar/negotiable-calendar/apps/api/internal/policy"
)

type Store interface {
	GetView(context.Context, string, string, time.Time, time.Time) (View, error)
	List(context.Context, string, time.Time, time.Time) ([]ScheduleProjection, error)
	ListForUser(context.Context, string) ([]ScheduleProjection, error)
}

type PostgresStore struct {
	database *sql.DB
}

func NewPostgresStore(database *sql.DB) *PostgresStore {
	return &PostgresStore{database: database}
}

func EnsureSchema(ctx context.Context, database *sql.DB) error {
	const schema = `
CREATE TABLE IF NOT EXISTS schedule_projections (
    id text PRIMARY KEY,
    user_id text NOT NULL,
    start_at timestamptz NOT NULL,
    end_at timestamptz NOT NULL,
    availability text NOT NULL,
    interruptibility text NOT NULL,
    requestability text NOT NULL,
    reschedulability text NOT NULL,
    expected_response_bucket text NOT NULL,
    generated_at timestamptz NOT NULL,
    expires_at timestamptz NOT NULL
);
CREATE INDEX IF NOT EXISTS schedule_projections_user_range_idx
    ON schedule_projections(user_id, start_at, end_at);
`
	if _, err := database.ExecContext(ctx, schema); err != nil {
		return fmt.Errorf("create schedule projection schema: %w", err)
	}
	return nil
}

func (store *PostgresStore) GetView(ctx context.Context, userID, timezone string, from, to time.Time) (View, error) {
	values, err := store.List(ctx, userID, from, to)
	if err != nil {
		return View{}, err
	}
	return NewView(userID, timezone, values)
}

func (store *PostgresStore) List(ctx context.Context, userID string, from, to time.Time) ([]ScheduleProjection, error) {
	return store.list(ctx, userID, from, to, false)
}

func (store *PostgresStore) DeleteForUser(ctx context.Context, userID string) error {
	if _, err := store.database.ExecContext(ctx, `DELETE FROM schedule_projections WHERE user_id = $1`, userID); err != nil {
		return fmt.Errorf("delete user projections: %w", err)
	}
	return nil
}

func (store *PostgresStore) ListForUser(ctx context.Context, userID string) ([]ScheduleProjection, error) {
	return store.list(ctx, userID, time.Time{}, time.Time{}, true)
}

func (store *PostgresStore) Replace(ctx context.Context, userID string, from, to time.Time, values []ScheduleProjection) error {
	for _, value := range values {
		if err := value.Validate(); err != nil {
			return fmt.Errorf("validate replacement projection: %w", err)
		}
		if value.UserID != userID {
			return fmt.Errorf("replacement projection user mismatch")
		}
	}
	transaction, err := store.database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin projection replacement: %w", err)
	}
	defer transaction.Rollback()
	if _, err := transaction.ExecContext(ctx, `
DELETE FROM schedule_projections
WHERE user_id = $1 AND start_at < $3 AND end_at > $2
`, userID, from, to); err != nil {
		return fmt.Errorf("clear projection window: %w", err)
	}
	for _, value := range values {
		_, err := transaction.ExecContext(ctx, `
INSERT INTO schedule_projections (
    id, user_id, start_at, end_at, availability, interruptibility,
    requestability, reschedulability, expected_response_bucket,
    generated_at, expires_at
) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)
`, value.ID, value.UserID, value.StartAt, value.EndAt,
			value.State.Availability, value.State.Interruptibility,
			value.State.Requestability, value.State.Reschedulability,
			value.ExpectedResponseBucket, value.GeneratedAt, value.ExpiresAt)
		if err != nil {
			return fmt.Errorf("insert replacement projection: %w", err)
		}
	}
	if err := transaction.Commit(); err != nil {
		return fmt.Errorf("commit projection replacement: %w", err)
	}
	return nil
}

func (store *PostgresStore) list(ctx context.Context, userID string, from, to time.Time, includeAll bool) ([]ScheduleProjection, error) {
	rows, err := store.database.QueryContext(ctx, `
SELECT id, user_id, start_at, end_at, availability, interruptibility,
       requestability, reschedulability, expected_response_bucket,
       generated_at, expires_at
FROM schedule_projections
WHERE user_id = $1 AND ($4 OR (start_at < $3 AND end_at > $2)) AND expires_at > NOW()
ORDER BY start_at, id
`, userID, from, to, includeAll)
	if err != nil {
		return nil, fmt.Errorf("list schedule projections: %w", err)
	}
	defer rows.Close()
	var values []ScheduleProjection
	for rows.Next() {
		var value ScheduleProjection
		if err := rows.Scan(
			&value.ID, &value.UserID, &value.StartAt, &value.EndAt,
			&value.State.Availability, &value.State.Interruptibility,
			&value.State.Requestability, &value.State.Reschedulability,
			&value.ExpectedResponseBucket, &value.GeneratedAt, &value.ExpiresAt,
		); err != nil {
			return nil, fmt.Errorf("scan schedule projection: %w", err)
		}
		value = normalizeTimestamps(value)
		values = append(values, value)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate schedule projections: %w", err)
	}
	return values, nil
}

func normalizeTimestamps(value ScheduleProjection) ScheduleProjection {
	value.StartAt = value.StartAt.UTC()
	value.EndAt = value.EndAt.UTC()
	value.GeneratedAt = value.GeneratedAt.UTC()
	value.ExpiresAt = value.ExpiresAt.UTC()
	return value
}

func SeedDemo(ctx context.Context, database *sql.DB, now time.Time) error {
	location, err := time.LoadLocation("Asia/Tokyo")
	if err != nil {
		return err
	}
	local := now.In(location)
	dayStart := time.Date(local.Year(), local.Month(), local.Day(), 9, 0, 0, 0, location).UTC()
	states := []struct {
		start, end time.Duration
		value      ScheduleProjection
	}{
		{0, time.Hour, demoProjection("available", "open", "open", "high", "within_window")},
		{time.Hour, 150 * time.Minute, demoProjection("limited", "urgent_only", "later", "low", "unknown")},
		{150 * time.Minute, 4 * time.Hour, demoProjection("limited", "do_not_interrupt", "later", "fixed", "unknown")},
		{4 * time.Hour, 390 * time.Minute, demoProjection("unavailable", "do_not_interrupt", "closed", "fixed", "unknown")},
		{390 * time.Minute, 9 * time.Hour, demoProjection("available", "normal", "open", "medium", "within_window")},
	}
	for index, item := range states {
		value := item.value
		value.ID = fmt.Sprintf("demo-manager:%s:%d", local.Format("2006-01-02"), index)
		value.UserID = "demo-manager"
		value.StartAt = dayStart.Add(item.start)
		value.EndAt = dayStart.Add(item.end)
		value.GeneratedAt = now.UTC()
		value.ExpiresAt = now.UTC().Add(48 * time.Hour)
		_, err := database.ExecContext(ctx, `
INSERT INTO schedule_projections (
    id, user_id, start_at, end_at, availability, interruptibility,
    requestability, reschedulability, expected_response_bucket,
    generated_at, expires_at
) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)
ON CONFLICT (id) DO UPDATE SET
    availability = EXCLUDED.availability,
    interruptibility = EXCLUDED.interruptibility,
    requestability = EXCLUDED.requestability,
    reschedulability = EXCLUDED.reschedulability,
    expected_response_bucket = EXCLUDED.expected_response_bucket,
    generated_at = EXCLUDED.generated_at,
    expires_at = EXCLUDED.expires_at
`, value.ID, value.UserID, value.StartAt, value.EndAt,
			value.State.Availability, value.State.Interruptibility,
			value.State.Requestability, value.State.Reschedulability,
			value.ExpectedResponseBucket, value.GeneratedAt, value.ExpiresAt)
		if err != nil {
			return fmt.Errorf("seed demo projection: %w", err)
		}
	}
	return nil
}

func demoProjection(availability, interruptibility, requestability, reschedulability, response string) ScheduleProjection {
	return ScheduleProjection{
		State: policy.InteractionState{
			Availability:     policy.Availability(availability),
			Interruptibility: policy.Interruptibility(interruptibility),
			Requestability:   policy.Requestability(requestability),
			Reschedulability: policy.Reschedulability(reschedulability),
		},
		ExpectedResponseBucket: response,
	}
}
