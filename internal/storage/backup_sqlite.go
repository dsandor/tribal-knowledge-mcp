package storage

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	vec "github.com/asg017/sqlite-vec-go-bindings/cgo"
)

func (s *SQLiteStore) EngineName() string { return "sqlite" }

// DumpTable streams rows of table as column->value maps. []byte values are
// converted to string (all covered text columns are stored as TEXT/JSON-text).
func (s *SQLiteStore) DumpTable(ctx context.Context, table string, fn func(map[string]any) error) error {
	if !validTableName(table) {
		return fmt.Errorf("invalid table name %q", table)
	}
	rows, err := s.db.QueryContext(ctx, "SELECT * FROM "+table) //nolint:gosec // table validated against allowlist
	if err != nil {
		return fmt.Errorf("select %s: %w", table, err)
	}
	defer rows.Close()
	cols, err := rows.Columns()
	if err != nil {
		return err
	}
	for rows.Next() {
		vals := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return fmt.Errorf("scan %s: %w", table, err)
		}
		row := make(map[string]any, len(cols))
		for i, c := range cols {
			if b, ok := vals[i].([]byte); ok {
				row[c] = string(b)
			} else {
				row[c] = vals[i]
			}
		}
		if err := fn(row); err != nil {
			return err
		}
	}
	return rows.Err()
}

// LoadTable inserts rows using the column set of the first row.
func (s *SQLiteStore) LoadTable(ctx context.Context, table string, rows []map[string]any) error {
	if len(rows) == 0 {
		return nil
	}
	if !validTableName(table) {
		return fmt.Errorf("invalid table name %q", table)
	}
	cols := sortedKeys(rows[0])
	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(cols)), ",")
	stmt := fmt.Sprintf("INSERT INTO %s (%s) VALUES (%s)", table, strings.Join(cols, ","), placeholders)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck // no-op after commit
	for _, r := range rows {
		args := make([]any, len(cols))
		for i, c := range cols {
			args[i] = r[c]
		}
		if _, err := tx.ExecContext(ctx, stmt, args...); err != nil {
			return fmt.Errorf("insert into %s: %w", table, err)
		}
	}
	return tx.Commit()
}

func (s *SQLiteStore) DumpEmbeddings(ctx context.Context, fn func(EmbeddingItem) error) error {
	rows, err := s.db.QueryContext(ctx, `
		SELECT e.id, ee.embedding
		FROM entries e JOIN entry_embeddings ee ON ee.rowid = e.rowid`)
	if err != nil {
		return fmt.Errorf("query embeddings: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		var blob []byte
		if err := rows.Scan(&id, &blob); err != nil {
			return err
		}
		v, err := deserializeFloat32(blob, s.embeddingDim)
		if err != nil {
			return fmt.Errorf("deserialize embedding %s: %w", id, err)
		}
		if err := fn(EmbeddingItem{EntryID: id, Vector: v}); err != nil {
			return err
		}
	}
	return rows.Err()
}

// LoadEmbeddings rebuilds vec_entries + entry_embeddings, resolving each entry
// UUID to its (possibly reassigned) rowid.
func (s *SQLiteStore) LoadEmbeddings(ctx context.Context, items []EmbeddingItem) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck // no-op after commit
	for _, it := range items {
		if len(it.Vector) != s.embeddingDim {
			return fmt.Errorf("embedding dim mismatch for %s: got %d want %d", it.EntryID, len(it.Vector), s.embeddingDim)
		}
		var rowID int64
		err := tx.QueryRowContext(ctx, "SELECT rowid FROM entries WHERE id = ?", it.EntryID).Scan(&rowID)
		if err == sql.ErrNoRows {
			return fmt.Errorf("embedding references unknown entry %s", it.EntryID)
		} else if err != nil {
			return err
		}
		blob, err := vec.SerializeFloat32(it.Vector)
		if err != nil {
			return fmt.Errorf("serialize embedding: %w", err)
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO vec_entries (rowid, embedding) VALUES (?, ?)`, rowID, blob); err != nil {
			return fmt.Errorf("insert vec_entries: %w", err)
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO entry_embeddings (rowid, embedding) VALUES (?, ?)`, rowID, blob); err != nil {
			return fmt.Errorf("insert entry_embeddings: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	// Rebuild chunk rows (entry_chunks + vec_chunks) from the restored entries and
	// per-entry embeddings so chunk-based similarity search works after restore.
	// Idempotent: only entries without chunks are touched.
	return s.backfillChunks()
}

func (s *SQLiteStore) IsEmpty(ctx context.Context) (bool, error) {
	var n int
	if err := s.db.QueryRowContext(ctx,
		`SELECT (SELECT COUNT(*) FROM entries) + (SELECT COUNT(*) FROM teams WHERE id != ?)`,
		UnassignedTeamID).Scan(&n); err != nil {
		return false, err
	}
	return n == 0, nil
}

// TruncateAll deletes covered tables in reverse dependency order, plus the
// embedding tables. Foreign keys are disabled for the duration to avoid order
// pitfalls, then re-enabled.
func (s *SQLiteStore) TruncateAll(ctx context.Context, tablesInInsertOrder []string) error {
	// Pin a single connection: PRAGMA foreign_keys is connection-scoped, so the
	// OFF pragma, the deletes, and the re-enable must all run on the same conn.
	conn, err := s.db.Conn(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()
	if _, err := conn.ExecContext(ctx, "PRAGMA foreign_keys = OFF"); err != nil {
		return err
	}
	// Re-enable on a background context so a cancelled ctx can't leave FK
	// enforcement disabled before the connection returns to the pool.
	defer func() { _, _ = conn.ExecContext(context.Background(), "PRAGMA foreign_keys = ON") }()
	del := func(t string) error {
		if !validTableName(t) {
			return fmt.Errorf("invalid table %q", t)
		}
		_, err := conn.ExecContext(ctx, "DELETE FROM "+t)
		return err
	}
	for _, t := range []string{"vec_chunks", "entry_chunks", "vec_entries", "entry_embeddings"} {
		if err := del(t); err != nil {
			return err
		}
	}
	for i := len(tablesInInsertOrder) - 1; i >= 0; i-- {
		if err := del(tablesInInsertOrder[i]); err != nil {
			return err
		}
	}
	return nil
}
