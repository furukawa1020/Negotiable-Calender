package calendar

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"
)

const (
	defaultSyncInterval = 15 * time.Minute
	defaultClaimLease   = 2 * time.Minute
)

type BackgroundStore interface {
	ClaimDueConnections(context.Context, time.Time, int, time.Duration) ([]Connection, error)
	ApplyChanges(context.Context, string, ChangeSet, time.Time, time.Time, time.Time) error
	MarkSyncSuccess(context.Context, string, string, time.Time, time.Time) error
	MarkSyncFailure(context.Context, string, string, time.Time, bool) error
}

func EnsureBackgroundSchema(ctx context.Context, database *sql.DB) error {
	const schema = `
ALTER TABLE calendar_connections ADD COLUMN IF NOT EXISTS sync_token text NOT NULL DEFAULT '';
ALTER TABLE calendar_connections ADD COLUMN IF NOT EXISTS last_attempt_at timestamptz;
ALTER TABLE calendar_connections ADD COLUMN IF NOT EXISTS next_attempt_at timestamptz;
ALTER TABLE calendar_connections ADD COLUMN IF NOT EXISTS last_error_code text NOT NULL DEFAULT '';
ALTER TABLE calendar_connections ADD COLUMN IF NOT EXISTS failure_count integer NOT NULL DEFAULT 0;
CREATE INDEX IF NOT EXISTS calendar_connections_due_idx
ON calendar_connections(next_attempt_at) WHERE reconnect_required=false;
`
	if _, err := database.ExecContext(ctx, schema); err != nil {
		return fmt.Errorf("create background calendar schema: %w", err)
	}
	_, err := database.ExecContext(ctx, `
UPDATE calendar_connections
SET next_attempt_at=COALESCE(next_attempt_at,last_synced_at,connected_at)
WHERE next_attempt_at IS NULL AND reconnect_required=false
`)
	if err != nil {
		return fmt.Errorf("schedule existing calendar connections: %w", err)
	}
	return nil
}

func (store *PostgresStore) ClaimDueConnections(ctx context.Context, now time.Time, limit int, lease time.Duration) ([]Connection, error) {
	if limit <= 0 {
		limit = 10
	}
	if lease <= 0 {
		lease = defaultClaimLease
	}
	rows, err := store.database.QueryContext(ctx, `
WITH due AS (
	SELECT user_id
	FROM calendar_connections
	WHERE reconnect_required=false
	  AND (next_attempt_at IS NULL OR next_attempt_at <= $1)
	ORDER BY next_attempt_at NULLS FIRST, user_id
	FOR UPDATE SKIP LOCKED
	LIMIT $2
)
UPDATE calendar_connections AS connection
SET last_attempt_at=$1,
    next_attempt_at=$1 + ($3 * interval '1 second')
FROM due
WHERE connection.user_id=due.user_id
RETURNING connection.user_id,connection.refresh_token_cipher,connection.granted_scopes,
          connection.connected_at,connection.last_synced_at,connection.last_attempt_at,
          connection.next_attempt_at,connection.last_error_code,connection.failure_count,
          connection.sync_token,connection.reconnect_required
`, now.UTC(), limit, lease.Seconds())
	if err != nil {
		return nil, fmt.Errorf("claim due calendar connections: %w", err)
	}
	defer rows.Close()
	var values []Connection
	for rows.Next() {
		value, err := scanBackgroundConnection(rows)
		if err != nil {
			return nil, err
		}
		values = append(values, value)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate due calendar connections: %w", err)
	}
	return values, nil
}

type connectionScanner interface {
	Scan(...any) error
}

func scanBackgroundConnection(scanner connectionScanner) (Connection, error) {
	var value Connection
	var scopes string
	if err := scanner.Scan(
		&value.UserID, &value.RefreshTokenCipher, &scopes, &value.ConnectedAt,
		&value.LastSyncedAt, &value.LastAttemptAt, &value.NextAttemptAt,
		&value.LastErrorCode, &value.FailureCount, &value.SyncToken,
		&value.ReconnectRequired,
	); err != nil {
		return Connection{}, fmt.Errorf("scan calendar connection: %w", err)
	}
	value.GrantedScopes = splitScopes(scopes)
	normalizeConnectionTimes(&value)
	return value, nil
}

