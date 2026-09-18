package request

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

func (store *PostgresStore) CancelConfirmed(ctx context.Context, requestID, actor, optionID string) (err error) {
	defer func() {
		var state interface{ SQLState() string }
		if errors.As(err, &state) && (state.SQLState() == "40001" || state.SQLState() == "40P01") {
			err = ErrBookingConflict
		}
	}()
	tx, err := store.database.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var value CoordinationRequest
	var accepted sql.NullString
	err = tx.QueryRowContext(ctx, `SELECT id,organization_id,requester_user_id,target_user_id,status,accepted_option_id FROM coordination_requests WHERE id=$1 FOR UPDATE`, requestID).Scan(&value.ID, &value.OrganizationID, &value.RequesterUserID, &value.TargetUserID, &value.Status, &accepted)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	value.AcceptedOptionID = accepted.String
	// Authorize before looking up the selection; outsiders see no lifecycle detail.
	if actor == "" || (actor != value.RequesterUserID && actor != value.TargetUserID) {
		return ErrNotFound
	}
	var option Option
	err = tx.QueryRowContext(ctx, `SELECT id,request_id,type,start_at,end_at,created_at FROM coordination_request_options WHERE request_id=$1 AND id=$2`, requestID, optionID).Scan(&option.ID, &option.RequestID, &option.Type, &option.StartAt, &option.EndAt, &option.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrCancellationInvalid
	}
	if err != nil {
		return err
	}
	option.CreatedAt = option.CreatedAt.UTC()
	if option.StartAt != nil {
		utc := option.StartAt.UTC()
		option.StartAt = &utc
	}
	if option.EndAt != nil {
		utc := option.EndAt.UTC()
		option.EndAt = &utc
	}
	value.Options = []Option{option}
	now := time.Now().UTC()
	if err := ValidateConfirmedCancellation(value, actor, optionID, now); err != nil {
		return err
	}
	note, event := ConfirmedCancellationEffects(value, actor, now)
	if _, err := tx.ExecContext(ctx, `UPDATE coordination_requests SET status=$1,updated_at=$2 WHERE id=$3`, Cancelled, now, requestID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO notifications(id,user_id,type,request_id,message,created_at) VALUES($1,$2,$3,$4,$5,$6)`, note.ID, note.UserID, note.Type, note.RequestID, note.Message, note.CreatedAt); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO audit_logs(id,organization_id,actor_user_id,action,resource_type,resource_id,created_at) VALUES($1,$2,$3,$4,$5,$6,$7)`, event.ID, event.OrganizationID, event.ActorUserID, event.Action, event.ResourceType, event.ResourceID, event.CreatedAt); err != nil {
		return err
	}
	return tx.Commit()
}
