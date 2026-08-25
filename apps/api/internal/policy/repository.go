package policy

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

var ErrNotFound = errors.New("sharing policy not found")

type Store interface {
	Get(context.Context, string) (SharingPolicy, error)
	Upsert(context.Context, SharingPolicy) error
	ListActiveOverrides(context.Context, string, time.Time) ([]ManualOverride, error)
	CreateOverride(context.Context, ManualOverride) error
}

type PostgresStore struct {
	database *sql.DB
}

func NewPostgresStore(database *sql.DB) *PostgresStore {
	return &PostgresStore{database: database}
}

func EnsureSchema(ctx context.Context, database *sql.DB) error {
	const schema = `
CREATE TABLE IF NOT EXISTS sharing_policies (
    id text PRIMARY KEY,
    user_id text NOT NULL UNIQUE,
    default_availability text NOT NULL,
    default_interruptibility text NOT NULL,
    default_requestability text NOT NULL,
    default_reschedulability text NOT NULL,
    working_hours_json jsonb NOT NULL,
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL
);
CREATE TABLE IF NOT EXISTS policy_rules (
    id text PRIMARY KEY,
    policy_id text NOT NULL REFERENCES sharing_policies(id) ON DELETE CASCADE,
    condition_type text NOT NULL,
    condition_json jsonb NOT NULL,
    availability text NOT NULL,
    interruptibility text NOT NULL,
    requestability text NOT NULL,
    reschedulability text NOT NULL,
    priority integer NOT NULL,
    enabled boolean NOT NULL
);
CREATE INDEX IF NOT EXISTS policy_rules_policy_priority_idx
    ON policy_rules(policy_id, priority DESC, id);
CREATE TABLE IF NOT EXISTS manual_overrides (
    id text PRIMARY KEY,
    user_id text NOT NULL,
    start_at timestamptz NOT NULL,
    end_at timestamptz NOT NULL,
    availability text NOT NULL,
    interruptibility text NOT NULL,
    requestability text NOT NULL,
    reschedulability text NOT NULL,
    expires_at timestamptz NOT NULL,
    created_at timestamptz NOT NULL
);
CREATE INDEX IF NOT EXISTS manual_overrides_user_active_idx
    ON manual_overrides(user_id, end_at, expires_at);
`
	if _, err := database.ExecContext(ctx, schema); err != nil {
		return fmt.Errorf("create sharing policy schema: %w", err)
	}
	return nil
}

func (store *PostgresStore) ListActiveOverrides(ctx context.Context, userID string, now time.Time) ([]ManualOverride, error) {
	rows, err := store.database.QueryContext(ctx, `
SELECT id, user_id, start_at, end_at, availability, interruptibility,
       requestability, reschedulability, expires_at, created_at
FROM manual_overrides
WHERE user_id = $1 AND end_at > $2 AND expires_at > $2
ORDER BY start_at, id
`, userID, now)
	if err != nil {
		return nil, fmt.Errorf("list manual overrides: %w", err)
	}
	defer rows.Close()
	var result []ManualOverride
	for rows.Next() {
		var value ManualOverride
		if err := rows.Scan(
			&value.ID, &value.UserID, &value.StartAt, &value.EndAt,
			&value.State.Availability, &value.State.Interruptibility,
			&value.State.Requestability, &value.State.Reschedulability,
			&value.ExpiresAt, &value.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan manual override: %w", err)
		}
		result = append(result, value)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate manual overrides: %w", err)
	}
	return result, nil
}

func (store *PostgresStore) CreateOverride(ctx context.Context, value ManualOverride) error {
	if err := value.Validate(); err != nil {
		return err
	}
	_, err := store.database.ExecContext(ctx, `
INSERT INTO manual_overrides (
    id, user_id, start_at, end_at, availability, interruptibility,
    requestability, reschedulability, expires_at, created_at
) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)
`, value.ID, value.UserID, value.StartAt, value.EndAt,
		value.State.Availability, value.State.Interruptibility,
		value.State.Requestability, value.State.Reschedulability,
		value.ExpiresAt, value.CreatedAt)
	if err != nil {
		return fmt.Errorf("create manual override: %w", err)
	}
	return nil
}