func (store *PostgresStore) ApplyChanges(ctx context.Context, userID string, changes ChangeSet, from, to, now time.Time) error {
	tx, err := store.database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin incremental calendar sync: %w", err)
	}
	defer tx.Rollback()

	if changes.Full {
		if _, err := tx.ExecContext(ctx, `DELETE FROM private_events WHERE user_id=$1 AND start_at<$3 AND end_at>$2`, userID, from.UTC(), to.UTC()); err != nil {
			return fmt.Errorf("clear full calendar sync window: %w", err)
		}
	}
	for _, providerEventID := range changes.DeletedProviderEventIDs {
		if providerEventID == "" {
			continue
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM private_events WHERE user_id=$1 AND provider_event_id=$2`, userID, providerEventID); err != nil {
			return fmt.Errorf("delete cancelled calendar instance: %w", err)
		}
	}
	for _, span := range changes.Upserts {
		status := "busy"
		if !span.Busy {
			status = "free"
		}
		if _, err := tx.ExecContext(ctx, `
INSERT INTO private_events
(user_id,provider_event_id,calendar_id,start_at,end_at,busy_status,visibility,created_at,updated_at)
VALUES ($1,$2,$3,$4,$5,$6,'default',$7,$7)
ON CONFLICT (user_id,provider_event_id) DO UPDATE SET
calendar_id=EXCLUDED.calendar_id,start_at=EXCLUDED.start_at,end_at=EXCLUDED.end_at,
busy_status=EXCLUDED.busy_status,visibility=EXCLUDED.visibility,updated_at=EXCLUDED.updated_at
`, userID, span.ProviderEventID, span.CalendarID, span.StartAt.UTC(), span.EndAt.UTC(), status, now.UTC()); err != nil {
			return fmt.Errorf("upsert changed calendar instance: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit incremental calendar sync: %w", err)
	}
	return nil
}

func (store *PostgresStore) MarkSyncSuccess(ctx context.Context, userID, syncToken string, now, next time.Time) error {
	result, err := store.database.ExecContext(ctx, `
UPDATE calendar_connections
SET sync_token=$2,last_synced_at=$3,last_attempt_at=$3,next_attempt_at=$4,
    last_error_code='',failure_count=0,reconnect_required=false
WHERE user_id=$1
`, userID, syncToken, now.UTC(), next.UTC())
	if err != nil {
		return fmt.Errorf("mark background calendar sync success: %w", err)
	}
	if count, countErr := result.RowsAffected(); countErr != nil || count != 1 {
		return fmt.Errorf("mark background calendar sync success: connection not found")
	}
	return nil
}

func (store *PostgresStore) MarkSyncFailure(ctx context.Context, userID, code string, next time.Time, reconnect bool) error {
	if code == "" {
		code = "temporary_failure"
	}
	result, err := store.database.ExecContext(ctx, `
UPDATE calendar_connections
SET next_attempt_at=$3,last_error_code=$2,failure_count=failure_count+1,
    reconnect_required=$4
WHERE user_id=$1
`, userID, code, next.UTC(), reconnect)
	if err != nil {
		return fmt.Errorf("mark background calendar sync failure: %w", err)
	}
	if count, countErr := result.RowsAffected(); countErr != nil || count != 1 {
		return fmt.Errorf("mark background calendar sync failure: connection not found")
	}
	return nil
}


func splitScopes(value string) []string {
	return strings.Fields(value)
}

func normalizeConnectionTimes(value *Connection) {
	value.ConnectedAt = value.ConnectedAt.UTC()
	if value.LastSyncedAt != nil {
		normalized := value.LastSyncedAt.UTC()
		value.LastSyncedAt = &normalized
	}
	if value.LastAttemptAt != nil {
		normalized := value.LastAttemptAt.UTC()
		value.LastAttemptAt = &normalized
	}
	if value.NextAttemptAt != nil {
		normalized := value.NextAttemptAt.UTC()
		value.NextAttemptAt = &normalized
	}
}
