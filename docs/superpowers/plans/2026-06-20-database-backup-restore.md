# Database Backup & Restore Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a logical, engine-neutral full-database backup/restore that round-trips all server state (knowledge, embeddings, agents, rules, teams, users, auth, settings, history) across SQLite ↔ PostgreSQL, exposed via CLI subcommands and a superadmin-only web UI.

**Architecture:** A new `internal/backup` package owns the archive format (`.tar.gz` of `manifest.json` + per-table JSONL) and the export/import orchestration. It talks to storage through a new `BackupStore` interface (generic `DumpTable`/`LoadTable` over `SELECT *`, plus a dedicated embeddings path that serializes vectors as float arrays). Both `SQLiteStore` and `PostgresStore` implement it. `cmd/server/main.go` dispatches `export`/`import` subcommands before starting the server; the web layer adds superadmin `GET /api/admin/backup` and `POST /api/admin/restore` routes.

**Tech Stack:** Go (`database/sql`, `archive/tar`, `compress/gzip`, `encoding/json`), mattn/go-sqlite3 + sqlite-vec, pgx/v5 + pgvector-go, chi router, React/TypeScript + Vite.

---

## Reference: facts established from the codebase

- **SQLite embeddings:** `entry_embeddings(rowid REFERENCES entries(rowid), embedding BLOB)` + virtual table `vec_entries(rowid, embedding FLOAT[dim])`. Blobs serialized with `vec.SerializeFloat32` / read with `deserializeFloat32(blob, dim)` (`internal/storage/analysis.go`). Join `entry_embeddings.rowid = entries.rowid` to get the UUID.
- **Postgres embeddings:** table `embeddings(entry_id UUID, embedding vector(dim))` via `pgvector.NewVector` / `pgvector.Vector`.
- **`SELECT *` on SQLite excludes the hidden `rowid`** — entries dumps carry the stable UUID `id`, never `rowid`. Good (rowid is not portable).
- **The only true BLOB column among covered tables is the embedding**, which is handled by the dedicated embeddings path. Every other "blob-ish" column (`tags`, `entry_ids`, etc.) is TEXT/JSON-as-text. Therefore generic `DumpTable` MUST convert `[]byte` scan results to `string` (otherwise JSON base64-encodes them and corrupts text columns on reload).
- **Both stores use `database/sql`** (`s.db *sql.DB`). SQLite placeholders `?`, Postgres `$1..$n`.
- **Config:** `cfg.EmbeddingDim` (`internal/config/config.go:17`), `cfg.DatabaseURL`, `cfg.DBPath`. Store constructors: `storage.NewSQLiteStore(path, dim)`, `storage.NewPostgresStore(dsn, dim)`.
- **main.go** currently never inspects `os.Args`; it always starts the server (`cmd/server/main.go:35`). Store construction lives at lines 80-99.
- **Web routes** registered in `internal/web/server.go`. Global `r.Use(maxBodySize)` at line 164 caps ALL bodies at 1 MiB (`maxBodySize` at server.go:150-162). Superadmin group uses `r.Use(auth.RequireSuperadmin())` (server.go ~line 245). Server holds `s.store` and `s.router`.
- **Covered tables, dependency order (parents first):** `teams`, `users`, `api_keys`, `auth_config`, `team_settings`, `entries`, `clusters`, `pipeline_runs`, `dataset_snapshots`, `analysis_cache`, `rules`, `agents`, `agent_versions`, `activity_log`, `usage_events`, `outcome_ratings`, `feed_activity`. **Excluded:** `sessions` (ephemeral), `vec_entries` (rebuilt from embeddings). Embeddings are carried by the dedicated path, not generic `DumpTable`.

> **Known risk (timestamps):** Generic JSON round-trips `time.Time` → RFC3339 string. SQLite `DATETIME` columns accept strings transparently. For Postgres `timestamptz`, the `LoadTable` impl must let pgx coerce text→timestamp (pass the string value; pgx simple-query casts text literals). The cross-engine test (Task 9) is the gate that proves this; if a column rejects the cast, handle it in `PostgresStore.LoadTable` by leaving the value as a string and relying on the implicit cast, or quoting as needed. Do not pre-optimize — let the test drive it.

---

## Task 1: Archive types, manifest, and canonical table list

**Files:**
- Create: `internal/backup/archive.go`
- Test: `internal/backup/archive_test.go`

- [ ] **Step 1: Write the failing test**

```go
package backup

import "testing"

func TestCoveredTablesOrderingAndExclusions(t *testing.T) {
	got := CoveredTables()
	if len(got) == 0 {
		t.Fatal("CoveredTables is empty")
	}
	// teams must precede users and api_keys (FK parents first)
	idx := map[string]int{}
	for i, name := range got {
		idx[name] = i
	}
	for _, child := range []string{"users", "api_keys"} {
		if idx["teams"] > idx[child] {
			t.Errorf("teams must come before %s", child)
		}
	}
	if idx["entries"] > idx["clusters"] {
		t.Error("entries must come before clusters")
	}
	for _, excluded := range []string{"sessions", "vec_entries", "entry_embeddings", "embeddings"} {
		if _, ok := idx[excluded]; ok {
			t.Errorf("%s must not be in CoveredTables", excluded)
		}
	}
}

func TestFormatVersionConst(t *testing.T) {
	if FormatVersion != 1 {
		t.Errorf("FormatVersion = %d, want 1", FormatVersion)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/backup/ -run TestCovered -v`
Expected: FAIL — package/undefined `CoveredTables`, `FormatVersion`.

- [ ] **Step 3: Write minimal implementation**

```go
// Package backup provides engine-neutral logical backup and restore of the
// entire tribal-knowledge database, enabling migration across storage engines
// (SQLite <-> PostgreSQL).
package backup

import "time"

// FormatVersion is the archive format version. Bump on incompatible changes.
const FormatVersion = 1

// coveredTables lists every backed-up table in dependency order (parents first).
// Used forward for insert, reversed for truncate. Embeddings travel via the
// dedicated embeddings path, not as a generic table. sessions are excluded
// (ephemeral); vec_entries is rebuilt from embeddings on restore.
var coveredTables = []string{
	"teams",
	"users",
	"api_keys",
	"auth_config",
	"team_settings",
	"entries",
	"clusters",
	"pipeline_runs",
	"dataset_snapshots",
	"analysis_cache",
	"rules",
	"agents",
	"agent_versions",
	"activity_log",
	"usage_events",
	"outcome_ratings",
	"feed_activity",
}

// CoveredTables returns the ordered list of backed-up tables (a copy).
func CoveredTables() []string {
	out := make([]string, len(coveredTables))
	copy(out, coveredTables)
	return out
}

// Manifest describes an archive's provenance and contents.
type Manifest struct {
	FormatVersion int            `json:"format_version"`
	ToolVersion   string         `json:"tool_version"`
	CreatedAt     time.Time      `json:"created_at"`
	SourceEngine  string         `json:"source_engine"` // "sqlite" | "postgres"
	EmbeddingDim  int            `json:"embedding_dim"`
	Tables        map[string]int `json:"tables"` // table name -> row count
	Embeddings    int            `json:"embeddings"`
}

// Report summarizes a completed restore.
type Report struct {
	TablesRestored     map[string]int
	EmbeddingsRestored int
}

// ImportOptions controls restore behavior.
type ImportOptions struct {
	Force bool // truncate a non-empty target before restoring
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/backup/ -run "TestCovered|TestFormat" -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/backup/archive.go internal/backup/archive_test.go
git commit -m "feat(backup): archive types, manifest, covered-table list"
```

