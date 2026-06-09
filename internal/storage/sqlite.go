package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	vec "github.com/asg017/sqlite-vec-go-bindings/cgo"
	"github.com/google/uuid"
	_ "github.com/mattn/go-sqlite3"
)

func init() {
	vec.Auto()
}

type SQLiteStore struct {
	db           *sql.DB
	embeddingDim int
}

func NewSQLiteStore(path string, embeddingDim int) (*SQLiteStore, error) {
	db, err := sql.Open("sqlite3", path+"?_journal_mode=WAL&_foreign_keys=on")
	if err != nil {
		return nil, fmt.Errorf("open db: %w", err)
	}
	s := &SQLiteStore{db: db, embeddingDim: embeddingDim}
	if err := s.migrate(); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}
	return s, nil
}

func (s *SQLiteStore) migrate() error {
	_, err := s.db.Exec(`
		CREATE TABLE IF NOT EXISTS entries (
			rowid       INTEGER PRIMARY KEY AUTOINCREMENT,
			id          TEXT    UNIQUE NOT NULL,
			type        TEXT    NOT NULL,
			title       TEXT    NOT NULL,
			content     TEXT    NOT NULL,
			description TEXT    DEFAULT '',
			domain      TEXT    DEFAULT '',
			tags        TEXT    DEFAULT '[]',
			author      TEXT    DEFAULT '',
			team        TEXT    DEFAULT '',
			created_at  DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at  DATETIME DEFAULT CURRENT_TIMESTAMP,
			version     INTEGER DEFAULT 1
		);
	`)
	if err != nil {
		return fmt.Errorf("create entries table: %w", err)
	}

	_, err = s.db.Exec(fmt.Sprintf(`
		CREATE VIRTUAL TABLE IF NOT EXISTS vec_entries USING vec0(
			embedding FLOAT[%d]
		);
	`, s.embeddingDim))
	if err != nil {
		return fmt.Errorf("create vec_entries table: %w", err)
	}

	_, err = s.db.Exec(`
		CREATE TABLE IF NOT EXISTS entry_embeddings (
			rowid     INTEGER PRIMARY KEY REFERENCES entries(rowid) ON DELETE CASCADE,
			embedding BLOB NOT NULL
		);
	`)
	if err != nil {
		return fmt.Errorf("create entry_embeddings table: %w", err)
	}

	// Idempotent ALTER TABLE — ignore "duplicate column name" errors.
	for _, col := range []string{
		"ALTER TABLE entries ADD COLUMN rating REAL DEFAULT 0.0",
		"ALTER TABLE entries ADD COLUMN usage_count INTEGER DEFAULT 0",
		"ALTER TABLE entries ADD COLUMN content_hash TEXT NOT NULL DEFAULT ''",
	} {
		if _, err := s.db.Exec(col); err != nil {
			if !strings.Contains(err.Error(), "duplicate column name") {
				return fmt.Errorf("alter entries: %w", err)
			}
		}
	}

	_, err = s.db.Exec(`
		CREATE TABLE IF NOT EXISTS clusters (
			id              TEXT PRIMARY KEY,
			domain          TEXT    DEFAULT '',
			title           TEXT    NOT NULL,
			summary         TEXT    DEFAULT '',
			entry_ids       TEXT    DEFAULT '[]',
			quality_score   REAL    DEFAULT 0.0,
			pipeline_run_id TEXT    DEFAULT '',
			created_at      DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at      DATETIME DEFAULT CURRENT_TIMESTAMP
		);
	`)
	if err != nil {
		return fmt.Errorf("create clusters table: %w", err)
	}

	_, err = s.db.Exec(`
		CREATE TABLE IF NOT EXISTS pipeline_runs (
			id                TEXT PRIMARY KEY,
			status            TEXT    NOT NULL DEFAULT 'running',
			trigger           TEXT    DEFAULT '',
			entries_processed INTEGER DEFAULT 0,
			clusters_found    INTEGER DEFAULT 0,
			errors            TEXT    DEFAULT '[]',
			started_at        DATETIME DEFAULT CURRENT_TIMESTAMP,
			completed_at      DATETIME
		);
	`)
	if err != nil {
		return fmt.Errorf("create pipeline_runs table: %w", err)
	}

	_, err = s.db.Exec(`
		CREATE TABLE IF NOT EXISTS dataset_snapshots (
			id              TEXT PRIMARY KEY,
			version         INTEGER NOT NULL,
			cluster_count   INTEGER DEFAULT 0,
			entry_count     INTEGER DEFAULT 0,
			data            TEXT    DEFAULT '{}',
			pipeline_run_id TEXT    DEFAULT '',
			created_at      DATETIME DEFAULT CURRENT_TIMESTAMP
		);
	`)
	if err != nil {
		return fmt.Errorf("create dataset_snapshots table: %w", err)
	}

	_, err = s.db.Exec(`
		CREATE TABLE IF NOT EXISTS rules (
			id          TEXT    PRIMARY KEY,
			title       TEXT    NOT NULL,
			content     TEXT    NOT NULL,
			scope       TEXT    NOT NULL DEFAULT 'team',
			scope_value TEXT    NOT NULL DEFAULT '',
			priority    INTEGER NOT NULL DEFAULT 0,
			enabled     INTEGER NOT NULL DEFAULT 1,
			author      TEXT    DEFAULT '',
			created_at  DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at  DATETIME DEFAULT CURRENT_TIMESTAMP
		);
	`)
	if err != nil {
		return fmt.Errorf("create rules table: %w", err)
	}

	_, err = s.db.Exec(`
		CREATE TABLE IF NOT EXISTS agents (
			id            TEXT PRIMARY KEY,
			domain        TEXT NOT NULL UNIQUE,
			version       INTEGER NOT NULL DEFAULT 1,
			status        TEXT NOT NULL DEFAULT 'draft',
			system_prompt TEXT NOT NULL DEFAULT '',
			instructions  TEXT NOT NULL DEFAULT '',
			anti_patterns TEXT NOT NULL DEFAULT '',
			source_refs   TEXT NOT NULL DEFAULT '[]',
			cluster_id    TEXT NOT NULL DEFAULT '',
			created_at    DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at    DATETIME DEFAULT CURRENT_TIMESTAMP
		);
	`)
	if err != nil {
		return fmt.Errorf("create agents table: %w", err)
	}

	_, err = s.db.Exec(`
		CREATE TABLE IF NOT EXISTS agent_versions (
			id            TEXT PRIMARY KEY,
			agent_id      TEXT NOT NULL REFERENCES agents(id),
			version       INTEGER NOT NULL,
			system_prompt TEXT NOT NULL DEFAULT '',
			instructions  TEXT NOT NULL DEFAULT '',
			anti_patterns TEXT NOT NULL DEFAULT '',
			changelog     TEXT NOT NULL DEFAULT '',
			created_at    DATETIME DEFAULT CURRENT_TIMESTAMP,
			UNIQUE(agent_id, version)
		);
	`)
	if err != nil {
		return fmt.Errorf("create agent_versions table: %w", err)
	}

	// Phase 5: teams, users, sessions, API keys, auth config, settings, activity log.
	phase5Tables := []string{
		`CREATE TABLE IF NOT EXISTS teams (
			id              TEXT PRIMARY KEY,
			name            TEXT NOT NULL,
			domain_patterns TEXT NOT NULL DEFAULT '[]',
			enabled         INTEGER NOT NULL DEFAULT 1,
			created_at      DATETIME DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE TABLE IF NOT EXISTS users (
			id                TEXT PRIMARY KEY,
			team_id           TEXT REFERENCES teams(id),
			email             TEXT NOT NULL UNIQUE,
			name              TEXT NOT NULL DEFAULT '',
			external_id       TEXT NOT NULL DEFAULT '',
			password_hash     TEXT NOT NULL DEFAULT '',
			role              TEXT NOT NULL DEFAULT 'member',
			manually_assigned INTEGER NOT NULL DEFAULT 0,
			created_at        DATETIME DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE TABLE IF NOT EXISTS sessions (
			id          TEXT PRIMARY KEY,
			user_id     TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			token_hash  TEXT NOT NULL UNIQUE,
			expires_at  DATETIME NOT NULL,
			created_at  DATETIME DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE TABLE IF NOT EXISTS api_keys (
			id           TEXT PRIMARY KEY,
			team_id      TEXT REFERENCES teams(id),
			user_id      TEXT REFERENCES users(id),
			key_type     TEXT NOT NULL DEFAULT 'team',
			name         TEXT NOT NULL DEFAULT '',
			key_hash     TEXT NOT NULL UNIQUE,
			role         TEXT NOT NULL DEFAULT 'member',
			created_at   DATETIME DEFAULT CURRENT_TIMESTAMP,
			last_used_at DATETIME
		)`,
		`CREATE TABLE IF NOT EXISTS auth_config (
			id                INTEGER PRIMARY KEY CHECK (id = 1),
			provider          TEXT NOT NULL DEFAULT 'local',
			oidc_issuer       TEXT NOT NULL DEFAULT '',
			oidc_client_id    TEXT NOT NULL DEFAULT '',
			oidc_redirect_url TEXT NOT NULL DEFAULT '',
			updated_at        DATETIME DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE TABLE IF NOT EXISTS team_settings (
			team_id              TEXT PRIMARY KEY REFERENCES teams(id),
			domains              TEXT NOT NULL DEFAULT '[]',
			cluster_threshold    REAL NOT NULL DEFAULT 0.85,
			pipeline_min_entries INTEGER NOT NULL DEFAULT 10,
			agent_model          TEXT NOT NULL DEFAULT 'claude-haiku-4-5-20251001',
			updated_at           DATETIME DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE TABLE IF NOT EXISTS activity_log (
			id          TEXT PRIMARY KEY,
			team_id     TEXT NOT NULL DEFAULT '',
			key_id      TEXT NOT NULL DEFAULT '',
			user_id     TEXT NOT NULL DEFAULT '',
			action      TEXT NOT NULL,
			entity_type TEXT NOT NULL DEFAULT '',
			entity_id   TEXT NOT NULL DEFAULT '',
			created_at  DATETIME DEFAULT CURRENT_TIMESTAMP
		)`,
	}
	for _, stmt := range phase5Tables {
		if _, err := s.db.Exec(stmt); err != nil {
			return fmt.Errorf("phase5 create table: %w", err)
		}
	}

	// Idempotent ALTER TABLE for existing tables — ignore "duplicate column name" errors.
	phase5Alters := []string{
		"ALTER TABLE entries ADD COLUMN team_id TEXT NOT NULL DEFAULT ''",
		"ALTER TABLE entries ADD COLUMN status  TEXT NOT NULL DEFAULT 'approved'",
		"ALTER TABLE clusters ADD COLUMN team_id TEXT NOT NULL DEFAULT ''",
		"ALTER TABLE agents ADD COLUMN team_id TEXT NOT NULL DEFAULT ''",
		"ALTER TABLE agent_versions ADD COLUMN team_id TEXT NOT NULL DEFAULT ''",
		"ALTER TABLE rules ADD COLUMN team_id TEXT NOT NULL DEFAULT ''",
	}
	for _, alter := range phase5Alters {
		if _, err := s.db.Exec(alter); err != nil {
			if !strings.Contains(err.Error(), "duplicate column name") {
				return fmt.Errorf("phase5 alter: %w", err)
			}
		}
	}

	// Idempotent ALTER TABLE for team_settings AI provider columns.
	teamSettingsAlters := []string{
		"ALTER TABLE team_settings ADD COLUMN anthropic_api_key TEXT NOT NULL DEFAULT ''",
		"ALTER TABLE team_settings ADD COLUMN anthropic_model    TEXT NOT NULL DEFAULT ''",
		"ALTER TABLE team_settings ADD COLUMN ollama_url         TEXT NOT NULL DEFAULT ''",
		"ALTER TABLE team_settings ADD COLUMN ollama_model       TEXT NOT NULL DEFAULT ''",
	}
	for _, alter := range teamSettingsAlters {
		if _, err := s.db.Exec(alter); err != nil {
			if !strings.Contains(err.Error(), "duplicate column name") {
				return fmt.Errorf("team_settings alter: %w", err)
			}
		}
	}

	if err := s.migrateUsageTracking(); err != nil {
		return fmt.Errorf("migrate usage tracking: %w", err)
	}

	if err := s.migrateFTS(); err != nil {
		return fmt.Errorf("migrate fts: %w", err)
	}

	return nil
}

