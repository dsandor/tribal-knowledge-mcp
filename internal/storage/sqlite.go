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

	_, err = s.db.Exec(`
		CREATE TABLE IF NOT EXISTS entry_chunks (
			rowid          INTEGER PRIMARY KEY AUTOINCREMENT,
			entry_id       TEXT    NOT NULL REFERENCES entries(id) ON DELETE CASCADE,
			chunk_index    INTEGER NOT NULL,
			content        TEXT    NOT NULL,
			token_estimate INTEGER NOT NULL DEFAULT 0,
			UNIQUE(entry_id, chunk_index)
		);`)
	if err != nil {
		return fmt.Errorf("create entry_chunks table: %w", err)
	}
	_, err = s.db.Exec(fmt.Sprintf(`
		CREATE VIRTUAL TABLE IF NOT EXISTS vec_chunks USING vec0(
			embedding FLOAT[%d]
		);`, s.embeddingDim))
	if err != nil {
		return fmt.Errorf("create vec_chunks table: %w", err)
	}
	_, err = s.db.Exec(`CREATE INDEX IF NOT EXISTS idx_entry_chunks_entry ON entry_chunks(entry_id)`)
	if err != nil {
		return fmt.Errorf("create idx_entry_chunks_entry: %w", err)
	}

	// Idempotent ALTER TABLE — ignore "duplicate column name" errors.
	for _, col := range []string{
		"ALTER TABLE entries ADD COLUMN rating REAL DEFAULT 0.0",
		"ALTER TABLE entries ADD COLUMN usage_count INTEGER DEFAULT 0",
		"ALTER TABLE entries ADD COLUMN content_hash TEXT NOT NULL DEFAULT ''",
		"ALTER TABLE entries ADD COLUMN auto_tags TEXT NOT NULL DEFAULT '[]'",
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
		CREATE TABLE IF NOT EXISTS analysis_cache (
			kind       TEXT NOT NULL,
			key        TEXT NOT NULL,
			value      TEXT NOT NULL,
			team_id    TEXT NOT NULL DEFAULT '',
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			PRIMARY KEY (kind, key)
		);
	`)
	if err != nil {
		return fmt.Errorf("create analysis_cache table: %w", err)
	}

	_, err = s.db.Exec(`
		CREATE TABLE IF NOT EXISTS user_visibility_rules (
			id         TEXT PRIMARY KEY,
			user_id    TEXT NOT NULL,
			rule_type  TEXT NOT NULL,
			value      TEXT NOT NULL,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			UNIQUE(user_id, rule_type, value)
		);
	`)
	if err != nil {
		return fmt.Errorf("create user_visibility_rules table: %w", err)
	}
	if _, err = s.db.Exec(`CREATE INDEX IF NOT EXISTS idx_uvr_user ON user_visibility_rules(user_id)`); err != nil {
		return fmt.Errorf("create idx_uvr_user index: %w", err)
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
			raw_key      TEXT NOT NULL DEFAULT '',
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
		`CREATE TABLE IF NOT EXISTS team_members (
			user_id    TEXT NOT NULL,
			team_id    TEXT NOT NULL,
			created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			PRIMARY KEY (user_id, team_id)
		)`,
	}
	for _, stmt := range phase5Tables {
		if _, err := s.db.Exec(stmt); err != nil {
			return fmt.Errorf("phase5 create table: %w", err)
		}
	}

	// Backfill each existing user's home team as a membership row (idempotent).
	if _, err := s.db.Exec(`
		INSERT OR IGNORE INTO team_members (user_id, team_id)
		SELECT id, team_id FROM users WHERE team_id IS NOT NULL AND team_id <> ''
	`); err != nil {
		return fmt.Errorf("phase5 backfill team_members: %w", err)
	}

	// Idempotent ALTER TABLE for existing tables — ignore "duplicate column name" errors.
	phase5Alters := []string{
		"ALTER TABLE entries ADD COLUMN team_id TEXT NOT NULL DEFAULT ''",
		"ALTER TABLE entries ADD COLUMN status  TEXT NOT NULL DEFAULT 'approved'",
		"ALTER TABLE clusters ADD COLUMN team_id TEXT NOT NULL DEFAULT ''",
		"ALTER TABLE agents ADD COLUMN team_id TEXT NOT NULL DEFAULT ''",
		"ALTER TABLE agent_versions ADD COLUMN team_id TEXT NOT NULL DEFAULT ''",
		"ALTER TABLE rules ADD COLUMN team_id TEXT NOT NULL DEFAULT ''",
		"ALTER TABLE api_keys ADD COLUMN raw_key TEXT NOT NULL DEFAULT ''",
		"ALTER TABLE dataset_snapshots ADD COLUMN team_id TEXT NOT NULL DEFAULT ''",
		"ALTER TABLE pipeline_runs ADD COLUMN team_id TEXT NOT NULL DEFAULT ''",
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
		"ALTER TABLE team_settings ADD COLUMN llm_provider       TEXT NOT NULL DEFAULT ''",
		"ALTER TABLE team_settings ADD COLUMN ollama_llm_model   TEXT NOT NULL DEFAULT ''",
		"ALTER TABLE team_settings ADD COLUMN ai_touchpoints     TEXT NOT NULL DEFAULT '{}'",
		"ALTER TABLE team_settings ADD COLUMN embedding_max_tokens INTEGER NOT NULL DEFAULT 0",
		"ALTER TABLE team_settings ADD COLUMN chunk_overlap_tokens INTEGER NOT NULL DEFAULT 0",
		"ALTER TABLE team_settings ADD COLUMN max_chunks           INTEGER NOT NULL DEFAULT 0",
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

	if err := s.migrateAgentsTeamUnique(); err != nil {
		return fmt.Errorf("migrate agents team unique: %w", err)
	}

	if err := s.backfillChunks(); err != nil {
		return fmt.Errorf("backfill chunks: %w", err)
	}

	if err := s.migrateShares(); err != nil {
		return fmt.Errorf("migrate shares: %w", err)
	}

	return nil
}

// migrateShares creates the cross-team knowledge sharing table and its index.
func (s *SQLiteStore) migrateShares() error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS knowledge_shares (
			id                TEXT PRIMARY KEY,
			entry_id          TEXT NOT NULL,
			source_team_id    TEXT NOT NULL DEFAULT '',
			created_by        TEXT NOT NULL DEFAULT '',
			used_at           DATETIME,
			used_by           TEXT NOT NULL DEFAULT '',
			imported_entry_id TEXT NOT NULL DEFAULT '',
			revoked_at        DATETIME,
			created_at        DATETIME DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE INDEX IF NOT EXISTS idx_shares_entry ON knowledge_shares(entry_id)`,
	}
	for _, stmt := range stmts {
		if _, err := s.db.Exec(stmt); err != nil {
			return fmt.Errorf("create knowledge_shares: %w", err)
		}
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

// StoreEntry creates a new entry with a single embedding (legacy single-chunk
// path). It delegates to StoreEntryChunked.
func (s *SQLiteStore) StoreEntry(ctx context.Context, entry KnowledgeEntry, embedding []float32) (string, error) {
	return s.StoreEntryChunked(ctx, entry, []EntryChunk{{Index: 0, Content: entry.Content, Embedding: embedding}})
}

// StoreEntryChunked creates a new entry represented by one or more chunks.
// chunks[0] is the representative chunk: its vector is also written to
// vec_entries/entry_embeddings so the pipeline's per-entry embedding queries
// keep working.
func (s *SQLiteStore) StoreEntryChunked(ctx context.Context, entry KnowledgeEntry, chunks []EntryChunk) (string, error) {
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

	res, err := tx.ExecContext(ctx, `
		INSERT INTO entries (id, type, title, content, description, domain, tags, auto_tags, author, team, team_id, status, content_hash)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, entry.ID, string(entry.Type), entry.Title, entry.Content,
		entry.Description, entry.Domain, string(tagsJSON), string(autoTagsJSON), entry.Author, entry.Team,
		entry.TeamID, statusOrDefaultSQLite(entry.Status), contentHash)
	if err != nil {
		return "", fmt.Errorf("insert entry: %w", err)
	}

	entryRowID, err := res.LastInsertId()
	if err != nil {
		return "", fmt.Errorf("last insert id: %w", err)
	}

	// Representative chunk: write per-entry vector for the pipeline.
	if len(chunks) > 0 && chunks[0].Embedding != nil {
		blob, err := vec.SerializeFloat32(chunks[0].Embedding)
		if err != nil {
			return "", fmt.Errorf("serialize embedding: %w", err)
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO vec_entries (rowid, embedding) VALUES (?, ?)`, entryRowID, blob); err != nil {
			return "", fmt.Errorf("insert vec_entries: %w", err)
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO entry_embeddings (rowid, embedding) VALUES (?, ?)`, entryRowID, blob); err != nil {
			return "", fmt.Errorf("insert entry_embeddings: %w", err)
		}
	}

	if err := insertChunksTx(ctx, tx, entry.ID, chunks); err != nil {
		return "", err
	}

	if err := tx.Commit(); err != nil {
		return "", err
	}
	return entry.ID, nil
}

// insertChunksTx inserts each chunk into entry_chunks and, for chunks with a
// non-nil embedding, into vec_chunks keyed by the new entry_chunks rowid.
func insertChunksTx(ctx context.Context, tx *sql.Tx, entryID string, chunks []EntryChunk) error {
	for _, c := range chunks {
		cres, err := tx.ExecContext(ctx, `
			INSERT INTO entry_chunks (entry_id, chunk_index, content, token_estimate)
			VALUES (?, ?, ?, ?)
		`, entryID, c.Index, c.Content, c.TokenEstimate)
		if err != nil {
			return fmt.Errorf("insert entry_chunks: %w", err)
		}
		if c.Embedding == nil {
			continue
		}
		chunkRowID, err := cres.LastInsertId()
		if err != nil {
			return fmt.Errorf("chunk last insert id: %w", err)
		}
		blob, err := vec.SerializeFloat32(c.Embedding)
		if err != nil {
			return fmt.Errorf("serialize chunk embedding: %w", err)
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO vec_chunks (rowid, embedding) VALUES (?, ?)`, chunkRowID, blob); err != nil {
			return fmt.Errorf("insert vec_chunks: %w", err)
		}
	}
	return nil
}