---

## Task 2: BackupStore interface

**Files:**
- Create: `internal/storage/backup.go`
- Test: `internal/storage/backup_test.go`

- [ ] **Step 1: Write the failing test**

```go
package storage

import (
	"context"
	"testing"
)

// Compile-time assertions that both stores implement BackupStore.
var (
	_ BackupStore = (*SQLiteStore)(nil)
	_ BackupStore = (*PostgresStore)(nil)
)

func TestEmbeddingItemRoundTripsType(t *testing.T) {
	item := EmbeddingItem{EntryID: "abc", Vector: []float32{1, 2, 3}}
	if item.EntryID != "abc" || len(item.Vector) != 3 {
		t.Fatal("EmbeddingItem fields wrong")
	}
	_ = context.Background()
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/storage/ -run TestEmbeddingItem -v`
Expected: FAIL — undefined `BackupStore`, `EmbeddingItem`.

- [ ] **Step 3: Write minimal implementation**

```go
package storage

import "context"

// EmbeddingItem is an engine-neutral embedding keyed by the stable entry UUID.
type EmbeddingItem struct {
	EntryID string    `json:"entry_id"`
	Vector  []float32 `json:"embedding"`
}

// BackupStore is implemented by every storage engine to support logical
// backup and restore. Row values are exchanged as ordered column->value maps;
// implementations MUST return text columns as Go strings (never []byte) so the
// JSON encoder does not base64-encode them.
type BackupStore interface {
	// EngineName returns "sqlite" or "postgres".
	EngineName() string

	// DumpTable streams every row of table as a column->value map.
	DumpTable(ctx context.Context, table string, fn func(row map[string]any) error) error

	// LoadTable inserts rows (parameterized) into table. No-op if rows is empty.
	LoadTable(ctx context.Context, table string, rows []map[string]any) error

	// DumpEmbeddings streams every embedding keyed by entry UUID.
	DumpEmbeddings(ctx context.Context, fn func(item EmbeddingItem) error) error

	// LoadEmbeddings writes embeddings in the engine's native format.
	LoadEmbeddings(ctx context.Context, items []EmbeddingItem) error

	// IsEmpty reports whether the target has no entries and no teams beyond the
	// bootstrap "unassigned" team (i.e. safe to restore into without --force).
	IsEmpty(ctx context.Context) (bool, error)

	// TruncateAll deletes all covered tables (and embeddings) in FK-safe order.
	TruncateAll(ctx context.Context, tablesInInsertOrder []string) error
}
```

