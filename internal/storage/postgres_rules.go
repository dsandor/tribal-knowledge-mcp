package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

// migrateRules creates the rules table.
func (s *PostgresStore) migrateRules(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS rules (
			id          TEXT PRIMARY KEY,
			title       TEXT NOT NULL DEFAULT '',
			content     TEXT NOT NULL DEFAULT '',
			scope       TEXT NOT NULL DEFAULT 'team',
			scope_value TEXT NOT NULL DEFAULT '',
			priority    INT NOT NULL DEFAULT 0,
			enabled     BOOLEAN NOT NULL DEFAULT TRUE,
			author      TEXT NOT NULL DEFAULT '',
			created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)
	`)
	if err != nil {
		return fmt.Errorf("create rules table: %w", err)
	}
	return nil
}

func (s *PostgresStore) StoreRule(ctx context.Context, rule Rule) (string, error) {
	rule.ID = uuid.NewString()
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO rules (id, title, content, scope, scope_value, priority, enabled, author)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
	`, rule.ID, rule.Title, rule.Content, string(rule.Scope), rule.ScopeValue,
		rule.Priority, rule.Enabled, rule.Author)
	if err != nil {
		return "", fmt.Errorf("insert rule: %w", err)
	}
	return rule.ID, nil
}

func (s *PostgresStore) GetRule(ctx context.Context, id string) (*Rule, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, title, content, scope, scope_value, priority, enabled, author, created_at, updated_at
		FROM rules WHERE id = $1
	`, id)
	r, err := scanRuleRowPG(row.Scan)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("get rule: %w", err)
	}
	return r, nil
}

func (s *PostgresStore) ListRules(ctx context.Context, filter RuleFilter) ([]Rule, error) {
	limit := filter.Limit
	if limit == 0 {
		limit = 50
	}

	args := []any{}
	n := 0
	nextArg := func(v any) string {
		n++
		args = append(args, v)
		return fmt.Sprintf("$%d", n)
	}

	query := `SELECT id, title, content, scope, scope_value, priority, enabled, author, created_at, updated_at
	          FROM rules WHERE 1=1`

	if filter.Scope != "" {
		query += " AND scope = " + nextArg(string(filter.Scope))
	}
	if filter.ScopeValue != "" {
		query += " AND scope_value = " + nextArg(filter.ScopeValue)
	}
	query += " ORDER BY priority DESC, created_at ASC"
	if limit > 0 {
		query += " LIMIT " + nextArg(limit)
	}

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list rules: %w", err)
	}
	defer rows.Close()

	rules := []Rule{}
	for rows.Next() {
		r, err := scanRuleRowPG(rows.Scan)
		if err != nil {
			return nil, fmt.Errorf("scan rule: %w", err)
		}
		rules = append(rules, *r)
	}
	return rules, rows.Err()
}

func (s *PostgresStore) UpdateRule(ctx context.Context, rule Rule) error {
	res, err := s.db.ExecContext(ctx, `
		UPDATE rules
		SET title = $1, content = $2, scope = $3, scope_value = $4,
		    priority = $5, enabled = $6, author = $7,
		    updated_at = NOW()
		WHERE id = $8
	`, rule.Title, rule.Content, string(rule.Scope), rule.ScopeValue,
		rule.Priority, rule.Enabled, rule.Author, rule.ID)
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

func (s *PostgresStore) DeleteRule(ctx context.Context, id string) error {
	res, err := s.db.ExecContext(ctx, "DELETE FROM rules WHERE id = $1", id)
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

func (s *PostgresStore) GetApplicableRules(ctx context.Context, team, category, user string) ([]Rule, error) {
	var conds []string
	var args []any
	n := 0
	nextArg := func(v any) string {
		n++
		args = append(args, v)
		return fmt.Sprintf("$%d", n)
	}

	if team != "" {
		p := nextArg(team)
		conds = append(conds, fmt.Sprintf("(scope = 'team' AND scope_value = %s)", p))
	}
	if category != "" {
		p := nextArg(category)
		conds = append(conds, fmt.Sprintf("(scope = 'category' AND scope_value = %s)", p))
	}
	if user != "" {
		p := nextArg(user)
		conds = append(conds, fmt.Sprintf("(scope = 'user' AND scope_value = %s)", p))
	}

	if len(conds) == 0 {
		return []Rule{}, nil
	}

	query := fmt.Sprintf(`
		SELECT id, title, content, scope, scope_value, priority, enabled, author, created_at, updated_at
		FROM rules
		WHERE enabled = TRUE AND (%s)
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
		r, err := scanRuleRowPG(rows.Scan)
		if err != nil {
			return nil, fmt.Errorf("scan applicable rule: %w", err)
		}
		rules = append(rules, *r)
	}
	return rules, rows.Err()
}

// scanRuleRowPG scans a rule row from PostgreSQL. BOOLEAN scans directly into bool
// and TIMESTAMPTZ scans directly into time.Time (no string parsing needed).
func scanRuleRowPG(scan func(...any) error) (*Rule, error) {
	var r Rule
	var scopeStr string
	var createdAt, updatedAt time.Time
	err := scan(&r.ID, &r.Title, &r.Content, &scopeStr, &r.ScopeValue,
		&r.Priority, &r.Enabled, &r.Author, &createdAt, &updatedAt)
	if err != nil {
		return nil, err
	}
	r.Scope = RuleScope(scopeStr)
	r.CreatedAt = createdAt
	r.UpdatedAt = updatedAt
	return &r, nil
}