func (s *SQLiteStore) migrateFTS() error {
	// Use FTS4 (available when go-sqlite3 is compiled with ENABLE_FTS3).
	// We use a standalone (non-content) table for simplicity: the FTS index
	// stores its own copy of title/content/tags and is kept in sync via triggers.
	stmts := []string{
		`CREATE VIRTUAL TABLE IF NOT EXISTS entries_fts USING fts4(
			title, content, tags
		)`,
		// Populate from existing rows; use docid=rowid so lookups work.
		// Ignore duplicate docid errors for idempotent re-runs.
		`INSERT OR IGNORE INTO entries_fts(docid, title, content, tags)
			SELECT rowid, title, content, tags FROM entries`,
		`CREATE TRIGGER IF NOT EXISTS entries_ai AFTER INSERT ON entries BEGIN
			INSERT INTO entries_fts(docid, title, content, tags) VALUES (new.rowid, new.title, new.content, new.tags);
		END`,
		`CREATE TRIGGER IF NOT EXISTS entries_ad AFTER DELETE ON entries BEGIN
			DELETE FROM entries_fts WHERE docid = old.rowid;
		END`,
		`CREATE TRIGGER IF NOT EXISTS entries_au AFTER UPDATE ON entries BEGIN
			DELETE FROM entries_fts WHERE docid = old.rowid;
			INSERT INTO entries_fts(docid, title, content, tags) VALUES (new.rowid, new.title, new.content, new.tags);
		END`,
	}
	for _, stmt := range stmts {
		if _, err := s.db.Exec(stmt); err != nil {
			snippet := stmt
			if len(snippet) > 60 {
				snippet = snippet[:60]
			}
			return fmt.Errorf("fts migration: %w: sql=%s", err, snippet)
		}
	}
	return nil
}

