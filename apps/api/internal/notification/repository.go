package notification

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

type Store interface {
	Create(context.Context, Notification) error
	List(context.Context, string) ([]Notification, error)
	MarkRead(context.Context, string, string, time.Time) (bool, error)
}

type PostgresStore struct{ database *sql.DB }

func NewPostgresStore(database *sql.DB) *PostgresStore {
	return &PostgresStore{database: database}
}

func EnsureSchema(ctx context.Context, database *sql.DB) error {
	_, err := database.ExecContext(ctx, `
CREATE TABLE IF NOT EXISTS notifications (
    id text PRIMARY KEY,
    user_id text NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    type text NOT NULL,
    request_id text NOT NULL REFERENCES coordination_requests(id) ON DELETE CASCADE,
    message text NOT NULL,
    read_at timestamptz,
    created_at timestamptz NOT NULL
);
CREATE INDEX IF NOT EXISTS notifications_user_created_idx
    ON notifications(user_id, created_at DESC, id DESC);
`)
	if err != nil {
		return fmt.Errorf("create notification schema: %w", err)
	}
	return nil
}

func (store *PostgresStore) Create(ctx context.Context, value Notification) error {
	_, err := store.database.ExecContext(ctx, `
INSERT INTO notifications (id, user_id, type, request_id, message, read_at, created_at)
VALUES ($1,$2,$3,$4,$5,$6,$7)
`, value.ID, value.UserID, value.Type, value.RequestID, value.Message, value.ReadAt, value.CreatedAt)
	if err != nil {
		return fmt.Errorf("create notification: %w", err)
	}
	return nil
}

func (store *PostgresStore) List(ctx context.Context, userID string) ([]Notification, error) {
	rows, err := store.database.QueryContext(ctx, `
SELECT id, user_id, type, request_id, message, read_at, created_at
FROM notifications WHERE user_id = $1
ORDER BY created_at DESC, id DESC LIMIT 100
`, userID)
	if err != nil {
		return nil, fmt.Errorf("list notifications: %w", err)
	}
	defer rows.Close()
	values := []Notification{}
	for rows.Next() {
		var value Notification
		var readAt sql.NullTime
		if err := rows.Scan(&value.ID, &value.UserID, &value.Type, &value.RequestID, &value.Message, &readAt, &value.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan notification: %w", err)
		}
		value.CreatedAt = value.CreatedAt.UTC()
		if readAt.Valid {
			normalized := readAt.Time.UTC()
			value.ReadAt = &normalized
		}
		values = append(values, value)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate notifications: %w", err)
	}
	return values, nil
}

func (store *PostgresStore) MarkRead(ctx context.Context, id, userID string, now time.Time) (bool, error) {
	result, err := store.database.ExecContext(ctx, `
UPDATE notifications SET read_at = COALESCE(read_at, $1)
WHERE id = $2 AND user_id = $3
`, now, id, userID)
	if err != nil {
		return false, fmt.Errorf("mark notification read: %w", err)
	}
	count, err := result.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("count notification update: %w", err)
	}
	return count == 1, nil
}
