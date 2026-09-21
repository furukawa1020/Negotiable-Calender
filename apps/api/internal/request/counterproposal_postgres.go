package request

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

func (store *PostgresStore) ProposeMeeting(ctx context.Context, id, actor, org string, start, end time.Time) (option Option, replay bool, err error) {
	defer func() {
		var state interface{ SQLState() string }
		if errors.As(err, &state) && (state.SQLState() == "40001" || state.SQLState() == "40P01") {
			err = ErrProposalConflict
		}
	}()
	tx, err := store.database.BeginTx(ctx, nil)
	if err != nil {
		return option, false, err
	}
	defer tx.Rollback()
	var value CoordinationRequest
	var accepted sql.NullString
	err = tx.QueryRowContext(ctx, `SELECT id,organization_id,requester_user_id,target_user_id,status,accepted_option_id,deadline_at,duration_minutes FROM coordination_requests WHERE id=$1 FOR UPDATE`, id).Scan(&value.ID, &value.OrganizationID, &value.RequesterUserID, &value.TargetUserID, &value.Status, &accepted, &value.DeadlineAt, &value.DurationMinutes)
	if errors.Is(err, sql.ErrNoRows) {
		return option, false, ErrNotFound
	}
	if err != nil {
		return option, false, err
	}
	if actor == "" || actor != value.TargetUserID || org == "" || org != value.OrganizationID {
		return option, false, ErrNotFound
	}
	if err := guardCreationMembers(ctx, tx, value); err != nil {
		return option, false, err
	}
	value.AcceptedOptionID = accepted.String
	value.Options, err = listOptions(ctx, tx, id)
	if err != nil {
		return option, false, err
	}
	option, replay, err = PrepareProposal(&value, actor, org, start, end, time.Now().UTC())
	if err != nil {
		return Option{}, false, err
	}
	if replay {
		return option, true, tx.Commit()
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO coordination_request_options(id,request_id,type,start_at,end_at,created_at,proposed_by_user_id) VALUES($1,$2,$3,$4,$5,$6,$7)`, option.ID, id, option.Type, option.StartAt, option.EndAt, option.CreatedAt, actor); err != nil {
		return Option{}, false, err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE coordination_requests SET updated_at=$1 WHERE id=$2`, value.UpdatedAt, id); err != nil {
		return Option{}, false, err
	}
	note, event := ProposalEffects(value, option)
	if _, err := tx.ExecContext(ctx, `INSERT INTO notifications(id,user_id,type,request_id,message,created_at) VALUES($1,$2,$3,$4,$5,$6)`, note.ID, note.UserID, note.Type, note.RequestID, note.Message, note.CreatedAt); err != nil {
		return Option{}, false, err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO audit_logs(id,organization_id,actor_user_id,action,resource_type,resource_id,created_at) VALUES($1,$2,$3,$4,$5,$6,$7)`, event.ID, event.OrganizationID, event.ActorUserID, event.Action, event.ResourceType, event.ResourceID, event.CreatedAt); err != nil {
		return Option{}, false, err
	}
	return option, false, tx.Commit()
}

func (store *PostgresStore) ConfirmMeeting(ctx context.Context, id, actor, org, option string) error {
	if org == "" {
		return ErrNotFound
	}
	return store.confirmMeeting(ctx, id, actor, org, option)
}

var _ CounterproposalStore = (*PostgresStore)(nil)
