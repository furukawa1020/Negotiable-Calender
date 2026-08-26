package organization

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

type Person struct {
	ID          string `json:"id"`
	DisplayName string `json:"displayName"`
	AvatarURL   string `json:"avatarUrl,omitempty"`
	Timezone    string `json:"timezone"`
	Role        Role   `json:"role"`
}

type Store interface {
	ListPeople(context.Context, string) ([]Person, error)
}

type PostgresStore struct {
	database *sql.DB
}

func NewPostgresStore(database *sql.DB) *PostgresStore {
	return &PostgresStore{database: database}
}

func EnsureSchema(ctx context.Context, database *sql.DB) error {
	const schema = `
CREATE TABLE IF NOT EXISTS organizations (
    id text PRIMARY KEY,
    name text NOT NULL,
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL
);
CREATE TABLE IF NOT EXISTS users (
    id text PRIMARY KEY,
    email text NOT NULL UNIQUE,
    display_name text NOT NULL,
    avatar_url text NOT NULL DEFAULT '',
    timezone text NOT NULL,
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL
);
CREATE TABLE IF NOT EXISTS memberships (
    id text PRIMARY KEY,
    organization_id text NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    user_id text NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    role text NOT NULL,
    created_at timestamptz NOT NULL,
    UNIQUE (organization_id, user_id)
);
`
	if _, err := database.ExecContext(ctx, schema); err != nil {
		return fmt.Errorf("create organization schema: %w", err)
	}
	return nil
}

func (store *PostgresStore) ListPeople(ctx context.Context, organizationID string) ([]Person, error) {
	rows, err := store.database.QueryContext(ctx, `
SELECT users.id, users.display_name, users.avatar_url, users.timezone, memberships.role
FROM memberships
JOIN users ON users.id = memberships.user_id
WHERE memberships.organization_id = $1
  AND memberships.role IN ('OWNER', 'ADMIN', 'MANAGER')
ORDER BY users.display_name, users.id
`, organizationID)
	if err != nil {
		return nil, fmt.Errorf("list organization people: %w", err)
	}
	defer rows.Close()
	people := []Person{}
	for rows.Next() {
		var person Person
		if err := rows.Scan(&person.ID, &person.DisplayName, &person.AvatarURL, &person.Timezone, &person.Role); err != nil {
			return nil, fmt.Errorf("scan organization person: %w", err)
		}
		people = append(people, person)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate organization people: %w", err)
	}
	return people, nil
}

func SeedDemo(ctx context.Context, database *sql.DB, now time.Time) error {
	statements := []struct {
		query string
		args  []any
	}{
		{`INSERT INTO organizations (id, name, created_at, updated_at)
VALUES ($1,$2,$3,$3) ON CONFLICT (id) DO NOTHING`, []any{"demo-org", "Product Studio", now}},
		{`INSERT INTO users (id, email, display_name, avatar_url, timezone, created_at, updated_at)
VALUES ($1,$2,$3,'',$4,$5,$5) ON CONFLICT (id) DO NOTHING`,
			[]any{"demo-manager", "manager@example.invalid", "山田 太郎", "Asia/Tokyo", now}},
		{`INSERT INTO users (id, email, display_name, avatar_url, timezone, created_at, updated_at)
VALUES ($1,$2,$3,'',$4,$5,$5) ON CONFLICT (id) DO NOTHING`,
			[]any{"demo-member", "member@example.invalid", "佐藤 花子", "Asia/Tokyo", now}},
		{`INSERT INTO memberships (id, organization_id, user_id, role, created_at)
VALUES ($1,$2,$3,$4,$5) ON CONFLICT (organization_id, user_id) DO NOTHING`,
			[]any{"demo-membership-manager", "demo-org", "demo-manager", Manager, now}},
		{`INSERT INTO memberships (id, organization_id, user_id, role, created_at)
VALUES ($1,$2,$3,$4,$5) ON CONFLICT (organization_id, user_id) DO NOTHING`,
			[]any{"demo-membership-member", "demo-org", "demo-member", Member, now}},
	}
	for _, statement := range statements {
		if _, err := database.ExecContext(ctx, statement.query, statement.args...); err != nil {
			return fmt.Errorf("seed demo organization: %w", err)
		}
	}
	return nil
}
