package request

import (
	"context"
	"database/sql"
	"errors"
	calendarintegration "github.com/negotiable-calendar/negotiable-calendar/apps/api/internal/calendar"
	"time"

	"github.com/negotiable-calendar/negotiable-calendar/apps/api/internal/projection"
)

func (store *PostgresStore) acceptMeeting(ctx context.Context, requestID, userID, optionID string) error {
	return store.confirmMeeting(ctx, requestID, userID, "", optionID)
}

func (store *PostgresStore) confirmMeeting(ctx context.Context, requestID, userID, org, optionID string) (err error) {
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
	err = tx.QueryRowContext(ctx, `SELECT id, organization_id, requester_user_id, target_user_id, status, deadline_at, accepted_option_id FROM coordination_requests WHERE id=$1 FOR UPDATE`, requestID).Scan(&value.ID, &value.OrganizationID, &value.RequesterUserID, &value.TargetUserID, &value.Status, &value.DeadlineAt, &accepted)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if (value.TargetUserID != userID && value.RequesterUserID != userID) || (org != "" && org != value.OrganizationID) {
		return ErrNotFound
	}
	if org != "" {
		if err := guardCreationMembers(ctx, tx, value); err != nil {
			return err
		}
	}
	var selected Option
	var proposer sql.NullString
	err = tx.QueryRowContext(ctx, `SELECT id,request_id,type,start_at,end_at,created_at,proposed_by_user_id FROM coordination_request_options WHERE request_id=$1 AND id=$2`, requestID, optionID).Scan(&selected.ID, &selected.RequestID, &selected.Type, &selected.StartAt, &selected.EndAt, &selected.CreatedAt, &proposer)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrCandidateInvalid
	}
	if err != nil {
		return err
	}
	selected.CreatedAt = selected.CreatedAt.UTC()
	if selected.StartAt != nil {
		utc := selected.StartAt.UTC()
		selected.StartAt = &utc
	}
	if selected.EndAt != nil {
		utc := selected.EndAt.UTC()
		selected.EndAt = &utc
	}
	selected.ProposedByUserID = proposer.String
	value.Options = []Option{selected}
	if err := AuthorizeConfirmation(value, userID, optionID); err != nil {
		return err
	}
	if value.Status == Accepted && accepted.String == optionID {
		return ErrAlreadyAccepted
	}
	if value.Status != Suggested {
		return ErrNotFound
	}
	if _, err := ConfirmableMeeting(value, optionID, time.Now().UTC()); err != nil {
		return err
	}
	if err := checkMeetingSlotPostgres(ctx, tx, value, selected); err != nil {
		return err
	}
	now := time.Now().UTC()
	note, event := ConfirmationEffectsForActor(value, userID, now)
	if _, err := tx.ExecContext(ctx, `UPDATE coordination_requests SET status=$1,accepted_option_id=$2,updated_at=$3 WHERE id=$4`, Accepted, optionID, now, requestID); err != nil {
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

func checkMeetingSlotPostgres(ctx context.Context, tx *sql.Tx, value CoordinationRequest, selected Option) error {
	// Serialize against source replacement, reconnect and sync state transitions.
	if err := calendarintegration.LockCalendarTransaction(ctx, tx, value.TargetUserID); err != nil {
		return err
	}
	source, err := calendarintegration.ReadSourcePostgres(ctx, tx, value.TargetUserID)
	if err != nil {
		return err
	}
	if !source.Readable(time.Now().UTC()) || (source.Managed && !source.Snapshot.Covers(*selected.StartAt, *selected.EndAt)) {
		return ErrAvailabilityChanged
	}
	// Serializable predicate reads prevent write skew for both roles, even across orgs.
	rows, err := tx.QueryContext(ctx, `SELECT r.id,r.accepted_option_id,o.type,o.start_at,o.end_at FROM coordination_requests r LEFT JOIN coordination_request_options o ON o.id=r.accepted_option_id AND o.request_id=r.id WHERE r.status=$1 AND r.id<>$2 AND (r.requester_user_id IN ($3,$4) OR r.target_user_id IN ($3,$4))`, Accepted, value.ID, value.RequesterUserID, value.TargetUserID)
	if err != nil {
		return err
	}
	for rows.Next() {
		var id string
		var selectedID, kind sql.NullString
		var start, end sql.NullTime
		if err := rows.Scan(&id, &selectedID, &kind, &start, &end); err != nil {
			rows.Close()
			return err
		}
		if !kind.Valid || (kind.String == string(OptionMeeting) && (!start.Valid || !end.Valid || !end.Time.After(start.Time) || (selected.StartAt.Before(end.Time) && start.Time.Before(*selected.EndAt)))) {
			rows.Close()
			return ErrBookingConflict
		}
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()
	rows, err = tx.QueryContext(ctx, `SELECT id,user_id,start_at,end_at,availability,interruptibility,requestability,reschedulability,expected_response_bucket,generated_at,expires_at FROM schedule_projections WHERE user_id=$1 AND start_at<$3 AND end_at>$2`, value.TargetUserID, *selected.StartAt, *selected.EndAt)
	if err != nil {
		return err
	}
	values := []projection.ScheduleProjection{}
	for rows.Next() {
		var p projection.ScheduleProjection
		if err := rows.Scan(&p.ID, &p.UserID, &p.StartAt, &p.EndAt, &p.State.Availability, &p.State.Interruptibility, &p.State.Requestability, &p.State.Reschedulability, &p.ExpectedResponseBucket, &p.GeneratedAt, &p.ExpiresAt); err != nil {
			rows.Close()
			return err
		}
		values = append(values, p)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()
	now := time.Now().UTC()
	if !source.Readable(now) {
		return ErrAvailabilityChanged
	}
	if _, err := ConfirmableMeeting(value, selected.ID, now); err != nil {
		return err
	}
	for i := range values {
		values[i].StartAt = values[i].StartAt.UTC()
		values[i].EndAt = values[i].EndAt.UTC()
		values[i].GeneratedAt = values[i].GeneratedAt.UTC()
		values[i].ExpiresAt = values[i].ExpiresAt.UTC()
	}
	if err := ValidateMeetingAvailability(value.TargetUserID, selected, values, now); err != nil {
		return err
	}
	return nil
}