- [ ] **Step 4: Run test to verify it fails to compile (stores don't implement yet)**

Run: `go build ./internal/storage/`
Expected: FAIL — `*SQLiteStore`/`*PostgresStore` do not implement `BackupStore`. This is expected; Tasks 3-4 add the methods. Temporarily comment out the two `var _ BackupStore = ...` assertions to let unrelated work compile if needed, OR proceed directly to Task 3 and keep them.

- [ ] **Step 5: Commit**

```bash
git add internal/storage/backup.go internal/storage/backup_test.go
git commit -m "feat(storage): BackupStore interface + EmbeddingItem"
```

---

## Task 3: SQLiteStore implements BackupStore

**Files:**
- Create: `internal/storage/backup_sqlite.go`
- Test: `internal/storage/backup_sqlite_test.go`

- [ ] **Step 1: Write the failing test**

```go
package storage

import (
	"context"
	"testing"
)

func newTestSQLite(t *testing.T) *SQLiteStore {
	t.Helper()
	s, err := NewSQLiteStore(t.TempDir()+"/test.db", 4)
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestSQLiteDumpLoadTableRoundTrip(t *testing.T) {
	ctx := context.Background()
	s := newTestSQLite(t)
	if _, err := s.StoreEntry(ctx, KnowledgeEntry{Type: "note", Title: "T1", Content: "C1", Tags: []string{"a", "b"}}, []float32{1, 2, 3, 4}); err != nil {
		t.Fatalf("StoreEntry: %v", err)
	}

	var rows []map[string]any
	if err := s.DumpTable(ctx, "entries", func(r map[string]any) error {
		rows = append(rows, r)
		return nil
	}); err != nil {
		t.Fatalf("DumpTable: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("got %d rows, want 1", len(rows))
	}
	// tags is a TEXT column — must be a string, not []byte.
	if _, ok := rows[0]["tags"].(string); !ok {
		t.Fatalf("tags should be string, got %T", rows[0]["tags"])
	}

	dst := newTestSQLite(t)
	if err := dst.LoadTable(ctx, "entries", rows); err != nil {
		t.Fatalf("LoadTable: %v", err)
	}
	got, err := dst.GetEntry(ctx, rows[0]["id"].(string))
	if err != nil {
		t.Fatalf("GetEntry: %v", err)
	}
	if got.Title != "T1" {
		t.Errorf("title = %q, want T1", got.Title)
	}
}

func TestSQLiteEmbeddingsRoundTrip(t *testing.T) {
	ctx := context.Background()
	s := newTestSQLite(t)
	id, _ := s.StoreEntry(ctx, KnowledgeEntry{Type: "note", Title: "T", Content: "C"}, []float32{1, 2, 3, 4})

	var items []EmbeddingItem
	if err := s.DumpEmbeddings(ctx, func(it EmbeddingItem) error {
		items = append(items, it)
		return nil
	}); err != nil {
		t.Fatalf("DumpEmbeddings: %v", err)
	}
	if len(items) != 1 || items[0].EntryID != id || len(items[0].Vector) != 4 {
		t.Fatalf("unexpected embeddings: %+v", items)
	}

	dst := newTestSQLite(t)
	dst.LoadTable(ctx, "entries", dumpAll(t, s, "entries"))
	if err := dst.LoadEmbeddings(ctx, items); err != nil {
		t.Fatalf("LoadEmbeddings: %v", err)
	}
	// vec_entries must be rebuilt: a similarity search should return the entry.
	res, err := dst.SearchSimilar(ctx, []float32{1, 2, 3, 4}, 1)
	if err != nil {
		t.Fatalf("SearchSimilar: %v", err)
	}
	if len(res) != 1 {
		t.Fatalf("expected 1 search result, got %d", len(res))
	}
}

func TestSQLiteIsEmpty(t *testing.T) {
	ctx := context.Background()
	s := newTestSQLite(t)
	empty, err := s.IsEmpty(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !empty {
		t.Error("fresh store should be empty")
	}
	s.StoreEntry(ctx, KnowledgeEntry{Type: "note", Title: "X", Content: "Y"}, nil)
	empty, _ = s.IsEmpty(ctx)
	if empty {
		t.Error("store with an entry should not be empty")
	}
}

// dumpAll is a tiny test helper.
func dumpAll(t *testing.T, s BackupStore, table string) []map[string]any {
	t.Helper()
	var rows []map[string]any
	if err := s.DumpTable(context.Background(), table, func(r map[string]any) error {
		rows = append(rows, r)
		return nil
	}); err != nil {
		t.Fatalf("dumpAll %s: %v", table, err)
	}
	return rows
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/storage/ -run "TestSQLiteDumpLoad|TestSQLiteEmbeddings|TestSQLiteIsEmpty" -v`
Expected: FAIL — methods undefined.

- [ ] **Step 3: Write minimal implementation**

```go
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
	for _, r := range rows {
		args := make([]any, len(cols))
		for i, c := range cols {
			args[i] = r[c]
		}
		if _, err := s.db.ExecContext(ctx, stmt, args...); err != nil {
			return fmt.Errorf("insert into %s: %w", table, err)
		}
	}
	return nil
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
	for _, it := range items {
		if len(it.Vector) != s.embeddingDim {
			return fmt.Errorf("embedding dim mismatch for %s: got %d want %d", it.EntryID, len(it.Vector), s.embeddingDim)
		}
		var rowID int64
		err := s.db.QueryRowContext(ctx, "SELECT rowid FROM entries WHERE id = ?", it.EntryID).Scan(&rowID)
		if err == sql.ErrNoRows {
			return fmt.Errorf("embedding references unknown entry %s", it.EntryID)
		} else if err != nil {
			return err
		}
		blob, err := vec.SerializeFloat32(it.Vector)
		if err != nil {
			return fmt.Errorf("serialize embedding: %w", err)
		}
		if _, err := s.db.ExecContext(ctx, `INSERT INTO vec_entries (rowid, embedding) VALUES (?, ?)`, rowID, blob); err != nil {
			return fmt.Errorf("insert vec_entries: %w", err)
		}
		if _, err := s.db.ExecContext(ctx, `INSERT INTO entry_embeddings (rowid, embedding) VALUES (?, ?)`, rowID, blob); err != nil {
			return fmt.Errorf("insert entry_embeddings: %w", err)
		}
	}
	return nil
}

func (s *SQLiteStore) IsEmpty(ctx context.Context) (bool, error) {
	var n int
	if err := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM entries").Scan(&n); err != nil {
		return false, err
	}
	return n == 0, nil
}

// TruncateAll deletes covered tables in reverse dependency order, plus the
// embedding tables. Foreign keys are disabled for the duration to avoid order
// pitfalls, then re-enabled.
func (s *SQLiteStore) TruncateAll(ctx context.Context, tablesInInsertOrder []string) error {
	if _, err := s.db.ExecContext(ctx, "PRAGMA foreign_keys = OFF"); err != nil {
		return err
	}
	defer s.db.ExecContext(ctx, "PRAGMA foreign_keys = ON")
	del := func(t string) error {
		if !validTableName(t) {
			return fmt.Errorf("invalid table %q", t)
		}
		_, err := s.db.ExecContext(ctx, "DELETE FROM "+t)
		return err
	}
	for _, t := range []string{"vec_entries", "entry_embeddings"} {
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
```

Add shared helpers (place in `internal/storage/backup.go`):

```go
import "sort"

// validTableName guards string-concatenated table names against injection.
// Only covered tables plus the embedding tables are permitted.
func validTableName(t string) bool {
	switch t {
	case "teams", "users", "api_keys", "auth_config", "team_settings",
		"entries", "clusters", "pipeline_runs", "dataset_snapshots",
		"analysis_cache", "rules", "agents", "agent_versions",
		"activity_log", "usage_events", "outcome_ratings", "feed_activity",
		"vec_entries", "entry_embeddings", "embeddings":
		return true
	}
	return false
}

func sortedKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/storage/ -run "TestSQLite(DumpLoad|Embeddings|IsEmpty)" -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/storage/backup_sqlite.go internal/storage/backup.go internal/storage/backup_sqlite_test.go
git commit -m "feat(storage): SQLite BackupStore implementation"
```

---

## Task 4: PostgresStore implements BackupStore

**Files:**
- Create: `internal/storage/backup_postgres.go`
- Test: covered by Task 9 cross-engine test (gated on `TEST_DATABASE_URL`).

- [ ] **Step 1: Write the implementation** (mirrors SQLite; embeddings use pgvector; truncate uses `TRUNCATE ... CASCADE`).

```go
package storage

import (
	"context"
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
	cols := sortedKeys(rows[0])
	ph := make([]string, len(cols))
	for i := range cols {
		ph[i] = fmt.Sprintf("$%d", i+1)
	}
	stmt := fmt.Sprintf("INSERT INTO %s (%s) VALUES (%s)", table, strings.Join(cols, ","), strings.Join(ph, ","))
	for _, r := range rows {
		args := make([]any, len(cols))
		for i, c := range cols {
			args[i] = r[c]
		}
		if _, err := s.db.ExecContext(ctx, stmt, args...); err != nil {
			return fmt.Errorf("insert into %s: %w", table, err)
		}
	}
	return nil
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
	for _, it := range items {
		if len(it.Vector) != s.embeddingDim {
			return fmt.Errorf("embedding dim mismatch for %s: got %d want %d", it.EntryID, len(it.Vector), s.embeddingDim)
		}
		if _, err := s.db.ExecContext(ctx,
			`INSERT INTO embeddings (entry_id, embedding) VALUES ($1, $2)`,
			it.EntryID, pgvector.NewVector(it.Vector)); err != nil {
			return fmt.Errorf("insert embedding %s: %w", it.EntryID, err)
		}
	}
	return nil
}

func (s *PostgresStore) IsEmpty(ctx context.Context) (bool, error) {
	var n int
	if err := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM entries").Scan(&n); err != nil {
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
```

- [ ] **Step 2: Verify it compiles and the interface assertion passes**

Run: `go build ./internal/storage/ && go vet ./internal/storage/`
Expected: builds clean; `var _ BackupStore = (*PostgresStore)(nil)` in Task 2 now satisfied.

- [ ] **Step 3: Commit**

```bash
git add internal/storage/backup_postgres.go
git commit -m "feat(storage): Postgres BackupStore implementation"
```

---

## Task 5: Export — write the .tar.gz archive

**Files:**
- Create: `internal/backup/export.go`
- Test: `internal/backup/export_test.go` (uses an in-memory fake `BackupStore`)

- [ ] **Step 1: Write the failing test**

```go
package backup

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"io"
	"strings"
	"testing"

	"tribal-knowledge/internal/storage"
)

func TestExportWritesManifestAndTables(t *testing.T) {
	fake := newFakeStore()
	fake.tables["entries"] = []map[string]any{{"id": "e1", "title": "Hello"}}
	fake.tables["teams"] = []map[string]any{{"id": "t1", "name": "Team"}}
	fake.embeddings = []storage.EmbeddingItem{{EntryID: "e1", Vector: []float32{1, 2, 3, 4}}}

	var buf bytes.Buffer
	man, err := Export(context.Background(), fake, &buf, "test-1.0", 4)
	if err != nil {
		t.Fatalf("Export: %v", err)
	}
	if man.EmbeddingDim != 4 || man.SourceEngine != "fake" {
		t.Errorf("manifest = %+v", man)
	}
	if man.Tables["entries"] != 1 || man.Embeddings != 1 {
		t.Errorf("counts wrong: %+v", man)
	}

	files := readTarGz(t, &buf)
	if _, ok := files["manifest.json"]; !ok {
		t.Fatal("manifest.json missing")
	}
	if _, ok := files["tables/entries.jsonl"]; !ok {
		t.Fatal("tables/entries.jsonl missing")
	}
	if !strings.Contains(files["tables/entry_embeddings.jsonl"], "e1") {
		t.Fatal("embeddings jsonl missing entry")
	}
	var gotMan Manifest
	if err := json.Unmarshal([]byte(files["manifest.json"]), &gotMan); err != nil {
		t.Fatalf("manifest parse: %v", err)
	}
}

func readTarGz(t *testing.T, r io.Reader) map[string]string {
	t.Helper()
	gz, err := gzip.NewReader(r)
	if err != nil {
		t.Fatal(err)
	}
	tr := tar.NewReader(gz)
	out := map[string]string{}
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		b, _ := io.ReadAll(tr)
		out[h.Name] = string(b)
	}
	return out
}
```

Add the fake store in `internal/backup/fake_store_test.go`:

```go
package backup

import (
	"context"

	"tribal-knowledge/internal/storage"
)

type fakeStore struct {
	tables     map[string][]map[string]any
	embeddings []storage.EmbeddingItem
	empty      bool
}

func newFakeStore() *fakeStore {
	return &fakeStore{tables: map[string][]map[string]any{}, empty: true}
}

func (f *fakeStore) EngineName() string { return "fake" }
func (f *fakeStore) DumpTable(_ context.Context, t string, fn func(map[string]any) error) error {
	for _, r := range f.tables[t] {
		if err := fn(r); err != nil {
			return err
		}
	}
	return nil
}
func (f *fakeStore) LoadTable(_ context.Context, t string, rows []map[string]any) error {
	f.tables[t] = append(f.tables[t], rows...)
	f.empty = false
	return nil
}
func (f *fakeStore) DumpEmbeddings(_ context.Context, fn func(storage.EmbeddingItem) error) error {
	for _, e := range f.embeddings {
		if err := fn(e); err != nil {
			return err
		}
	}
	return nil
}
func (f *fakeStore) LoadEmbeddings(_ context.Context, items []storage.EmbeddingItem) error {
	f.embeddings = append(f.embeddings, items...)
	return nil
}
func (f *fakeStore) IsEmpty(context.Context) (bool, error) { return f.empty, nil }
func (f *fakeStore) TruncateAll(context.Context, []string) error {
	f.tables = map[string][]map[string]any{}
	f.embeddings = nil
	f.empty = true
	return nil
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/backup/ -run TestExport -v`
Expected: FAIL — undefined `Export`.

- [ ] **Step 3: Write minimal implementation**

```go
package backup

import (
	"archive/tar"
	"bufio"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"

	"tribal-knowledge/internal/storage"
)

// nowFunc is overridable in tests; production uses time.Now via the caller.
// We accept createdAt through the manifest to avoid time.Now in this package.

// Export streams a .tar.gz logical backup of store to w and returns the manifest.
// createdAt is supplied by the caller (CLI/web) since this package avoids time.Now.
func Export(ctx context.Context, store storage.BackupStore, w io.Writer, toolVersion string, embeddingDim int) (Manifest, error) {
	man := Manifest{
		FormatVersion: FormatVersion,
		ToolVersion:   toolVersion,
		SourceEngine:  store.EngineName(),
		EmbeddingDim:  embeddingDim,
		Tables:        map[string]int{},
	}

	gz := gzip.NewWriter(w)
	tw := tar.NewWriter(gz)

	// Per-table JSONL files. We must know the byte length before writing a tar
	// header, so each table is buffered to memory. Tables are small relative to
	// embeddings; embeddings stream row-by-row but are also buffered here for
	// the same reason. (Acceptable: full DB easily fits in memory for v1.)
	writeFile := func(name string, body []byte) error {
		if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0o600, Size: int64(len(body))}); err != nil {
			return err
		}
		_, err := tw.Write(body)
		return err
	}

	for _, table := range CoveredTables() {
		var buf bufferWriter
		count := 0
		enc := json.NewEncoder(&buf)
		if err := store.DumpTable(ctx, table, func(row map[string]any) error {
			count++
			return enc.Encode(row)
		}); err != nil {
			return man, fmt.Errorf("dump %s: %w", table, err)
		}
		man.Tables[table] = count
		if err := writeFile("tables/"+table+".jsonl", buf.Bytes()); err != nil {
			return man, err
		}
	}

	// Embeddings.
	var ebuf bufferWriter
	eenc := json.NewEncoder(&ebuf)
	ecount := 0
	if err := store.DumpEmbeddings(ctx, func(it storage.EmbeddingItem) error {
		ecount++
		return eenc.Encode(it)
	}); err != nil {
		return man, fmt.Errorf("dump embeddings: %w", err)
	}
	man.Embeddings = ecount
	if err := writeFile("tables/entry_embeddings.jsonl", ebuf.Bytes()); err != nil {
		return man, err
	}

	// Manifest last (so counts are final). Tar order does not matter for reads.
	mb, err := json.MarshalIndent(man, "", "  ")
	if err != nil {
		return man, err
	}
	if err := writeFile("manifest.json", mb); err != nil {
		return man, err
	}

	if err := tw.Close(); err != nil {
		return man, err
	}
	if err := gz.Close(); err != nil {
		return man, err
	}
	return man, nil
}

// bufferWriter is a minimal bytes buffer wrapper to keep imports tidy.
type bufferWriter struct{ b []byte }

func (w *bufferWriter) Write(p []byte) (int, error) { w.b = append(w.b, p...); return len(p), nil }
func (w *bufferWriter) Bytes() []byte               { return w.b }

var _ = bufio.NewWriter // keep import set stable if later switched to streaming
```

> Note: `CreatedAt` is set by the caller (CLI/web) after `Export` returns, or pass it in. To keep the manifest self-contained, set `man.CreatedAt` in the caller before serialization is not possible here (manifest is written inside Export). Simplest: add a `createdAt time.Time` parameter to `Export`. Update the signature to `Export(ctx, store, w, toolVersion, embeddingDim int, createdAt time.Time)` and set `man.CreatedAt = createdAt`. Update the test call accordingly (pass `time.Unix(0,0)`).

Apply that signature tweak now (add the param, set the field, update the test to pass `time.Unix(0, 0)`).

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/backup/ -run TestExport -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/backup/export.go internal/backup/export_test.go internal/backup/fake_store_test.go
git commit -m "feat(backup): tar.gz export with manifest + per-table JSONL"
```

---

## Task 6: Import — validate manifest, restore tables + embeddings

**Files:**
- Create: `internal/backup/import.go`
- Test: `internal/backup/import_test.go`

- [ ] **Step 1: Write the failing test**

```go
package backup

import (
	"bytes"
	"context"
	"testing"
	"time"
)

func TestImportRoundTripViaFake(t *testing.T) {
	src := newFakeStore()
	src.tables["teams"] = []map[string]any{{"id": "t1", "name": "Team"}}
	src.tables["entries"] = []map[string]any{{"id": "e1", "title": "Hi"}}
	src.embeddings = []EmbItem(t, "e1")

	var buf bytes.Buffer
	if _, err := Export(context.Background(), src, &buf, "v", 4, time.Unix(0, 0)); err != nil {
		t.Fatal(err)
	}

	dst := newFakeStore()
	rep, err := Import(context.Background(), dst, &buf, ImportOptions{}, 4)
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	if rep.TablesRestored["entries"] != 1 || rep.EmbeddingsRestored != 1 {
		t.Errorf("report = %+v", rep)
	}
	if len(dst.tables["entries"]) != 1 || len(dst.embeddings) != 1 {
		t.Errorf("dst not populated: %+v", dst.tables)
	}
}

func TestImportRefusesNonEmptyWithoutForce(t *testing.T) {
	var buf bytes.Buffer
	Export(context.Background(), newFakeStore(), &buf, "v", 4, time.Unix(0, 0))
	dst := newFakeStore()
	dst.empty = false
	if _, err := Import(context.Background(), dst, &buf, ImportOptions{}, 4); err == nil {
		t.Fatal("expected refusal on non-empty target")
	}
}

func TestImportRejectsDimMismatch(t *testing.T) {
	var buf bytes.Buffer
	Export(context.Background(), newFakeStore(), &buf, "v", 4, time.Unix(0, 0))
	if _, err := Import(context.Background(), newFakeStore(), &buf, ImportOptions{}, 8); err == nil {
		t.Fatal("expected embedding_dim mismatch error")
	}
}
```

Add helper in `import_test.go`:

```go
import "tribal-knowledge/internal/storage"

func EmbItem(t *testing.T, id string) []storage.EmbeddingItem {
	t.Helper()
	return []storage.EmbeddingItem{{EntryID: id, Vector: []float32{1, 2, 3, 4}}}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/backup/ -run TestImport -v`
Expected: FAIL — undefined `Import`.

- [ ] **Step 3: Write minimal implementation**

```go
package backup

import (
	"archive/tar"
	"bufio"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"

	"tribal-knowledge/internal/storage"
)

// Import restores an archive (read from r) into store. targetDim is the target
// store's configured embedding dimension; the archive's embedding_dim must match.
func Import(ctx context.Context, store storage.BackupStore, r io.Reader, opts ImportOptions, targetDim int) (Report, error) {
	rep := Report{TablesRestored: map[string]int{}}

	// Load the whole archive into memory (acceptable for v1 DB sizes).
	gz, err := gzip.NewReader(r)
	if err != nil {
		return rep, fmt.Errorf("gzip: %w", err)
	}
	tr := tar.NewReader(gz)
	files := map[string][]byte{}
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return rep, fmt.Errorf("tar: %w", err)
		}
		b, err := io.ReadAll(tr)
		if err != nil {
			return rep, err
		}
		files[h.Name] = b
	}

	// Validate manifest.
	mb, ok := files["manifest.json"]
	if !ok {
		return rep, fmt.Errorf("archive missing manifest.json")
	}
	var man Manifest
	if err := json.Unmarshal(mb, &man); err != nil {
		return rep, fmt.Errorf("parse manifest: %w", err)
	}
	if man.FormatVersion != FormatVersion {
		return rep, fmt.Errorf("unsupported archive format_version %d (this build supports %d)", man.FormatVersion, FormatVersion)
	}
	if man.EmbeddingDim != targetDim {
		return rep, fmt.Errorf("embedding_dim mismatch: archive=%d target=%d; cannot restore", man.EmbeddingDim, targetDim)
	}

	// Emptiness / force check.
	empty, err := store.IsEmpty(ctx)
	if err != nil {
		return rep, err
	}
	if !empty && !opts.Force {
		return rep, fmt.Errorf("target database is not empty; re-run with --force to overwrite")
	}
	if !empty && opts.Force {
		if err := store.TruncateAll(ctx, CoveredTables()); err != nil {
			return rep, fmt.Errorf("truncate target: %w", err)
		}
	}

	// Restore tables in dependency order.
	for _, table := range CoveredTables() {
		body, ok := files["tables/"+table+".jsonl"]
		if !ok {
			continue
		}
		rows, err := decodeRows(body)
		if err != nil {
			return rep, fmt.Errorf("decode %s: %w", table, err)
		}
		if err := store.LoadTable(ctx, table, rows); err != nil {
			return rep, fmt.Errorf("load %s: %w", table, err)
		}
		rep.TablesRestored[table] = len(rows)
	}

	// Restore embeddings.
	if body, ok := files["tables/entry_embeddings.jsonl"]; ok {
		var items []storage.EmbeddingItem
		sc := bufio.NewScanner(bytes.NewReader(body))
		sc.Buffer(make([]byte, 0, 1024*1024), 16*1024*1024)
		for sc.Scan() {
			line := sc.Bytes()
			if len(bytes.TrimSpace(line)) == 0 {
				continue
			}
			var it storage.EmbeddingItem
			if err := json.Unmarshal(line, &it); err != nil {
				return rep, fmt.Errorf("decode embedding: %w", err)
			}
			items = append(items, it)
		}
		if err := sc.Err(); err != nil {
			return rep, err
		}
		if err := store.LoadEmbeddings(ctx, items); err != nil {
			return rep, fmt.Errorf("load embeddings: %w", err)
		}
		rep.EmbeddingsRestored = len(items)
	}

	return rep, nil
}

