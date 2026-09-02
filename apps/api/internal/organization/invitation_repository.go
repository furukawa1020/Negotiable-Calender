package organization

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/negotiable-calendar/negotiable-calendar/apps/api/internal/audit"
)

var (
	ErrInvitationNotFound = errors.New("invitation not found")
	ErrForbidden          = errors.New("organization action forbidden")
)

type InvitationStore interface {
	CreateInvitation(context.Context, Invitation) error
	PreviewInvitation(context.Context, []byte, time.Time) (InvitationPreview, error)
	AcceptInvitation(context.Context, []byte, string, time.Time) (Workspace, error)
	ListWorkspaces(context.Context, string) ([]Workspace, error)
	SwitchWorkspace(context.Context, []byte, string, string, time.Time) (Workspace, error)
}

func EnsureInvitationSchema(ctx context.Context, database *sql.DB) error {
	_, err := database.ExecContext(ctx, `
CREATE TABLE IF NOT EXISTS organization_invitations (
    id text PRIMARY KEY,
    organization_id text NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    invited_by text NOT NULL REFERENCES users(id),
    role text NOT NULL,
    token_hash bytea NOT NULL UNIQUE,
    expires_at timestamptz NOT NULL,
    accepted_at timestamptz,
    accepted_by text REFERENCES users(id),
    created_at timestamptz NOT NULL
);
CREATE INDEX IF NOT EXISTS organization_invitations_expiry_idx
    ON organization_invitations(expires_at) WHERE accepted_at IS NULL;
`)
	if err != nil {
		return fmt.Errorf("create organization invitation schema: %w", err)
	}
	return nil
}

func (store *PostgresStore) CreateInvitation(ctx context.Context, value Invitation) error {
	if err := value.Validate(); err != nil {
		return err
	}
	transaction, err := store.database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin invitation creation: %w", err)
	}
	defer transaction.Rollback()
	var actorRole Role
	err = transaction.QueryRowContext(ctx, `SELECT role FROM memberships WHERE organization_id=$1 AND user_id=$2`, value.OrganizationID, value.InvitedBy).Scan(&actorRole)
	if errors.Is(err, sql.ErrNoRows) || (err == nil && !CanInvite(actorRole, value.Role)) {
		return ErrForbidden
	}
	if err != nil {
		return fmt.Errorf("authorize invitation creation: %w", err)
	}
	if _, err := transaction.ExecContext(ctx, `INSERT INTO organization_invitations
(id,organization_id,invited_by,role,token_hash,expires_at,created_at)
VALUES ($1,$2,$3,$4,$5,$6,$7)`, value.ID, value.OrganizationID, value.InvitedBy, value.Role, value.TokenHash, value.ExpiresAt, value.CreatedAt); err != nil {
		return fmt.Errorf("insert organization invitation: %w", err)
	}
	if err := insertOrganizationAudit(ctx, transaction, value.OrganizationID, value.InvitedBy, audit.InvitationCreated, "invitation", value.ID, value.CreatedAt); err != nil {
		return err
	}
	if err := transaction.Commit(); err != nil {
		return fmt.Errorf("commit invitation creation: %w", err)
	}
	return nil
}

func (store *PostgresStore) PreviewInvitation(ctx context.Context, tokenHash []byte, now time.Time) (InvitationPreview, error) {
	var value InvitationPreview
	err := store.database.QueryRowContext(ctx, `SELECT organization_invitations.id,organizations.id,organizations.name,
organization_invitations.role,organization_invitations.expires_at
FROM organization_invitations JOIN organizations ON organizations.id=organization_invitations.organization_id
WHERE organization_invitations.token_hash=$1 AND organization_invitations.accepted_at IS NULL
AND organization_invitations.expires_at>$2`, tokenHash, now).
		Scan(&value.ID, &value.OrganizationID, &value.OrganizationName, &value.Role, &value.ExpiresAt)
	if errors.Is(err, sql.ErrNoRows) {
		return InvitationPreview{}, ErrInvitationNotFound
	}
	if err != nil {
		return InvitationPreview{}, fmt.Errorf("preview organization invitation: %w", err)
	}
	value.ExpiresAt = value.ExpiresAt.UTC()
	return value, nil
}

