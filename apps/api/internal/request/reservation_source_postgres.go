package request

import (
	"context"
	"database/sql"
	"fmt"
)

func (store *PostgresStore) LoadConfirmedRanges(ctx context.Context, requester, target string) ([]ReservedRange, error) {
	if requester == "" || target == "" || requester == target {
		return nil, ErrReservationUnavailable
	}
	ctx, cancel := context.WithTimeout(ctx, ReservationReadTimeout)
	defer cancel()
	tx, err := store.database.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	values := []CoordinationRequest{}
	seen := map[string]bool{}
	for _, participant := range []string{requester, target} {
		var exists bool
		if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM users WHERE id=$1)`, participant).Scan(&exists); err != nil {
			return nil, err
		}
		if !exists {
			return nil, ErrReservationUnavailable
		}
		for _, role := range []string{"requester_user_id", "target_user_id"} {
			// role comes only from the fixed list above, never from client input.
			// LEFT JOIN preserves a missing/wrong-request selected option as unsafe.
			query := fmt.Sprintf(`SELECT r.id,r.accepted_option_id,o.id,o.type,o.start_at,o.end_at
FROM coordination_requests r LEFT JOIN coordination_request_options o ON o.id=r.accepted_option_id AND o.request_id=r.id
WHERE r.%s=$1 AND r.status=$2 ORDER BY r.id LIMIT $3`, role)
			rows, err := tx.QueryContext(ctx, query, participant, Accepted, ReservationRoleLimit+1)
			if err != nil {
				return nil, err
			}
			count := 0
			for rows.Next() {
				count++
				if count > ReservationRoleLimit {
					rows.Close()
					return nil, ErrReservationUnavailable
				}
				var value CoordinationRequest
				var accepted, optionID, kind sql.NullString
				var start, end sql.NullTime
				if err := rows.Scan(&value.ID, &accepted, &optionID, &kind, &start, &end); err != nil {
					rows.Close()
					return nil, err
				}
				value.Status, value.AcceptedOptionID = Accepted, accepted.String
				if optionID.Valid {
					option := Option{ID: optionID.String, RequestID: value.ID, Type: OptionType(kind.String)}
					if start.Valid {
						at := start.Time.UTC()
						option.StartAt = &at
					}
					if end.Valid {
						at := end.Time.UTC()
						option.EndAt = &at
					}
					value.Options = []Option{option}
				}
				if !seen[value.ID] {
					seen[value.ID] = true
					values = append(values, value)
				}
			}
			err = rows.Err()
			rows.Close()
			if err != nil {
				return nil, err
			}
		}
	}
	ranges, err := ReservationRanges(values)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return ranges, nil
}

var _ ReservationStore = (*PostgresStore)(nil)