// ReplaceEntryChunks atomically swaps all chunks (and their vectors) for an
// existing entry, and refreshes the representative per-entry vector from
// chunks[0].
func (s *SQLiteStore) ReplaceEntryChunks(ctx context.Context, entryID string, chunks []EntryChunk) error {
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

	var entryRowID int64
	err = tx.QueryRowContext(ctx, `SELECT rowid FROM entries WHERE id = ?`, entryID).Scan(&entryRowID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("entry %q: %w", entryID, ErrNotFound)
		}
		return fmt.Errorf("find entry rowid: %w", err)
	}

	// Drop existing chunk vectors then chunk rows.
	if _, err := tx.ExecContext(ctx, `
		DELETE FROM vec_chunks
		WHERE rowid IN (SELECT rowid FROM entry_chunks WHERE entry_id = ?)`, entryID); err != nil {
		return fmt.Errorf("delete vec_chunks: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM entry_chunks WHERE entry_id = ?`, entryID); err != nil {
		return fmt.Errorf("delete entry_chunks: %w", err)
	}

	if err := insertChunksTx(ctx, tx, entryID, chunks); err != nil {
		return err
	}

	// Refresh the representative per-entry vector.
	if _, err := tx.ExecContext(ctx, `DELETE FROM vec_entries WHERE rowid = ?`, entryRowID); err != nil {
		return fmt.Errorf("delete vec_entries: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM entry_embeddings WHERE rowid = ?`, entryRowID); err != nil {
		return fmt.Errorf("delete entry_embeddings: %w", err)
	}
	if len(chunks) > 0 && chunks[0].Embedding != nil {
		blob, err := vec.SerializeFloat32(chunks[0].Embedding)
		if err != nil {
			return fmt.Errorf("serialize embedding: %w", err)
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO vec_entries (rowid, embedding) VALUES (?, ?)`, entryRowID, blob); err != nil {
			return fmt.Errorf("insert vec_entries: %w", err)
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO entry_embeddings (rowid, embedding) VALUES (?, ?)`, entryRowID, blob); err != nil {
			return fmt.Errorf("insert entry_embeddings: %w", err)
		}
	}

	return tx.Commit()
}

// backfillChunks creates a chunk-0 row for every entry that has none, copying
// the per-entry embedding (if present) into vec_chunks. Idempotent.
func (s *SQLiteStore) backfillChunks() error {
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	rows, err := tx.Query(`
		SELECT e.id, e.content, ee.embedding
		FROM entries e
		LEFT JOIN entry_embeddings ee ON ee.rowid = e.rowid
		WHERE NOT EXISTS (SELECT 1 FROM entry_chunks ec WHERE ec.entry_id = e.id)
	`)
	if err != nil {
		return fmt.Errorf("scan entries for backfill: %w", err)
	}

	type pending struct {
		id      string
		content string
		blob    []byte
	}
	var todo []pending
	for rows.Next() {
		var p pending
		var blob []byte
		if err := rows.Scan(&p.id, &p.content, &blob); err != nil {
			rows.Close()
			return fmt.Errorf("scan backfill row: %w", err)
		}
		p.blob = blob
		todo = append(todo, p)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()

	for _, p := range todo {
		cres, err := tx.Exec(`
			INSERT INTO entry_chunks (entry_id, chunk_index, content, token_estimate)
			VALUES (?, 0, ?, 0)
		`, p.id, p.content)
		if err != nil {
			return fmt.Errorf("backfill insert entry_chunks: %w", err)
		}
		if p.blob == nil {
			continue
		}
		chunkRowID, err := cres.LastInsertId()
		if err != nil {
			return fmt.Errorf("backfill chunk last insert id: %w", err)
		}
		if _, err := tx.Exec(`INSERT INTO vec_chunks (rowid, embedding) VALUES (?, ?)`, chunkRowID, p.blob); err != nil {
			return fmt.Errorf("backfill insert vec_chunks: %w", err)
		}
	}

	return tx.Commit()
}

func (s *SQLiteStore) GetEntry(ctx context.Context, id string) (*KnowledgeEntry, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, type, title, content, description, domain, tags, auto_tags, author, team,
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

	query := `SELECT id, type, title, content, description, domain, tags, auto_tags, author, team,
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
	if filter.Tag != "" {
		query += ` AND (EXISTS (SELECT 1 FROM json_each(entries.tags) WHERE json_each.value = ?)
		            OR EXISTS (SELECT 1 FROM json_each(entries.auto_tags) WHERE json_each.value = ?))`
		args = append(args, filter.Tag, filter.Tag)
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

func (s *SQLiteStore) ReassignEntriesTeam(ctx context.Context, entryIDs []string, teamID string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck
	stmt, err := tx.PrepareContext(ctx, `UPDATE entries SET team_id = ? WHERE id = ?`)
	if err != nil {
		return err
	}
	defer stmt.Close()
	for _, id := range entryIDs {
		if _, err := stmt.ExecContext(ctx, teamID, id); err != nil {
			return fmt.Errorf("reassign entry %s: %w", id, err)
		}
	}
	return tx.Commit()
}

func (s *SQLiteStore) UpdateAutoTags(ctx context.Context, id string, tags []string) error {
	autoTagsJSON, err := json.Marshal(tags)
	if err != nil {
		return fmt.Errorf("marshal auto tags: %w", err)
	}
	res, err := s.db.ExecContext(ctx,
		`UPDATE entries SET auto_tags=?, updated_at=CURRENT_TIMESTAMP WHERE id=?`,
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

func (s *SQLiteStore) BackfillTeamID(ctx context.Context, teamID string) error {
	if teamID == "" {
		return nil
	}
	for _, table := range []string{"entries", "clusters", "agents", "agent_versions", "dataset_snapshots", "pipeline_runs"} {
		if _, err := s.db.ExecContext(ctx,
			fmt.Sprintf("UPDATE %s SET team_id = ? WHERE team_id = ''", table), teamID); err != nil {
			return fmt.Errorf("backfill %s: %w", table, err)
		}
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
	if _, err := tx.ExecContext(ctx,
		`DELETE FROM vec_chunks WHERE rowid IN (SELECT rowid FROM entry_chunks WHERE entry_id = ?)`, id); err != nil {
		return fmt.Errorf("delete vec_chunks: %w", err)
	}
	if _, err := tx.ExecContext(ctx, "DELETE FROM entry_chunks WHERE entry_id = ?", id); err != nil {
		return fmt.Errorf("delete entry_chunks: %w", err)
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

	// Query the chunk vectors. An entry may have several chunks, so a fixed
	// multiple of topK chunk hits can collapse to far fewer than topK distinct
	// entries (e.g. when a handful of entries each own many of the closest
	// chunks). Use an expanding retrieval: fetch a pool of chunk hits, dedup to
	// entries keeping the minimum distance, and if we still have fewer than topK
	// distinct entries while the chunk table is not yet exhausted, grow the pool
	// and retry.
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
			SELECT vc.rowid, vc.distance, ec.entry_id
			FROM vec_chunks vc
			JOIN entry_chunks ec ON ec.rowid = vc.rowid
			WHERE vc.embedding MATCH ?
			AND k = ?
			ORDER BY vc.distance
		`, blob, k)
		if err != nil {
			return nil, fmt.Errorf("vector search: %w", err)
		}

		// Dedup to one best (minimum) distance per entry_id, preserving the order
		// in which each entry's best distance was first seen (ascending distance).
		rowCount := 0
		for rows.Next() {
			var chunkRowID int64
			var distance float64
			var entryID string
			if err := rows.Scan(&chunkRowID, &distance, &entryID); err != nil {
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

	// Stable sort entries by ascending best distance (order already ascending,
	// but min-update may have lowered a later entry — re-sort to be safe).
	sort.SliceStable(order, func(i, j int) bool {
		return bestDist[order[i]] < bestDist[order[j]]
	})
	if len(order) > topK {
		order = order[:topK]
	}

	placeholders := make([]string, len(order))
	args := make([]any, len(order))
	for i, id := range order {
		placeholders[i] = "?"
		args[i] = id
	}

	rows2, err := s.db.QueryContext(ctx,
		fmt.Sprintf(`SELECT id, type, title, content, description, domain, tags, auto_tags, author, team,
		                    created_at, updated_at, version, rating, usage_count, team_id, status
		             FROM entries WHERE id IN (%s)`, strings.Join(placeholders, ",")),
		args...)
	if err != nil {
		return nil, fmt.Errorf("fetch entries by id: %w", err)
	}
	defer rows2.Close()

	entryMap := make(map[string]*KnowledgeEntry, len(order))
	for rows2.Next() {
		e, err := scanEntry(rows2)
		if err != nil {
			return nil, fmt.Errorf("scan search entry: %w", err)
		}
		entryMap[e.ID] = e
	}
	if err := rows2.Err(); err != nil {
		return nil, fmt.Errorf("rows error after fetch: %w", err)
	}

	results := make([]SearchResult, 0, len(order))
	for _, id := range order {
		e, ok := entryMap[id]
		if !ok {
			return nil, fmt.Errorf("entry %q: %w", id, ErrNotFound)
		}
		results = append(results, SearchResult{Entry: *e, Score: bestDist[id]})
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
	var autoTagsJSON string
	var createdAt, updatedAt string

	err := row.Scan(
		&e.ID, &e.Type, &e.Title, &e.Content, &e.Description,
		&e.Domain, &tagsJSON, &autoTagsJSON, &e.Author, &e.Team,
		&createdAt, &updatedAt, &e.Version,
		&e.Rating, &e.UsageCount, &e.TeamID, &e.Status,
	)
	if err != nil {
		return nil, err
	}

	if err := json.Unmarshal([]byte(tagsJSON), &e.Tags); err != nil {
		e.Tags = []string{}
	}
	if err := json.Unmarshal([]byte(autoTagsJSON), &e.AutoTags); err != nil {
		e.AutoTags = []string{}
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

// migrateAgentsTeamUnique rebuilds the agents table so that uniqueness is on
// (domain, team_id) rather than on domain alone. This is idempotent: if the
// table already has the new shape (detected by checking for the old inline
// UNIQUE column constraint in sqlite_master) it is a no-op.
//
// The legacy single-column UNIQUE constraint cannot be dropped via ALTER TABLE
// in SQLite, so we use the standard SQLite table-rebuild pattern:
//
//  1. Save agent_versions rows (they reference agents.id via FK).
//  2. Drop agent_versions (removes the FK pointing at agents).
//  3. Create agents_new with UNIQUE(domain, team_id) table constraint.
//  4. Copy all rows from agents into agents_new.
//  5. DROP agents (now safe — no FK references it).
//  6. Rename agents_new → agents.
//  7. Recreate agent_versions with FK pointing at the new agents table.
//  8. Restore saved agent_versions rows.
//
// Wrapped in a transaction so a crash leaves the DB in the old state.
// FK enforcement is disabled for the connection while the rebuild is in progress.
func (s *SQLiteStore) migrateAgentsTeamUnique() error {
	// Check whether the old shape is still present. We detect the new shape by
	// the presence of "UNIQUE(domain, team_id)" in the DDL.
	var createSQL string
	row := s.db.QueryRow(`SELECT sql FROM sqlite_master WHERE type='table' AND name='agents'`)
	if err := row.Scan(&createSQL); err != nil {
		// Table doesn't exist yet — nothing to migrate.
		return nil
	}

	if strings.Contains(createSQL, "UNIQUE(domain, team_id)") {
		// Already migrated.
		return nil
	}

	// No FK pragma toggle is needed: agent_versions (the only table with an
	// FK into agents) is dropped inside the transaction before agents is
	// touched, so the rebuild never violates a live constraint. (A pragma
	// toggle here would also be unreliable — PRAGMA foreign_keys is
	// per-connection and database/sql pools connections.)

	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("begin agents rebuild tx: %w", err)
	}
	defer tx.Rollback()

	stmts := []string{
		// 1. Save agent_versions into a temp table (no FK constraints).
		`CREATE TABLE IF NOT EXISTS agent_versions_bak AS SELECT * FROM agent_versions`,
		// 2. Drop agent_versions so the FK on agents can be removed.
		`DROP TABLE agent_versions`,
		// 3. New agents table with composite uniqueness.
		`CREATE TABLE agents_new (
			id            TEXT PRIMARY KEY,
			domain        TEXT NOT NULL,
			version       INTEGER NOT NULL DEFAULT 1,
			status        TEXT NOT NULL DEFAULT 'draft',
			system_prompt TEXT NOT NULL DEFAULT '',
			instructions  TEXT NOT NULL DEFAULT '',
			anti_patterns TEXT NOT NULL DEFAULT '',
			source_refs   TEXT NOT NULL DEFAULT '[]',
			cluster_id    TEXT NOT NULL DEFAULT '',
			team_id       TEXT NOT NULL DEFAULT '',
			created_at    DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at    DATETIME DEFAULT CURRENT_TIMESTAMP,
			UNIQUE(domain, team_id)
		)`,
		// 4. Copy existing agents data.
		`INSERT INTO agents_new
			SELECT id, domain, version, status, system_prompt, instructions,
			       anti_patterns, source_refs, cluster_id, team_id, created_at, updated_at
			FROM agents`,
		// 5. Drop old agents table.
		`DROP TABLE agents`,
		// 6. Rename new table.
		`ALTER TABLE agents_new RENAME TO agents`,
		// 7. Recreate agent_versions with the FK pointing at the new agents table.
		//    Include team_id (added via phase5Alters in the original migration).
		`CREATE TABLE agent_versions (
			id            TEXT PRIMARY KEY,
			agent_id      TEXT NOT NULL REFERENCES agents(id),
			version       INTEGER NOT NULL,
			system_prompt TEXT NOT NULL DEFAULT '',
			instructions  TEXT NOT NULL DEFAULT '',
			anti_patterns TEXT NOT NULL DEFAULT '',
			changelog     TEXT NOT NULL DEFAULT '',
			team_id       TEXT NOT NULL DEFAULT '',
			created_at    DATETIME DEFAULT CURRENT_TIMESTAMP,
			UNIQUE(agent_id, version)
		)`,
		// 8. Restore saved versions (only those whose agent_id still exists).
		//    We try to restore team_id from the backup; if the backup was taken
		//    before the team_id column existed (very old DBs), fall back to ''.
		`INSERT OR IGNORE INTO agent_versions (id, agent_id, version, system_prompt, instructions, anti_patterns, changelog, team_id, created_at)
			SELECT id, agent_id, version, system_prompt, instructions, anti_patterns, changelog,
			       IFNULL(team_id, '') AS team_id, created_at
			FROM agent_versions_bak
			WHERE agent_id IN (SELECT id FROM agents)`,
		// 9. Clean up backup table.
		`DROP TABLE agent_versions_bak`,
	}

	for _, stmt := range stmts {
		if _, err := tx.Exec(stmt); err != nil {
			snippet := stmt
			if len(snippet) > 80 {
				snippet = snippet[:80]
			}
			return fmt.Errorf("agents rebuild: %w — sql: %s", err, snippet)
		}
	}

	return tx.Commit()
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
			e.id, e.type, e.title, e.content, e.description, e.domain, e.tags, e.auto_tags,
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
		var tagsJSON, autoTagsJSON, createdAt, updatedAt string
		if err := rows.Scan(
			&t.ID, &t.Type, &t.Title, &t.Content, &t.Description, &t.Domain, &tagsJSON, &autoTagsJSON,
			&t.Author, &t.Team, &createdAt, &updatedAt, &t.Version,
			&t.Rating, &t.UsageCount, &t.TeamID, &t.Status,
			&t.UsageCount7d, &t.UsageCount30d, &t.AvgOutcome, &t.SignalScore,
		); err != nil {
			return nil, fmt.Errorf("scan trending entry: %w", err)
		}
		if err := json.Unmarshal([]byte(tagsJSON), &t.Tags); err != nil {
			t.Tags = []string{}
		}
		if err := json.Unmarshal([]byte(autoTagsJSON), &t.AutoTags); err != nil {
			t.AutoTags = []string{}
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
			e.id, e.type, e.title, e.content, e.description, e.domain, e.tags, e.auto_tags,
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
		SELECT e.id, e.type, e.title, e.content, e.description, e.domain, e.tags, e.auto_tags,
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
		var tagsJSON, autoTagsJSON, createdAt, updatedAt string
		var mi []byte
		if err := rows.Scan(
			&e.ID, &e.Type, &e.Title, &e.Content, &e.Description,
			&e.Domain, &tagsJSON, &autoTagsJSON, &e.Author, &e.Team,
			&createdAt, &updatedAt, &e.Version,
			&e.Rating, &e.UsageCount, &e.TeamID, &e.Status, &mi,
		); err != nil {
			return nil, fmt.Errorf("scan keyword result: %w", err)
		}
		if err2 := json.Unmarshal([]byte(tagsJSON), &e.Tags); err2 != nil {
			e.Tags = []string{}
		}
		if err2 := json.Unmarshal([]byte(autoTagsJSON), &e.AutoTags); err2 != nil {
			e.AutoTags = []string{}
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
			SELECT e.id, e.type, e.title, e.content, e.description, e.domain, e.tags, e.auto_tags,
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
			var tagsJSON, autoTagsJSON, createdAt, updatedAt string
			var mi []byte
			if err := rows.Scan(
				&e.ID, &e.Type, &e.Title, &e.Content, &e.Description,
				&e.Domain, &tagsJSON, &autoTagsJSON, &e.Author, &e.Team,
				&createdAt, &updatedAt, &e.Version,
				&e.Rating, &e.UsageCount, &e.TeamID, &e.Status, &mi,
			); err != nil {
				return nil, fmt.Errorf("scan hybrid fts row: %w", err)
			}
			if err2 := json.Unmarshal([]byte(tagsJSON), &e.Tags); err2 != nil {
				e.Tags = []string{}
			}
			if err2 := json.Unmarshal([]byte(autoTagsJSON), &e.AutoTags); err2 != nil {
				e.AutoTags = []string{}
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
		autoTagsJSON, _ := json.Marshal(entry.AutoTags)
		_, insertErr := tx.ExecContext(ctx, `
			INSERT INTO entries (id, type, title, content, description, domain, tags, auto_tags, author, team, team_id, status)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		`, entry.ID, string(entry.Type), entry.Title, entry.Content,
			entry.Description, entry.Domain, string(tagsJSON), string(autoTagsJSON),
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
		SELECT id, type, title, content, description, domain, tags, auto_tags, author, team,
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