func decodeRows(body []byte) ([]map[string]any, error) {
	var rows []map[string]any
	sc := bufio.NewScanner(bytes.NewReader(body))
	sc.Buffer(make([]byte, 0, 1024*1024), 16*1024*1024)
	for sc.Scan() {
		line := sc.Bytes()
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		var row map[string]any
		if err := json.Unmarshal(line, &row); err != nil {
			return nil, err
		}
		rows = append(rows, row)
	}
	return rows, sc.Err()
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/backup/ -run TestImport -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/backup/import.go internal/backup/import_test.go
git commit -m "feat(backup): restore with manifest validation + force/empty guards"
```

---

## Task 7: SQLite end-to-end round-trip test

**Files:**
- Create: `internal/backup/roundtrip_sqlite_test.go`

- [ ] **Step 1: Write the test (exercises real SQLite store through the full archive)**

```go
package backup_test

import (
	"bytes"
	"context"
	"testing"
	"time"

	"tribal-knowledge/internal/backup"
	"tribal-knowledge/internal/storage"
)

func newSQLite(t *testing.T) *storage.SQLiteStore {
	t.Helper()
	s, err := storage.NewSQLiteStore(t.TempDir()+"/db.sqlite", 4)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestSQLiteFullRoundTrip(t *testing.T) {
	ctx := context.Background()
	src := newSQLite(t)
	id, err := src.StoreEntry(ctx, storage.KnowledgeEntry{
		Type: "note", Title: "Round", Content: "Trip", Tags: []string{"x", "y"},
	}, []float32{1, 2, 3, 4})
	if err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	if _, err := backup.Export(ctx, src, &buf, "test", 4, time.Unix(0, 0)); err != nil {
		t.Fatalf("export: %v", err)
	}

	dst := newSQLite(t)
	if _, err := backup.Import(ctx, dst, &buf, backup.ImportOptions{}, 4); err != nil {
		t.Fatalf("import: %v", err)
	}

	got, err := dst.GetEntry(ctx, id)
	if err != nil {
		t.Fatalf("get restored entry: %v", err)
	}
	if got.Title != "Round" || len(got.Tags) != 2 {
		t.Errorf("restored entry wrong: %+v", got)
	}
	res, err := dst.SearchSimilar(ctx, []float32{1, 2, 3, 4}, 1)
	if err != nil || len(res) != 1 {
		t.Fatalf("vector search after restore failed: res=%d err=%v", len(res), err)
	}
}

func TestForceOverwrites(t *testing.T) {
	ctx := context.Background()
	src := newSQLite(t)
	src.StoreEntry(ctx, storage.KnowledgeEntry{Type: "note", Title: "A", Content: "a"}, nil)
	var buf bytes.Buffer
	backup.Export(ctx, src, &buf, "test", 4, time.Unix(0, 0))

	dst := newSQLite(t)
	dst.StoreEntry(ctx, storage.KnowledgeEntry{Type: "note", Title: "PreExisting", Content: "z"}, nil)

	if _, err := backup.Import(ctx, dst, bytes.NewReader(buf.Bytes()), backup.ImportOptions{}, 4); err == nil {
		t.Fatal("expected refusal without force")
	}
	if _, err := backup.Import(ctx, dst, bytes.NewReader(buf.Bytes()), backup.ImportOptions{Force: true}, 4); err != nil {
		t.Fatalf("force import: %v", err)
	}
	entries, _ := dst.ListEntries(ctx, storage.ListFilter{})
	if len(entries) != 1 || entries[0].Title != "A" {
		t.Errorf("after force restore expected only 'A', got %+v", entries)
	}
}
```

- [ ] **Step 2: Run the tests**

Run: `go test ./internal/backup/ -run "TestSQLiteFullRoundTrip|TestForceOverwrites" -v`
Expected: PASS. If `ListFilter` field names differ, check `internal/storage/storage.go` for the actual struct and adjust the empty-filter literal.

- [ ] **Step 3: Commit**

```bash
git add internal/backup/roundtrip_sqlite_test.go
git commit -m "test(backup): SQLite full round-trip + force overwrite"
```

---

## Task 8: CLI subcommands (`export` / `import`)

**Files:**
- Create: `cmd/server/backup_cmd.go`
- Modify: `cmd/server/main.go` (dispatch before server start, around line 35-56)

- [ ] **Step 1: Add subcommand dispatch in `main.go`**

Immediately after `cfg, err := config.Load()` succeeds and the logger is configured (after line 70), but BEFORE store construction at line 80, insert:

```go
	// Subcommand dispatch: export/import run an operation and exit.
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "export":
			if err := runExport(cfg, os.Args[2:]); err != nil {
				slog.Error("export failed", "err", err)
				os.Exit(1)
			}
			return
		case "import":
			if err := runImport(cfg, os.Args[2:]); err != nil {
				slog.Error("import failed", "err", err)
				os.Exit(1)
			}
			return
		}
	}
