package calendar

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

var ErrNotFound = errors.New("calendar record not found")

type Store interface {
	CreateFlow(context.Context, Flow) error
	ConsumeFlow(context.Context, string, string, []byte, time.Time) (Flow, error)
	SaveConnection(context.Context, Connection) error
	GetConnection(context.Context, string) (Connection, error)
	ReplaceBusySpans(context.Context, string, []BusySpan, time.Time, time.Time, time.Time) error
	MarkSynced(context.Context, string, time.Time) error
	MarkReconnectRequired(context.Context, string) error
	DeleteConnection(context.Context, string) error
}

type PostgresStore struct{ database *sql.DB }

func NewPostgresStore(database *sql.DB) *PostgresStore { return &PostgresStore{database: database} }

func EnsureSchema(ctx context.Context, database *sql.DB) error {
	const schema = `
CREATE TABLE IF NOT EXISTS calendar_oauth_flows (
    id text PRIMARY KEY,
    user_id text NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    state_hash bytea NOT NULL,
    code_verifier text NOT NULL,
    expires_at timestamptz NOT NULL,
    created_at timestamptz NOT NULL
);
CREATE TABLE IF NOT EXISTS calendar_connections (
    user_id text PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    refresh_token_cipher bytea NOT NULL,
    granted_scopes text NOT NULL,
    connected_at timestamptz NOT NULL,
    last_synced_at timestamptz,
    reconnect_required boolean NOT NULL DEFAULT false
);
CREATE TABLE IF NOT EXISTS private_events (
    user_id text NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    provider_event_id text NOT NULL,
    calendar_id text NOT NULL,
    start_at timestamptz NOT NULL,
    end_at timestamptz NOT NULL,
    busy_status text NOT NULL,
    visibility text NOT NULL,
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL,
    PRIMARY KEY (user_id, provider_event_id)
);
CREATE INDEX IF NOT EXISTS private_events_user_range_idx ON private_events(user_id, start_at, end_at);
`
	if _, err := database.ExecContext(ctx, schema); err != nil {
		return fmt.Errorf("create calendar schema: %w", err)
	}
	return nil
}

func (store *PostgresStore) CreateFlow(ctx context.Context, value Flow) error {
	_, err := store.database.ExecContext(ctx, `INSERT INTO calendar_oauth_flows
(id,user_id,state_hash,code_verifier,expires_at,created_at) VALUES ($1,$2,$3,$4,$5,$6)`,
		value.ID, value.UserID, value.StateHash, value.CodeVerifier, value.ExpiresAt, value.CreatedAt)
	if err != nil {
		return fmt.Errorf("create calendar oauth flow: %w", err)
	}
	return nil
}

func (store *PostgresStore) ConsumeFlow(ctx context.Context, id, userID string, stateHash []byte, now time.Time) (Flow, error) {
	var value Flow
	err := store.database.QueryRowContext(ctx, `DELETE FROM calendar_oauth_flows
WHERE id=$1 AND user_id=$2 AND state_hash=$3 AND expires_at>$4
RETURNING id,user_id,state_hash,code_verifier,expires_at,created_at`,
		id, userID, stateHash, now).Scan(&value.ID, &value.UserID, &value.StateHash, &value.CodeVerifier, &value.ExpiresAt, &value.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return Flow{}, ErrNotFound
	}
	if err != nil {
		return Flow{}, fmt.Errorf("consume calendar oauth flow: %w", err)
	}
	return value, nil
}

