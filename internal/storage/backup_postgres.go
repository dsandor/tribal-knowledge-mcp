package storage

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	pgvector "github.com/pgvector/pgvector-go"
)

func (s *PostgresStore) EngineName() string { return "postgres" }

func (s *PostgresStore) DumpTable(ctx context.Context, table string, fn func(map[string]any) error) error {
	if !validTableName(table) {
		return fmt.Errorf("invalid table name %q", table)
	}
	rows, err := s.db.QueryContext(ctx, "SELECT * FROM "+table) //nolint:gosec // allowlisted
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

func (s *PostgresStore) LoadTable(ctx context.Context, table string, rows []map[string]any) error {
	if len(rows) == 0 {
		return nil
	}
	if !validTableName(table) {
		return fmt.Errorf("invalid table name %q", table)
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck // no-op after commit
	// SQLite stores booleans as INTEGER 0/1, so dumped rows carry numeric
	// values for columns that are BOOLEAN in Postgres. Introspect the target
	// table's boolean columns once and coerce numbers->bool before marshalling
	// so json_populate_record loads them reliably.
	boolCols, err := s.booleanColumns(ctx, table)
	if err != nil {
		return err
	}
	// SQLite allows NULL in columns that are NOT NULL in the Postgres schema
	// (e.g. api_keys.team_id / user_id are nullable in SQLite but NOT NULL DEFAULT
	// '' in Postgres — superadmin keys carry NULL there). A NULL dumped from SQLite
	// would violate the Postgres constraint, so introspect the target's NOT NULL
	// columns and coerce any NULL value to that column's typed zero ('' / 0 /
	// false). This normalizes SQLite's NULL to the canonical empty value the Go
	// model already uses (empty string = "no team").
	notNullCols, err := s.notNullColumns(ctx, table)
	if err != nil {
		return err
	}
	// json_populate_record coerces every column to the table's real types
	// (text->timestamptz, number->integer, etc.) using Postgres input functions.
	stmt := fmt.Sprintf("INSERT INTO %s SELECT * FROM json_populate_record(NULL::%s, $1::json)", table, table) //nolint:gosec // table allowlisted
	for _, r := range rows {
		coerceBooleanColumns(r, boolCols)
		coerceNotNullColumns(r, notNullCols)
		b, err := json.Marshal(r)
		if err != nil {
			return fmt.Errorf("marshal row for %s: %w", table, err)
		}
		if _, err := tx.ExecContext(ctx, stmt, string(b)); err != nil {
			return fmt.Errorf("insert into %s: %w", table, err)
		}
	}
	return tx.Commit()
}

func (s *PostgresStore) DumpEmbeddings(ctx context.Context, fn func(EmbeddingItem) error) error {
	rows, err := s.db.QueryContext(ctx, `SELECT entry_id, embedding FROM embeddings`)
	if err != nil {
		return fmt.Errorf("query embeddings: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		var v pgvector.Vector
		if err := rows.Scan(&id, &v); err != nil {
			return err
		}
		if err := fn(EmbeddingItem{EntryID: id, Vector: v.Slice()}); err != nil {
			return err
		}
	}
	return rows.Err()
}

func (s *PostgresStore) LoadEmbeddings(ctx context.Context, items []EmbeddingItem) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck // no-op after commit
	for _, it := range items {
		if len(it.Vector) != s.embeddingDim {
			return fmt.Errorf("embedding dim mismatch for %s: got %d want %d", it.EntryID, len(it.Vector), s.embeddingDim)
		}
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO embeddings (entry_id, embedding) VALUES ($1, $2)`,
			it.EntryID, pgvector.NewVector(it.Vector)); err != nil {
			return fmt.Errorf("insert embedding %s: %w", it.EntryID, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	// Rebuild chunk rows (entry_chunks + chunk_embeddings) from restored entries
	// and per-entry embeddings so chunk-based similarity search works after
	// restore. Idempotent: only entries without chunks are touched.
	return s.backfillChunks(ctx)
}

func (s *PostgresStore) IsEmpty(ctx context.Context) (bool, error) {
	var n int
	if err := s.db.QueryRowContext(ctx,
		`SELECT (SELECT COUNT(*) FROM entries) + (SELECT COUNT(*) FROM teams WHERE id != $1)`,
		UnassignedTeamID).Scan(&n); err != nil {
		return false, err
	}
	return n == 0, nil
}

// TruncateAll truncates every covered table plus embeddings in one statement
// with CASCADE, which ignores FK ordering.
func (s *PostgresStore) TruncateAll(ctx context.Context, tablesInInsertOrder []string) error {
	all := append([]string{"embeddings"}, tablesInInsertOrder...)
	for _, t := range all {
		if !validTableName(t) {
			return fmt.Errorf("invalid table %q", t)
		}
	}
	stmt := "TRUNCATE " + strings.Join(all, ", ") + " RESTART IDENTITY CASCADE"
	_, err := s.db.ExecContext(ctx, stmt) //nolint:gosec // allowlisted
	return err
}

// booleanColumns returns the set of column names that are BOOLEAN-typed for the
// given table, via information_schema introspection.
func (s *PostgresStore) booleanColumns(ctx context.Context, table string) (map[string]bool, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT column_name FROM information_schema.columns WHERE table_name = $1 AND data_type = 'boolean'`, table)
	if err != nil {
		return nil, fmt.Errorf("introspect bool columns for %s: %w", table, err)
	}
	defer rows.Close()
	out := map[string]bool{}
	for rows.Next() {
		var c string
		if err := rows.Scan(&c); err != nil {
			return nil, err
		}
		out[c] = true
	}
	return out, rows.Err()
}

// notNullColumns returns each NOT NULL column of table mapped to its SQL
// data_type (as reported by information_schema), via introspection.
func (s *PostgresStore) notNullColumns(ctx context.Context, table string) (map[string]string, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT column_name, data_type FROM information_schema.columns
		 WHERE table_name = $1 AND is_nullable = 'NO'`, table)
	if err != nil {
		return nil, fmt.Errorf("introspect not-null columns for %s: %w", table, err)
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var name, dtype string
		if err := rows.Scan(&name, &dtype); err != nil {
			return nil, err
		}
		out[name] = dtype
	}
	return out, rows.Err()
}

// coerceNotNullColumns replaces a NULL (or missing) value in any NOT NULL column
// with that column's typed zero value, so a cross-engine restore (SQLite NULL ->
// Postgres NOT NULL) does not violate the constraint. Mutates row in place.
// Unknown/unhandled types are left untouched (they are expected to always carry
// a value in a dump).
func coerceNotNullColumns(row map[string]any, notNullCols map[string]string) {
	for col, dtype := range notNullCols {
		if v, ok := row[col]; ok && v != nil {
			continue // already has a concrete value
		}
		switch dtype {
		case "text", "character varying", "character", "uuid", "name", "citext":
			row[col] = ""
		case "integer", "bigint", "smallint", "numeric", "real", "double precision":
			row[col] = 0
		case "boolean":
			row[col] = false
			// Other types (timestamps, json/jsonb, etc.) always carry a value in a
			// dump for NOT NULL columns, so they are intentionally left alone.
		}
	}
}

// coerceBooleanColumns rewrites numeric 0/1 values to bool for the named
// boolean columns, so json_populate_record loads them into BOOLEAN columns
// reliably (SQLite dumps booleans as INTEGER). Mutates row in place.
func coerceBooleanColumns(row map[string]any, boolCols map[string]bool) {
	for col := range boolCols {
		v, ok := row[col]
		if !ok || v == nil {
			continue
		}
		switch n := v.(type) {
		case bool:
			// already fine
		case float64:
			row[col] = n != 0
		case int64:
			row[col] = n != 0
		case int:
			row[col] = n != 0
		}
	}
}