```

- [ ] **Step 2: Implement the commands in `cmd/server/backup_cmd.go`**

```go
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"time"

	"tribal-knowledge/internal/backup"
	"tribal-knowledge/internal/config"
	"tribal-knowledge/internal/storage"
)

// openBackupStore builds the same store the server uses, returning it as a
// BackupStore. Caller must Close it.
func openBackupStore(cfg *config.Config) (storage.BackupStore, func(), error) {
	if cfg.DatabaseURL != "" {
		s, err := storage.NewPostgresStore(cfg.DatabaseURL, cfg.EmbeddingDim)
		if err != nil {
			return nil, nil, err
		}
		return s, func() { s.Close() }, nil
	}
	s, err := storage.NewSQLiteStore(cfg.DBPath, cfg.EmbeddingDim)
	if err != nil {
		return nil, nil, err
	}
	return s, func() { s.Close() }, nil
}

func runExport(cfg *config.Config, args []string) error {
	fs := flag.NewFlagSet("export", flag.ContinueOnError)
	out := fs.String("out", "", "output archive path (default: backup-<timestamp>.tar.gz)")
	toStdout := fs.Bool("stdout", false, "write the archive to stdout instead of a file")
	if err := fs.Parse(args); err != nil {
		return err
	}

	store, closeFn, err := openBackupStore(cfg)
	if err != nil {
		return err
	}
	defer closeFn()

	now := time.Now()
	var w *os.File
	target := *out
	if *toStdout {
		w = os.Stdout
	} else {
		if target == "" {
			target = fmt.Sprintf("backup-%s.tar.gz", now.Format("20060102-150405"))
		}
		// 0600: the archive contains secrets (API key hashes, auth config).
		w, err = os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
		if err != nil {
			return err
		}
		defer w.Close()
	}

	man, err := backup.Export(context.Background(), store, w, version(), cfg.EmbeddingDim, now)
	if err != nil {
		return err
	}
	if !*toStdout {
		fmt.Fprintf(os.Stderr, "WARNING: %s contains secrets (API keys, auth config, password hashes). Protect it like a credential.\n", target)
		fmt.Fprintf(os.Stderr, "Backup written to %s (engine=%s, embeddings=%d)\n", target, man.SourceEngine, man.Embeddings)
	}
	return nil
}

