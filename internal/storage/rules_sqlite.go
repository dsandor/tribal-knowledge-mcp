package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
)

func (s *SQLiteStore) StoreRule(ctx context.Context, rule Rule) (string, error) {
	rule.ID = uuid.NewString()
	enabled := 1
	if !rule.Enabled {
		enabled = 0
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO rules (id, title, content, scope, scope_value, priority, enabled, author)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`, rule.ID, rule.Title, rule.Content, string(rule.Scope), rule.ScopeValue,
		rule.Priority, enabled, rule.Author)
	if err != nil {
		return "", fmt.Errorf("insert rule: %w", err)
	}
	return rule.ID, nil
}

func (s *SQLiteStore) GetRule(ctx context.Context, id string) (*Rule, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, title, content, scope, scope_value, priority, enabled, author, created_at, updated_at
		FROM rules WHERE id = ?
	`, id)
	r, err := scanRuleRow(row.Scan)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("get rule: %w", err)
	}
	return r, nil
}

func (s *SQLiteStore) ListRules(ctx context.Context, filter RuleFilter) ([]Rule, error) {
	limit := filter.Limit
	if limit == 0 {
		limit = 50
	}

	query := `SELECT id, title, content, scope, scope_value, priority, enabled, author, created_at, updated_at
	          FROM rules WHERE 1=1`
	args := []any{}

	if filter.Scope != "" {
		query += " AND scope = ?"
		args = append(args, string(filter.Scope))
	}
	if filter.ScopeValue != "" {
		query += " AND scope_value = ?"
		args = append(args, filter.ScopeValue)
	}
	query += " ORDER BY priority DESC, created_at ASC"
	if limit > 0 {
		query += " LIMIT ?"
		args = append(args, limit)
	}

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list rules: %w", err)
	}
	defer rows.Close()

	rules := []Rule{}
	for rows.Next() {
		r, err := scanRuleRow(rows.Scan)
		if err != nil {
			return nil, fmt.Errorf("scan rule: %w", err)
		}
		rules = append(rules, *r)
	}
	return rules, rows.Err()
}

func (s *SQLiteStore) UpdateRule(ctx context.Context, rule Rule) error {
	enabled := 1
	if !rule.Enabled {
		enabled = 0
	}
	res, err := s.db.ExecContext(ctx, `
		UPDATE rules
		SET title = ?, content = ?, scope = ?, scope_value = ?,
		    priority = ?, enabled = ?, author = ?,
		    updated_at = CURRENT_TIMESTAMP
		WHERE id = ?
	`, rule.Title, rule.Content, string(rule.Scope), rule.ScopeValue,
		rule.Priority, enabled, rule.Author, rule.ID)
	if err != nil {
		return fmt.Errorf("update rule: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("rows affected: %w", err)
	}
	if n == 0 {
		return fmt.Errorf("rule %q: %w", rule.ID, ErrNotFound)
	}
	return nil
}

func (s *SQLiteStore) DeleteRule(ctx context.Context, id string) error {
	res, err := s.db.ExecContext(ctx, "DELETE FROM rules WHERE id = ?", id)
	if err != nil {
		return fmt.Errorf("delete rule: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("rows affected: %w", err)
	}
	if n == 0 {
		return fmt.Errorf("rule %q: %w", id, ErrNotFound)
	}
	return nil
}

func (s *SQLiteStore) GetApplicableRules(ctx context.Context, team, category, user string) ([]Rule, error) {
	var conds []string
	var args []any

	if team != "" {
		conds = append(conds, "(scope = 'team' AND scope_value = ?)")
		args = append(args, team)
	}
	if category != "" {
		conds = append(conds, "(scope = 'category' AND scope_value = ?)")
		args = append(args, category)
	}
	if user != "" {
		conds = append(conds, "(scope = 'user' AND scope_value = ?)")
		args = append(args, user)
	}

	if len(conds) == 0 {
		return []Rule{}, nil
	}

	query := fmt.Sprintf(`
		SELECT id, title, content, scope, scope_value, priority, enabled, author, created_at, updated_at
		FROM rules
		WHERE enabled = 1 AND (%s)
		ORDER BY
		  CASE scope WHEN 'team' THEN 1 WHEN 'category' THEN 2 WHEN 'user' THEN 3 END ASC,
		  priority DESC,
		  created_at ASC
	`, strings.Join(conds, " OR "))

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("get applicable rules: %w", err)
	}
	defer rows.Close()

	rules := []Rule{}
	for rows.Next() {
		r, err := scanRuleRow(rows.Scan)
		if err != nil {
			return nil, fmt.Errorf("scan applicable rule: %w", err)
		}
		rules = append(rules, *r)
	}
	return rules, rows.Err()
}

// scanRuleRow scans a rule using a variadic Scan func (works for both *sql.Row and *sql.Rows).
func scanRuleRow(scan func(...any) error) (*Rule, error) {
	var r Rule
	var scopeStr string
	var enabledInt int
	var createdAt, updatedAt string
	err := scan(&r.ID, &r.Title, &r.Content, &scopeStr, &r.ScopeValue,
		&r.Priority, &enabledInt, &r.Author, &createdAt, &updatedAt)
	if err != nil {
		return nil, err
	}
	r.Scope = RuleScope(scopeStr)
	r.Enabled = enabledInt != 0
	r.CreatedAt = parseTimestamp(createdAt)
	r.UpdatedAt = parseTimestamp(updatedAt)
	return &r, nil
}

var _ RuleStore = (*SQLiteStore)(nil)
