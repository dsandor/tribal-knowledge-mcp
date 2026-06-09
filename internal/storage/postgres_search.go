package storage

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	pgvector "github.com/pgvector/pgvector-go"
)

// migrateSearch adds the generated tsvector column and GIN index to the entries
// table. It is called from PostgresStore.migrate().
func (s *PostgresStore) migrateSearch(ctx context.Context) error {
	stmts := []string{
		`ALTER TABLE entries ADD COLUMN IF NOT EXISTS fts_vector tsvector
		    GENERATED ALWAYS AS (
		        to_tsvector('english',
		            coalesce(title,'') || ' ' ||
		            coalesce(content,'') || ' ' ||
		            coalesce(tags::text,'')
		        )
		    ) STORED`,
		`CREATE INDEX IF NOT EXISTS idx_entries_fts ON entries USING GIN(fts_vector)`,
	}
	for _, stmt := range stmts {
		if _, err := s.db.ExecContext(ctx, stmt); err != nil {
			snippet := stmt
			if len(snippet) > 60 {
				snippet = snippet[:60]
			}
			return fmt.Errorf("search migration: %w: sql=%s", err, snippet)
		}
	}
	return nil
}

// SearchHybrid combines full-text and vector similarity search for PostgreSQL.
func (s *PostgresStore) SearchHybrid(ctx context.Context, teamID string, query string, embedding []float32, mode string, limit int) ([]KnowledgeEntry, error) {
	if limit <= 0 {
		limit = 20
	}

	switch mode {
	case "keyword":
		return s.pgSearchKeyword(ctx, teamID, query, limit)
	case "semantic":
		return s.pgSearchSemantic(ctx, teamID, embedding, limit)
	default: // "hybrid"
		if embedding == nil {
			return s.pgSearchKeyword(ctx, teamID, query, limit)
		}
		return s.pgSearchHybridMerge(ctx, teamID, query, embedding, limit)
	}
}

// pgSearchKeyword uses the generated tsvector column for full-text search.
func (s *PostgresStore) pgSearchKeyword(ctx context.Context, teamID, query string, limit int) ([]KnowledgeEntry, error) {
	if strings.TrimSpace(query) == "" {
		return nil, nil
	}

	args := []any{query, teamID, teamID, limit}
	q := `
		SELECT id, type, title, content, description, domain, tags, author, team,
		       created_at, updated_at, version, rating, usage_count, team_id, status
		FROM entries
		WHERE fts_vector @@ plainto_tsquery('english', $1)
		  AND ($2 = '' OR team_id = $3)
		ORDER BY ts_rank(fts_vector, plainto_tsquery('english', $1)) DESC
		LIMIT $4
	`

	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("pg keyword search: %w", err)
	}
	defer rows.Close()

	var entries []KnowledgeEntry
	for rows.Next() {
		e, err := scanEntryPG(rows)
		if err != nil {
			return nil, fmt.Errorf("scan pg keyword result: %w", err)
		}
		entries = append(entries, *e)
	}
	return entries, rows.Err()
}

// pgSearchSemantic uses pgvector cosine distance, filtered by teamID.
func (s *PostgresStore) pgSearchSemantic(ctx context.Context, teamID string, embedding []float32, limit int) ([]KnowledgeEntry, error) {
	if embedding == nil {
		return nil, nil
	}
	results, err := s.SearchSimilar(ctx, embedding, limit*2)
	if err != nil {
		return nil, err
	}
	out := make([]KnowledgeEntry, 0, len(results))
	for _, r := range results {
		if teamID == "" || r.Entry.TeamID == teamID {
			out = append(out, r.Entry)
			if len(out) >= limit {
				break
			}
		}
	}
	return out, nil
}