func runImport(cfg *config.Config, args []string) error {
	fs := flag.NewFlagSet("import", flag.ContinueOnError)
	in := fs.String("in", "", "input archive path (required)")
	force := fs.Bool("force", false, "overwrite a non-empty target database")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *in == "" {
		return fmt.Errorf("--in is required")
	}

	store, closeFn, err := openBackupStore(cfg)
	if err != nil {
		return err
	}
	defer closeFn()

	f, err := os.Open(*in)
	if err != nil {
		return err
	}
	defer f.Close()

	rep, err := backup.Import(context.Background(), store, f, backup.ImportOptions{Force: *force}, cfg.EmbeddingDim)
	if err != nil {
		return err
	}
	total := 0
	for _, n := range rep.TablesRestored {
		total += n
	}
	fmt.Fprintf(os.Stderr, "Restore complete: %d rows across %d tables, %d embeddings.\n", total, len(rep.TablesRestored), rep.EmbeddingsRestored)
	return nil
}

// version returns a build version string. If a version var already exists in
// the package (e.g. set via -ldflags), reuse it instead of this fallback.
func version() string {
	if v := os.Getenv("APP_VERSION"); v != "" {
		return v
	}
	return "dev"
}
```

> Before adding `version()`, grep the package: `grep -rn "func version\|var [Vv]ersion\|Version =" cmd/server/`. If one exists, delete the fallback above and use the existing symbol.

- [ ] **Step 3: Build and smoke-test the CLI**

```bash
go build -o /tmp/tk ./cmd/server
DB_PATH=/tmp/smoke.db /tmp/tk export --out /tmp/smoke-backup.tar.gz
DB_PATH=/tmp/smoke2.db /tmp/tk import --in /tmp/smoke-backup.tar.gz
```
Expected: export prints the secret warning + "Backup written"; import prints "Restore complete". (Empty DB → 0 rows is fine.) Confirm `config.Config` field names (`DatabaseURL`, `DBPath`, `EmbeddingDim`) match `internal/config/config.go`; adjust if different.

- [ ] **Step 4: Commit**

```bash
git add cmd/server/backup_cmd.go cmd/server/main.go
git commit -m "feat(cli): export/import subcommands for full DB backup/restore"
```

---

## Task 9: Cross-engine test (SQLite → Postgres), gated on env

**Files:**
- Create: `internal/backup/roundtrip_postgres_test.go`

- [ ] **Step 1: Write the gated test** (mirror how existing Postgres tests gate; check `internal/storage/*_test.go` for the exact env var — assume `TEST_DATABASE_URL`, adjust if the repo uses another).

```go
package backup_test

import (
	"bytes"
	"context"
	"os"
	"testing"
	"time"

	"tribal-knowledge/internal/backup"
	"tribal-knowledge/internal/storage"
)

func TestCrossEngineSQLiteToPostgres(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL not set; skipping cross-engine test")
	}
	ctx := context.Background()

	src := newSQLite(t)
	id, err := src.StoreEntry(ctx, storage.KnowledgeEntry{
		Type: "note", Title: "X-Engine", Content: "Body", Tags: []string{"k"},
	}, []float32{0.1, 0.2, 0.3, 0.4})
	if err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	if _, err := backup.Export(ctx, src, &buf, "test", 4, time.Unix(0, 0)); err != nil {
		t.Fatal(err)
	}

	pg, err := storage.NewPostgresStore(dsn, 4)
	if err != nil {
		t.Fatalf("connect postgres: %v", err)
	}
	defer pg.Close()
	// Always force so the test is repeatable against a shared DB.
	if _, err := backup.Import(ctx, pg, &buf, backup.ImportOptions{Force: true}, 4); err != nil {
		t.Fatalf("import into postgres: %v", err)
	}

	got, err := pg.GetEntry(ctx, id)
	if err != nil {
		t.Fatalf("get from postgres: %v", err)
	}
	if got.Title != "X-Engine" {
		t.Errorf("title = %q", got.Title)
	}
	res, err := pg.SearchSimilar(ctx, []float32{0.1, 0.2, 0.3, 0.4}, 1)
	if err != nil || len(res) != 1 {
		t.Fatalf("pg vector search failed: res=%d err=%v", len(res), err)
	}
}
```

- [ ] **Step 2: Run it (requires a pgvector Postgres; run.sh / docker-compose can provide one)**

```bash
# Example, using the project's Docker postgres:
TEST_DATABASE_URL="postgres://tribal:tribal@localhost:5432/tribal?sslmode=disable" \
  go test ./internal/backup/ -run TestCrossEngine -v
```
Expected: PASS. **This is the gate for the timestamp-coercion risk.** If a `timestamptz`/`integer` column rejects a JSON-decoded value, fix it in `PostgresStore.LoadTable` (e.g. detect the column type or pass time values as strings and rely on implicit cast). Iterate here until green.

- [ ] **Step 3: Commit**

```bash
git add internal/backup/roundtrip_postgres_test.go
git commit -m "test(backup): cross-engine SQLite->Postgres round-trip (gated)"
```

---

## Task 10: Web endpoints — superadmin backup download + restore upload

**Files:**
- Modify: `internal/web/server.go` (register routes in the superadmin group; exempt restore from `maxBodySize`)
- Create: `internal/web/backup_handlers.go`
- Test: `internal/web/backup_handlers_test.go`

- [ ] **Step 1: Exempt the restore route from the 1 MiB body cap**

In `maxBodySize` (`server.go:150`), skip the restore path:

```go
func maxBodySize(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Restore uploads a full archive; exempt it from the 1 MiB cap.
		if r.URL.Path == "/api/admin/restore" {
			next.ServeHTTP(w, r)
			return
		}
		if r.Method == http.MethodPost || r.Method == http.MethodPut {
			r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
		}
		next.ServeHTTP(w, r)
	})
}
```

> Confirm the exact current body of `maxBodySize` and preserve its existing logic; only add the early-return guard.

- [ ] **Step 2: Register routes in the superadmin group** (`server.go`, the `r.Use(auth.RequireSuperadmin())` group):

```go
		r.Get("/api/admin/backup", s.handleBackupDownload)
		r.Post("/api/admin/restore", s.handleRestoreUpload)
```

- [ ] **Step 3: Write the failing test**

```go
package web

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestBackupDownloadSuperadmin(t *testing.T) {
	srv := newSuperadminTestServer(t) // reuse existing admin test harness
	req := saRequest(t, http.MethodGet, "/api/admin/backup", nil)
	rec := httptest.NewRecorder()
	srv.router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/gzip" {
		t.Errorf("content-type = %q", ct)
	}
	if cd := rec.Header().Get("Content-Disposition"); cd == "" {
		t.Error("missing Content-Disposition")
	}
}
```

> Match the existing admin test setup: `admin_handlers_test.go` already defines `superadminMockStore`, `saRequest`, and a server builder. Reuse those exact helpers; `newSuperadminTestServer` may already exist under a different name — grep `admin_handlers_test.go` and use it.

- [ ] **Step 4: Run test to verify it fails**

Run: `go test ./internal/web/ -run TestBackupDownload -v`
Expected: FAIL — handler undefined / 404.

- [ ] **Step 5: Implement handlers** (`internal/web/backup_handlers.go`)

```go
package web

import (
	"fmt"
	"net/http"
	"time"

	"tribal-knowledge/internal/backup"
	"tribal-knowledge/internal/storage"
)

func (s *Server) handleBackupDownload(w http.ResponseWriter, r *http.Request) {
	bs, ok := s.store.(storage.BackupStore)
	if !ok {
		writeError(w, http.StatusInternalServerError, "backup_unsupported", "storage engine does not support backup")
		return
	}
	now := time.Now()
	name := fmt.Sprintf("tribal-backup-%s.tar.gz", now.Format("20060102-150405"))
	w.Header().Set("Content-Type", "application/gzip")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", name))
	if _, err := backup.Export(r.Context(), bs, w, s.appVersion, s.embeddingDim, now); err != nil {
		// Headers already sent; log only.
		s.log.Error("backup export failed", "err", err)
	}
}

func (s *Server) handleRestoreUpload(w http.ResponseWriter, r *http.Request) {
	bs, ok := s.store.(storage.BackupStore)
	if !ok {
		writeError(w, http.StatusInternalServerError, "backup_unsupported", "storage engine does not support restore")
		return
	}
	force := r.URL.Query().Get("force") == "true"

	file, _, err := r.FormFile("archive")
	if err != nil {
		writeError(w, http.StatusBadRequest, "missing_archive", "expected multipart field 'archive'")
		return
	}
	defer file.Close()

	rep, err := backup.Import(r.Context(), bs, file, backup.ImportOptions{Force: force}, s.embeddingDim)
	if err != nil {
		writeError(w, http.StatusBadRequest, "restore_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"tables_restored":     rep.TablesRestored,
		"embeddings_restored": rep.EmbeddingsRestored,
	})
}
```

> Confirm the `Server` struct fields: `s.store`, and how it exposes version, embedding dim, and logger. Grep `internal/web/server.go` for the struct definition. If `s.appVersion`/`s.embeddingDim`/`s.log` don't exist, add them to the struct and populate them in the server constructor (the constructor already receives config — pass `cfg.EmbeddingDim` and a version string through). Use the existing `writeError`/`writeJSON` helpers (referenced in `import_handlers.go`); match their signatures exactly.

- [ ] **Step 6: Run tests to verify they pass**

Run: `go test ./internal/web/ -run TestBackupDownload -v`
Expected: PASS

- [ ] **Step 7: Build the whole backend**

Run: `go build ./... && go test ./internal/... `
Expected: all packages build; all non-gated tests pass.

- [ ] **Step 8: Commit**

```bash
git add internal/web/backup_handlers.go internal/web/server.go internal/web/backup_handlers_test.go
git commit -m "feat(web): superadmin backup download + restore upload endpoints"
```

---

## Task 11: Web UI — Backup & Restore section on Settings

**Files:**
- Modify: the Settings page component (grep `web/src` for the settings route, e.g. `web/src/pages/Settings.tsx`)
- Possibly create: `web/src/components/BackupRestore.tsx`

- [ ] **Step 1: Identify the Settings page and API client pattern**

```bash
grep -rn "settings" web/src --include=*.tsx -l
grep -rn "fetch(\|api(" web/src/lib 2>/dev/null | head
```
Follow the existing data-fetching/auth-header pattern (the app already calls `/api/settings`).

- [ ] **Step 2: Add a Backup & Restore card** (concrete component; adapt imports/styling to the existing shadcn/Tailwind setup):

```tsx
import { useState } from "react";

export function BackupRestore() {
  const [busy, setBusy] = useState(false);
  const [msg, setMsg] = useState<string | null>(null);
  const [force, setForce] = useState(false);

  async function download() {
    setBusy(true);
    setMsg(null);
    try {
      const res = await fetch("/api/admin/backup", { credentials: "include" });
      if (!res.ok) throw new Error(`backup failed: ${res.status}`);
      const blob = await res.blob();
      const url = URL.createObjectURL(blob);
      const a = document.createElement("a");
      a.href = url;
      a.download = `tribal-backup-${new Date().toISOString().slice(0, 19).replace(/[:T]/g, "")}.tar.gz`;
      a.click();
      URL.revokeObjectURL(url);
      setMsg("Backup downloaded. Treat this file as a secret — it contains API keys and auth config.");
    } catch (e) {
      setMsg(String(e));
    } finally {
      setBusy(false);
    }
  }

  async function restore(file: File) {
    setBusy(true);
    setMsg(null);
    try {
      const fd = new FormData();
      fd.append("archive", file);
      const res = await fetch(`/api/admin/restore?force=${force}`, {
        method: "POST",
        body: fd,
        credentials: "include",
      });
      const body = await res.json();
      if (!res.ok) throw new Error(body?.message || `restore failed: ${res.status}`);
      setMsg(`Restore complete: ${body.embeddings_restored} embeddings restored.`);
    } catch (e) {
      setMsg(String(e));
    } finally {
      setBusy(false);
    }
  }

  return (
    <section className="rounded-lg border border-zinc-800 p-4 space-y-4">
      <h2 className="text-lg font-semibold">Backup &amp; Restore</h2>
      <p className="text-sm text-zinc-400">
        Download a full archive of this server (all teams, knowledge, agents, users, settings)
        or restore one into this database. The archive contains secrets — store it securely.
      </p>

      <button
        onClick={download}
        disabled={busy}
        className="rounded bg-amber-600 px-3 py-1.5 text-sm font-medium disabled:opacity-50"
      >
        Download backup
      </button>

      <div className="space-y-2 border-t border-zinc-800 pt-4">
        <label className="flex items-center gap-2 text-sm">
          <input type="checkbox" checked={force} onChange={(e) => setForce(e.target.checked)} />
          Force overwrite (wipe this database before restoring)
        </label>
        <input
          type="file"
          accept=".gz,.tar.gz,application/gzip"
          disabled={busy}
          onChange={(e) => e.target.files?.[0] && restore(e.target.files[0])}
          className="block text-sm"
        />
      </div>

      {msg && <p className="text-sm text-zinc-300">{msg}</p>}
    </section>
  );
}
```

- [ ] **Step 3: Mount `<BackupRestore />`** in the Settings page (superadmin-only area, matching how other admin-only UI is gated in the app).

- [ ] **Step 4: Build the web app (REQUIRED before claiming done)**

Run: `make web` (or the project's web build target — check `Makefile`)
Expected: Vite production build completes with no errors.

- [ ] **Step 5: Commit**

```bash
git add web/src
git commit -m "feat(web-ui): Backup & Restore section on Settings page"
```

---

## Task 12: Documentation

**Files:**
- Modify: `README.md` (add a "Backup & Restore / Migration" section)
- Modify: `CHANGELOG.md`

- [ ] **Step 1: Document CLI + web usage and the SQLite→Postgres migration recipe**

Add a README section covering:
- `tribal-knowledge export --out backup.tar.gz` and `import --in backup.tar.gz [--force]`.
- Engine selection via `DATABASE_URL` (Postgres) vs `DB_PATH` (SQLite).
- The migration recipe: export from SQLite, set `DATABASE_URL`, `import --in ... --force` into the fresh Postgres.
- The superadmin web Backup/Restore UI.
- **Security warning:** the archive contains API key hashes, auth config, and password hashes; protect it like a credential; `export` writes it `0600`.
- The `embedding_dim` must match between source and target.

- [ ] **Step 2: Add a CHANGELOG entry** under the current version describing logical backup/restore.

- [ ] **Step 3: Commit**

```bash
git add README.md CHANGELOG.md
git commit -m "docs: backup/restore + SQLite->Postgres migration guide"
```

---

## Final verification

- [ ] `go build ./...` — clean.
- [ ] `go test ./internal/... ./cmd/...` — all non-gated tests pass.
- [ ] (If Postgres available) `TEST_DATABASE_URL=... go test ./internal/backup/ -run TestCrossEngine -v` — passes.
- [ ] `make web` — Vite build clean.
- [ ] Manual smoke: `export` then `import --force` round-trips a populated SQLite DB; restored entries are searchable.

---

## Self-review notes (author)

- **Spec coverage:** CLI ✓ (T8), Web UI ✓ (T10/T11), all-tables-except-sessions ✓ (T1 list), embeddings as float arrays ✓ (T3/T4/T5/T6), refuse-unless-force ✓ (T6/T7), plaintext+warning+0600 ✓ (T8/T11/T12), whole-DB all-teams ✓ (no team filter), generic dump ✓ (T3/T4), tar.gz+JSONL+manifest ✓ (T5), embedding_dim validation ✓ (T6), tests incl. cross-engine ✓ (T7/T9).
- **Type consistency:** `BackupStore`, `EmbeddingItem{EntryID,Vector}`, `Manifest`, `Report{TablesRestored,EmbeddingsRestored}`, `ImportOptions{Force}`, `CoveredTables()`, `FormatVersion`, `validTableName`, `sortedKeys` used consistently across tasks. `Export(ctx, store, w, toolVersion, embeddingDim, createdAt)` and `Import(ctx, store, r, opts, targetDim)` signatures stable from T5/T6 onward.
- **Known risk flagged:** timestamp text→`timestamptz` coercion on Postgres; T9 is the gate. `sessions` excluded everywhere. SQLite `rowid` never exported (entries keyed by UUID; embeddings re-linked by UUID→rowid on load).
