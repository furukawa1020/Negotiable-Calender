package request

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"
)

func (store *PostgresStore) Reschedule(ctx context.Context, id, actor string, command RescheduleCommand) (err error) {
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
	var proposalJSON []byte
	err = tx.QueryRowContext(ctx, `SELECT id,organization_id,requester_user_id,target_user_id,status,accepted_option_id,duration_minutes,deadline_at,reschedule_proposal FROM coordination_requests WHERE id=$1 FOR UPDATE`, id).Scan(&value.ID, &value.OrganizationID, &value.RequesterUserID, &value.TargetUserID, &value.Status, &accepted, &value.DurationMinutes, &value.DeadlineAt, &proposalJSON)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if actor != value.RequesterUserID && actor != value.TargetUserID {
		return ErrNotFound
	}
	value.AcceptedOptionID = accepted.String
	value.DeadlineAt = value.DeadlineAt.UTC()
	if err := json.Unmarshal(proposalJSON, &value.RescheduleProposal); err != nil {
		return err
	}
	rows, err := tx.QueryContext(ctx, `SELECT id,request_id,type,start_at,end_at,created_at FROM coordination_request_options WHERE request_id=$1`, id)
	if err != nil {
		return err
	}
	for rows.Next() {
		var option Option
		if err := rows.Scan(&option.ID, &option.RequestID, &option.Type, &option.StartAt, &option.EndAt, &option.CreatedAt); err != nil {
			rows.Close()
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
		value.Options = append(value.Options, option)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()
	now := time.Now().UTC()
	if err := ApplyReschedule(&value, actor, command, now); err != nil {
		return err
	}
	if command.Action == "accept" {
		selected, err := ConfirmableMeeting(value, command.ProposalID, now)
		if err != nil {
			return err
		}
		if err := checkMeetingSlotPostgres(ctx, tx, value, selected); err != nil {
			return err
		}
		old := value
		old.AcceptedOptionID = command.ExpectedOptionID
		if err := ValidateConfirmedCancellation(old, actor, command.ExpectedOptionID, time.Now().UTC()); err != nil {
			return ErrRescheduleInvalid
		}
	}
	if command.Action == "propose" {
		option := value.Options[len(value.Options)-1]
		if _, err := tx.ExecContext(ctx, `INSERT INTO coordination_request_options(id,request_id,type,start_at,end_at,created_at) VALUES($1,$2,$3,$4,$5,$6)`, option.ID, id, option.Type, option.StartAt, option.EndAt, option.CreatedAt); err != nil {
			return err
		}
	}
	proposalJSON, err = json.Marshal(value.RescheduleProposal)
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE coordination_requests SET accepted_option_id=$1,reschedule_proposal=$2,updated_at=$3 WHERE id=$4`, value.AcceptedOptionID, proposalJSON, now, id); err != nil {
		return err
	}
	note, event := RescheduleEffects(value, actor, command.Action, now)
	if _, err := tx.ExecContext(ctx, `INSERT INTO notifications(id,user_id,type,request_id,message,created_at) VALUES($1,$2,$3,$4,$5,$6)`, note.ID, note.UserID, note.Type, note.RequestID, note.Message, note.CreatedAt); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO audit_logs(id,organization_id,actor_user_id,action,resource_type,resource_id,created_at) VALUES($1,$2,$3,$4,$5,$6,$7)`, event.ID, event.OrganizationID, event.ActorUserID, event.Action, event.ResourceType, event.ResourceID, event.CreatedAt); err != nil {
		return err
	}
	return tx.Commit()
}
