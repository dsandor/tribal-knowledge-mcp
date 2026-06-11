package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	_ "github.com/jackc/pgx/v5/stdlib"
	pgvector "github.com/pgvector/pgvector-go"
)

// Compile-time checks that *PostgresStore satisfies every store interface.
// AgentStore embeds AnalysisStore which embeds Store, so one assertion covers all three base interfaces.
var _ AgentStore = (*PostgresStore)(nil)
var _ TeamStore = (*PostgresStore)(nil)
var _ RuleStore = (*PostgresStore)(nil)

type PostgresStore struct {
	db           *sql.DB
	embeddingDim int
}

func NewPostgresStore(dsn string, embeddingDim int) (*PostgresStore, error) {
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return nil, fmt.Errorf("open postgres: %w", err)
	}
	s := &PostgresStore{db: db, embeddingDim: embeddingDim}
	if err := s.migrate(); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}
	return s, nil
}

func (s *PostgresStore) migrate() error {
	_, err := s.db.Exec(`CREATE EXTENSION IF NOT EXISTS vector`)
	if err != nil {
		return fmt.Errorf("create vector extension: %w", err)
	}

	_, err = s.db.Exec(`
		CREATE TABLE IF NOT EXISTS entries (
			id           TEXT PRIMARY KEY,
			type         TEXT NOT NULL DEFAULT '',
			title        TEXT NOT NULL DEFAULT '',
			content      TEXT NOT NULL DEFAULT '',
			description  TEXT NOT NULL DEFAULT '',
			domain       TEXT NOT NULL DEFAULT '',
			tags         JSONB NOT NULL DEFAULT '[]',
			author       TEXT NOT NULL DEFAULT '',
			team         TEXT NOT NULL DEFAULT '',
			team_id      TEXT NOT NULL DEFAULT '',
			created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			updated_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			version      INT NOT NULL DEFAULT 1,
			rating       FLOAT NOT NULL DEFAULT 0,
			usage_count  INT NOT NULL DEFAULT 0,
			status       TEXT NOT NULL DEFAULT 'pending',
			content_hash TEXT NOT NULL DEFAULT ''
		)
	`)
	if err != nil {
		return fmt.Errorf("create entries table: %w", err)
	}

	// Idempotent: add content_hash column to existing databases.
	_, _ = s.db.Exec(`ALTER TABLE entries ADD COLUMN IF NOT EXISTS content_hash TEXT NOT NULL DEFAULT ''`)
	// Idempotent: add auto_tags column to existing databases.
	_, _ = s.db.Exec(`ALTER TABLE entries ADD COLUMN IF NOT EXISTS auto_tags JSONB NOT NULL DEFAULT '[]'`)

	_, err = s.db.ExecContext(context.Background(), fmt.Sprintf(`
		CREATE TABLE IF NOT EXISTS embeddings (
			entry_id  TEXT PRIMARY KEY REFERENCES entries(id) ON DELETE CASCADE,
			embedding vector(%d) NOT NULL
		)
	`, s.embeddingDim))
	if err != nil {
		return fmt.Errorf("create embeddings table: %w", err)
	}

	// ivfflat index requires at least one row to build; errors here are non-fatal.
	_, _ = s.db.Exec(`
		CREATE INDEX IF NOT EXISTS embeddings_cosine_idx
		ON embeddings USING ivfflat (embedding vector_cosine_ops)
		WITH (lists = 100)
	`)

	ctx := context.Background()
	if err := s.migrateAnalysis(ctx); err != nil {
		return fmt.Errorf("migrate analysis: %w", err)
	}
	if err := s.migrateAgents(ctx); err != nil {
		return fmt.Errorf("migrate agents: %w", err)
	}
	if err := s.migrateTeams(context.Background()); err != nil {
		return fmt.Errorf("migrate teams: %w", err)
	}
	if err := s.migrateRules(context.Background()); err != nil {
		return fmt.Errorf("migrate rules: %w", err)
	}
	if err := s.migrateUsage(context.Background()); err != nil {
		return fmt.Errorf("migrate usage: %w", err)
	}
	if err := s.migrateSearch(context.Background()); err != nil {
		return fmt.Errorf("migrate search: %w", err)
	}

	return nil
}

