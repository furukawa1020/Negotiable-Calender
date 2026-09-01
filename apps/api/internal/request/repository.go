package request

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

var ErrNotFound = fmt.Errorf("coordination request not found")

type Store interface {
	Create(context.Context, CoordinationRequest) error
	ListForTarget(context.Context, string) ([]CoordinationRequest, error)
	ListForUser(context.Context, string) ([]CoordinationRequest, error)
	Respond(context.Context, string, string, Status, string) error
	Suggest(context.Context, string, string, Option) error
	Delegate(context.Context, string, string, Option) error
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
ALTER TABLE coordination_requests ADD COLUMN IF NOT EXISTS accepted_option_id text;
ALTER TABLE coordination_requests ADD COLUMN IF NOT EXISTS delegated_user_id text;
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

func (store *PostgresStore) ListForTarget(ctx context.Context, targetUserID string) ([]CoordinationRequest, error) {
	return store.listForUser(ctx, targetUserID, false)
}

func (store *PostgresStore) ListForUser(ctx context.Context, userID string) ([]CoordinationRequest, error) {
	return store.listForUser(ctx, userID, true)
}

func (store *PostgresStore) listForUser(ctx context.Context, userID string, includeRequested bool) ([]CoordinationRequest, error) {
	rows, err := store.database.QueryContext(ctx, `
SELECT id, organization_id, requester_user_id, target_user_id, type, title,
       duration_minutes, deadline_at, sync_preference, priority, status,
       created_at, updated_at, accepted_option_id, delegated_user_id
FROM coordination_requests
WHERE target_user_id = $1 OR ($2 AND requester_user_id = $1)
ORDER BY created_at DESC, id DESC
`, userID, includeRequested)
	if err != nil {
		return nil, fmt.Errorf("list coordination requests: %w", err)
	}
	defer rows.Close()
	values := make([]CoordinationRequest, 0)
	for rows.Next() {
		var value CoordinationRequest
		var acceptedOptionID, delegatedUserID sql.NullString
		if err := rows.Scan(
			&value.ID, &value.OrganizationID, &value.RequesterUserID, &value.TargetUserID,
			&value.Type, &value.Title, &value.DurationMinutes, &value.DeadlineAt,
			&value.SyncPreference, &value.Priority, &value.Status,
			&value.CreatedAt, &value.UpdatedAt, &acceptedOptionID, &delegatedUserID,
		); err != nil {
			return nil, fmt.Errorf("scan coordination request: %w", err)
		}
		value.DeadlineAt = value.DeadlineAt.UTC()
		value.CreatedAt = value.CreatedAt.UTC()
		value.UpdatedAt = value.UpdatedAt.UTC()
		value.AcceptedOptionID = acceptedOptionID.String
		value.DelegatedUserID = delegatedUserID.String
		value.Options = []Option{}
		values = append(values, value)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate coordination requests: %w", err)
	}
	if err := rows.Close(); err != nil {
		return nil, fmt.Errorf("close coordination request rows: %w", err)
	}
	for index := range values {
		options, err := store.listOptions(ctx, values[index].ID)
		if err != nil {
			return nil, err
		}
		values[index].Options = options
	}
	return values, nil
}

func (store *PostgresStore) Respond(ctx context.Context, requestID, targetUserID string, status Status, optionID string) error {
	if status != Accepted && status != Declined {
		return fmt.Errorf("unsupported response status")
	}
	var result sql.Result
	var err error
	if status == Accepted {
		result, err = store.database.ExecContext(ctx, `
UPDATE coordination_requests
SET status = $1, accepted_option_id = $2, updated_at = $3
WHERE id = $4 AND target_user_id = $5 AND status = $6
  AND EXISTS (
    SELECT 1 FROM coordination_request_options
    WHERE id = $2 AND request_id = coordination_requests.id
  )
`, status, optionID, time.Now().UTC(), requestID, targetUserID, Suggested)
	} else {
		result, err = store.database.ExecContext(ctx, `
UPDATE coordination_requests
SET status = $1, accepted_option_id = NULL, updated_at = $2
WHERE id = $3 AND target_user_id = $4 AND status = $5
`, status, time.Now().UTC(), requestID, targetUserID, Suggested)
	}
	if err != nil {
		return fmt.Errorf("respond to coordination request: %w", err)
	}
	updated, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("count coordination request response: %w", err)
	}
	if updated == 0 {
		return ErrNotFound
	}
	return nil
}

