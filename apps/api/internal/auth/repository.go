package auth

import (
	"context"
	"crypto/rand"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

type Store interface {
	CreateFlow(context.Context, Flow) error
	ConsumeFlow(context.Context, string, []byte, time.Time) (Flow, error)
	UpsertGoogleIdentity(context.Context, Profile, time.Time) (Identity, error)
	CreateSession(context.Context, Session) error
	GetSession(context.Context, []byte, time.Time) (Identity, error)
	DeleteSession(context.Context, []byte) error
}

type PostgresStore struct{ database *sql.DB }

func NewPostgresStore(database *sql.DB) *PostgresStore { return &PostgresStore{database: database} }

func EnsureSchema(ctx context.Context, database *sql.DB) error {
	const schema = `
CREATE TABLE IF NOT EXISTS auth_identities (
    provider text NOT NULL,
    subject text NOT NULL,
    user_id text NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    email text NOT NULL,
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL,
    PRIMARY KEY (provider, subject)
);
CREATE TABLE IF NOT EXISTS oauth_flows (
    id text PRIMARY KEY,
    state_hash bytea NOT NULL,
    code_verifier text NOT NULL,
    expires_at timestamptz NOT NULL,
    created_at timestamptz NOT NULL
);
CREATE TABLE IF NOT EXISTS auth_sessions (
    token_hash bytea PRIMARY KEY,
    user_id text NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    organization_id text NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    expires_at timestamptz NOT NULL,
    created_at timestamptz NOT NULL
);
CREATE INDEX IF NOT EXISTS auth_sessions_expiry_idx ON auth_sessions(expires_at);
`
	if _, err := database.ExecContext(ctx, schema); err != nil {
		return fmt.Errorf("create auth schema: %w", err)
	}
	return nil
}

func (store *PostgresStore) CreateFlow(ctx context.Context, value Flow) error {
	_, err := store.database.ExecContext(ctx, `
INSERT INTO oauth_flows (id, state_hash, code_verifier, expires_at, created_at)
VALUES ($1,$2,$3,$4,$5)
`, value.ID, value.StateHash, value.CodeVerifier, value.ExpiresAt, value.CreatedAt)
	if err != nil {
		return fmt.Errorf("create oauth flow: %w", err)
	}
	return nil
}

func (store *PostgresStore) ConsumeFlow(ctx context.Context, id string, stateHash []byte, now time.Time) (Flow, error) {
	var value Flow
	err := store.database.QueryRowContext(ctx, `
DELETE FROM oauth_flows
WHERE id = $1 AND state_hash = $2 AND expires_at > $3
RETURNING id, state_hash, code_verifier, expires_at, created_at
`, id, stateHash, now).Scan(&value.ID, &value.StateHash, &value.CodeVerifier, &value.ExpiresAt, &value.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return Flow{}, ErrNotFound
	}
	if err != nil {
		return Flow{}, fmt.Errorf("consume oauth flow: %w", err)
	}
	return value, nil
}

func (store *PostgresStore) UpsertGoogleIdentity(ctx context.Context, profile Profile, now time.Time) (Identity, error) {
	transaction, err := store.database.BeginTx(ctx, nil)
	if err != nil {
		return Identity{}, fmt.Errorf("begin google identity upsert: %w", err)
	}
	defer transaction.Rollback()

	var userID string
	err = transaction.QueryRowContext(ctx, `SELECT user_id FROM auth_identities WHERE provider = 'google' AND subject = $1`, profile.Subject).Scan(&userID)
	if errors.Is(err, sql.ErrNoRows) {
		userID = newID("user")
		_, err = transaction.ExecContext(ctx, `
INSERT INTO users (id, email, display_name, avatar_url, timezone, created_at, updated_at)
VALUES ($1,$2,$3,$4,'Asia/Tokyo',$5,$5)
ON CONFLICT (email) DO UPDATE SET display_name = EXCLUDED.display_name, avatar_url = EXCLUDED.avatar_url, updated_at = EXCLUDED.updated_at
`, userID, profile.Email, displayName(profile), profile.AvatarURL, now)
		if err != nil {
			return Identity{}, fmt.Errorf("upsert google user: %w", err)
		}
		if err := transaction.QueryRowContext(ctx, `SELECT id FROM users WHERE email = $1`, profile.Email).Scan(&userID); err != nil {
			return Identity{}, fmt.Errorf("resolve google user: %w", err)
		}
		_, err = transaction.ExecContext(ctx, `
INSERT INTO auth_identities (provider, subject, user_id, email, created_at, updated_at)
VALUES ('google',$1,$2,$3,$4,$4)
`, profile.Subject, userID, profile.Email, now)
	} else if err == nil {
		_, err = transaction.ExecContext(ctx, `
UPDATE users SET email = $1, display_name = $2, avatar_url = $3, updated_at = $4 WHERE id = $5
`, profile.Email, displayName(profile), profile.AvatarURL, now, userID)
		if err == nil {
			_, err = transaction.ExecContext(ctx, `UPDATE auth_identities SET email = $1, updated_at = $2 WHERE provider = 'google' AND subject = $3`, profile.Email, now, profile.Subject)
		}
	}
	if err != nil {
		return Identity{}, fmt.Errorf("persist google identity: %w", err)
	}

	identity := Identity{UserID: userID, Email: profile.Email, DisplayName: displayName(profile), AvatarURL: profile.AvatarURL}
	err = transaction.QueryRowContext(ctx, `
SELECT memberships.organization_id, memberships.role
FROM memberships WHERE user_id = $1 ORDER BY memberships.created_at LIMIT 1
`, userID).Scan(&identity.OrganizationID, &identity.Role)
	if errors.Is(err, sql.ErrNoRows) {
		identity.OrganizationID = newID("organization")
		identity.Role = "OWNER"
		_, err = transaction.ExecContext(ctx, `INSERT INTO organizations (id, name, created_at, updated_at) VALUES ($1,$2,$3,$3)`, identity.OrganizationID, identity.DisplayName+" Workspace", now)
		if err == nil {
			_, err = transaction.ExecContext(ctx, `INSERT INTO memberships (id, organization_id, user_id, role, created_at) VALUES ($1,$2,$3,$4,$5)`, newID("membership"), identity.OrganizationID, userID, identity.Role, now)
		}
	}
	if err != nil {
		return Identity{}, fmt.Errorf("provision google organization: %w", err)
	}
	if err := transaction.Commit(); err != nil {
		return Identity{}, fmt.Errorf("commit google identity: %w", err)
	}
	return identity, nil
}

func (store *PostgresStore) CreateSession(ctx context.Context, value Session) error {
	_, err := store.database.ExecContext(ctx, `
INSERT INTO auth_sessions (token_hash, user_id, organization_id, expires_at, created_at)
VALUES ($1,$2,$3,$4,$5)
`, value.TokenHash, value.UserID, value.OrganizationID, value.ExpiresAt, value.CreatedAt)
	if err != nil {
		return fmt.Errorf("create auth session: %w", err)
	}
	return nil
}

func (store *PostgresStore) GetSession(ctx context.Context, tokenHash []byte, now time.Time) (Identity, error) {
	var value Identity
	err := store.database.QueryRowContext(ctx, `
SELECT users.id, auth_sessions.organization_id, users.email, users.display_name, users.avatar_url, memberships.role
FROM auth_sessions
JOIN users ON users.id = auth_sessions.user_id
JOIN memberships ON memberships.user_id = auth_sessions.user_id AND memberships.organization_id = auth_sessions.organization_id
WHERE auth_sessions.token_hash = $1 AND auth_sessions.expires_at > $2
`, tokenHash, now).Scan(&value.UserID, &value.OrganizationID, &value.Email, &value.DisplayName, &value.AvatarURL, &value.Role)
	if errors.Is(err, sql.ErrNoRows) {
		return Identity{}, ErrNotFound
	}
	if err != nil {
		return Identity{}, fmt.Errorf("get auth session: %w", err)
	}
	return value, nil
}

func (store *PostgresStore) DeleteSession(ctx context.Context, tokenHash []byte) error {
	if _, err := store.database.ExecContext(ctx, `DELETE FROM auth_sessions WHERE token_hash = $1`, tokenHash); err != nil {
		return fmt.Errorf("delete auth session: %w", err)
	}
	return nil
}

func newID(prefix string) string {
	value := make([]byte, 16)
	if _, err := rand.Read(value); err != nil {
		panic("secure random source unavailable")
	}
	return fmt.Sprintf("%s-%x", prefix, value)
}

func displayName(profile Profile) string {
	if profile.DisplayName != "" {
		return profile.DisplayName
	}
	return profile.Email
}