func (store *PostgresStore) AcceptInvitation(ctx context.Context, tokenHash []byte, userID string, now time.Time) (Workspace, error) {
	transaction, err := store.database.BeginTx(ctx, nil)
	if err != nil {
		return Workspace{}, fmt.Errorf("begin invitation acceptance: %w", err)
	}
	defer transaction.Rollback()
	var invitationID string
	var value Workspace
	err = transaction.QueryRowContext(ctx, `UPDATE organization_invitations SET accepted_at=$2,accepted_by=$3
WHERE token_hash=$1 AND accepted_at IS NULL AND expires_at>$2
RETURNING id,organization_id,role`, tokenHash, now, userID).Scan(&invitationID, &value.ID, &value.Role)
	if errors.Is(err, sql.ErrNoRows) {
		return Workspace{}, ErrInvitationNotFound
	}
	if err != nil {
		return Workspace{}, fmt.Errorf("consume organization invitation: %w", err)
	}
	if _, err := transaction.ExecContext(ctx, `INSERT INTO memberships (id,organization_id,user_id,role,created_at)
VALUES ($1,$2,$3,$4,$5) ON CONFLICT (organization_id,user_id) DO NOTHING`, newID("membership"), value.ID, userID, value.Role, now); err != nil {
		return Workspace{}, fmt.Errorf("create invited membership: %w", err)
	}
	if err := transaction.QueryRowContext(ctx, `SELECT organizations.name,memberships.role FROM organizations
JOIN memberships ON memberships.organization_id=organizations.id
WHERE organizations.id=$1 AND memberships.user_id=$2`, value.ID, userID).Scan(&value.Name, &value.Role); err != nil {
		return Workspace{}, fmt.Errorf("load accepted workspace: %w", err)
	}
	if err := insertOrganizationAudit(ctx, transaction, value.ID, userID, audit.InvitationAccepted, "invitation", invitationID, now); err != nil {
		return Workspace{}, err
	}
	if err := transaction.Commit(); err != nil {
		return Workspace{}, fmt.Errorf("commit invitation acceptance: %w", err)
	}
	return value, nil
}

func (store *PostgresStore) ListWorkspaces(ctx context.Context, userID string) ([]Workspace, error) {
	rows, err := store.database.QueryContext(ctx, `SELECT organizations.id,organizations.name,memberships.role
FROM memberships JOIN organizations ON organizations.id=memberships.organization_id
WHERE memberships.user_id=$1 ORDER BY organizations.name,organizations.id`, userID)
	if err != nil {
		return nil, fmt.Errorf("list user workspaces: %w", err)
	}
	defer rows.Close()
	values := []Workspace{}
	for rows.Next() {
		var value Workspace
		if err := rows.Scan(&value.ID, &value.Name, &value.Role); err != nil {
			return nil, fmt.Errorf("scan user workspace: %w", err)
		}
		values = append(values, value)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate user workspaces: %w", err)
	}
	return values, nil
}

func (store *PostgresStore) SwitchWorkspace(ctx context.Context, sessionHash []byte, userID, organizationID string, now time.Time) (Workspace, error) {
	transaction, err := store.database.BeginTx(ctx, nil)
	if err != nil {
		return Workspace{}, fmt.Errorf("begin workspace switch: %w", err)
	}
	defer transaction.Rollback()
	var value Workspace
	err = transaction.QueryRowContext(ctx, `SELECT organizations.id,organizations.name,memberships.role
FROM memberships JOIN organizations ON organizations.id=memberships.organization_id
WHERE memberships.user_id=$1 AND memberships.organization_id=$2`, userID, organizationID).
		Scan(&value.ID, &value.Name, &value.Role)
	if errors.Is(err, sql.ErrNoRows) {
		return Workspace{}, ErrForbidden
	}
	if err != nil {
		return Workspace{}, fmt.Errorf("authorize workspace switch: %w", err)
	}
	result, err := transaction.ExecContext(ctx, `UPDATE auth_sessions SET organization_id=$3
WHERE token_hash=$1 AND user_id=$2 AND expires_at>$4`, sessionHash, userID, organizationID, now)
	if err != nil {
		return Workspace{}, fmt.Errorf("update active workspace: %w", err)
	}
	count, err := result.RowsAffected()
	if err != nil || count != 1 {
		return Workspace{}, ErrForbidden
	}
	if err := insertOrganizationAudit(ctx, transaction, organizationID, userID, audit.WorkspaceSwitched, "workspace", organizationID, now); err != nil {
		return Workspace{}, err
	}
	if err := transaction.Commit(); err != nil {
		return Workspace{}, fmt.Errorf("commit workspace switch: %w", err)
	}
	return value, nil
}

type organizationAuditExecutor interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}

func insertOrganizationAudit(ctx context.Context, executor organizationAuditExecutor, organizationID, actorUserID string, action audit.Action, resourceType, resourceID string, now time.Time) error {
	_, err := executor.ExecContext(ctx, `INSERT INTO audit_logs
(id,organization_id,actor_user_id,action,resource_type,resource_id,created_at)
VALUES ($1,$2,$3,$4,$5,$6,$7)`, newID("audit"), organizationID, actorUserID, action, resourceType, resourceID, now)
	if err != nil {
		return fmt.Errorf("insert organization audit: %w", err)
	}
	return nil
}
