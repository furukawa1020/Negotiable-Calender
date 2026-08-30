package audit

import (
	"context"
	"database/sql"
	"fmt"
)

type Store interface {
	Create(context.Context, Event) error
	List(context.Context, string) ([]Event, error)
}

type PostgresStore struct{ database *sql.DB }

func NewPostgresStore(database *sql.DB) *PostgresStore {
	return &PostgresStore{database: database}
}

func EnsureSchema(ctx context.Context, database *sql.DB) error {
	_, err := database.ExecContext(ctx, `
CREATE TABLE IF NOT EXISTS audit_logs (
    id text PRIMARY KEY,
    organization_id text NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    actor_user_id text NOT NULL REFERENCES users(id),
    action text NOT NULL,
    resource_type text NOT NULL,
    resource_id text NOT NULL,
    created_at timestamptz NOT NULL
);
CREATE INDEX IF NOT EXISTS audit_logs_organization_created_idx
    ON audit_logs(organization_id, created_at DESC, id DESC);
`)
	if err != nil {
		return fmt.Errorf("create audit schema: %w", err)
	}
	return nil
}

func (store *PostgresStore) Create(ctx context.Context, value Event) error {
	result, err := store.database.ExecContext(ctx, `
INSERT INTO audit_logs (
    id, organization_id, actor_user_id, action,
    resource_type, resource_id, created_at
)
SELECT $1, coordination_requests.organization_id, $2, $3, $4, $5, $6
FROM coordination_requests
WHERE coordination_requests.id = $5
`, value.ID, value.ActorUserID, value.Action, value.ResourceType, value.ResourceID, value.CreatedAt)
	if err != nil {
		return fmt.Errorf("create audit event: %w", err)
	}
	count, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("count audit event insert: %w", err)
	}
	if count != 1 {
		return fmt.Errorf("audit resource not found")
	}
	return nil
}

func (store *PostgresStore) List(ctx context.Context, organizationID string) ([]Event, error) {
	rows, err := store.database.QueryContext(ctx, `
SELECT id, organization_id, actor_user_id, action, resource_type, resource_id, created_at
FROM audit_logs WHERE organization_id = $1
ORDER BY created_at DESC, id DESC LIMIT 200
`, organizationID)
	if err != nil {
		return nil, fmt.Errorf("list audit events: %w", err)
	}
	defer rows.Close()
	values := []Event{}
	for rows.Next() {
		var value Event
		if err := rows.Scan(&value.ID, &value.OrganizationID, &value.ActorUserID, &value.Action, &value.ResourceType, &value.ResourceID, &value.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan audit event: %w", err)
		}
		value.CreatedAt = value.CreatedAt.UTC()
		values = append(values, value)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate audit events: %w", err)
	}
	return values, nil
}
