package request

import (
	"context"
	"database/sql"
	"errors"
	"sort"
)

func guardCreationMembers(ctx context.Context, tx *sql.Tx, value CoordinationRequest) error {
	users := []string{value.RequesterUserID, value.TargetUserID}
	sort.Strings(users)
	for _, user := range users {
		var id string
		err := tx.QueryRowContext(ctx, `SELECT user_id FROM memberships WHERE organization_id=$1 AND user_id=$2 FOR SHARE`, value.OrganizationID, user).Scan(&id)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrCreationForbidden
		}
		if err != nil {
			return err
		}
	}
	return nil
}

func lookupCreationEnvelope(ctx context.Context, tx *sql.Tx, command CoordinationRequest) error {
	var value CoordinationRequest
	err := tx.QueryRowContext(ctx, `SELECT id,organization_id,requester_user_id,target_user_id,type,title,duration_minutes,deadline_at,sync_preference,priority FROM coordination_requests WHERE id=$1`, command.ID).
		Scan(&value.ID, &value.OrganizationID, &value.RequesterUserID, &value.TargetUserID, &value.Type, &value.Title, &value.DurationMinutes, &value.DeadlineAt, &value.SyncPreference, &value.Priority)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if !SameCreation(value, command) {
		return ErrCreationConflict
	}
	return nil
}

func (store *PostgresStore) LookupCreation(ctx context.Context, command CoordinationRequest) (CoordinationRequest, error) {
	tx, err := store.database.BeginTx(ctx, nil)
	if err != nil {
		return CoordinationRequest{}, err
	}
	defer tx.Rollback()
	if err := guardCreationMembers(ctx, tx, command); err != nil {
		return CoordinationRequest{}, err
	}
	if err := lookupCreationEnvelope(ctx, tx, command); err != nil {
		return CoordinationRequest{}, err
	}
	if err := tx.Commit(); err != nil {
		return CoordinationRequest{}, err
	}
	return store.GetForUser(ctx, command.ID, command.RequesterUserID)
}

func (store *PostgresStore) CreateOnce(ctx context.Context, value CoordinationRequest) (bool, error) {
	tx, err := store.database.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	// Serialize same-key commands. Read Committed sees the winner after this lock.
	if _, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, "request-create:"+value.ID); err != nil {
		return false, err
	}
	if err := guardCreationMembers(ctx, tx, value); err != nil {
		return false, err
	}
	err = lookupCreationEnvelope(ctx, tx, value)
	if err == nil {
		return false, tx.Commit()
	}
	if !errors.Is(err, ErrNotFound) {
		return false, err
	}
	if err := value.Validate(); err != nil {
		return false, err
	}
	if err := insertCoordinationRequest(ctx, tx, value); err != nil {
		return false, err
	}
	note, event := CreationEffects(value)
	if _, err := tx.ExecContext(ctx, `INSERT INTO notifications(id,user_id,type,request_id,message,created_at) VALUES($1,$2,$3,$4,$5,$6)`, note.ID, note.UserID, note.Type, note.RequestID, note.Message, note.CreatedAt); err != nil {
		return false, err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO audit_logs(id,organization_id,actor_user_id,action,resource_type,resource_id,created_at) VALUES($1,$2,$3,$4,$5,$6,$7)`, event.ID, event.OrganizationID, event.ActorUserID, event.Action, event.ResourceType, event.ResourceID, event.CreatedAt); err != nil {
		return false, err
	}
	if err := tx.Commit(); err != nil {
		return false, err
	}
	return true, nil
}

var _ CreationStore = (*PostgresStore)(nil)