func (store *PostgresStore) Suggest(ctx context.Context, requestID, targetUserID string, option Option) error {
	if option.RequestID != requestID || option.Type != OptionMeeting {
		return fmt.Errorf("invalid suggested option")
	}
	if err := option.Validate(); err != nil {
		return err
	}
	transaction, err := store.database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin suggested option: %w", err)
	}
	defer transaction.Rollback()
	result, err := transaction.ExecContext(ctx, `
UPDATE coordination_requests
SET updated_at = $1
WHERE id = $2 AND target_user_id = $3 AND status = $4
`, time.Now().UTC(), requestID, targetUserID, Suggested)
	if err != nil {
		return fmt.Errorf("authorize suggested option: %w", err)
	}
	updated, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("count suggested option request: %w", err)
	}
	if updated == 0 {
		return ErrNotFound
	}
	_, err = transaction.ExecContext(ctx, `
INSERT INTO coordination_request_options (
    id, request_id, type, start_at, end_at, response_by,
    delegate_user_id, created_at
) VALUES ($1,$2,$3,$4,$5,$6,$7,$8)
`, option.ID, option.RequestID, option.Type, option.StartAt, option.EndAt,
		option.ResponseBy, nullableString(option.DelegateUserID), option.CreatedAt)
	if err != nil {
		return fmt.Errorf("create suggested option: %w", err)
	}
	if err := transaction.Commit(); err != nil {
		return fmt.Errorf("commit suggested option: %w", err)
	}
	return nil
}

func (store *PostgresStore) Delegate(ctx context.Context, requestID, targetUserID string, option Option) error {
	if option.RequestID != requestID || option.Type != OptionDelegate {
		return fmt.Errorf("invalid delegate option")
	}
	if err := option.Validate(); err != nil {
		return err
	}
	transaction, err := store.database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin request delegation: %w", err)
	}
	defer transaction.Rollback()
	result, err := transaction.ExecContext(ctx, `
UPDATE coordination_requests
SET status = $1, delegated_user_id = $2, updated_at = $3
WHERE id = $4 AND target_user_id = $5 AND status = $6
  AND $2 <> target_user_id
  AND EXISTS (
    SELECT 1 FROM memberships
    WHERE memberships.organization_id = coordination_requests.organization_id
      AND memberships.user_id = $2
  )
`, Delegated, option.DelegateUserID, time.Now().UTC(), requestID, targetUserID, Suggested)
	if err != nil {
		return fmt.Errorf("delegate coordination request: %w", err)
	}
	updated, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("count delegated request: %w", err)
	}
	if updated == 0 {
		return ErrNotFound
	}
	_, err = transaction.ExecContext(ctx, `
INSERT INTO coordination_request_options (
    id, request_id, type, start_at, end_at, response_by,
    delegate_user_id, created_at
) VALUES ($1,$2,$3,$4,$5,$6,$7,$8)
`, option.ID, option.RequestID, option.Type, option.StartAt, option.EndAt,
		option.ResponseBy, option.DelegateUserID, option.CreatedAt)
	if err != nil {
		return fmt.Errorf("create delegate option: %w", err)
	}
	if err := transaction.Commit(); err != nil {
		return fmt.Errorf("commit request delegation: %w", err)
	}
	return nil
}

func (store *PostgresStore) listOptions(ctx context.Context, requestID string) ([]Option, error) {
	rows, err := store.database.QueryContext(ctx, `
SELECT id, request_id, type, start_at, end_at, response_by, delegate_user_id, created_at
FROM coordination_request_options
WHERE request_id = $1
ORDER BY created_at, id
`, requestID)
	if err != nil {
		return nil, fmt.Errorf("list coordination options: %w", err)
	}
	defer rows.Close()
	options := make([]Option, 0)
	for rows.Next() {
		var option Option
		var startAt, endAt, responseBy sql.NullTime
		var delegateUserID sql.NullString
		if err := rows.Scan(
			&option.ID, &option.RequestID, &option.Type, &startAt, &endAt,
			&responseBy, &delegateUserID, &option.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan coordination option: %w", err)
		}
		option.StartAt = utcPointer(startAt)
		option.EndAt = utcPointer(endAt)
		option.ResponseBy = utcPointer(responseBy)
		option.DelegateUserID = delegateUserID.String
		option.CreatedAt = option.CreatedAt.UTC()
		options = append(options, option)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate coordination options: %w", err)
	}
	return options, nil
}

func utcPointer(value sql.NullTime) *time.Time {
	if !value.Valid {
		return nil
	}
	normalized := value.Time.UTC()
	return &normalized
}

func nullableString(value string) any {
	if value == "" {
		return nil
	}
	return value
}
