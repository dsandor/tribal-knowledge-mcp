package storage

import (
	"context"
	"database/sql"
	"fmt"
)

// GetEnrichmentPrefs reads the per-user enrichment prefs row plus all rule rows.
// A missing prefs row yields zero scalars with all *Set flags false (the caller
// applies deployment defaults). Rule lists are always non-nil but may be empty.
func (s *PostgresStore) GetEnrichmentPrefs(ctx context.Context, userID string) (*EnrichmentPrefs, error) {
	p := &EnrichmentPrefs{
		AllowDomains:  []string{},
		DenyDomains:   []string{},
		AllowTags:     []string{},
		DenyTags:      []string{},
		PinnedEntries: []string{},
	}

	var minRel sql.NullFloat64
	var maxMem, llmRewrite sql.NullInt64
	err := s.db.QueryRowContext(ctx, `
		SELECT min_relevance, max_memories, llm_rewrite
		FROM enrichment_prefs
		WHERE user_id = $1
	`, userID).Scan(&minRel, &maxMem, &llmRewrite)
	if err != nil && err != sql.ErrNoRows {
		return nil, fmt.Errorf("get enrichment prefs: %w", err)
	}
	if err == nil {
		if minRel.Valid {
			p.MinRelevance = minRel.Float64
			p.MinRelevanceSet = true
		}
		if maxMem.Valid {
			p.MaxMemories = int(maxMem.Int64)
			p.MaxMemoriesSet = true
		}
		if llmRewrite.Valid {
			p.LLMRewrite = llmRewrite.Int64 != 0
			p.LLMRewriteSet = true
		}
	}

	rows, err := s.db.QueryContext(ctx, `
		SELECT kind, value FROM enrichment_rules WHERE user_id = $1 ORDER BY created_at ASC, value ASC
	`, userID)
	if err != nil {
		return nil, fmt.Errorf("get enrichment rules: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var kind, value string
		if err := rows.Scan(&kind, &value); err != nil {
			return nil, fmt.Errorf("scan enrichment rule: %w", err)
		}
		bucketEnrichmentRule(p, kind, value)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate enrichment rules: %w", err)
	}
	return p, nil
}

// PutEnrichmentPrefs upserts the prefs row. A nil pointer writes SQL NULL
// (revert to default); a non-nil pointer writes the value.
func (s *PostgresStore) PutEnrichmentPrefs(ctx context.Context, userID string, minRel *float64, maxMem *int, llmRewrite *bool) error {
	var minRelArg, maxMemArg, llmArg interface{}
	if minRel != nil {
		minRelArg = *minRel
	}
	if maxMem != nil {
		maxMemArg = *maxMem
	}
	if llmRewrite != nil {
		if *llmRewrite {
			llmArg = 1
		} else {
			llmArg = 0
		}
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO enrichment_prefs (user_id, min_relevance, max_memories, llm_rewrite, updated_at)
		VALUES ($1, $2, $3, $4, now())
		ON CONFLICT (user_id) DO UPDATE SET
			min_relevance = EXCLUDED.min_relevance,
			max_memories  = EXCLUDED.max_memories,
			llm_rewrite   = EXCLUDED.llm_rewrite,
			updated_at    = now()
	`, userID, minRelArg, maxMemArg, llmArg)
	if err != nil {
		return fmt.Errorf("put enrichment prefs: %w", err)
	}
	return nil
}

// ReplaceEnrichmentRules replaces all rules of a kind for a user atomically.
func (s *PostgresStore) ReplaceEnrichmentRules(ctx context.Context, userID, kind string, values []string) error {
	if !enrichmentRuleKinds[kind] {
		return fmt.Errorf("invalid enrichment rule kind: %q", kind)
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, `DELETE FROM enrichment_rules WHERE user_id = $1 AND kind = $2`, userID, kind); err != nil {
		return fmt.Errorf("delete enrichment rules: %w", err)
	}
	for _, v := range values {
		nv := normalizeEnrichmentValue(kind, v)
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO enrichment_rules (user_id, kind, value) VALUES ($1, $2, $3)
			ON CONFLICT (user_id, kind, value) DO NOTHING
		`, userID, kind, nv); err != nil {
			return fmt.Errorf("insert enrichment rule: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit tx: %w", err)
	}
	return nil
}

// AddEnrichmentRule idempotently inserts a single rule.
func (s *PostgresStore) AddEnrichmentRule(ctx context.Context, userID, kind, value string) error {
	if !enrichmentRuleKinds[kind] {
		return fmt.Errorf("invalid enrichment rule kind: %q", kind)
	}
	nv := normalizeEnrichmentValue(kind, value)
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO enrichment_rules (user_id, kind, value) VALUES ($1, $2, $3)
		ON CONFLICT (user_id, kind, value) DO NOTHING
	`, userID, kind, nv)
	if err != nil {
		return fmt.Errorf("add enrichment rule: %w", err)
	}
	return nil
}

// RemoveEnrichmentRule deletes a single rule by (user, kind, value). No-op if absent.
func (s *PostgresStore) RemoveEnrichmentRule(ctx context.Context, userID, kind, value string) error {
	if !enrichmentRuleKinds[kind] {
		return fmt.Errorf("invalid enrichment rule kind: %q", kind)
	}
	nv := normalizeEnrichmentValue(kind, value)
	_, err := s.db.ExecContext(ctx, `
		DELETE FROM enrichment_rules WHERE user_id = $1 AND kind = $2 AND value = $3
	`, userID, kind, nv)
	if err != nil {
		return fmt.Errorf("remove enrichment rule: %w", err)
	}
	return nil
}
