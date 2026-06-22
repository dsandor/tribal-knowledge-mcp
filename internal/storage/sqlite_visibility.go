package storage

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/google/uuid"
)

// AddVisibilityRule inserts a per-user suppression rule. It is idempotent:
// re-adding an existing (user_id, rule_type, value) tuple returns the existing
// row without error.
func (s *SQLiteStore) AddVisibilityRule(ctx context.Context, userID, ruleType, value string) (VisibilityRule, error) {
	id := uuid.NewString()
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO user_visibility_rules (id, user_id, rule_type, value)
		VALUES (?, ?, ?, ?)
		ON CONFLICT(user_id, rule_type, value) DO NOTHING
	`, id, userID, ruleType, value)
	if err != nil {
		return VisibilityRule{}, fmt.Errorf("insert visibility rule: %w", err)
	}

	// Return the existing/inserted row so callers always get the canonical ID
	// and created_at (which differ from the generated id when a conflict occurred).
	var r VisibilityRule
	var createdAt string
	err = s.db.QueryRowContext(ctx, `
		SELECT id, user_id, rule_type, value, created_at
		FROM user_visibility_rules
		WHERE user_id = ? AND rule_type = ? AND value = ?
	`, userID, ruleType, value).Scan(&r.ID, &r.UserID, &r.RuleType, &r.Value, &createdAt)
	if err != nil {
		if err == sql.ErrNoRows {
			return VisibilityRule{}, ErrNotFound
		}
		return VisibilityRule{}, fmt.Errorf("read back visibility rule: %w", err)
	}
	r.CreatedAt = parseTimestamp(createdAt)
	return r, nil
}

// DeleteVisibilityRule removes a rule by its (user_id, rule_type, value) tuple.
// Deleting a rule that does not exist is a no-op (no error).
func (s *SQLiteStore) DeleteVisibilityRule(ctx context.Context, userID, ruleType, value string) error {
	_, err := s.db.ExecContext(ctx, `
		DELETE FROM user_visibility_rules
		WHERE user_id = ? AND rule_type = ? AND value = ?
	`, userID, ruleType, value)
	if err != nil {
		return fmt.Errorf("delete visibility rule: %w", err)
	}
	return nil
}

// ListVisibilityRules returns all rules for a user ordered by created_at.
func (s *SQLiteStore) ListVisibilityRules(ctx context.Context, userID string) ([]VisibilityRule, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, user_id, rule_type, value, created_at
		FROM user_visibility_rules
		WHERE user_id = ?
		ORDER BY created_at ASC
	`, userID)
	if err != nil {
		return nil, fmt.Errorf("list visibility rules: %w", err)
	}
	defer rows.Close()

	rules := []VisibilityRule{}
	for rows.Next() {
		var r VisibilityRule
		var createdAt string
		if err := rows.Scan(&r.ID, &r.UserID, &r.RuleType, &r.Value, &createdAt); err != nil {
			return nil, fmt.Errorf("scan visibility rule: %w", err)
		}
		r.CreatedAt = parseTimestamp(createdAt)
		rules = append(rules, r)
	}
	return rules, rows.Err()
}
