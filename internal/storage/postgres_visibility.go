package storage

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// migrateVisibility creates the per-user visibility rules table and index.
func (s *PostgresStore) migrateVisibility(ctx context.Context) error {
	if _, err := s.db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS user_visibility_rules (
			id         TEXT PRIMARY KEY,
			user_id    TEXT NOT NULL,
			rule_type  TEXT NOT NULL,
			value      TEXT NOT NULL,
			created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
			UNIQUE(user_id, rule_type, value)
		)
	`); err != nil {
		return fmt.Errorf("create user_visibility_rules table: %w", err)
	}
	if _, err := s.db.ExecContext(ctx, `CREATE INDEX IF NOT EXISTS idx_uvr_user ON user_visibility_rules(user_id)`); err != nil {
		return fmt.Errorf("create idx_uvr_user index: %w", err)
	}
	return nil
}

// AddVisibilityRule inserts a per-user suppression rule. It is idempotent:
// re-adding an existing (user_id, rule_type, value) tuple returns the existing
// row without error.
func (s *PostgresStore) AddVisibilityRule(ctx context.Context, userID, ruleType, value string) (VisibilityRule, error) {
	id := uuid.NewString()
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO user_visibility_rules (id, user_id, rule_type, value)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (user_id, rule_type, value) DO NOTHING
	`, id, userID, ruleType, value)
	if err != nil {
		return VisibilityRule{}, fmt.Errorf("insert visibility rule: %w", err)
	}

	var r VisibilityRule
	var createdAt time.Time
	err = s.db.QueryRowContext(ctx, `
		SELECT id, user_id, rule_type, value, created_at
		FROM user_visibility_rules
		WHERE user_id = $1 AND rule_type = $2 AND value = $3
	`, userID, ruleType, value).Scan(&r.ID, &r.UserID, &r.RuleType, &r.Value, &createdAt)
	if err != nil {
		if err == sql.ErrNoRows {
			return VisibilityRule{}, ErrNotFound
		}
		return VisibilityRule{}, fmt.Errorf("read back visibility rule: %w", err)
	}
	r.CreatedAt = createdAt
	return r, nil
}

// DeleteVisibilityRule removes a rule by its (user_id, rule_type, value) tuple.
// Deleting a rule that does not exist is a no-op (no error).
func (s *PostgresStore) DeleteVisibilityRule(ctx context.Context, userID, ruleType, value string) error {
	_, err := s.db.ExecContext(ctx, `
		DELETE FROM user_visibility_rules
		WHERE user_id = $1 AND rule_type = $2 AND value = $3
	`, userID, ruleType, value)
	if err != nil {
		return fmt.Errorf("delete visibility rule: %w", err)
	}
	return nil
}

// ListVisibilityRules returns all rules for a user ordered by created_at.
func (s *PostgresStore) ListVisibilityRules(ctx context.Context, userID string) ([]VisibilityRule, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, user_id, rule_type, value, created_at
		FROM user_visibility_rules
		WHERE user_id = $1
		ORDER BY created_at ASC
	`, userID)
	if err != nil {
		return nil, fmt.Errorf("list visibility rules: %w", err)
	}
	defer rows.Close()

	rules := []VisibilityRule{}
	for rows.Next() {
		var r VisibilityRule
		var createdAt time.Time
		if err := rows.Scan(&r.ID, &r.UserID, &r.RuleType, &r.Value, &createdAt); err != nil {
			return nil, fmt.Errorf("scan visibility rule: %w", err)
		}
		r.CreatedAt = createdAt
		rules = append(rules, r)
	}
	return rules, rows.Err()
}
