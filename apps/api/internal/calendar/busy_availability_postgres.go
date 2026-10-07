package calendar

import (
	"context"
	"database/sql"
	"time"
)

// Caller holds this user's calendar advisory lock in the booking transaction.
// Return only the source receipt, never private event data, to the booking domain.
func CheckBusyPostgres(ctx context.Context, tx *sql.Tx, userID string, from, to time.Time) (SourceState, error) {
	source, err := ReadSourcePostgres(ctx, tx, userID)
	if err != nil {
		return source, err
	}
	if !source.PrivateReadable(from, to, time.Now().UTC()) {
		return source, ErrSourceUnavailable
	}
	if !source.Managed {
		return source, nil
	}
	intervals, err := readBusyIntervalsPostgres(ctx, tx, userID)
	if err != nil {
		return source, err
	}
	return source, ValidateBusyIntervals(source, intervals, from, to, time.Now().UTC())
}

func LoadCandidateAvailabilityPostgres(ctx context.Context, tx *sql.Tx, userID string) (CandidateAvailability, error) {
	source, err := ReadSourcePostgres(ctx, tx, userID)
	if err != nil {
		return CandidateAvailability{}, err
	}
	if source.Managed && !source.Committed(time.Now().UTC()) {
		return CandidateAvailability{}, ErrSourceUnavailable
	}
	var intervals []BusyInterval
	if source.Managed {
		intervals, err = readBusyIntervalsPostgres(ctx, tx, userID)
		if err != nil {
			return CandidateAvailability{}, err
		}
	}
	return NewCandidateAvailability(source, intervals, time.Now().UTC())
}

func readBusyIntervalsPostgres(ctx context.Context, tx *sql.Tx, userID string) ([]BusyInterval, error) {
	rows, err := tx.QueryContext(ctx, `SELECT start_at,end_at,busy_status FROM private_events WHERE user_id=$1 LIMIT $2`, userID, BusyEvidenceLimit+1)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	intervals := []BusyInterval{}
	for rows.Next() {
		var interval BusyInterval
		if err := rows.Scan(&interval.StartAt, &interval.EndAt, &interval.Status); err != nil {
			return nil, err
		}
		intervals = append(intervals, interval)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return intervals, nil
}