// pgSearchHybridMerge runs both searches and merges with 0.5*norm_rank + 0.5*cosine_sim.
func (s *PostgresStore) pgSearchHybridMerge(ctx context.Context, teamID, query string, embedding []float32, limit int) ([]KnowledgeEntry, error) {
	type scoredEntry struct {
		entry KnowledgeEntry
		score float64
	}
	scoreMap := make(map[string]*scoredEntry)

	// Keyword leg.
	if strings.TrimSpace(query) != "" {
		q := `
			SELECT id, type, title, content, description, domain, tags, author, team,
			       created_at, updated_at, version, rating, usage_count, team_id, status,
			       ts_rank(fts_vector, plainto_tsquery('english', $1)) AS rank
			FROM entries
			WHERE fts_vector @@ plainto_tsquery('english', $1)
			  AND ($2 = '' OR team_id = $3)
			ORDER BY rank DESC
			LIMIT $4
		`
		rows, err := s.db.QueryContext(ctx, q, query, teamID, teamID, limit*2)
		if err != nil {
			return nil, fmt.Errorf("pg hybrid keyword: %w", err)
		}
		defer rows.Close()

		type rankRow struct {
			e    KnowledgeEntry
			rank float64
		}
		var rankRows []rankRow
		maxRank := 0.0
		for rows.Next() {
			e, rank, err := scanEntryPGWithRank(rows)
			if err != nil {
				return nil, fmt.Errorf("scan pg hybrid keyword row: %w", err)
			}
			if rank > maxRank {
				maxRank = rank
			}
			rankRows = append(rankRows, rankRow{e: *e, rank: rank})
		}
		if err := rows.Err(); err != nil {
			return nil, err
		}
		for _, r := range rankRows {
			norm := 0.0
			if maxRank > 0 {
				norm = r.rank / maxRank
			}
			scoreMap[r.e.ID] = &scoredEntry{entry: r.e, score: 0.5 * norm}
		}
	}

	// Semantic leg.
	if embedding != nil {
		v := pgvector.NewVector(embedding)
		semRows, err := s.db.QueryContext(ctx, `
			SELECT entry_id, embedding <=> $1 AS distance
			FROM embeddings
			ORDER BY distance
			LIMIT $2
		`, v, limit*2)
		if err != nil {
			return nil, fmt.Errorf("pg hybrid semantic: %w", err)
		}
		defer semRows.Close()

		type semResult struct {
			entryID  string
			distance float64
		}
		var semResults []semResult
		for semRows.Next() {
			var r semResult
			if err := semRows.Scan(&r.entryID, &r.distance); err != nil {
				return nil, fmt.Errorf("scan pg hybrid sem: %w", err)
			}
			semResults = append(semResults, r)
		}
		if err := semRows.Err(); err != nil {
			return nil, err
		}

		if len(semResults) > 0 {
			idArgs := make([]any, len(semResults))
			placeholders := make([]string, len(semResults))
			for i, r := range semResults {
				idArgs[i] = r.entryID
				placeholders[i] = fmt.Sprintf("$%d", i+1)
			}
			rows2, err := s.db.QueryContext(ctx,
				fmt.Sprintf(`SELECT id, type, title, content, description, domain, tags, author, team,
				       created_at, updated_at, version, rating, usage_count, team_id, status
				FROM entries WHERE id IN (%s)`, strings.Join(placeholders, ",")),
				idArgs...,
			)
			if err != nil {
				return nil, fmt.Errorf("pg hybrid sem fetch entries: %w", err)
			}
			defer rows2.Close()

			entryMap := make(map[string]*KnowledgeEntry)
			for rows2.Next() {
				e, err := scanEntryPG(rows2)
				if err != nil {
					return nil, err
				}
				entryMap[e.ID] = e
			}
			if err := rows2.Err(); err != nil {
				return nil, err
			}

			for _, r := range semResults {
				e, ok := entryMap[r.entryID]
				if !ok {
					continue
				}
				if teamID != "" && e.TeamID != teamID {
					continue
				}
				// cosine distance [0,2]; similarity = 1 - distance/2
				similarity := 1.0 - r.distance/2.0
				if se, exists := scoreMap[e.ID]; exists {
					se.score += 0.5 * similarity
				} else {
					scoreMap[e.ID] = &scoredEntry{entry: *e, score: 0.5 * similarity}
				}
			}
		}
	}

	// Sort descending by score (insertion sort — result sets are small).
	scored := make([]*scoredEntry, 0, len(scoreMap))
	for _, se := range scoreMap {
		scored = append(scored, se)
	}
	for i := 1; i < len(scored); i++ {
		for j := i; j > 0 && scored[j].score > scored[j-1].score; j-- {
			scored[j], scored[j-1] = scored[j-1], scored[j]
		}
	}
	if len(scored) > limit {
		scored = scored[:limit]
	}
	out := make([]KnowledgeEntry, len(scored))
	for i, se := range scored {
		out[i] = se.entry
	}
	return out, nil
}

// scanEntryPGWithRank scans a row with the standard 16 entry columns followed by a ts_rank float.
func scanEntryPGWithRank(row rowScanner) (*KnowledgeEntry, float64, error) {
	var e KnowledgeEntry
	var tagsRaw []byte
	var createdAt, updatedAt time.Time
	var rank float64

	err := row.Scan(
		&e.ID, &e.Type, &e.Title, &e.Content, &e.Description,
		&e.Domain, &tagsRaw, &e.Author, &e.Team,
		&createdAt, &updatedAt, &e.Version,
		&e.Rating, &e.UsageCount, &e.TeamID, &e.Status, &rank,
	)
	if err != nil {
		return nil, 0, err
	}
	if err := json.Unmarshal(tagsRaw, &e.Tags); err != nil {
		e.Tags = []string{}
	}
	e.CreatedAt = createdAt
	e.UpdatedAt = updatedAt
	return &e, rank, nil
}
