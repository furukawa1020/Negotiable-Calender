package calendar

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

var (
	ErrSyncBusy = errors.New("calendar sync already running")
	ErrSyncLeaseLost = errors.New("calendar sync lease lost")
)

type SyncLease struct { UserID, ID string }
type syncLeaseKey struct{}
type SyncLeaseStore interface {
	AcquireSync(context.Context, string, time.Time, time.Duration) (Connection, error)
}
func NewSyncLeaseID() string { return randomToken(32) }
func WithSyncLease(ctx context.Context, lease SyncLease) context.Context {
	return context.WithValue(ctx, syncLeaseKey{}, lease)
}
func SyncLeaseFromContext(ctx context.Context) (SyncLease, bool) {
	value, ok := ctx.Value(syncLeaseKey{}).(SyncLease)
	return value, ok
}

// Serialize lifecycle changes even when no connection row exists yet.
func LockCalendarTransaction(ctx context.Context, tx *sql.Tx, userID string) error {
	_, err := tx.ExecContext(ctx, "SELECT pg_advisory_xact_lock(hashtextextended($1, 0))", "calendar:"+userID)
	return err
}

func GuardSyncTransaction(ctx context.Context, tx *sql.Tx, userID string) error {
	if err := LockCalendarTransaction(ctx, tx, userID); err != nil { return err }
	lease, ok := SyncLeaseFromContext(ctx)
	if !ok { return nil }
	if lease.UserID != userID || lease.ID == "" { return ErrSyncLeaseLost }
	var id string
	var valid bool
	err := tx.QueryRowContext(ctx, "SELECT sync_lease_id, COALESCE(sync_lease_until > clock_timestamp(), false) FROM calendar_connections WHERE user_id=$1 FOR UPDATE", userID).Scan(&id, &valid)
	if errors.Is(err, sql.ErrNoRows) { return ErrSyncLeaseLost }
	if err != nil { return err }
	if id != lease.ID || !valid { return ErrSyncLeaseLost }
	return nil
}

func (store *PostgresStore) AcquireSync(ctx context.Context, userID string, now time.Time, duration time.Duration) (Connection, error) {
	if duration <= 0 { duration = defaultClaimLease }
	tx, err := store.database.BeginTx(ctx, nil)
	if err != nil { return Connection{}, err }
	defer tx.Rollback()
	if err := LockCalendarTransaction(ctx, tx, userID); err != nil { return Connection{}, err }
	value, err := scanBackgroundConnection(tx.QueryRowContext(ctx, `SELECT user_id,refresh_token_cipher,granted_scopes,connected_at,last_synced_at,last_attempt_at,next_attempt_at,last_error_code,failure_count,sync_token,reconnect_required,sync_lease_id,sync_lease_until FROM calendar_connections WHERE user_id=$1 FOR UPDATE`, userID))
	if errors.Is(err, sql.ErrNoRows) { return Connection{}, ErrNotFound }
	if err != nil { return Connection{}, err }
	if value.ReconnectRequired { return Connection{}, ErrReconnectRequired }
	if value.SyncLeaseID != "" && value.SyncLeaseUntil != nil && value.SyncLeaseUntil.After(now) { return Connection{}, ErrSyncBusy }
	id, until := NewSyncLeaseID(), now.Add(duration)
	if _, err := tx.ExecContext(ctx, "UPDATE calendar_connections SET sync_lease_id=$2,sync_lease_until=$3,last_attempt_at=$4,next_attempt_at=$3 WHERE user_id=$1", userID,id,until,now); err != nil { return Connection{}, err }
	if err := tx.Commit(); err != nil { return Connection{}, err }
	value.SyncLeaseID, value.SyncLeaseUntil = id, &until
	value.LastAttemptAt, value.NextAttemptAt = &now, &until
	return value, nil
}

func (store *PostgresStore) syncWrite(ctx context.Context, userID string, write func(*sql.Tx) error) error {
	tx, err := store.database.BeginTx(ctx,nil)
	if err != nil { return err }
	defer tx.Rollback()
	if err := GuardSyncTransaction(ctx,tx,userID); err != nil { return err }
	if err := write(tx); err != nil { return err }
	return tx.Commit()
}
