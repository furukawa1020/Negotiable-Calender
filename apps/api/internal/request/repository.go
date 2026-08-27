package request

import (
	"context"
	"database/sql"
	"fmt"
)

type Store interface {
	Create(context.Context, CoordinationRequest) error
}

type PostgresStore struct {
	database *sql.DB
}

func NewPostgresStore(database *sql.DB) *PostgresStore {
	return &PostgresStore{database: database}
}

func EnsureSchema(ctx context.Context, database *sql.DB) error {
	const schema = `
CREATE TABLE IF NOT EXISTS coordination_requests (
    id text PRIMARY KEY,
    organization_id text NOT NULL REFERENCES organizations(id),
    requester_user_id text NOT NULL REFERENCES users(id),
    target_user_id text NOT NULL REFERENCES users(id),
    type text NOT NULL,
    title text NOT NULL,
    duration_minutes integer NOT NULL,
    deadline_at timestamptz NOT NULL,
    sync_preference text NOT NULL,
    priority text NOT NULL,
    status text NOT NULL,
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL
);
CREATE INDEX IF NOT EXISTS coordination_requests_target_status_idx
    ON coordination_requests(target_user_id, status, created_at DESC);
CREATE INDEX IF NOT EXISTS coordination_requests_requester_idx
    ON coordination_requests(requester_user_id, created_at DESC);
CREATE TABLE IF NOT EXISTS coordination_request_options (
    id text PRIMARY KEY,
    request_id text NOT NULL REFERENCES coordination_requests(id) ON DELETE CASCADE,
    type text NOT NULL,
    start_at timestamptz,
    end_at timestamptz,
    response_by timestamptz,
    delegate_user_id text,
    created_at timestamptz NOT NULL
);
CREATE INDEX IF NOT EXISTS coordination_request_options_request_idx
    ON coordination_request_options(request_id, created_at, id);
`
	if _, err := database.ExecContext(ctx, schema); err != nil {
		return fmt.Errorf("create coordination request schema: %w", err)
	}
	return nil
}

func (store *PostgresStore) Create(ctx context.Context, value CoordinationRequest) error {
	if err := value.Validate(); err != nil {
		return err
	}
	transaction, err := store.database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin coordination request: %w", err)
	}
	defer transaction.Rollback()
	_, err = transaction.ExecContext(ctx, `
INSERT INTO coordination_requests (
    id, organization_id, requester_user_id, target_user_id, type, title,
    duration_minutes, deadline_at, sync_preference, priority, status,
    created_at, updated_at
) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)
`, value.ID, value.OrganizationID, value.RequesterUserID, value.TargetUserID,
		value.Type, value.Title, value.DurationMinutes, value.DeadlineAt,
		value.SyncPreference, value.Priority, value.Status, value.CreatedAt, value.UpdatedAt)
	if err != nil {
		return fmt.Errorf("create coordination request: %w", err)
	}
	for _, option := range value.Options {
		_, err := transaction.ExecContext(ctx, `
INSERT INTO coordination_request_options (
    id, request_id, type, start_at, end_at, response_by,
    delegate_user_id, created_at
) VALUES ($1,$2,$3,$4,$5,$6,$7,$8)
`, option.ID, option.RequestID, option.Type, option.StartAt, option.EndAt,
			option.ResponseBy, nullableString(option.DelegateUserID), option.CreatedAt)
		if err != nil {
			return fmt.Errorf("create coordination option: %w", err)
		}
	}
	if err := transaction.Commit(); err != nil {
		return fmt.Errorf("commit coordination request: %w", err)
	}
	return nil
}

func nullableString(value string) any {
	if value == "" {
		return nil
	}
	return value
}
