package request

import (
	"context"
	"database/sql"
	"errors"
	"sort"
	"time"
)

func (store *PostgresStore) InspectHandoff(ctx context.Context, id, actor, org, recipient string) (CoordinationRequest, bool, error) {
	return store.handoff(ctx, id, actor, org, recipient, nil, false)
}
func (store *PostgresStore) Handoff(ctx context.Context, id, actor, org, recipient string, options []Option) (bool, error) {
	_, replay, err := store.handoff(ctx, id, actor, org, recipient, options, true)
	return replay, err
}

func (store *PostgresStore) handoff(ctx context.Context, id, actor, org, recipient string, options []Option, commit bool) (result CoordinationRequest, replay bool, err error) {
	defer func() {
		var state interface{ SQLState() string }
		if errors.As(err, &state) && (state.SQLState() == "40001" || state.SQLState() == "40P01") {
			err = ErrHandoffConflict
		}
	}()
	tx, err := store.database.BeginTx(ctx, nil)
	if err != nil {
		return result, false, err
	}
	defer tx.Rollback()
	var value CoordinationRequest
	var from, to, accepted sql.NullString
	err = tx.QueryRowContext(ctx, `SELECT id,organization_id,requester_user_id,target_user_id,type,title,duration_minutes,deadline_at,sync_preference,priority,status,created_at,updated_at,delegated_from_user_id,delegated_user_id,accepted_option_id FROM coordination_requests WHERE id=$1 FOR UPDATE`, id).Scan(&value.ID, &value.OrganizationID, &value.RequesterUserID, &value.TargetUserID, &value.Type, &value.Title, &value.DurationMinutes, &value.DeadlineAt, &value.SyncPreference, &value.Priority, &value.Status, &value.CreatedAt, &value.UpdatedAt, &from, &to, &accepted)
	if errors.Is(err, sql.ErrNoRows) {
		return result, false, ErrNotFound
	}
	if err != nil {
		return result, false, err
	}
	value.DelegatedFromUserID, value.DelegatedUserID, value.AcceptedOptionID = from.String, to.String, accepted.String
	value.DeadlineAt, value.CreatedAt, value.UpdatedAt = value.DeadlineAt.UTC(), value.CreatedAt.UTC(), value.UpdatedAt.UTC()
	replay, err = ValidateHandoff(value, actor, org, recipient, time.Now().UTC())
	if err != nil {
		return result, false, err
	}
	users := []string{value.RequesterUserID, actor, recipient}
	sort.Strings(users)
	for _, user := range users {
		var id string
		if err := tx.QueryRowContext(ctx, `SELECT user_id FROM memberships WHERE organization_id=$1 AND user_id=$2 FOR SHARE`, org, user).Scan(&id); errors.Is(err, sql.ErrNoRows) {
			return result, false, ErrCreationForbidden
		} else if err != nil {
			return result, false, err
		}
	}
	if replay {
		return result, true, tx.Commit()
	}
	if !commit {
		return value, false, tx.Commit()
	}
	if err := ApplyHandoff(&value, actor, recipient, options, time.Now().UTC()); err != nil {
		return result, false, err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE coordination_requests SET target_user_id=$1,delegated_user_id=$1,delegated_from_user_id=$2,updated_at=$3 WHERE id=$4`, recipient, actor, value.UpdatedAt, id); err != nil {
		return result, false, err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM coordination_request_options WHERE request_id=$1`, id); err != nil {
		return result, false, err
	}
	for _, option := range value.Options {
		if _, err := tx.ExecContext(ctx, `INSERT INTO coordination_request_options(id,request_id,type,start_at,end_at,response_by,delegate_user_id,created_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8)`, option.ID, id, option.Type, option.StartAt, option.EndAt, option.ResponseBy, nullableString(option.DelegateUserID), option.CreatedAt); err != nil {
			return result, false, err
		}
	}
	notes, event := HandoffEffects(value, actor)
	for _, note := range notes {
		if _, err := tx.ExecContext(ctx, `INSERT INTO notifications(id,user_id,type,request_id,message,created_at) VALUES($1,$2,$3,$4,$5,$6)`, note.ID, note.UserID, note.Type, note.RequestID, note.Message, note.CreatedAt); err != nil {
			return result, false, err
		}
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO audit_logs(id,organization_id,actor_user_id,action,resource_type,resource_id,created_at) VALUES($1,$2,$3,$4,$5,$6,$7)`, event.ID, event.OrganizationID, event.ActorUserID, event.Action, event.ResourceType, event.ResourceID, event.CreatedAt); err != nil {
		return result, false, err
	}
	return result, false, tx.Commit()
}

var _ HandoffStore = (*PostgresStore)(nil)
