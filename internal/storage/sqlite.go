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
	return nil
}

func (s *SQLiteStore) StoreEntry(ctx context.Context, entry KnowledgeEntry, embedding []float32) (string, error) {
	if len(embedding) != s.embeddingDim {
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

	res, err := tx.ExecContext(ctx, `
		INSERT INTO entries (id, type, title, content, description, domain, tags, author, team)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, entry.ID, string(entry.Type), entry.Title, entry.Content,
		entry.Description, entry.Domain, string(tagsJSON), entry.Author, entry.Team)
	if err != nil {
		return "", fmt.Errorf("insert entry: %w", err)
	}

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

	if err := tx.Commit(); err != nil {
		return "", err
	}
	return entry.ID, nil
}

func (s *SQLiteStore) GetEntry(ctx context.Context, id string) (*KnowledgeEntry, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, type, title, content, description, domain, tags, author, team,
		       created_at, updated_at, version
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
	                 created_at, updated_at, version
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
	query += " ORDER BY created_at DESC LIMIT ?"
	args = append(args, limit)

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
		                    created_at, updated_at, version
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