func (s *PostgresStore) StoreEntry(ctx context.Context, entry KnowledgeEntry, embedding []float32) (string, error) {
	if embedding != nil && len(embedding) != s.embeddingDim {
		return "", fmt.Errorf("embedding dim mismatch: got %d, want %d", len(embedding), s.embeddingDim)
	}

	entry.ID = uuid.NewString()

	tagsJSON, err := json.Marshal(entry.Tags)
	if err != nil {
		return "", fmt.Errorf("marshal tags: %w", err)
	}
	autoTagsJSON, err := json.Marshal(entry.AutoTags)
	if err != nil {
		return "", fmt.Errorf("marshal auto tags: %w", err)
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return "", fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	contentHash := sha256Hex(entry.Title + entry.Content)

	_, err = tx.ExecContext(ctx, `
		INSERT INTO entries
			(id, type, title, content, description, domain, tags, auto_tags, author, team, team_id, status, content_hash)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)
	`,
		entry.ID, string(entry.Type), entry.Title, entry.Content,
		entry.Description, entry.Domain, string(tagsJSON), string(autoTagsJSON),
		entry.Author, entry.Team, entry.TeamID, statusOrDefault(entry.Status), contentHash,
	)
	if err != nil {
		return "", fmt.Errorf("insert entry: %w", err)
	}

	if embedding != nil {
		v := pgvector.NewVector(embedding)
		_, err = tx.ExecContext(ctx,
			`INSERT INTO embeddings (entry_id, embedding) VALUES ($1, $2)`,
			entry.ID, v,
		)
		if err != nil {
			return "", fmt.Errorf("insert embedding: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return "", err
	}
	return entry.ID, nil
}

func (s *PostgresStore) GetEntry(ctx context.Context, id string) (*KnowledgeEntry, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, type, title, content, description, domain, tags, auto_tags, author, team,
		       created_at, updated_at, version, rating, usage_count, team_id, status
		FROM entries WHERE id = $1
	`, id)
	entry, err := scanEntryPG(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("entry %q: %w", id, ErrNotFound)
		}
		return nil, fmt.Errorf("get entry: %w", err)
	}
	return entry, nil
}

func (s *PostgresStore) ListEntries(ctx context.Context, filter ListFilter) ([]KnowledgeEntry, error) {
	limit := filter.Limit
	if limit <= 0 {
		limit = 50
	}

	args := []any{}
	n := 0
	nextArg := func(v any) string {
		n++
		args = append(args, v)
		return fmt.Sprintf("$%d", n)
	}

	query := `
		SELECT id, type, title, content, description, domain, tags, auto_tags, author, team,
		       created_at, updated_at, version, rating, usage_count, team_id, status
		FROM entries WHERE 1=1`

	if filter.Domain != "" {
		query += " AND domain = " + nextArg(filter.Domain)
	}
	if filter.Type != "" {
		query += " AND type = " + nextArg(string(filter.Type))
	}
	if filter.Search != "" {
		escaped := strings.ReplaceAll(filter.Search, `\`, `\\`)
		escaped = strings.ReplaceAll(escaped, `%`, `\%`)
		escaped = strings.ReplaceAll(escaped, `_`, `\_`)
		pattern := "%" + escaped + "%"
		p1 := nextArg(pattern)
		p2 := nextArg(pattern)
		query += fmt.Sprintf(" AND (title ILIKE %s OR content ILIKE %s)", p1, p2)
	}
	if filter.Status != "" {
		query += " AND status = " + nextArg(filter.Status)
	}
	if filter.TeamID != "" {
		query += " AND team_id = " + nextArg(filter.TeamID)
	}
	if filter.Tag != "" {
		p1 := nextArg(filter.Tag)
		p2 := nextArg(filter.Tag)
		query += fmt.Sprintf(" AND (jsonb_exists(tags, %s) OR jsonb_exists(auto_tags, %s))", p1, p2)
	}

	query += " ORDER BY created_at DESC"
	query += " LIMIT " + nextArg(limit)
	if filter.Offset > 0 {
		query += " OFFSET " + nextArg(filter.Offset)
	}

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list entries: %w", err)
	}
	defer rows.Close()

	var entries []KnowledgeEntry
	for rows.Next() {
		entry, err := scanEntryPG(rows)
		if err != nil {
			return nil, fmt.Errorf("scan entry: %w", err)
		}
		entries = append(entries, *entry)
	}
	return entries, rows.Err()
}

func (s *PostgresStore) DeleteEntry(ctx context.Context, id string) error {
	// ON DELETE CASCADE handles the embeddings row; just confirm the entry exists.
	res, err := s.db.ExecContext(ctx, `DELETE FROM entries WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("delete entry: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("entry %q: %w", id, ErrNotFound)
	}
	return nil
}

func (s *PostgresStore) SearchSimilar(ctx context.Context, embedding []float32, topK int) ([]SearchResult, error) {
	if len(embedding) != s.embeddingDim {
		return nil, fmt.Errorf("embedding dim mismatch: got %d, want %d", len(embedding), s.embeddingDim)
	}

	v := pgvector.NewVector(embedding)

	rows, err := s.db.QueryContext(ctx, `
		SELECT entry_id, embedding <=> $1 AS distance
		FROM embeddings
		ORDER BY distance
		LIMIT $2
	`, v, topK)
	if err != nil {
		return nil, fmt.Errorf("vector search: %w", err)
	}
	defer rows.Close()

	type vecResult struct {
		entryID  string
		distance float64
	}
	var vecResults []vecResult
	for rows.Next() {
		var r vecResult
		if err := rows.Scan(&r.entryID, &r.distance); err != nil {
			return nil, fmt.Errorf("scan vec result: %w", err)
		}
		vecResults = append(vecResults, r)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(vecResults) == 0 {
		return []SearchResult{}, nil
	}

	// Build an IN clause with $N placeholders preserving order.
	idArgs := make([]any, len(vecResults))
	placeholders := make([]string, len(vecResults))
	for i, r := range vecResults {
		idArgs[i] = r.entryID
		placeholders[i] = fmt.Sprintf("$%d", i+1)
	}

	rows2, err := s.db.QueryContext(ctx,
		fmt.Sprintf(`
			SELECT id, type, title, content, description, domain, tags, auto_tags, author, team,
			       created_at, updated_at, version, rating, usage_count, team_id, status
			FROM entries WHERE id IN (%s)
		`, strings.Join(placeholders, ",")),
		idArgs...,
	)
	if err != nil {
		return nil, fmt.Errorf("fetch entries by id: %w", err)
	}
	defer rows2.Close()

	entryMap := make(map[string]*KnowledgeEntry, len(vecResults))
	for rows2.Next() {
		entry, err := scanEntryPG(rows2)
		if err != nil {
			return nil, fmt.Errorf("scan search entry: %w", err)
		}
		entryMap[entry.ID] = entry
	}
	if err := rows2.Err(); err != nil {
		return nil, fmt.Errorf("rows error after fetch: %w", err)
	}

	results := make([]SearchResult, 0, len(vecResults))
	for _, r := range vecResults {
		e, ok := entryMap[r.entryID]
		if !ok {
			return nil, fmt.Errorf("entry %q: %w", r.entryID, ErrNotFound)
		}
		results = append(results, SearchResult{Entry: *e, Score: r.distance})
	}
	return results, nil
}

func (s *PostgresStore) RateEntry(ctx context.Context, id string, rating float64) error {
	res, err := s.db.ExecContext(ctx,
		`UPDATE entries SET rating = $1, updated_at = NOW() WHERE id = $2`,
		rating, id,
	)
	if err != nil {
		return fmt.Errorf("rate entry: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("entry %q: %w", id, ErrNotFound)
	}
	return nil
}

func (s *PostgresStore) ApproveEntry(ctx context.Context, id string) error {
	res, err := s.db.ExecContext(ctx,
		`UPDATE entries SET status = 'approved', updated_at = NOW() WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("approve entry: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("entry %q: %w", id, ErrNotFound)
	}
	return nil
}

func (s *PostgresStore) RejectEntry(ctx context.Context, id string) error {
	res, err := s.db.ExecContext(ctx,
		`UPDATE entries SET status = 'rejected', updated_at = NOW() WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("reject entry: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("entry %q: %w", id, ErrNotFound)
	}
	return nil
}

func (s *PostgresStore) UpdateEntry(ctx context.Context, entry KnowledgeEntry) error {
	tagsJSON, _ := json.Marshal(entry.Tags)
	res, err := s.db.ExecContext(ctx, `
		UPDATE entries
		SET title = $1, content = $2, description = $3, domain = $4, tags = $5, updated_at = NOW()
		WHERE id = $6
	`, entry.Title, entry.Content, entry.Description, entry.Domain, string(tagsJSON), entry.ID)
	if err != nil {
		return fmt.Errorf("update entry: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("entry %q: %w", entry.ID, ErrNotFound)
	}
	return nil
}

func (s *PostgresStore) UpdateAutoTags(ctx context.Context, id string, tags []string) error {
	autoTagsJSON, err := json.Marshal(tags)
	if err != nil {
		return fmt.Errorf("marshal auto tags: %w", err)
	}
	res, err := s.db.ExecContext(ctx,
		`UPDATE entries SET auto_tags = $1, updated_at = NOW() WHERE id = $2`,
		string(autoTagsJSON), id,
	)
	if err != nil {
		return fmt.Errorf("update auto tags: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("entry %q: %w", id, ErrNotFound)
	}
	return nil
}

func (s *PostgresStore) BackfillTeamID(ctx context.Context, teamID string) error {
	if teamID == "" {
		return nil
	}
	for _, table := range []string{"entries", "clusters", "agents", "agent_versions", "dataset_snapshots", "pipeline_runs"} {
		if _, err := s.db.ExecContext(ctx,
			fmt.Sprintf("UPDATE %s SET team_id = $1 WHERE team_id = ''", table), teamID); err != nil {
			return fmt.Errorf("backfill %s: %w", table, err)
		}
	}
	return nil
}

func (s *PostgresStore) Ping(ctx context.Context) error {
	return s.db.PingContext(ctx)
}

func (s *PostgresStore) Close() error {
	return s.db.Close()
}

// scanEntryPG scans a row from the entries table. Unlike the SQLite variant,
// TIMESTAMPTZ columns scan directly into time.Time with the pgx driver.
func scanEntryPG(row rowScanner) (*KnowledgeEntry, error) {
	var e KnowledgeEntry
	var tagsRaw []byte
	var autoTagsRaw []byte
	var createdAt, updatedAt time.Time

	err := row.Scan(
		&e.ID, &e.Type, &e.Title, &e.Content, &e.Description,
		&e.Domain, &tagsRaw, &autoTagsRaw, &e.Author, &e.Team,
		&createdAt, &updatedAt, &e.Version,
		&e.Rating, &e.UsageCount, &e.TeamID, &e.Status,
	)
	if err != nil {
		return nil, err
	}

	if err := json.Unmarshal(tagsRaw, &e.Tags); err != nil {
		e.Tags = []string{}
	}
	if err := json.Unmarshal(autoTagsRaw, &e.AutoTags); err != nil {
		e.AutoTags = []string{}
	}

	e.CreatedAt = createdAt
	e.UpdatedAt = updatedAt
	return &e, nil
}

// statusOrDefault returns s if non-empty, otherwise "pending".
func statusOrDefault(s string) string {
	if s == "" {
		return "pending"
	}
	return s
}

func (s *PostgresStore) GetEntryByContentHash(ctx context.Context, hash string) (*KnowledgeEntry, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, type, title, content, description, domain, tags, auto_tags, author, team,
		       created_at, updated_at, version, rating, usage_count, team_id, status
		FROM entries WHERE content_hash = $1 LIMIT 1
	`, hash)
	entry, err := scanEntryPG(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("get entry by content hash: %w", err)
	}
	return entry, nil
}
