package request

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

func (store *PostgresStore) ResolveRequest(ctx context.Context, id, actor, organization string, command ResolutionCommand) (replayed bool, err error) {
	defer func() {
		var state interface{ SQLState() string }
		if errors.As(err, &state) && (state.SQLState() == "40001" || state.SQLState() == "40P01") {
			err = ErrResolutionConflict
		}
	}()
	tx, err := store.database.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	var value CoordinationRequest
	var accepted, message sql.NullString
	err = tx.QueryRowContext(ctx, `SELECT id,organization_id,requester_user_id,target_user_id,status,accepted_option_id,async_message,deadline_at,updated_at FROM coordination_requests WHERE id=$1 FOR UPDATE`, id).
		Scan(&value.ID, &value.OrganizationID, &value.RequesterUserID, &value.TargetUserID, &value.Status, &accepted, &message, &value.DeadlineAt, &value.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return false, ErrNotFound
	}
	if err != nil {
		return false, err
	}
	value.AcceptedOptionID, value.AsyncMessage = accepted.String, message.String
	if err := AuthorizeResolution(value, actor, organization, command); err != nil {
		return false, err
	}
	if err := guardCreationMembers(ctx, tx, value); err != nil {
		return false, err
	}
	replayed, err = PrepareResolution(&value, command, time.Now().UTC())
	if err != nil {
		return false, err
	}
	if replayed {
		return true, tx.Commit()
	}
	note, event := ResolutionEffects(value, actor)
	if _, err := tx.ExecContext(ctx, `UPDATE coordination_requests SET status=$1,async_message=$2,updated_at=$3 WHERE id=$4`, value.Status, nullableString(value.AsyncMessage), value.UpdatedAt, id); err != nil {
		return false, err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO notifications(id,user_id,type,request_id,message,created_at) VALUES($1,$2,$3,$4,$5,$6)`, note.ID, note.UserID, note.Type, note.RequestID, note.Message, note.CreatedAt); err != nil {
		return false, err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO audit_logs(id,organization_id,actor_user_id,action,resource_type,resource_id,created_at) VALUES($1,$2,$3,$4,$5,$6,$7)`, event.ID, event.OrganizationID, event.ActorUserID, event.Action, event.ResourceType, event.ResourceID, event.CreatedAt); err != nil {
		return false, err
	}
	return false, tx.Commit()
}

var _ ResolutionStore = (*PostgresStore)(nil)
