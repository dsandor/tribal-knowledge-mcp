package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
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
	// Ensure pgvector is available. Many managed / least-privilege deployments
	// (e.g. AWS Aurora) pre-create the extension and run the app with a user that
	// lacks CREATE EXTENSION rights — and CREATE EXTENSION IF NOT EXISTS can still
	// trip a privilege check there. So read the catalog first (a plain SELECT needs
	// no special privilege) and only attempt creation when it is genuinely missing.
	var hasVector bool
	if err := s.db.QueryRow(
		`SELECT EXISTS (SELECT 1 FROM pg_extension WHERE extname = 'vector')`,
	).Scan(&hasVector); err != nil {
		return fmt.Errorf("check vector extension: %w", err)
	}
	if !hasVector {
		if _, err := s.db.Exec(`CREATE EXTENSION IF NOT EXISTS vector`); err != nil {
			return fmt.Errorf("vector extension is not installed and could not be created "+
				"(the database user may lack CREATE EXTENSION rights — have a superuser run "+
				"'CREATE EXTENSION vector;' once): %w", err)
		}
	}

	_, err := s.db.Exec(`
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

	_, err = s.db.Exec(`
		CREATE TABLE IF NOT EXISTS entry_chunks (
			id             BIGSERIAL PRIMARY KEY,
			entry_id       TEXT NOT NULL REFERENCES entries(id) ON DELETE CASCADE,
			chunk_index    INT NOT NULL,
			content        TEXT NOT NULL,
			token_estimate INT NOT NULL DEFAULT 0,
			UNIQUE(entry_id, chunk_index)
		)
	`)
	if err != nil {
		return fmt.Errorf("create entry_chunks table: %w", err)
	}
	_, _ = s.db.Exec(`CREATE INDEX IF NOT EXISTS idx_entry_chunks_entry ON entry_chunks(entry_id)`)

	_, err = s.db.ExecContext(context.Background(), fmt.Sprintf(`
		CREATE TABLE IF NOT EXISTS chunk_embeddings (
			chunk_id  BIGINT PRIMARY KEY REFERENCES entry_chunks(id) ON DELETE CASCADE,
			embedding vector(%d) NOT NULL
		)
	`, s.embeddingDim))
	if err != nil {
		return fmt.Errorf("create chunk_embeddings table: %w", err)
	}
	_, _ = s.db.Exec(`
		CREATE INDEX IF NOT EXISTS chunk_embeddings_cosine_idx
		ON chunk_embeddings USING ivfflat (embedding vector_cosine_ops)
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
	if err := s.migrateVisibility(context.Background()); err != nil {
		return fmt.Errorf("migrate visibility: %w", err)
	}
	if err := s.migrateShares(context.Background()); err != nil {
		return fmt.Errorf("migrate shares: %w", err)
	}
	if err := s.backfillChunks(context.Background()); err != nil {
		return fmt.Errorf("backfill chunks: %w", err)
	}

	return nil
}

// StoreEntry creates a new entry with a single embedding (legacy single-chunk
// path). It delegates to StoreEntryChunked.
func (s *PostgresStore) StoreEntry(ctx context.Context, entry KnowledgeEntry, embedding []float32) (string, error) {
	return s.StoreEntryChunked(ctx, entry, []EntryChunk{{Index: 0, Content: entry.Content, Embedding: embedding}})
}