func (store *PostgresStore) SaveConnection(ctx context.Context, value Connection) error {
	_, err := store.database.ExecContext(ctx, `INSERT INTO calendar_connections
(user_id,refresh_token_cipher,granted_scopes,connected_at,last_synced_at,reconnect_required,next_attempt_at,last_error_code,failure_count,sync_token)
VALUES ($1,$2,$3,$4,$5,$6,$4,'',0,'')
ON CONFLICT (user_id) DO UPDATE SET refresh_token_cipher=EXCLUDED.refresh_token_cipher,
granted_scopes=EXCLUDED.granted_scopes, connected_at=EXCLUDED.connected_at,
reconnect_required=false,next_attempt_at=EXCLUDED.connected_at,last_error_code='',failure_count=0,sync_token=''`, value.UserID, value.RefreshTokenCipher, strings.Join(value.GrantedScopes, " "), value.ConnectedAt, value.LastSyncedAt, value.ReconnectRequired)
	if err != nil {
		return fmt.Errorf("save calendar connection: %w", err)
	}
	return nil
}

func (store *PostgresStore) GetConnection(ctx context.Context, userID string) (Connection, error) {
	var value Connection
	var scopes string
	err := store.database.QueryRowContext(ctx, `SELECT user_id,refresh_token_cipher,granted_scopes,
connected_at,last_synced_at,last_attempt_at,next_attempt_at,last_error_code,failure_count,sync_token,reconnect_required
FROM calendar_connections WHERE user_id=$1`, userID).
		Scan(&value.UserID, &value.RefreshTokenCipher, &scopes, &value.ConnectedAt, &value.LastSyncedAt,
			&value.LastAttemptAt, &value.NextAttemptAt, &value.LastErrorCode, &value.FailureCount,
			&value.SyncToken, &value.ReconnectRequired)
	if errors.Is(err, sql.ErrNoRows) {
		return Connection{}, ErrNotFound
	}
	if err != nil {
		return Connection{}, fmt.Errorf("get calendar connection: %w", err)
	}
	value.GrantedScopes = strings.Fields(scopes)
	normalizeConnectionTimes(&value)
	return value, nil
}

func (store *PostgresStore) ReplaceBusySpans(ctx context.Context, userID string, spans []BusySpan, from, to, now time.Time) error {
	tx, err := store.database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin calendar sync: %w", err)
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `DELETE FROM private_events WHERE user_id=$1 AND start_at<$3 AND end_at>$2`, userID, from, to); err != nil {
		return fmt.Errorf("clear calendar sync window: %w", err)
	}
	for _, span := range spans {
		status := "busy"
		if !span.Busy {
			status = "free"
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO private_events
(user_id,provider_event_id,calendar_id,start_at,end_at,busy_status,visibility,created_at,updated_at)
VALUES ($1,$2,$3,$4,$5,$6,'default',$7,$7)`, userID, span.ProviderEventID, span.CalendarID, span.StartAt, span.EndAt, status, now)
		if err != nil {
			return fmt.Errorf("insert busy span: %w", err)
		}
	}
	if err = tx.Commit(); err != nil {
		return fmt.Errorf("commit calendar sync: %w", err)
	}
	return nil
}

func (store *PostgresStore) MarkSynced(ctx context.Context, userID string, now time.Time) error {
	if _, err := store.database.ExecContext(ctx, `UPDATE calendar_connections SET last_synced_at=$2,reconnect_required=false WHERE user_id=$1`, userID, now); err != nil {
		return fmt.Errorf("mark calendar sync: %w", err)
	}
	return nil
}

func (store *PostgresStore) MarkReconnectRequired(ctx context.Context, userID string) error {
	if _, err := store.database.ExecContext(ctx, `UPDATE calendar_connections SET reconnect_required=true WHERE user_id=$1`, userID); err != nil {
		return fmt.Errorf("mark calendar reconnect required: %w", err)
	}
	return nil
}

func (store *PostgresStore) DeleteConnection(ctx context.Context, userID string) error {
	tx, err := store.database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin calendar disconnect: %w", err)
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `DELETE FROM private_events WHERE user_id=$1`, userID); err != nil {
		return fmt.Errorf("delete calendar spans: %w", err)
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM calendar_connections WHERE user_id=$1`, userID); err != nil {
		return fmt.Errorf("delete calendar connection: %w", err)
	}
	if err = tx.Commit(); err != nil {
		return fmt.Errorf("commit calendar disconnect: %w", err)
	}
	return nil
}
