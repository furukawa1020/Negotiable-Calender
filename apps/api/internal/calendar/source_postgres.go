package calendar

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"
)

func ReadSourcePostgres(ctx context.Context, tx *sql.Tx, userID string) (SourceState, error) {
	var state SourceState
	var data []byte
	err := tx.QueryRowContext(ctx, "SELECT snapshot,published_revision FROM calendar_source_snapshots WHERE user_id=$1", userID).Scan(&data, &state.PublishedRevision)
	if err == nil {
		state.Managed = true
		if err := json.Unmarshal(data, &state.Snapshot); err != nil {
			return state, err
		}
	} else if !errors.Is(err, sql.ErrNoRows) {
		return state, err
	}
	var c Connection
	err = tx.QueryRowContext(ctx, "SELECT user_id,last_synced_at,last_error_code,reconnect_required,sync_lease_id,sync_lease_until FROM calendar_connections WHERE user_id=$1", userID).Scan(&c.UserID, &c.LastSyncedAt, &c.LastErrorCode, &c.ReconnectRequired, &c.SyncLeaseID, &c.SyncLeaseUntil)
	if err == nil {
		state.Managed, state.Connection = true, &c
	} else if !errors.Is(err, sql.ErrNoRows) {
		return state, err
	}
	return state, nil
}

func (store *PostgresStore) LoadSourceSnapshot(ctx context.Context, userID string) (SourceSnapshot, error) {
	var data []byte
	err := store.database.QueryRowContext(ctx, "SELECT snapshot FROM calendar_source_snapshots WHERE user_id=$1", userID).Scan(&data)
	if errors.Is(err, sql.ErrNoRows) {
		return SourceSnapshot{}, nil
	}
	if err != nil {
		return SourceSnapshot{}, err
	}
	var value SourceSnapshot
	err = json.Unmarshal(data, &value)
	return value, err
}

func AdvanceSourcePostgres(ctx context.Context, tx *sql.Tx, userID string, full bool, from, to, now time.Time) error {
	state, err := ReadSourcePostgres(ctx, tx, userID)
	if err != nil {
		return err
	}
	if !state.Managed {
		return nil
	} // Explicit policy-only/test data, not a Google receipt.
	snapshot, err := NextSourceSnapshot(state.Snapshot, full, NewSyncLeaseID(), from, to, now)
	if err != nil {
		return err
	}
	data, err := json.Marshal(snapshot)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO calendar_source_snapshots(user_id,snapshot,published_revision) VALUES($1,$2,'') ON CONFLICT(user_id) DO UPDATE SET snapshot=EXCLUDED.snapshot,published_revision=''`, userID, data)
	return err
}