// StoreEntryChunked creates a new entry represented by one or more chunks.
// chunks[0] is the representative chunk: its vector is also written to the
// per-entry embeddings table so pipeline queries keep working.
func (s *PostgresStore) StoreEntryChunked(ctx context.Context, entry KnowledgeEntry, chunks []EntryChunk) (string, error) {
	for _, c := range chunks {
		if c.Embedding != nil && len(c.Embedding) != s.embeddingDim {
			return "", fmt.Errorf("embedding dim mismatch: got %d, want %d", len(c.Embedding), s.embeddingDim)
		}
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

	// Representative chunk: write per-entry vector for the pipeline.
	if len(chunks) > 0 && chunks[0].Embedding != nil {
		v := pgvector.NewVector(chunks[0].Embedding)
		if _, err = tx.ExecContext(ctx,
			`INSERT INTO embeddings (entry_id, embedding) VALUES ($1, $2)`, entry.ID, v); err != nil {
			return "", fmt.Errorf("insert embedding: %w", err)
		}
	}

	if err := insertChunksTxPG(ctx, tx, entry.ID, chunks); err != nil {
		return "", err
	}

	if err := tx.Commit(); err != nil {
		return "", err
	}
	return entry.ID, nil
}

// insertChunksTxPG inserts each chunk into entry_chunks and, for chunks with a
// non-nil embedding, into chunk_embeddings keyed by the new entry_chunks id.
func insertChunksTxPG(ctx context.Context, tx *sql.Tx, entryID string, chunks []EntryChunk) error {
	for _, c := range chunks {
		var chunkID int64
		err := tx.QueryRowContext(ctx, `
			INSERT INTO entry_chunks (entry_id, chunk_index, content, token_estimate)
			VALUES ($1, $2, $3, $4)
			RETURNING id
		`, entryID, c.Index, c.Content, c.TokenEstimate).Scan(&chunkID)
		if err != nil {
			return fmt.Errorf("insert entry_chunks: %w", err)
		}
		if c.Embedding == nil {
			continue
		}
		v := pgvector.NewVector(c.Embedding)
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO chunk_embeddings (chunk_id, embedding) VALUES ($1, $2)`, chunkID, v); err != nil {
			return fmt.Errorf("insert chunk_embeddings: %w", err)
		}
	}
	return nil
}

// ReplaceEntryChunks atomically swaps all chunks (and their vectors) for an
// existing entry, and refreshes the representative per-entry vector.
func (s *PostgresStore) ReplaceEntryChunks(ctx context.Context, entryID string, chunks []EntryChunk) error {
	for _, c := range chunks {
		if c.Embedding != nil && len(c.Embedding) != s.embeddingDim {
			return fmt.Errorf("embedding dim mismatch: got %d, want %d", len(c.Embedding), s.embeddingDim)
		}
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	var exists bool
	err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM entries WHERE id = $1)`, entryID).Scan(&exists)
	if err != nil {
		return fmt.Errorf("check entry exists: %w", err)
	}
	if !exists {
		return fmt.Errorf("entry %q: %w", entryID, ErrNotFound)
	}

	// chunk_embeddings cascades when entry_chunks rows are deleted.
	if _, err := tx.ExecContext(ctx, `DELETE FROM entry_chunks WHERE entry_id = $1`, entryID); err != nil {
		return fmt.Errorf("delete entry_chunks: %w", err)
	}

	if err := insertChunksTxPG(ctx, tx, entryID, chunks); err != nil {
		return err
	}

	// Refresh the representative per-entry vector.
	if _, err := tx.ExecContext(ctx, `DELETE FROM embeddings WHERE entry_id = $1`, entryID); err != nil {
		return fmt.Errorf("delete embeddings: %w", err)
	}
	if len(chunks) > 0 && chunks[0].Embedding != nil {
		v := pgvector.NewVector(chunks[0].Embedding)
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO embeddings (entry_id, embedding) VALUES ($1, $2)`, entryID, v); err != nil {
			return fmt.Errorf("insert embedding: %w", err)
		}
	}

	return tx.Commit()
}

// backfillChunks creates a chunk-0 row for every entry that has none, copying
// the per-entry embedding (if present) into chunk_embeddings. Idempotent.
func (s *PostgresStore) backfillChunks(ctx context.Context) error {
	// Insert missing chunk-0 rows.
	if _, err := s.db.ExecContext(ctx, `
		INSERT INTO entry_chunks (entry_id, chunk_index, content, token_estimate)
		SELECT e.id, 0, e.content, 0
		FROM entries e
		WHERE NOT EXISTS (SELECT 1 FROM entry_chunks ec WHERE ec.entry_id = e.id)
	`); err != nil {
		return fmt.Errorf("backfill entry_chunks: %w", err)
	}

	// Copy per-entry embeddings into chunk_embeddings for chunk-0 rows that lack one.
	if _, err := s.db.ExecContext(ctx, `
		INSERT INTO chunk_embeddings (chunk_id, embedding)
		SELECT ec.id, em.embedding
		FROM entry_chunks ec
		JOIN embeddings em ON em.entry_id = ec.entry_id
		WHERE ec.chunk_index = 0
		  AND NOT EXISTS (SELECT 1 FROM chunk_embeddings ce WHERE ce.chunk_id = ec.id)
	`); err != nil {
		return fmt.Errorf("backfill chunk_embeddings: %w", err)
	}
	return nil
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

	type vecResult struct {
		entryID  string
		distance float64
	}

	// Query the chunk vectors and collapse to one best (minimum) distance per
	// entry. A fixed multiple of topK chunk hits can collapse to far fewer than
	// topK distinct entries when a handful of entries each own many of the
	// closest chunks. Use an expanding retrieval: fetch a pool, dedup to entries,
	// and grow the pool while we have fewer than topK distinct entries and the
	// chunk table is not yet exhausted.
	const maxK = 4096
	k := topK * 8
	if k < topK {
		k = topK
	}

	bestDist := make(map[string]float64)
	var order []string
	for {
		bestDist = make(map[string]float64)
		order = order[:0]

		rows, err := s.db.QueryContext(ctx, `
			SELECT ec.entry_id, ce.embedding <=> $1 AS distance
			FROM chunk_embeddings ce
			JOIN entry_chunks ec ON ec.id = ce.chunk_id
			ORDER BY distance
			LIMIT $2
		`, v, k)
		if err != nil {
			return nil, fmt.Errorf("vector search: %w", err)
		}

		rowCount := 0
		for rows.Next() {
			var entryID string
			var distance float64
			if err := rows.Scan(&entryID, &distance); err != nil {
				rows.Close()
				return nil, fmt.Errorf("scan vec result: %w", err)
			}
			rowCount++
			if d, ok := bestDist[entryID]; !ok {
				bestDist[entryID] = distance
				order = append(order, entryID)
			} else if distance < d {
				bestDist[entryID] = distance
			}
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return nil, err
		}
		rows.Close()

		// Stop expanding once we have enough distinct entries, the chunk table is
		// exhausted (returned fewer rows than requested), or we hit the cap.
		if len(order) >= topK || rowCount < k || k >= maxK {
			break
		}
		k *= 4
		if k > maxK {
			k = maxK
		}
	}
	if len(order) == 0 {
		return []SearchResult{}, nil
	}

	sort.SliceStable(order, func(i, j int) bool {
		return bestDist[order[i]] < bestDist[order[j]]
	})
	if len(order) > topK {
		order = order[:topK]
	}
	vecResults := make([]vecResult, len(order))
	for i, id := range order {
		vecResults[i] = vecResult{entryID: id, distance: bestDist[id]}
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
