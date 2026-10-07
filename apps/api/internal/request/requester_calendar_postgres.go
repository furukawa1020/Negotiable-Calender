package request

import (
	"context"
	"database/sql"
	cal "github.com/negotiable-calendar/negotiable-calendar/apps/api/internal/calendar"
	"time"
)

func (store *PostgresStore) LoadRequesterCalendar(ctx context.Context, userID string) (cal.CandidateAvailability, error) {
	ctx, cancel := context.WithTimeout(ctx, ReservationReadTimeout)
	defer cancel()
	zero := cal.CandidateAvailability{}
	if userID == "" {
		return zero, ErrAvailabilityChanged
	}
	tx, err := store.database.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return zero, err
	}
	defer tx.Rollback()
	var exists bool
	if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM users WHERE id=$1)`, userID).Scan(&exists); err != nil {
		return zero, err
	}
	if !exists {
		return zero, ErrAvailabilityChanged
	}
	snapshot, err := cal.LoadCandidateAvailabilityPostgres(ctx, tx, userID)
	if err != nil {
		return zero, err
	}
	if err := tx.Commit(); err != nil {
		return zero, err
	}
	if err := snapshot.Validate(time.Now().UTC()); err != nil {
		return zero, err
	}
	return snapshot, nil
}

var _ RequesterCalendarStore = (*PostgresStore)(nil)