func (s *SQLiteStore) migrateUsageTracking() error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS usage_events (
			id             TEXT PRIMARY KEY,
			entry_id       TEXT NOT NULL REFERENCES entries(id) ON DELETE CASCADE,
			user_id        TEXT NOT NULL DEFAULT '',
			tool           TEXT NOT NULL DEFAULT '',
			selected_index INTEGER NOT NULL DEFAULT 0,
			created_at     DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE TABLE IF NOT EXISTS outcome_ratings (
			id         TEXT PRIMARY KEY,
			entry_id   TEXT NOT NULL REFERENCES entries(id) ON DELETE CASCADE,
			user_id    TEXT NOT NULL DEFAULT '',
			rating     INTEGER NOT NULL CHECK(rating BETWEEN 1 AND 5),
			note       TEXT NOT NULL DEFAULT '',
			created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
		)`,
		// Phase 8 activity feed — separate from the Phase 5 activity_log (which tracks
		// API-key/action audit events). This table carries richer event_type + metadata.
		`CREATE TABLE IF NOT EXISTS feed_activity (
			id         TEXT PRIMARY KEY,
			team_id    TEXT NOT NULL DEFAULT '',
			event_type TEXT NOT NULL DEFAULT '',
			actor_id   TEXT NOT NULL DEFAULT '',
			entry_id   TEXT NOT NULL DEFAULT '',
			metadata   TEXT NOT NULL DEFAULT '{}',
			created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE INDEX IF NOT EXISTS idx_usage_entry    ON usage_events(entry_id)`,
		`CREATE INDEX IF NOT EXISTS idx_usage_created  ON usage_events(created_at)`,
		`CREATE INDEX IF NOT EXISTS idx_outcome_entry  ON outcome_ratings(entry_id)`,
		`CREATE INDEX IF NOT EXISTS idx_feed_team_created ON feed_activity(team_id, created_at)`,
	}
	for _, stmt := range stmts {
		if _, err := s.db.Exec(stmt); err != nil {
			snippet := stmt
			if len(snippet) > 50 {
				snippet = snippet[:50]
			}
			return fmt.Errorf("usage tracking migration: %w: sql=%s", err, snippet)
		}
	}
	return nil
}


func (s *SQLiteStore) StoreEntry(ctx context.Context, entry KnowledgeEntry, embedding []float32) (string, error) {
	if embedding != nil && len(embedding) != s.embeddingDim {
		return "", fmt.Errorf("embedding dim mismatch: got %d, want %d", len(embedding), s.embeddingDim)
	}

	entry.ID = uuid.NewString()

	tagsJSON, err := json.Marshal(entry.Tags)
	if err != nil {
		return "", fmt.Errorf("marshal tags: %w", err)
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return "", fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	contentHash := sha256Hex(entry.Title + entry.Content)

	res, err := tx.ExecContext(ctx, `
		INSERT INTO entries (id, type, title, content, description, domain, tags, author, team, content_hash)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, entry.ID, string(entry.Type), entry.Title, entry.Content,
		entry.Description, entry.Domain, string(tagsJSON), entry.Author, entry.Team, contentHash)
	if err != nil {
		return "", fmt.Errorf("insert entry: %w", err)
	}

	if embedding != nil {
		rowID, err := res.LastInsertId()
		if err != nil {
			return "", fmt.Errorf("last insert id: %w", err)
		}

		blob, err := vec.SerializeFloat32(embedding)
		if err != nil {
			return "", fmt.Errorf("serialize embedding: %w", err)
		}

		_, err = tx.ExecContext(ctx, `INSERT INTO vec_entries (rowid, embedding) VALUES (?, ?)`, rowID, blob)
		if err != nil {
			return "", fmt.Errorf("insert vec_entries: %w", err)
		}

		_, err = tx.ExecContext(ctx, `INSERT INTO entry_embeddings (rowid, embedding) VALUES (?, ?)`, rowID, blob)
		if err != nil {
			return "", fmt.Errorf("insert entry_embeddings: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return "", err
	}
	return entry.ID, nil
}

func (s *SQLiteStore) GetEntry(ctx context.Context, id string) (*KnowledgeEntry, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, type, title, content, description, domain, tags, author, team,
		       created_at, updated_at, version, rating, usage_count, team_id, status
		FROM entries WHERE id = ?
	`, id)
	entry, err := scanEntry(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("entry %q: %w", id, ErrNotFound)
		}
		return nil, fmt.Errorf("get entry: %w", err)
	}
	return entry, nil
}

func (s *SQLiteStore) ListEntries(ctx context.Context, filter ListFilter) ([]KnowledgeEntry, error) {
	limit := filter.Limit
	if limit <= 0 {
		limit = 50
	}

	query := `SELECT id, type, title, content, description, domain, tags, author, team,
                     created_at, updated_at, version, rating, usage_count, team_id, status
              FROM entries WHERE 1=1`
	args := []any{}

	if filter.Domain != "" {
		query += " AND domain = ?"
		args = append(args, filter.Domain)
	}
	if filter.Type != "" {
		query += " AND type = ?"
		args = append(args, string(filter.Type))
	}
	if filter.Search != "" {
		query += " AND (title LIKE ? ESCAPE '\\' OR content LIKE ? ESCAPE '\\')"
		escaped := strings.ReplaceAll(filter.Search, `\`, `\\`)
		escaped = strings.ReplaceAll(escaped, `%`, `\%`)
		escaped = strings.ReplaceAll(escaped, `_`, `\_`)
		pattern := "%" + escaped + "%"
		args = append(args, pattern, pattern)
	}
	if filter.Status != "" {
		query += " AND status = ?"
		args = append(args, filter.Status)
	}
	if filter.TeamID != "" {
		query += " AND team_id = ?"
		args = append(args, filter.TeamID)
	}
	query += " ORDER BY created_at DESC"
	if limit > 0 {
		query += " LIMIT ?"
		args = append(args, limit)
	}
	if filter.Offset > 0 {
		query += " OFFSET ?"
		args = append(args, filter.Offset)
	}

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list entries: %w", err)
	}
	defer rows.Close()

	var entries []KnowledgeEntry
	for rows.Next() {
		entry, err := scanEntry(rows)
		if err != nil {
			return nil, fmt.Errorf("scan entry: %w", err)
		}
		entries = append(entries, *entry)
	}
	return entries, rows.Err()
}

func (s *SQLiteStore) RateEntry(ctx context.Context, id string, rating float64) error {
	res, err := s.db.ExecContext(ctx,
		"UPDATE entries SET rating = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?",
		rating, id,
	)
	if err != nil {
		return fmt.Errorf("rate entry: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("entry %q: %w", id, ErrNotFound)
	}
	return nil
}

func (s *SQLiteStore) ApproveEntry(ctx context.Context, id string) error {
	res, err := s.db.ExecContext(ctx,
		`UPDATE entries SET status='approved', updated_at=CURRENT_TIMESTAMP WHERE id=?`, id)
	if err != nil {
		return fmt.Errorf("approve entry: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("entry %q: %w", id, ErrNotFound)
	}
	return nil
}

func (s *SQLiteStore) RejectEntry(ctx context.Context, id string) error {
	res, err := s.db.ExecContext(ctx,
		`UPDATE entries SET status='rejected', updated_at=CURRENT_TIMESTAMP WHERE id=?`, id)
	if err != nil {
		return fmt.Errorf("reject entry: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("entry %q: %w", id, ErrNotFound)
	}
	return nil
}

func (s *SQLiteStore) UpdateEntry(ctx context.Context, entry KnowledgeEntry) error {
	tagsJSON, _ := json.Marshal(entry.Tags)
	res, err := s.db.ExecContext(ctx,
		`UPDATE entries SET title=?, content=?, description=?, domain=?, tags=?, updated_at=CURRENT_TIMESTAMP WHERE id=?`,
		entry.Title, entry.Content, entry.Description, entry.Domain, string(tagsJSON), entry.ID,
	)
	if err != nil {
		return fmt.Errorf("update entry: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("entry %q: %w", entry.ID, ErrNotFound)
	}
	return nil
}

func (s *SQLiteStore) DeleteEntry(ctx context.Context, id string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	var rowID int64
	err = tx.QueryRowContext(ctx, "SELECT rowid FROM entries WHERE id = ?", id).Scan(&rowID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("entry %q: %w", id, ErrNotFound)
		}
		return fmt.Errorf("find entry rowid: %w", err)
	}

	if _, err := tx.ExecContext(ctx, "DELETE FROM vec_entries WHERE rowid = ?", rowID); err != nil {
		return fmt.Errorf("delete vec_entries: %w", err)
	}
	// Delete explicitly rather than relying on ON DELETE CASCADE so this is safe
	// even if foreign_keys pragma is disabled on the connection.
	if _, err := tx.ExecContext(ctx, "DELETE FROM entry_embeddings WHERE rowid = ?", rowID); err != nil {
		return fmt.Errorf("delete entry_embeddings: %w", err)
	}
	if _, err := tx.ExecContext(ctx, "DELETE FROM entries WHERE id = ?", id); err != nil {
		return fmt.Errorf("delete entry: %w", err)
	}
	return tx.Commit()
}

func (s *SQLiteStore) SearchSimilar(ctx context.Context, embedding []float32, topK int) ([]SearchResult, error) {
	if len(embedding) != s.embeddingDim {
		return nil, fmt.Errorf("embedding dim mismatch: got %d, want %d", len(embedding), s.embeddingDim)
	}

	blob, err := vec.SerializeFloat32(embedding)
	if err != nil {
		return nil, fmt.Errorf("serialize query embedding: %w", err)
	}

	rows, err := s.db.QueryContext(ctx, `
		SELECT rowid, distance
		FROM vec_entries
		WHERE embedding MATCH ?
		AND k = ?
		ORDER BY distance
	`, blob, topK)
	if err != nil {
		return nil, fmt.Errorf("vector search: %w", err)
	}
	defer rows.Close()

	type rowResult struct {
		rowID    int64
		distance float64
	}
	var rowResults []rowResult
	for rows.Next() {
		var r rowResult
		if err := rows.Scan(&r.rowID, &r.distance); err != nil {
			return nil, fmt.Errorf("scan vec result: %w", err)
		}
		rowResults = append(rowResults, r)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	if len(rowResults) == 0 {
		return []SearchResult{}, nil
	}

	// Collect rowIDs in distance order for the IN query.
	rowIDs := make([]int64, len(rowResults))
	for i, r := range rowResults {
		rowIDs[i] = r.rowID
	}

	placeholders := make([]string, len(rowIDs))
	args := make([]any, len(rowIDs))
	for i, id := range rowIDs {
		placeholders[i] = "?"
		args[i] = id
	}

	rows2, err := s.db.QueryContext(ctx,
		fmt.Sprintf(`SELECT rowid, id, type, title, content, description, domain, tags, author, team,
		                    created_at, updated_at, version, rating, usage_count, team_id, status
		             FROM entries WHERE rowid IN (%s)`, strings.Join(placeholders, ",")),
		args...)
	if err != nil {
		return nil, fmt.Errorf("fetch entries by rowid: %w", err)
	}
	defer rows2.Close()

	entryMap := make(map[int64]*KnowledgeEntry, len(rowIDs))
	for rows2.Next() {
		var e KnowledgeEntry
		var tagsJSON string
		var createdAt, updatedAt string
		var rid int64
		if err := rows2.Scan(
			&rid, &e.ID, &e.Type, &e.Title, &e.Content, &e.Description,
			&e.Domain, &tagsJSON, &e.Author, &e.Team,
			&createdAt, &updatedAt, &e.Version,
			&e.Rating, &e.UsageCount, &e.TeamID, &e.Status,
		); err != nil {
			return nil, fmt.Errorf("scan entry for rowid %d: %w", rid, err)
		}
		if err := json.Unmarshal([]byte(tagsJSON), &e.Tags); err != nil {
			e.Tags = []string{}
		}
		e.CreatedAt = parseTimestamp(createdAt)
		e.UpdatedAt = parseTimestamp(updatedAt)
		entryMap[rid] = &e
	}
	if err := rows2.Err(); err != nil {
		return nil, fmt.Errorf("rows error after fetch: %w", err)
	}

	results := make([]SearchResult, 0, len(rowResults))
	for _, r := range rowResults {
		e, ok := entryMap[r.rowID]
		if !ok {
			return nil, fmt.Errorf("scan entry for rowid %d: %w", r.rowID, ErrNotFound)
		}
		results = append(results, SearchResult{Entry: *e, Score: r.distance})
	}
	return results, nil
}

func (s *SQLiteStore) Ping(ctx context.Context) error {
	return s.db.PingContext(ctx)
}

func (s *SQLiteStore) Close() error {
	return s.db.Close()
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanEntry(row rowScanner) (*KnowledgeEntry, error) {
	var e KnowledgeEntry
	var tagsJSON string
	var createdAt, updatedAt string

	err := row.Scan(
		&e.ID, &e.Type, &e.Title, &e.Content, &e.Description,
		&e.Domain, &tagsJSON, &e.Author, &e.Team,
		&createdAt, &updatedAt, &e.Version,
		&e.Rating, &e.UsageCount, &e.TeamID, &e.Status,
	)
	if err != nil {
		return nil, err
	}

	if err := json.Unmarshal([]byte(tagsJSON), &e.Tags); err != nil {
		e.Tags = []string{}
	}

	e.CreatedAt = parseTimestamp(createdAt)
	e.UpdatedAt = parseTimestamp(updatedAt)
	return &e, nil
}

// parseTimestamp handles multiple SQLite timestamp formats:
//   - RFC3339:          "2006-01-02T15:04:05Z"
//   - SQLite datetime:  "2006-01-02 15:04:05"
func parseTimestamp(s string) time.Time {
	formats := []string{
		time.RFC3339,
		"2006-01-02 15:04:05",
	}
	for _, f := range formats {
		if t, err := time.Parse(f, s); err == nil {
			return t
		}
	}
	return time.Time{}
}

// ── Phase 8: Usage tracking & activity feed ───────────────────────────────────

func (s *SQLiteStore) RecordUsage(ctx context.Context, event UsageEvent) error {
	id := event.ID
	if id == "" {
		id = uuid.NewString()
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO usage_events (id, entry_id, user_id, tool, selected_index)
		VALUES (?, ?, ?, ?, ?)
	`, id, event.EntryID, event.UserID, event.Tool, event.SelectedIndex)
	if err != nil {
		return fmt.Errorf("record usage: %w", err)
	}
	return nil
}

func (s *SQLiteStore) RecordOutcome(ctx context.Context, rating OutcomeRating) error {
	id := rating.ID
	if id == "" {
		id = uuid.NewString()
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO outcome_ratings (id, entry_id, user_id, rating, note)
		VALUES (?, ?, ?, ?, ?)
	`, id, rating.EntryID, rating.UserID, rating.Rating, rating.Note)
	if err != nil {
		return fmt.Errorf("record outcome: %w", err)
	}
	return nil
}

func (s *SQLiteStore) GetTrendingEntries(ctx context.Context, teamID string, days int, limit int) ([]TrendingEntry, error) {
	if limit <= 0 {
		limit = 10
	}
	// signal_score uses a Michaelis-Menten dampening instead of log() to avoid requiring
	// SQLITE_ENABLE_MATH_FUNCTIONS. cnt30/(cnt30+10) saturates toward 1 as usage grows,
	// giving similar sub-linear dampening without any math extension.
	const query = `
		SELECT
			e.id, e.type, e.title, e.content, e.description, e.domain, e.tags,
			e.author, e.team, e.created_at, e.updated_at, e.version,
			e.rating, e.usage_count, e.team_id, e.status,
			COALESCE(u.cnt7,  0)         AS usage_count_7d,
			COALESCE(u.cnt30, 0)         AS usage_count_30d,
			COALESCE(o.avg_rating, 0.0)  AS avg_outcome,
			(
				COALESCE(o.avg_rating, 0.0) * 2.0
				+ COALESCE(u.cnt30, 0) * 0.5
				+ (COALESCE(u.cnt30, 0) * 1.0 / (COALESCE(u.cnt30, 0) + 10.0))
			) AS signal_score
		FROM entries e
		LEFT JOIN (
			SELECT
				entry_id,
				COUNT(CASE WHEN created_at >= datetime('now', '-7 days')  THEN 1 END) AS cnt7,
				COUNT(CASE WHEN created_at >= datetime('now', '-30 days') THEN 1 END) AS cnt30
			FROM usage_events
			WHERE created_at >= datetime('now', ? || ' days')
			GROUP BY entry_id
		) u ON u.entry_id = e.id
		LEFT JOIN (
			SELECT entry_id, AVG(CAST(rating AS REAL)) AS avg_rating
			FROM outcome_ratings
			GROUP BY entry_id
		) o ON o.entry_id = e.id
		WHERE (? = '' OR e.team_id = ?)
		  AND (u.entry_id IS NOT NULL OR o.entry_id IS NOT NULL)
		ORDER BY signal_score DESC
		LIMIT ?
	`
	cutoffDays := fmt.Sprintf("-%d", days)
	rows, err := s.db.QueryContext(ctx, query, cutoffDays, teamID, teamID, limit)
	if err != nil {
		return nil, fmt.Errorf("get trending entries: %w", err)
	}
	defer rows.Close()

	var results []TrendingEntry
	for rows.Next() {
		var t TrendingEntry
		var tagsJSON, createdAt, updatedAt string
		if err := rows.Scan(
			&t.ID, &t.Type, &t.Title, &t.Content, &t.Description, &t.Domain, &tagsJSON,
			&t.Author, &t.Team, &createdAt, &updatedAt, &t.Version,
			&t.Rating, &t.UsageCount, &t.TeamID, &t.Status,
			&t.UsageCount7d, &t.UsageCount30d, &t.AvgOutcome, &t.SignalScore,
		); err != nil {
			return nil, fmt.Errorf("scan trending entry: %w", err)
		}
		if err := json.Unmarshal([]byte(tagsJSON), &t.Tags); err != nil {
			t.Tags = []string{}
		}
		t.CreatedAt = parseTimestamp(createdAt)
		t.UpdatedAt = parseTimestamp(updatedAt)
		results = append(results, t)
	}
	return results, rows.Err()
}

func (s *SQLiteStore) GetWeakSignalEntries(ctx context.Context, teamID string, minRatings int, maxAvgOutcome float64) ([]KnowledgeEntry, error) {
	const query = `
		SELECT
			e.id, e.type, e.title, e.content, e.description, e.domain, e.tags,
			e.author, e.team, e.created_at, e.updated_at, e.version,
			e.rating, e.usage_count, e.team_id, e.status
		FROM entries e
		INNER JOIN (
			SELECT entry_id, COUNT(*) AS cnt, AVG(CAST(rating AS REAL)) AS avg_rating
			FROM outcome_ratings
			GROUP BY entry_id
			HAVING COUNT(*) >= ? AND AVG(CAST(rating AS REAL)) <= ?
		) o ON o.entry_id = e.id
		WHERE (? = '' OR e.team_id = ?)
		ORDER BY o.avg_rating ASC
	`
	rows, err := s.db.QueryContext(ctx, query, minRatings, maxAvgOutcome, teamID, teamID)
	if err != nil {
		return nil, fmt.Errorf("get weak signal entries: %w", err)
	}
	defer rows.Close()

	var entries []KnowledgeEntry
	for rows.Next() {
		entry, err := scanEntry(rows)
		if err != nil {
			return nil, fmt.Errorf("scan weak signal entry: %w", err)
		}
		entries = append(entries, *entry)
	}
	return entries, rows.Err()
}

func (s *SQLiteStore) RecordActivity(ctx context.Context, event ActivityEvent) error {
	id := event.ID
	if id == "" {
		id = uuid.NewString()
	}
	metaJSON, err := json.Marshal(event.Metadata)
	if err != nil {
		return fmt.Errorf("marshal metadata: %w", err)
	}
	// Determine team_id: look it up from the entry if EntryID is set, otherwise
	// the caller should set a teamID via a wrapper — we store empty here and rely
	// on the caller to supply the TeamID through the ActivityEvent's EntryID entry.
	// For now we store team_id as empty; callers that need team filtering should
	// populate it externally or use ListActivity with an empty teamID.
	_, err = s.db.ExecContext(ctx, `
		INSERT INTO feed_activity (id, team_id, event_type, actor_id, entry_id, metadata)
		VALUES (?, '', ?, ?, ?, ?)
	`, id, event.EventType, event.ActorID, event.EntryID, string(metaJSON))
	if err != nil {
		return fmt.Errorf("record activity: %w", err)
	}
	return nil
}

// SearchHybrid combines FTS5 keyword search and vector-similarity search.
func (s *SQLiteStore) SearchHybrid(ctx context.Context, teamID string, query string, embedding []float32, mode string, limit int) ([]KnowledgeEntry, error) {
	if limit <= 0 {
		limit = 20
	}

	switch mode {
	case "keyword":
		return s.searchKeyword(ctx, teamID, query, limit)
	case "semantic":
		return s.searchSemantic(ctx, teamID, embedding, limit)
	default: // "hybrid"
		if embedding == nil {
			return s.searchKeyword(ctx, teamID, query, limit)
		}
		return s.searchHybridMerge(ctx, teamID, query, embedding, limit)
	}
}

// ftsQuery sanitises the user query for FTS4 MATCH: strips special chars.
// FTS4 supports prefix matching with "term*" syntax.
func ftsQuery(q string) string {
	replacer := strings.NewReplacer(`"`, " ", `(`, " ", `)`, " ", `-`, " ", `^`, " ")
	clean := replacer.Replace(q)
	words := strings.Fields(clean)
	for i, w := range words {
		words[i] = w + "*"
	}
	return strings.Join(words, " ")
}

// fts4Rank computes a simple term-frequency rank from FTS4 matchinfo() data.
// matchinfo(fts, "pcx") returns: phrase count, column count, then for each
// phrase+column: hits_this_row, hits_all_rows, docs_with_hit.
// We sum hits_this_row across all phrases and columns as a proxy for relevance.
func fts4Rank(matchinfoBlob []byte) float64 {
	if len(matchinfoBlob) < 8 {
		return 0
	}
	// matchinfo data is a sequence of uint32 values in native byte order.
	uint32LE := func(b []byte, i int) uint32 {
		return uint32(b[i]) | uint32(b[i+1])<<8 | uint32(b[i+2])<<16 | uint32(b[i+3])<<24
	}
	p := int(uint32LE(matchinfoBlob, 0)) // number of phrases
	c := int(uint32LE(matchinfoBlob, 4)) // number of columns
	if len(matchinfoBlob) < 8+p*c*3*4 {
		return 0
	}
	var score float64
	for phrase := 0; phrase < p; phrase++ {
		for col := 0; col < c; col++ {
			base := 8 + (phrase*c+col)*12
			hitsThisRow := uint32LE(matchinfoBlob, base)
			score += float64(hitsThisRow)
		}
	}
	return score
}

// searchKeyword runs an FTS4 full-text search and returns full KnowledgeEntry rows.
func (s *SQLiteStore) searchKeyword(ctx context.Context, teamID, query string, limit int) ([]KnowledgeEntry, error) {
	fts := ftsQuery(query)
	if fts == "" {
		return nil, nil
	}

	q := `
		SELECT e.id, e.type, e.title, e.content, e.description, e.domain, e.tags,
		       e.author, e.team, e.created_at, e.updated_at, e.version,
		       e.rating, e.usage_count, e.team_id, e.status,
		       matchinfo(entries_fts, 'pcx') AS mi
		FROM entries_fts
		JOIN entries e ON e.rowid = entries_fts.docid
		WHERE entries_fts MATCH ?
		  AND (? = '' OR e.team_id = ?)
		LIMIT ?
	`
	rows, err := s.db.QueryContext(ctx, q, fts, teamID, teamID, limit*3)
	if err != nil {
		return nil, fmt.Errorf("fts keyword search: %w", err)
	}
	defer rows.Close()

	type ranked struct {
		e     KnowledgeEntry
		score float64
	}
	var ranked_ []ranked
	for rows.Next() {
		var e KnowledgeEntry
		var tagsJSON, createdAt, updatedAt string
		var mi []byte
		if err := rows.Scan(
			&e.ID, &e.Type, &e.Title, &e.Content, &e.Description,
			&e.Domain, &tagsJSON, &e.Author, &e.Team,
			&createdAt, &updatedAt, &e.Version,
			&e.Rating, &e.UsageCount, &e.TeamID, &e.Status, &mi,
		); err != nil {
			return nil, fmt.Errorf("scan keyword result: %w", err)
		}
		if err2 := json.Unmarshal([]byte(tagsJSON), &e.Tags); err2 != nil {
			e.Tags = []string{}
		}
		e.CreatedAt = parseTimestamp(createdAt)
		e.UpdatedAt = parseTimestamp(updatedAt)
		ranked_ = append(ranked_, ranked{e: e, score: fts4Rank(mi)})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// Sort descending by rank.
	for i := 1; i < len(ranked_); i++ {
		for j := i; j > 0 && ranked_[j].score > ranked_[j-1].score; j-- {
			ranked_[j], ranked_[j-1] = ranked_[j-1], ranked_[j]
		}
	}
	if len(ranked_) > limit {
		ranked_ = ranked_[:limit]
	}
	entries := make([]KnowledgeEntry, len(ranked_))
	for i, r := range ranked_ {
		entries[i] = r.e
	}
	return entries, nil
}

// searchSemantic delegates to existing vector search then filters by teamID.
func (s *SQLiteStore) searchSemantic(ctx context.Context, teamID string, embedding []float32, limit int) ([]KnowledgeEntry, error) {
	if embedding == nil {
		return nil, nil
	}
	results, err := s.SearchSimilar(ctx, embedding, limit*2) // fetch extra; filter below
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

// searchHybridMerge runs both searches, deduplicates, and scores with
// 0.5*normalised_bm25 + 0.5*cosine_score.
func (s *SQLiteStore) searchHybridMerge(ctx context.Context, teamID, query string, embedding []float32, limit int) ([]KnowledgeEntry, error) {
	// Run keyword search — fetch up to 2*limit to give the merge room to work.
	fts := ftsQuery(query)
	type scoredEntry struct {
		entry KnowledgeEntry
		score float64
	}

	scoreMap := make(map[string]*scoredEntry)

	if fts != "" {
		q := `
			SELECT e.id, e.type, e.title, e.content, e.description, e.domain, e.tags,
			       e.author, e.team, e.created_at, e.updated_at, e.version,
			       e.rating, e.usage_count, e.team_id, e.status,
			       matchinfo(entries_fts, 'pcx') AS mi
			FROM entries_fts
			JOIN entries e ON e.rowid = entries_fts.docid
			WHERE entries_fts MATCH ?
			  AND (? = '' OR e.team_id = ?)
			LIMIT ?
		`
		rows, err := s.db.QueryContext(ctx, q, fts, teamID, teamID, limit*2)
		if err != nil {
			return nil, fmt.Errorf("hybrid fts search: %w", err)
		}
		defer rows.Close()

		type ftsRow struct {
			e     KnowledgeEntry
			score float64
		}
		var ftsRows []ftsRow
		maxScore := 0.0
		for rows.Next() {
			var e KnowledgeEntry
			var tagsJSON, createdAt, updatedAt string
			var mi []byte
			if err := rows.Scan(
				&e.ID, &e.Type, &e.Title, &e.Content, &e.Description,
				&e.Domain, &tagsJSON, &e.Author, &e.Team,
				&createdAt, &updatedAt, &e.Version,
				&e.Rating, &e.UsageCount, &e.TeamID, &e.Status, &mi,
			); err != nil {
				return nil, fmt.Errorf("scan hybrid fts row: %w", err)
			}
			if err2 := json.Unmarshal([]byte(tagsJSON), &e.Tags); err2 != nil {
				e.Tags = []string{}
			}
			e.CreatedAt = parseTimestamp(createdAt)
			e.UpdatedAt = parseTimestamp(updatedAt)
			sc := fts4Rank(mi)
			if sc > maxScore {
				maxScore = sc
			}
			ftsRows = append(ftsRows, ftsRow{e: e, score: sc})
		}
		if err := rows.Err(); err != nil {
			return nil, err
		}

		// Normalise scores to [0,1].
		for _, r := range ftsRows {
			norm := 0.0
			if maxScore > 0 {
				norm = r.score / maxScore
			}
			scoreMap[r.e.ID] = &scoredEntry{entry: r.e, score: 0.5 * norm}
		}
	}

	// Run semantic search.
	semResults, err := s.SearchSimilar(ctx, embedding, limit*2)
	if err != nil {
		return nil, fmt.Errorf("hybrid semantic search: %w", err)
	}

	// Distances from sqlite-vec are cosine distances [0,2]; convert to similarity [0,1].
	for _, r := range semResults {
		if teamID != "" && r.Entry.TeamID != teamID {
			continue
		}
		// cosine distance 0 = identical, 2 = opposite; similarity = 1 - distance/2
		similarity := 1.0 - r.Score/2.0
		if se, ok := scoreMap[r.Entry.ID]; ok {
			se.score += 0.5 * similarity
		} else {
			scoreMap[r.Entry.ID] = &scoredEntry{entry: r.Entry, score: 0.5 * similarity}
		}
	}

	// Sort by combined score descending.
	scored := make([]*scoredEntry, 0, len(scoreMap))
	for _, se := range scoreMap {
		scored = append(scored, se)
	}
	// Simple insertion sort — result sets are small.
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

// BulkImport inserts multiple entries in a single transaction.
// Entries whose title already exists (case-insensitive) within the same team are skipped.
func (s *SQLiteStore) BulkImport(ctx context.Context, entries []KnowledgeEntry) (imported int, skipped int, errs []string, err error) {
	tx, txErr := s.db.BeginTx(ctx, nil)
	if txErr != nil {
		return 0, 0, nil, fmt.Errorf("begin tx: %w", txErr)
	}
	defer tx.Rollback()

	for _, entry := range entries {
		// Check for existing title in same team.
		var count int
		_ = tx.QueryRowContext(ctx,
			`SELECT COUNT(*) FROM entries WHERE lower(title)=lower(?) AND team_id=?`,
			entry.Title, entry.TeamID,
		).Scan(&count)
		if count > 0 {
			skipped++
			continue
		}

		entry.ID = uuid.NewString()
		tagsJSON, _ := json.Marshal(entry.Tags)
		_, insertErr := tx.ExecContext(ctx, `
			INSERT INTO entries (id, type, title, content, description, domain, tags, author, team, team_id, status)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		`, entry.ID, string(entry.Type), entry.Title, entry.Content,
			entry.Description, entry.Domain, string(tagsJSON),
			entry.Author, entry.Team, entry.TeamID, statusOrDefaultSQLite(entry.Status),
		)
		if insertErr != nil {
			errs = append(errs, fmt.Sprintf("%s: %v", entry.Title, insertErr))
			continue
		}
		imported++
	}

	if commitErr := tx.Commit(); commitErr != nil {
		return 0, 0, errs, fmt.Errorf("commit: %w", commitErr)
	}
	return imported, skipped, errs, nil
}

func statusOrDefaultSQLite(s string) string {
	if s == "" {
		return "approved"
	}
	return s
}

func (s *SQLiteStore) GetEntryByContentHash(ctx context.Context, hash string) (*KnowledgeEntry, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, type, title, content, description, domain, tags, author, team,
		       created_at, updated_at, version, rating, usage_count, team_id, status
		FROM entries WHERE content_hash = ? LIMIT 1
	`, hash)
	entry, err := scanEntry(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("get entry by content hash: %w", err)
	}
	return entry, nil
}

func (s *SQLiteStore) ListActivity(ctx context.Context, teamID string, limit int, offset int) ([]ActivityEvent, error) {
	if limit <= 0 {
		limit = 50
	}
	const query = `
		SELECT id, event_type, actor_id, entry_id, metadata, created_at
		FROM feed_activity
		WHERE (? = '' OR team_id = ?)
		ORDER BY created_at DESC
		LIMIT ? OFFSET ?
	`
	rows, err := s.db.QueryContext(ctx, query, teamID, teamID, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("list activity: %w", err)
	}
	defer rows.Close()

	var events []ActivityEvent
	for rows.Next() {
		var ev ActivityEvent
		var metaJSON, createdAt string
		if err := rows.Scan(&ev.ID, &ev.EventType, &ev.ActorID, &ev.EntryID, &metaJSON, &createdAt); err != nil {
			return nil, fmt.Errorf("scan activity event: %w", err)
		}
		if err := json.Unmarshal([]byte(metaJSON), &ev.Metadata); err != nil {
			ev.Metadata = map[string]string{}
		}
		ev.CreatedAt = parseTimestamp(createdAt)
		events = append(events, ev)
	}
	return events, rows.Err()
}