func (store *PostgresStore) Get(ctx context.Context, userID string) (SharingPolicy, error) {
	var result SharingPolicy
	var windowsJSON []byte
	err := store.database.QueryRowContext(ctx, `
SELECT id, user_id, default_availability, default_interruptibility,
       default_requestability, default_reschedulability, working_hours_json,
       created_at, updated_at
FROM sharing_policies
WHERE user_id = $1
`, userID).Scan(
		&result.ID, &result.UserID, &result.Default.Availability,
		&result.Default.Interruptibility, &result.Default.Requestability,
		&result.Default.Reschedulability, &windowsJSON,
		&result.CreatedAt, &result.UpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return SharingPolicy{}, ErrNotFound
	}
	if err != nil {
		return SharingPolicy{}, fmt.Errorf("get sharing policy: %w", err)
	}
	if err := json.Unmarshal(windowsJSON, &result.WorkingHours); err != nil {
		return SharingPolicy{}, fmt.Errorf("decode working hours: %w", err)
	}

	rows, err := store.database.QueryContext(ctx, `
SELECT id, policy_id, condition_type, condition_json, availability,
       interruptibility, requestability, reschedulability, priority, enabled
FROM policy_rules
WHERE policy_id = $1
ORDER BY priority DESC, id
`, result.ID)
	if err != nil {
		return SharingPolicy{}, fmt.Errorf("list policy rules: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var rule Rule
		if err := rows.Scan(
			&rule.ID, &rule.PolicyID, &rule.ConditionType, &rule.Condition,
			&rule.State.Availability, &rule.State.Interruptibility,
			&rule.State.Requestability, &rule.State.Reschedulability,
			&rule.Priority, &rule.Enabled,
		); err != nil {
			return SharingPolicy{}, fmt.Errorf("scan policy rule: %w", err)
		}
		result.Rules = append(result.Rules, rule)
	}
	if err := rows.Err(); err != nil {
		return SharingPolicy{}, fmt.Errorf("iterate policy rules: %w", err)
	}
	return result, nil
}

func (store *PostgresStore) Upsert(ctx context.Context, value SharingPolicy) error {
	if err := value.Validate(); err != nil {
		return err
	}
	windowsJSON, err := json.Marshal(value.WorkingHours)
	if err != nil {
		return fmt.Errorf("encode working hours: %w", err)
	}
	transaction, err := store.database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin policy update: %w", err)
	}
	defer transaction.Rollback()
	_, err = transaction.ExecContext(ctx, `
INSERT INTO sharing_policies (
    id, user_id, default_availability, default_interruptibility,
    default_requestability, default_reschedulability, working_hours_json,
    created_at, updated_at
) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)
ON CONFLICT (user_id) DO UPDATE SET
    default_availability = EXCLUDED.default_availability,
    default_interruptibility = EXCLUDED.default_interruptibility,
    default_requestability = EXCLUDED.default_requestability,
    default_reschedulability = EXCLUDED.default_reschedulability,
    working_hours_json = EXCLUDED.working_hours_json,
    updated_at = EXCLUDED.updated_at
`, value.ID, value.UserID, value.Default.Availability,
		value.Default.Interruptibility, value.Default.Requestability,
		value.Default.Reschedulability, windowsJSON, value.CreatedAt, value.UpdatedAt)
	if err != nil {
		return fmt.Errorf("upsert sharing policy: %w", err)
	}
	if _, err := transaction.ExecContext(ctx, "DELETE FROM policy_rules WHERE policy_id = $1", value.ID); err != nil {
		return fmt.Errorf("replace policy rules: %w", err)
	}
	for _, rule := range value.Rules {
		_, err := transaction.ExecContext(ctx, `
INSERT INTO policy_rules (
    id, policy_id, condition_type, condition_json, availability,
    interruptibility, requestability, reschedulability, priority, enabled
) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)
`, rule.ID, rule.PolicyID, rule.ConditionType, rule.Condition,
			rule.State.Availability, rule.State.Interruptibility,
			rule.State.Requestability, rule.State.Reschedulability,
			rule.Priority, rule.Enabled)
		if err != nil {
			return fmt.Errorf("insert policy rule: %w", err)
		}
	}
	if err := transaction.Commit(); err != nil {
		return fmt.Errorf("commit policy update: %w", err)
	}
	return nil
}
