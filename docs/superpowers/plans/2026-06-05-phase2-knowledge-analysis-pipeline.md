# Knowledge Analysis Pipeline — Phase 2 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a background analysis pipeline that clusters entries by semantic similarity, scores quality, summarizes clusters via Anthropic LLM, detects coverage gaps, and persists versioned dataset snapshots — queryable via two new MCP tools.

**Architecture:** A `pipeline.Pipeline` goroutine fires on a configurable interval when the entry count meets a threshold. It reads all embeddings, runs cosine-similarity clustering, calls the Anthropic Messages API (claude-haiku-4-5-20251001) for summarization and gap detection, and writes `Cluster` records plus a `DatasetSnapshot` back to SQLite. A new `AnalysisStore` interface extends `Store` without touching existing mocks or Phase 1 tests. Two new MCP tools (`cluster_list`, `analysis_status`) expose results.

**Tech Stack:** Go 1.24+, SQLite/sqlite-vec (existing), Anthropic Messages API via raw HTTP (no SDK — avoids version uncertainty), injectable `llm.Client` interface for test isolation.

---

## File Map

### New files
| File | Purpose |
|------|---------|
| `internal/storage/analysis.go` | AnalysisStore methods on *SQLiteStore |
| `internal/storage/analysis_test.go` | Tests for new store methods |
| `internal/llm/client.go` | LLMClient interface |
| `internal/llm/anthropic.go` | HTTP-based Anthropic client with retry |
| `internal/llm/anthropic_test.go` | Tests with httptest mock server |
| `internal/pipeline/cluster.go` | Cosine similarity clustering algorithm |
| `internal/pipeline/cluster_test.go` | Unit tests for clustering and cosine sim |
| `internal/pipeline/analyze.go` | LLM-backed analysis: summarize, score, gap detection |
| `internal/pipeline/analyze_test.go` | Tests with mock LLM |
| `internal/pipeline/testhelpers_test.go` | Shared test mocks (mockLLM, mockAnalysisStore) |
| `internal/pipeline/pipeline.go` | Pipeline orchestrator |
| `internal/pipeline/pipeline_test.go` | Integration test for full pipeline run |
| `internal/mcp/analysis_tools.go` | cluster_list and analysis_status handlers |
| `internal/mcp/analysis_tools_test.go` | Tests for new MCP tool handlers |

### Modified files
| File | Change |
|------|--------|
| `internal/storage/storage.go` | Add Cluster, PipelineRun, DatasetSnapshot types + AnalysisStore interface |
| `internal/storage/sqlite.go` | Extend migrate() with new tables and entry_embeddings; update StoreEntry/DeleteEntry |
| `internal/config/config.go` | Add Anthropic, pipeline, and cluster config fields |
| `internal/config/config_test.go` | Tests for new config fields |
| `internal/mcp/server.go` | Add RegisterAnalysisTools(s, store) function |
| `cmd/server/main.go` | Start pipeline goroutine; call RegisterAnalysisTools |

---

## Task 0: README

**Files:**
- Already created: `README.md`

- [ ] **Step 1: Verify README exists and builds correctly**

```bash
CGO_ENABLED=1 go build ./cmd/server/ && echo "build ok"
```

Expected: `build ok` (no compilation errors from README creation).

- [ ] **Step 2: Commit**

```bash
git add README.md
git commit -m "docs: add README with build, run, and config instructions"
```

---

## Task 1: Schema extensions, new types, and AnalysisStore interface

**Files:**
- Modify: `internal/storage/storage.go`
- Modify: `internal/storage/sqlite.go`
- Create: `internal/storage/analysis.go`
- Create: `internal/storage/analysis_test.go`

### Background

We need three new tables (`clusters`, `pipeline_runs`, `dataset_snapshots`), a new `entry_embeddings` table that mirrors embedding blobs for reliable retrieval (sqlite-vec's vec0 table is optimized for KNN search — using a regular table for full-scan retrieval is more reliable), two new columns on `entries` (`rating`, `usage_count`), and a new `AnalysisStore` interface that extends `Store`.

`StoreEntry` and `DeleteEntry` in `sqlite.go` must be updated to maintain `entry_embeddings` in the same transaction as `vec_entries`.

- [ ] **Step 1: Write the failing tests**

Create `internal/storage/analysis_test.go`:

```go
package storage

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func newTestAnalysisStore(t *testing.T) *SQLiteStore {
	t.Helper()
	path := filepath.Join(t.TempDir(), "test.db")
	s, err := NewSQLiteStore(path, 4)
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestCountEntries(t *testing.T) {
	s := newTestAnalysisStore(t)
	ctx := context.Background()

	n, err := s.CountEntries(ctx)
	if err != nil {
		t.Fatalf("CountEntries: %v", err)
	}
	if n != 0 {
		t.Errorf("want 0, got %d", n)
	}

	_, err = s.StoreEntry(ctx, KnowledgeEntry{
		Type: KTPrompt, Title: "T", Content: "C",
	}, []float32{1, 0, 0, 0})
	if err != nil {
		t.Fatalf("StoreEntry: %v", err)
	}

	n, err = s.CountEntries(ctx)
	if err != nil {
		t.Fatalf("CountEntries after insert: %v", err)
	}
	if n != 1 {
		t.Errorf("want 1, got %d", n)
	}
}

func TestGetAllEmbeddings(t *testing.T) {
	s := newTestAnalysisStore(t)
	ctx := context.Background()

	emb := []float32{0.1, 0.2, 0.3, 0.4}
	id, err := s.StoreEntry(ctx, KnowledgeEntry{
		Type: KTPrompt, Title: "T", Content: "C",
	}, emb)
	if err != nil {
		t.Fatalf("StoreEntry: %v", err)
	}

	got, err := s.GetAllEmbeddings(ctx)
	if err != nil {
		t.Fatalf("GetAllEmbeddings: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("want 1 embedding, got %d", len(got))
	}
	v, ok := got[id]
	if !ok {
		t.Fatalf("embedding for id %s not found", id)
	}
	for i, f := range emb {
		if v[i] != f {
			t.Errorf("embedding[%d]: got %v, want %v", i, v[i], f)
		}
	}
}

func TestGetAllEmbeddings_DeletedEntryAbsent(t *testing.T) {
	s := newTestAnalysisStore(t)
	ctx := context.Background()

	id, _ := s.StoreEntry(ctx, KnowledgeEntry{Type: KTPrompt, Title: "T", Content: "C"}, []float32{1, 0, 0, 0})
	_ = s.DeleteEntry(ctx, id)

	got, err := s.GetAllEmbeddings(ctx)
	if err != nil {
		t.Fatalf("GetAllEmbeddings: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("want 0 embeddings after delete, got %d", len(got))
	}
}

func TestStoreAndListClusters(t *testing.T) {
	s := newTestAnalysisStore(t)
	ctx := context.Background()

	_, err := s.StoreCluster(ctx, Cluster{
		Domain:   "finance",
		Title:    "Finance Patterns",
		Summary:  "A group of finance entries",
		EntryIDs: []string{"a", "b"},
	})
	if err != nil {
		t.Fatalf("StoreCluster: %v", err)
	}

	clusters, err := s.ListClusters(ctx)
	if err != nil {
		t.Fatalf("ListClusters: %v", err)
	}
	if len(clusters) != 1 {
		t.Fatalf("want 1 cluster, got %d", len(clusters))
	}
	if clusters[0].Title != "Finance Patterns" {
		t.Errorf("title = %q, want %q", clusters[0].Title, "Finance Patterns")
	}
	if len(clusters[0].EntryIDs) != 2 {
		t.Errorf("entry_ids len = %d, want 2", len(clusters[0].EntryIDs))
	}
}

func TestPipelineRun(t *testing.T) {
	s := newTestAnalysisStore(t)
	ctx := context.Background()

	runID, err := s.StartPipelineRun(ctx, "test")
	if err != nil {
		t.Fatalf("StartPipelineRun: %v", err)
	}
	if runID == "" {
		t.Fatal("runID is empty")
	}

	run, err := s.GetLatestPipelineRun(ctx)
	if err != nil {
		t.Fatalf("GetLatestPipelineRun: %v", err)
	}
	if run.Status != "running" {
		t.Errorf("status = %q, want %q", run.Status, "running")
	}

	if err := s.FinishPipelineRun(ctx, runID, "complete", 5, 2, nil); err != nil {
		t.Fatalf("FinishPipelineRun: %v", err)
	}

	run, err = s.GetLatestPipelineRun(ctx)
	if err != nil {
		t.Fatalf("GetLatestPipelineRun after finish: %v", err)
	}
	if run.Status != "complete" {
		t.Errorf("status = %q, want %q", run.Status, "complete")
	}
	if run.EntriesProcessed != 5 {
		t.Errorf("entries_processed = %d, want 5", run.EntriesProcessed)
	}
}

func TestStoreAndGetSnapshot(t *testing.T) {
	s := newTestAnalysisStore(t)
	ctx := context.Background()

	snap := DatasetSnapshot{
		Version:      1,
		ClusterCount: 3,
		EntryCount:   15,
		Data:         `{"gaps":[]}`,
	}
	id, err := s.StoreSnapshot(ctx, snap)
	if err != nil {
		t.Fatalf("StoreSnapshot: %v", err)
	}
	if id == "" {
		t.Fatal("snapshot id is empty")
	}

	latest, err := s.GetLatestSnapshot(ctx)
	if err != nil {
		t.Fatalf("GetLatestSnapshot: %v", err)
	}
	if latest.Version != 1 {
		t.Errorf("version = %d, want 1", latest.Version)
	}
	if latest.ClusterCount != 3 {
		t.Errorf("cluster_count = %d, want 3", latest.ClusterCount)
	}
}

func TestGetLatestPipelineRun_Empty(t *testing.T) {
	s := newTestAnalysisStore(t)
	run, err := s.GetLatestPipelineRun(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if run != nil {
		t.Errorf("expected nil run on empty store, got %+v", run)
	}
}

func TestGetLatestSnapshot_Empty(t *testing.T) {
	s := newTestAnalysisStore(t)
	snap, err := s.GetLatestSnapshot(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if snap != nil {
		t.Errorf("expected nil snapshot on empty store, got %+v", snap)
	}
}

// Ensure the test binary doesn't complain about unused import
var _ = os.DevNull
```

- [ ] **Step 2: Run tests — verify they fail with "undefined"**

```bash
CGO_ENABLED=1 go test ./internal/storage/... 2>&1 | head -30
```

Expected: compilation errors about `CountEntries`, `GetAllEmbeddings`, `StoreCluster`, etc. not defined.

- [ ] **Step 3: Add new types and AnalysisStore interface to storage.go**

Add after the existing `ListFilter` type in `internal/storage/storage.go`:

```go
type Cluster struct {
	ID            string
	Domain        string
	Title         string
	Summary       string
	EntryIDs      []string
	QualityScore  float64
	PipelineRunID string
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

type PipelineRun struct {
	ID               string
	Status           string
	Trigger          string
	EntriesProcessed int
	ClustersFound    int
	Errors           []string
	StartedAt        time.Time
	CompletedAt      *time.Time
}

type DatasetSnapshot struct {
	ID            string
	Version       int
	ClusterCount  int
	EntryCount    int
	Data          string
	PipelineRunID string
	CreatedAt     time.Time
}

// AnalysisStore extends Store with methods needed by the analysis pipeline.
type AnalysisStore interface {
	Store
	CountEntries(ctx context.Context) (int, error)
	GetAllEmbeddings(ctx context.Context) (map[string][]float32, error)
	ListClusters(ctx context.Context) ([]Cluster, error)
	StoreCluster(ctx context.Context, c Cluster) (string, error)
	DeleteClustersByRunID(ctx context.Context, runID string) error
	StartPipelineRun(ctx context.Context, trigger string) (string, error)
	FinishPipelineRun(ctx context.Context, id, status string, entriesProcessed, clustersFound int, errs []string) error
	GetLatestPipelineRun(ctx context.Context) (*PipelineRun, error)
	StoreSnapshot(ctx context.Context, snap DatasetSnapshot) (string, error)
	GetLatestSnapshot(ctx context.Context) (*DatasetSnapshot, error)
}
```

- [ ] **Step 4: Extend migrate() in sqlite.go with new tables and entry_embeddings**

Add after the `CREATE VIRTUAL TABLE IF NOT EXISTS vec_entries` block in the `migrate()` function in `internal/storage/sqlite.go`. Replace the entire `migrate()` function body with:

```go
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

	// entry_embeddings stores raw blobs alongside vec_entries for reliable full-scan retrieval.
	_, err = s.db.Exec(`
		CREATE TABLE IF NOT EXISTS entry_embeddings (
			rowid     INTEGER PRIMARY KEY REFERENCES entries(rowid) ON DELETE CASCADE,
			embedding BLOB NOT NULL
		);
	`)
	if err != nil {
		return fmt.Errorf("create entry_embeddings table: %w", err)
	}

	for _, col := range []string{
		`ALTER TABLE entries ADD COLUMN rating REAL DEFAULT 0.0`,
		`ALTER TABLE entries ADD COLUMN usage_count INTEGER DEFAULT 0`,
	} {
		if _, err := s.db.Exec(col); err != nil && !strings.Contains(err.Error(), "duplicate column name") {
			return fmt.Errorf("alter entries: %w", err)
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

	return nil
}
```

- [ ] **Step 5: Update StoreEntry in sqlite.go to maintain entry_embeddings**

In `StoreEntry`, after the `INSERT INTO vec_entries` line, add inside the same transaction:

```go
_, err = tx.ExecContext(ctx, `INSERT INTO entry_embeddings (rowid, embedding) VALUES (?, ?)`, rowID, blob)
if err != nil {
    return "", fmt.Errorf("insert entry_embeddings: %w", err)
}
```

- [ ] **Step 6: Update DeleteEntry in sqlite.go to remove from entry_embeddings**

In `DeleteEntry`, add a delete for `entry_embeddings` after the `vec_entries` delete:

```go
if _, err := tx.ExecContext(ctx, "DELETE FROM entry_embeddings WHERE rowid = ?", rowID); err != nil {
    return fmt.Errorf("delete entry_embeddings: %w", err)
}
```

- [ ] **Step 7: Create internal/storage/analysis.go with AnalysisStore methods**

```go
package storage

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
)

func (s *SQLiteStore) CountEntries(ctx context.Context) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM entries").Scan(&n)
	return n, err
}

func (s *SQLiteStore) GetAllEmbeddings(ctx context.Context) (map[string][]float32, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT e.id, ee.embedding
		FROM entries e
		JOIN entry_embeddings ee ON ee.rowid = e.rowid
	`)
	if err != nil {
		return nil, fmt.Errorf("query embeddings: %w", err)
	}
	defer rows.Close()

	result := make(map[string][]float32)
	for rows.Next() {
		var id string
		var blob []byte
		if err := rows.Scan(&id, &blob); err != nil {
			return nil, fmt.Errorf("scan embedding: %w", err)
		}
		v, err := deserializeFloat32(blob, s.embeddingDim)
		if err != nil {
			return nil, fmt.Errorf("deserialize embedding for %s: %w", id, err)
		}
		result[id] = v
	}
	return result, rows.Err()
}

func deserializeFloat32(blob []byte, dim int) ([]float32, error) {
	if len(blob) != dim*4 {
		return nil, fmt.Errorf("blob size %d != expected %d for dim %d", len(blob), dim*4, dim)
	}
	v := make([]float32, dim)
	if err := binary.Read(bytes.NewReader(blob), binary.LittleEndian, v); err != nil {
		return nil, err
	}
	return v, nil
}

func (s *SQLiteStore) ListClusters(ctx context.Context) ([]Cluster, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, domain, title, summary, entry_ids, quality_score, pipeline_run_id,
		       created_at, updated_at
		FROM clusters ORDER BY created_at DESC
	`)
	if err != nil {
		return nil, fmt.Errorf("list clusters: %w", err)
	}
	defer rows.Close()

	var clusters []Cluster
	for rows.Next() {
		var c Cluster
		var entryIDsJSON string
		var createdAt, updatedAt string
		if err := rows.Scan(&c.ID, &c.Domain, &c.Title, &c.Summary, &entryIDsJSON,
			&c.QualityScore, &c.PipelineRunID, &createdAt, &updatedAt); err != nil {
			return nil, fmt.Errorf("scan cluster: %w", err)
		}
		if err := json.Unmarshal([]byte(entryIDsJSON), &c.EntryIDs); err != nil {
			c.EntryIDs = []string{}
		}
		c.CreatedAt = parseTimestamp(createdAt)
		c.UpdatedAt = parseTimestamp(updatedAt)
		clusters = append(clusters, c)
	}
	return clusters, rows.Err()
}

func (s *SQLiteStore) StoreCluster(ctx context.Context, c Cluster) (string, error) {
	c.ID = uuid.NewString()
	entryIDsJSON, err := json.Marshal(c.EntryIDs)
	if err != nil {
		return "", fmt.Errorf("marshal entry_ids: %w", err)
	}
	_, err = s.db.ExecContext(ctx, `
		INSERT INTO clusters (id, domain, title, summary, entry_ids, quality_score, pipeline_run_id)
		VALUES (?, ?, ?, ?, ?, ?, ?)
	`, c.ID, c.Domain, c.Title, c.Summary, string(entryIDsJSON), c.QualityScore, c.PipelineRunID)
	if err != nil {
		return "", fmt.Errorf("insert cluster: %w", err)
	}
	return c.ID, nil
}

func (s *SQLiteStore) DeleteClustersByRunID(ctx context.Context, runID string) error {
	_, err := s.db.ExecContext(ctx, "DELETE FROM clusters WHERE pipeline_run_id = ?", runID)
	return err
}

func (s *SQLiteStore) StartPipelineRun(ctx context.Context, trigger string) (string, error) {
	id := uuid.NewString()
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO pipeline_runs (id, status, trigger) VALUES (?, 'running', ?)
	`, id, trigger)
	if err != nil {
		return "", fmt.Errorf("insert pipeline_run: %w", err)
	}
	return id, nil
}

func (s *SQLiteStore) FinishPipelineRun(ctx context.Context, id, status string, entriesProcessed, clustersFound int, errs []string) error {
	if errs == nil {
		errs = []string{}
	}
	errsJSON, _ := json.Marshal(errs)
	_, err := s.db.ExecContext(ctx, `
		UPDATE pipeline_runs
		SET status = ?, entries_processed = ?, clusters_found = ?, errors = ?,
		    completed_at = CURRENT_TIMESTAMP
		WHERE id = ?
	`, status, entriesProcessed, clustersFound, string(errsJSON), id)
	return err
}

func (s *SQLiteStore) GetLatestPipelineRun(ctx context.Context) (*PipelineRun, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, status, trigger, entries_processed, clusters_found, errors,
		       started_at, completed_at
		FROM pipeline_runs ORDER BY started_at DESC LIMIT 1
	`)
	var r PipelineRun
	var errsJSON string
	var startedAt string
	var completedAt sql.NullString
	err := row.Scan(&r.ID, &r.Status, &r.Trigger, &r.EntriesProcessed, &r.ClustersFound,
		&errsJSON, &startedAt, &completedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("scan pipeline_run: %w", err)
	}
	if err := json.Unmarshal([]byte(errsJSON), &r.Errors); err != nil {
		r.Errors = []string{}
	}
	r.StartedAt = parseTimestamp(startedAt)
	if completedAt.Valid && completedAt.String != "" {
		t := parseTimestamp(completedAt.String)
		r.CompletedAt = &t
	}
	return &r, nil
}

func (s *SQLiteStore) StoreSnapshot(ctx context.Context, snap DatasetSnapshot) (string, error) {
	snap.ID = uuid.NewString()
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO dataset_snapshots (id, version, cluster_count, entry_count, data, pipeline_run_id)
		VALUES (?, ?, ?, ?, ?, ?)
	`, snap.ID, snap.Version, snap.ClusterCount, snap.EntryCount, snap.Data, snap.PipelineRunID)
	if err != nil {
		return "", fmt.Errorf("insert snapshot: %w", err)
	}
	return snap.ID, nil
}

func (s *SQLiteStore) GetLatestSnapshot(ctx context.Context) (*DatasetSnapshot, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, version, cluster_count, entry_count, data, pipeline_run_id, created_at
		FROM dataset_snapshots ORDER BY version DESC LIMIT 1
	`)
	var snap DatasetSnapshot
	var createdAt string
	err := row.Scan(&snap.ID, &snap.Version, &snap.ClusterCount, &snap.EntryCount,
		&snap.Data, &snap.PipelineRunID, &createdAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("scan snapshot: %w", err)
	}
	snap.CreatedAt = parseTimestamp(createdAt)
	return &snap, nil
}

// Ensure *SQLiteStore implements AnalysisStore at compile time.
var _ AnalysisStore = (*SQLiteStore)(nil)

// Sentinel to silence unused import warning — time is used by parseTimestamp (defined in sqlite.go)
var _ = time.Now
```

- [ ] **Step 8: Run tests — verify they pass**

```bash
CGO_ENABLED=1 go test ./internal/storage/... -v 2>&1 | tail -20
```

Expected: all tests PASS including the 6 from Phase 1 plus 7 new tests.

- [ ] **Step 9: Build the full binary to confirm no regressions**

```bash
CGO_ENABLED=1 go build ./cmd/server/ && echo "ok"
```

Expected: `ok`

- [ ] **Step 10: Commit**

```bash
git add internal/storage/storage.go internal/storage/sqlite.go internal/storage/analysis.go internal/storage/analysis_test.go
git commit -m "feat(storage): add AnalysisStore interface and analysis pipeline schema"
```

---

## Task 2: Config extensions and LLM HTTP client

**Files:**
- Modify: `internal/config/config.go`
- Modify: `internal/config/config_test.go`
- Create: `internal/llm/client.go`
- Create: `internal/llm/anthropic.go`
- Create: `internal/llm/anthropic_test.go`

- [ ] **Step 1: Write failing LLM client tests**

Create `internal/llm/anthropic_test.go`:

```go
package llm

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func newTestClient(t *testing.T, handler http.HandlerFunc) *AnthropicClient {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return &AnthropicClient{
		apiKey:     "test-key",
		model:      "test-model",
		client:     srv.Client(),
		baseURL:    srv.URL,
		retryDelay: func(int) time.Duration { return 0 },
	}
}

func okHandler(t *testing.T, text string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("x-api-key") != "test-key" {
			t.Errorf("missing api key header")
		}
		if r.Header.Get("anthropic-version") == "" {
			t.Errorf("missing anthropic-version header")
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(anthropicResponse{
			Content: []contentBlock{{Type: "text", Text: text}},
		})
	}
}

func TestAnthropicClient_Complete(t *testing.T) {
	c := newTestClient(t, okHandler(t, "hello world"))
	got, err := c.Complete(context.Background(), "prompt")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "hello world" {
		t.Errorf("got %q, want %q", got, "hello world")
	}
}

func TestAnthropicClient_RetryOn429(t *testing.T) {
	attempts := 0
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		attempts++
		if attempts < 2 {
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		okHandler(t, "ok")(w, r)
	})
	got, err := c.Complete(context.Background(), "prompt")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "ok" {
		t.Errorf("got %q, want %q", got, "ok")
	}
	if attempts != 2 {
		t.Errorf("expected 2 attempts, got %d", attempts)
	}
}

func TestAnthropicClient_ErrorResponse(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(anthropicResponse{
			Error: &apiError{Type: "invalid_request_error", Message: "bad request"},
		})
	})
	_, err := c.Complete(context.Background(), "prompt")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}
```

- [ ] **Step 2: Run tests — verify they fail with "undefined"**

```bash
go test ./internal/llm/... 2>&1 | head -10
```

Expected: compilation errors (package/type not found).

- [ ] **Step 3: Create internal/llm/client.go**

```go
package llm

import "context"

// Client is the interface for LLM text completion.
type Client interface {
	Complete(ctx context.Context, prompt string) (string, error)
}
```

- [ ] **Step 4: Create internal/llm/anthropic.go**

```go
package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

const anthropicMessagesPath = "/v1/messages"
const anthropicVersion = "2023-06-01"
const maxRetries = 3

type AnthropicClient struct {
	apiKey     string
	model      string
	client     *http.Client
	baseURL    string
	retryDelay func(attempt int) time.Duration
}

func NewAnthropicClient(apiKey, model string) *AnthropicClient {
	return &AnthropicClient{
		apiKey:  apiKey,
		model:   model,
		client:  &http.Client{Timeout: 60 * time.Second},
		baseURL: "https://api.anthropic.com",
		retryDelay: func(attempt int) time.Duration {
			return time.Duration(1<<attempt) * time.Second
		},
	}
}

type anthropicRequest struct {
	Model     string    `json:"model"`
	MaxTokens int       `json:"max_tokens"`
	Messages  []message `json:"messages"`
}

type message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type contentBlock struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type apiError struct {
	Type    string `json:"type"`
	Message string `json:"message"`
}

type anthropicResponse struct {
	Content []contentBlock `json:"content"`
	Error   *apiError      `json:"error"`
}

func (c *AnthropicClient) Complete(ctx context.Context, prompt string) (string, error) {
	body, err := json.Marshal(anthropicRequest{
		Model:     c.model,
		MaxTokens: 1024,
		Messages:  []message{{Role: "user", Content: prompt}},
	})
	if err != nil {
		return "", fmt.Errorf("marshal request: %w", err)
	}

	url := c.baseURL + anthropicMessagesPath
	var lastErr error

	for attempt := 0; attempt < maxRetries; attempt++ {
		if attempt > 0 {
			delay := c.retryDelay(attempt)
			select {
			case <-ctx.Done():
				return "", ctx.Err()
			case <-time.After(delay):
			}
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
		if err != nil {
			return "", fmt.Errorf("create request: %w", err)
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("x-api-key", c.apiKey)
		req.Header.Set("anthropic-version", anthropicVersion)

		resp, err := c.client.Do(req)
		if err != nil {
			lastErr = fmt.Errorf("http: %w", err)
			continue
		}
		respBody, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			lastErr = fmt.Errorf("read body: %w", err)
			continue
		}

		if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500 {
			lastErr = fmt.Errorf("http %d: %s", resp.StatusCode, string(respBody))
			continue
		}

		var result anthropicResponse
		if err := json.Unmarshal(respBody, &result); err != nil {
			return "", fmt.Errorf("unmarshal response: %w", err)
		}
		if result.Error != nil {
			return "", fmt.Errorf("anthropic %s: %s", result.Error.Type, result.Error.Message)
		}
		for _, block := range result.Content {
			if block.Type == "text" {
				return block.Text, nil
			}
		}
		return "", fmt.Errorf("no text block in response")
	}
	return "", fmt.Errorf("max retries exceeded: %w", lastErr)
}
```

- [ ] **Step 5: Run LLM tests**

```bash
go test ./internal/llm/... -v 2>&1 | tail -15
```

Expected: all 3 tests PASS.

Note: the `time` import is needed in the test file. Add `"time"` to the import block in `anthropic_test.go`.

- [ ] **Step 6: Add new fields to config.go**

Replace the `Config` struct and `Load()` function in `internal/config/config.go` with:

```go
package config

import (
	"fmt"
	"os"
	"strconv"
	"time"
)

type Config struct {
	DBPath             string
	OllamaURL          string
	OllamaModel        string
	TeamID             string
	EmbeddingDim       int
	AnthropicAPIKey    string
	AnthropicModel     string
	PipelineInterval   time.Duration
	PipelineMinEntries int
	ClusterThreshold   float64
}

func Load() (Config, error) {
	dim := 768
	if v := os.Getenv("EMBEDDING_DIM"); v != "" {
		parsed, err := strconv.Atoi(v)
		if err != nil {
			return Config{}, fmt.Errorf("invalid EMBEDDING_DIM %q: must be a positive integer", v)
		}
		if parsed <= 0 {
			return Config{}, fmt.Errorf("invalid EMBEDDING_DIM %d: must be positive", parsed)
		}
		dim = parsed
	}

	minEntries := 10
	if v := os.Getenv("PIPELINE_MIN_ENTRIES"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n <= 0 {
			return Config{}, fmt.Errorf("invalid PIPELINE_MIN_ENTRIES %q: must be a positive integer", v)
		}
		minEntries = n
	}

	interval := time.Hour
	if v := os.Getenv("PIPELINE_INTERVAL"); v != "" {
		d, err := time.ParseDuration(v)
		if err != nil || d <= 0 {
			return Config{}, fmt.Errorf("invalid PIPELINE_INTERVAL %q: must be a positive duration (e.g. 30m, 2h)", v)
		}
		interval = d
	}

	clusterThresh := 0.85
	if v := os.Getenv("CLUSTER_THRESHOLD"); v != "" {
		f, err := strconv.ParseFloat(v, 64)
		if err != nil || f <= 0 || f > 1 {
			return Config{}, fmt.Errorf("invalid CLUSTER_THRESHOLD %q: must be a float in (0,1]", v)
		}
		clusterThresh = f
	}

	return Config{
		DBPath:             envOrDefault("DATABASE_PATH", "knowledge.db"),
		OllamaURL:          envOrDefault("OLLAMA_URL", "http://localhost:11434"),
		OllamaModel:        envOrDefault("OLLAMA_MODEL", "nomic-embed-text"),
		TeamID:             envOrDefault("TEAM_ID", "default"),
		EmbeddingDim:       dim,
		AnthropicAPIKey:    os.Getenv("ANTHROPIC_API_KEY"),
		AnthropicModel:     envOrDefault("ANTHROPIC_MODEL", "claude-haiku-4-5-20251001"),
		PipelineInterval:   interval,
		PipelineMinEntries: minEntries,
		ClusterThreshold:   clusterThresh,
	}, nil
}

func envOrDefault(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
```

- [ ] **Step 7: Add config tests for new fields**

Append to `internal/config/config_test.go` (inside the existing `package config` file, not replacing it):

```go
func TestLoad_PipelineDefaults(t *testing.T) {
	for _, k := range []string{"PIPELINE_MIN_ENTRIES", "PIPELINE_INTERVAL", "CLUSTER_THRESHOLD", "ANTHROPIC_MODEL"} {
		t.Setenv(k, "")
	}
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.PipelineMinEntries != 10 {
		t.Errorf("PipelineMinEntries = %d, want 10", cfg.PipelineMinEntries)
	}
	if cfg.PipelineInterval != time.Hour {
		t.Errorf("PipelineInterval = %v, want 1h", cfg.PipelineInterval)
	}
	if cfg.ClusterThreshold != 0.85 {
		t.Errorf("ClusterThreshold = %v, want 0.85", cfg.ClusterThreshold)
	}
	if cfg.AnthropicModel != "claude-haiku-4-5-20251001" {
		t.Errorf("AnthropicModel = %q, want claude-haiku-4-5-20251001", cfg.AnthropicModel)
	}
}

func TestLoad_InvalidPipelineInterval(t *testing.T) {
	t.Setenv("PIPELINE_INTERVAL", "not-a-duration")
	_, err := Load()
	if err == nil {
		t.Fatal("expected error for invalid PIPELINE_INTERVAL")
	}
}

func TestLoad_InvalidClusterThreshold(t *testing.T) {
	t.Setenv("CLUSTER_THRESHOLD", "1.5")
	_, err := Load()
	if err == nil {
		t.Fatal("expected error for CLUSTER_THRESHOLD > 1")
	}
}
```

Note: add `"time"` to the import block in `config_test.go`.

- [ ] **Step 8: Run all config and LLM tests**

```bash
go test ./internal/config/... ./internal/llm/... -v 2>&1 | tail -20
```

Expected: all tests PASS (6 config + 3 LLM = 9 total).

- [ ] **Step 9: Build check**

```bash
CGO_ENABLED=1 go build ./cmd/server/ && echo "ok"
```

Expected: `ok`

- [ ] **Step 10: Commit**

```bash
git add internal/config/config.go internal/config/config_test.go internal/llm/
git commit -m "feat: add Anthropic LLM client and pipeline config fields"
```

---

## Task 3: Clustering algorithm

**Files:**
- Create: `internal/pipeline/cluster.go`
- Create: `internal/pipeline/cluster_test.go`

- [ ] **Step 1: Write failing tests**

Create `internal/pipeline/cluster_test.go`:

```go
package pipeline

import (
	"math"
	"sort"
	"testing"
)

func TestCosineSim_Identical(t *testing.T) {
	a := []float32{1, 0, 0, 0}
	if got := cosineSim(a, a); math.Abs(got-1.0) > 1e-6 {
		t.Errorf("cosineSim(identical) = %v, want 1.0", got)
	}
}

func TestCosineSim_Orthogonal(t *testing.T) {
	a := []float32{1, 0}
	b := []float32{0, 1}
	if got := cosineSim(a, b); math.Abs(got) > 1e-6 {
		t.Errorf("cosineSim(orthogonal) = %v, want 0.0", got)
	}
}

func TestCosineSim_ZeroVector(t *testing.T) {
	a := []float32{0, 0}
	b := []float32{1, 0}
	if got := cosineSim(a, b); got != 0 {
		t.Errorf("cosineSim(zero) = %v, want 0.0", got)
	}
}

func TestCluster_GroupsSimilar(t *testing.T) {
	embs := map[string][]float32{
		"a": {1, 0, 0, 0},
		"b": {0.99, 0.14, 0, 0},  // very similar to a
		"c": {0, 0, 0, 1},         // orthogonal to a and b
	}
	domains := map[string]string{"a": "finance", "b": "finance", "c": "legal"}

	clusters := Cluster(embs, domains, 0.9)

	if len(clusters) != 1 {
		t.Fatalf("expected 1 cluster, got %d: %+v", len(clusters), clusters)
	}
	ids := clusters[0].EntryIDs
	sort.Strings(ids)
	if ids[0] != "a" || ids[1] != "b" {
		t.Errorf("cluster entries = %v, want [a b]", ids)
	}
	if clusters[0].Domain != "finance" {
		t.Errorf("cluster domain = %q, want finance", clusters[0].Domain)
	}
}

func TestCluster_NoClusters_AllDissimilar(t *testing.T) {
	embs := map[string][]float32{
		"a": {1, 0},
		"b": {0, 1},
	}
	clusters := Cluster(embs, map[string]string{}, 0.9)
	if len(clusters) != 0 {
		t.Errorf("expected 0 clusters, got %d", len(clusters))
	}
}

func TestCluster_SingleEntry_NotClustered(t *testing.T) {
	embs := map[string][]float32{
		"a": {1, 0},
	}
	clusters := Cluster(embs, map[string]string{}, 0.9)
	if len(clusters) != 0 {
		t.Errorf("single entry should not produce a cluster, got %d", len(clusters))
	}
}
```

- [ ] **Step 2: Run tests — verify they fail**

```bash
go test ./internal/pipeline/... 2>&1 | head -10
```

Expected: compilation error (package/function not found).

- [ ] **Step 3: Create internal/pipeline/cluster.go**

```go
package pipeline

import "math"

// ClusterCandidate is a group of semantically related entry IDs.
type ClusterCandidate struct {
	EntryIDs []string
	Domain   string
}

// Cluster groups entries whose embeddings have cosine similarity >= threshold.
// embeddings maps entry ID to its float32 vector.
// domains maps entry ID to its domain string (used to pick the cluster's majority domain).
// Only groups of 2+ entries are returned.
func Cluster(embeddings map[string][]float32, domains map[string]string, threshold float64) []ClusterCandidate {
	ids := make([]string, 0, len(embeddings))
	for id := range embeddings {
		ids = append(ids, id)
	}

	assigned := make(map[string]bool, len(ids))
	var candidates []ClusterCandidate

	for _, seed := range ids {
		if assigned[seed] {
			continue
		}
		group := []string{seed}
		assigned[seed] = true

		for _, other := range ids {
			if assigned[other] {
				continue
			}
			if cosineSim(embeddings[seed], embeddings[other]) >= threshold {
				group = append(group, other)
				assigned[other] = true
			}
		}

		if len(group) >= 2 {
			candidates = append(candidates, ClusterCandidate{
				EntryIDs: group,
				Domain:   majorityDomain(group, domains),
			})
		}
	}
	return candidates
}

func cosineSim(a, b []float32) float64 {
	var dot, normA, normB float64
	for i := range a {
		ai, bi := float64(a[i]), float64(b[i])
		dot += ai * bi
		normA += ai * ai
		normB += bi * bi
	}
	if normA == 0 || normB == 0 {
		return 0
	}
	return dot / (math.Sqrt(normA) * math.Sqrt(normB))
}

func majorityDomain(ids []string, domains map[string]string) string {
	counts := make(map[string]int)
	for _, id := range ids {
		counts[domains[id]]++
	}
	var best string
	var bestCount int
	for d, n := range counts {
		if n > bestCount || (n == bestCount && d < best) {
			best = d
			bestCount = n
		}
	}
	return best
}
```

- [ ] **Step 4: Run tests**

```bash
go test ./internal/pipeline/... -v 2>&1 | tail -15
```

Expected: all 5 cluster tests PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/pipeline/cluster.go internal/pipeline/cluster_test.go
git commit -m "feat(pipeline): add cosine similarity clustering algorithm"
```

---

## Task 4: LLM analysis functions

**Files:**
- Create: `internal/pipeline/analyze.go`
- Create: `internal/pipeline/analyze_test.go`

- [ ] **Step 1: Write failing tests**

Create `internal/pipeline/analyze_test.go`:

```go
package pipeline

import (
	"context"
	"testing"

	"github.com/dsandor/memory/internal/storage"
)

func TestSummarizeCluster(t *testing.T) {
	mock := &mockLLM{response: `{"title":"Finance Cluster","summary":"Related finance patterns."}`}
	entries := []storage.KnowledgeEntry{
		{Title: "Entry 1", Content: "Finance content 1"},
		{Title: "Entry 2", Content: "Finance content 2"},
	}
	result, err := SummarizeCluster(context.Background(), mock, entries)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Title != "Finance Cluster" {
		t.Errorf("title = %q, want %q", result.Title, "Finance Cluster")
	}
	if result.Summary == "" {
		t.Error("summary is empty")
	}
}

func TestSummarizeCluster_MarkdownFence(t *testing.T) {
	mock := &mockLLM{response: "```json\n{\"title\":\"T\",\"summary\":\"S\"}\n```"}
	entries := []storage.KnowledgeEntry{{Title: "E", Content: "C"}}
	result, err := SummarizeCluster(context.Background(), mock, entries)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Title != "T" {
		t.Errorf("title = %q, want T", result.Title)
	}
}

func TestScoreEntry(t *testing.T) {
	mock := &mockLLM{response: `{"coherence":0.8,"specificity":0.7}`}
	entry := storage.KnowledgeEntry{Title: "Test", Content: "Content"}
	score, err := ScoreEntry(context.Background(), mock, entry)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if score.Coherence != 0.8 {
		t.Errorf("coherence = %v, want 0.8", score.Coherence)
	}
	if score.Specificity != 0.7 {
		t.Errorf("specificity = %v, want 0.7", score.Specificity)
	}
	if math.Abs(score.Total-1.5) > 1e-9 {
		t.Errorf("total = %v, want 1.5", score.Total)
	}
}

func TestDetectGaps(t *testing.T) {
	mock := &mockLLM{response: `{"gaps":[{"domain":"risk","description":"Thin","entry_count":1,"recommendation":"Add more"}]}`}
	gaps, err := DetectGaps(context.Background(), mock, map[string]int{"finance": 10, "risk": 1})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(gaps) != 1 {
		t.Fatalf("expected 1 gap, got %d", len(gaps))
	}
	if gaps[0].Domain != "risk" {
		t.Errorf("domain = %q, want risk", gaps[0].Domain)
	}
}

func TestDetectGaps_Empty(t *testing.T) {
	mock := &mockLLM{response: `{"gaps":[]}`}
	gaps, err := DetectGaps(context.Background(), mock, map[string]int{"finance": 20})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(gaps) != 0 {
		t.Errorf("expected 0 gaps, got %d", len(gaps))
	}
}

func TestExtractJSON_StripsFences(t *testing.T) {
	cases := []struct{ input, want string }{
		{`{"k":"v"}`, `{"k":"v"}`},
		{"```json\n{\"k\":\"v\"}\n```", `{"k":"v"}`},
		{"```\n{\"k\":\"v\"}\n```", `{"k":"v"}`},
	}
	for _, tc := range cases {
		if got := extractJSON(tc.input); got != tc.want {
			t.Errorf("extractJSON(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}
```

Note: add `"math"` to the import block.

- [ ] **Step 2: Create internal/pipeline/testhelpers_test.go with shared mocks**

```go
package pipeline

import "context"

type mockLLM struct {
	response string
	err      error
}

func (m *mockLLM) Complete(_ context.Context, _ string) (string, error) {
	return m.response, m.err
}
```

- [ ] **Step 3: Run tests — verify they fail**

```bash
go test ./internal/pipeline/... 2>&1 | head -15
```

Expected: compilation errors (SummarizeCluster etc. not defined).

- [ ] **Step 4: Create internal/pipeline/analyze.go**

```go
package pipeline

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"regexp"
	"strings"

	"github.com/dsandor/memory/internal/llm"
	"github.com/dsandor/memory/internal/storage"
)

var jsonFenceRe = regexp.MustCompile("(?s)```(?:json)?\\s*(.*?)```")

func extractJSON(s string) string {
	if m := jsonFenceRe.FindStringSubmatch(s); len(m) > 1 {
		return strings.TrimSpace(m[1])
	}
	return strings.TrimSpace(s)
}

type SummarizeResult struct {
	Title   string `json:"title"`
	Summary string `json:"summary"`
}

// SummarizeCluster asks the LLM to produce a title and summary for a cluster of entries.
func SummarizeCluster(ctx context.Context, client llm.Client, entries []storage.KnowledgeEntry) (SummarizeResult, error) {
	var sb strings.Builder
	for i, e := range entries {
		fmt.Fprintf(&sb, "%d. Title: %s\nContent: %s\n\n", i+1, e.Title, truncate(e.Content, 200))
	}
	prompt := fmt.Sprintf(
		"Summarize this group of related knowledge entries into a concise title and 2-3 sentence summary.\n"+
			"Return ONLY valid JSON: {\"title\": \"...\", \"summary\": \"...\"}\n\nEntries:\n%s",
		sb.String(),
	)
	resp, err := client.Complete(ctx, prompt)
	if err != nil {
		return SummarizeResult{}, fmt.Errorf("llm: %w", err)
	}
	var result SummarizeResult
	if err := json.Unmarshal([]byte(extractJSON(resp)), &result); err != nil {
		return SummarizeResult{}, fmt.Errorf("parse summarize response: %w", err)
	}
	return result, nil
}

type QualityScore struct {
	Coherence   float64 `json:"coherence"`
	Specificity float64 `json:"specificity"`
	Total       float64
}

// ScoreEntry asks the LLM to evaluate coherence and specificity of an entry.
func ScoreEntry(ctx context.Context, client llm.Client, entry storage.KnowledgeEntry) (QualityScore, error) {
	prompt := fmt.Sprintf(
		"Evaluate this knowledge entry on two dimensions, each from 0.0 to 1.0:\n"+
			"- coherence: how clear, well-structured, and self-consistent the content is\n"+
			"- specificity: how actionable and domain-specific (vs. generic) the content is\n"+
			"Return ONLY valid JSON: {\"coherence\": 0.0, \"specificity\": 0.0}\n\n"+
			"Title: %s\nContent: %s",
		entry.Title, truncate(entry.Content, 300),
	)
	resp, err := client.Complete(ctx, prompt)
	if err != nil {
		return QualityScore{}, fmt.Errorf("llm: %w", err)
	}
	var score QualityScore
	if err := json.Unmarshal([]byte(extractJSON(resp)), &score); err != nil {
		return QualityScore{}, fmt.Errorf("parse score response: %w", err)
	}
	score.Total = score.Coherence + score.Specificity
	return score, nil
}

// DomainGap represents a domain with insufficient or missing knowledge coverage.
type DomainGap struct {
	Domain         string `json:"domain"`
	Description    string `json:"description"`
	EntryCount     int    `json:"entry_count"`
	Recommendation string `json:"recommendation"`
}

// DetectGaps asks the LLM to identify domains with insufficient coverage.
func DetectGaps(ctx context.Context, client llm.Client, domainCounts map[string]int) ([]DomainGap, error) {
	var sb strings.Builder
	for d, n := range domainCounts {
		fmt.Fprintf(&sb, "- %s: %d entries\n", d, n)
	}
	prompt := fmt.Sprintf(
		"Analyze this domain coverage for a team knowledge base and identify gaps — domains with insufficient entries or missing domains that would be valuable.\n"+
			"Return ONLY valid JSON: {\"gaps\": [{\"domain\": \"...\", \"description\": \"...\", \"entry_count\": 0, \"recommendation\": \"...\"}]}\n"+
			"If no gaps found, return {\"gaps\": []}.\n\nDomain coverage:\n%s",
		sb.String(),
	)
	resp, err := client.Complete(ctx, prompt)
	if err != nil {
		return nil, fmt.Errorf("llm: %w", err)
	}
	var result struct {
		Gaps []DomainGap `json:"gaps"`
	}
	if err := json.Unmarshal([]byte(extractJSON(resp)), &result); err != nil {
		return nil, fmt.Errorf("parse gaps response: %w", err)
	}
	return result.Gaps, nil
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

// ensure math is used (imported for QualityScore.Total calculation elsewhere)
var _ = math.Sqrt
```

Note: `math` is imported here to satisfy the test file's `math.Abs` usage in the same package. Actually the `math` import is in the test, not in analyze.go. Remove `var _ = math.Sqrt` and the `math` import from analyze.go. Only the test file imports `math`.

The correct `analyze.go` does NOT import `math`. Remove that import and the `var _ = math.Sqrt` line.

- [ ] **Step 5: Run tests**

```bash
go test ./internal/pipeline/... -v -run Test 2>&1 | tail -20
```

Expected: all 10 tests PASS (5 cluster + 5 analyze).

- [ ] **Step 6: Commit**

```bash
git add internal/pipeline/analyze.go internal/pipeline/analyze_test.go internal/pipeline/testhelpers_test.go
git commit -m "feat(pipeline): add LLM-backed cluster summarization, quality scoring, and gap detection"
```

---

## Task 5: Pipeline orchestrator

**Files:**
- Create: `internal/pipeline/pipeline.go`
- Create: `internal/pipeline/pipeline_test.go`
- Modify: `internal/pipeline/testhelpers_test.go` (add mockAnalysisStore)

- [ ] **Step 1: Write the failing test**

Append to `internal/pipeline/testhelpers_test.go`:

```go
package pipeline

import (
	"context"

	"github.com/dsandor/memory/internal/storage"
)

type mockAnalysisStore struct {
	entries   []storage.KnowledgeEntry
	embeddings map[string][]float32
	clusters  []storage.Cluster
	runs      []storage.PipelineRun
	snapshots []storage.DatasetSnapshot
}

func (m *mockAnalysisStore) CountEntries(_ context.Context) (int, error) {
	return len(m.entries), nil
}
func (m *mockAnalysisStore) GetAllEmbeddings(_ context.Context) (map[string][]float32, error) {
	return m.embeddings, nil
}
func (m *mockAnalysisStore) ListEntries(_ context.Context, _ storage.ListFilter) ([]storage.KnowledgeEntry, error) {
	return m.entries, nil
}
func (m *mockAnalysisStore) StoreCluster(_ context.Context, c storage.Cluster) (string, error) {
	c.ID = "cluster-" + c.Title
	m.clusters = append(m.clusters, c)
	return c.ID, nil
}
func (m *mockAnalysisStore) ListClusters(_ context.Context) ([]storage.Cluster, error) {
	return m.clusters, nil
}
func (m *mockAnalysisStore) DeleteClustersByRunID(_ context.Context, _ string) error { return nil }
func (m *mockAnalysisStore) StartPipelineRun(_ context.Context, trigger string) (string, error) {
	r := storage.PipelineRun{ID: "run-1", Status: "running", Trigger: trigger}
	m.runs = append(m.runs, r)
	return "run-1", nil
}
func (m *mockAnalysisStore) FinishPipelineRun(_ context.Context, id, status string, ep, cf int, errs []string) error {
	for i := range m.runs {
		if m.runs[i].ID == id {
			m.runs[i].Status = status
			m.runs[i].EntriesProcessed = ep
			m.runs[i].ClustersFound = cf
			m.runs[i].Errors = errs
		}
	}
	return nil
}
func (m *mockAnalysisStore) GetLatestPipelineRun(_ context.Context) (*storage.PipelineRun, error) {
	if len(m.runs) == 0 {
		return nil, nil
	}
	r := m.runs[len(m.runs)-1]
	return &r, nil
}
func (m *mockAnalysisStore) StoreSnapshot(_ context.Context, snap storage.DatasetSnapshot) (string, error) {
	snap.ID = "snap-1"
	m.snapshots = append(m.snapshots, snap)
	return snap.ID, nil
}
func (m *mockAnalysisStore) GetLatestSnapshot(_ context.Context) (*storage.DatasetSnapshot, error) {
	if len(m.snapshots) == 0 {
		return nil, nil
	}
	s := m.snapshots[len(m.snapshots)-1]
	return &s, nil
}

// Store interface stubs (not exercised in pipeline tests)
func (m *mockAnalysisStore) StoreEntry(_ context.Context, _ storage.KnowledgeEntry, _ []float32) (string, error) {
	return "", nil
}
func (m *mockAnalysisStore) GetEntry(_ context.Context, _ string) (*storage.KnowledgeEntry, error) {
	return nil, nil
}
func (m *mockAnalysisStore) DeleteEntry(_ context.Context, _ string) error { return nil }
func (m *mockAnalysisStore) SearchSimilar(_ context.Context, _ []float32, _ int) ([]storage.SearchResult, error) {
	return nil, nil
}
func (m *mockAnalysisStore) Close() error { return nil }
```

Create `internal/pipeline/pipeline_test.go`:

```go
package pipeline

import (
	"context"
	"testing"
	"time"
)

func TestPipeline_Run_CreatesClustersAndSnapshot(t *testing.T) {
	store := &mockAnalysisStore{
		entries: []storage.KnowledgeEntry{
			{ID: "a", Title: "Entry A", Content: "Finance pattern", Domain: "finance"},
			{ID: "b", Title: "Entry B", Content: "Finance workflow", Domain: "finance"},
		},
		embeddings: map[string][]float32{
			"a": {1, 0, 0, 0},
			"b": {0.99, 0.14, 0, 0},
		},
	}
	llmMock := &mockLLM{response: `{"title":"Finance","summary":"Finance entries."}`}

	p := New(store, llmMock, Config{
		MinEntries:       2,
		Interval:         time.Hour,
		ClusterThreshold: 0.9,
	})

	if err := p.Run(context.Background(), "test"); err != nil {
		t.Fatalf("pipeline run: %v", err)
	}

	if len(store.clusters) != 1 {
		t.Errorf("want 1 cluster, got %d", len(store.clusters))
	}
	if len(store.snapshots) != 1 {
		t.Errorf("want 1 snapshot, got %d", len(store.snapshots))
	}
	if len(store.runs) != 1 {
		t.Fatalf("want 1 run, got %d", len(store.runs))
	}
	if store.runs[0].Status == "running" {
		t.Error("run should be completed, not running")
	}
	if store.snapshots[0].EntryCount != 2 {
		t.Errorf("snapshot entry_count = %d, want 2", store.snapshots[0].EntryCount)
	}
	if store.snapshots[0].ClusterCount != 1 {
		t.Errorf("snapshot cluster_count = %d, want 1", store.snapshots[0].ClusterCount)
	}
}

func TestPipeline_Run_NoClusters_NoDissimilarEntries(t *testing.T) {
	store := &mockAnalysisStore{
		entries: []storage.KnowledgeEntry{
			{ID: "a", Title: "Entry A", Content: "A", Domain: "finance"},
			{ID: "b", Title: "Entry B", Content: "B", Domain: "legal"},
		},
		embeddings: map[string][]float32{
			"a": {1, 0, 0, 0},
			"b": {0, 0, 0, 1},
		},
	}
	llmMock := &mockLLM{response: `{"gaps":[]}`}

	p := New(store, llmMock, Config{
		MinEntries:       1,
		Interval:         time.Hour,
		ClusterThreshold: 0.9,
	})

	if err := p.Run(context.Background(), "test"); err != nil {
		t.Fatalf("pipeline run: %v", err)
	}

	if len(store.clusters) != 0 {
		t.Errorf("want 0 clusters for dissimilar entries, got %d", len(store.clusters))
	}
	if len(store.snapshots) != 1 {
		t.Errorf("snapshot should still be created, got %d", len(store.snapshots))
	}
}
```

Note: add `"github.com/dsandor/memory/internal/storage"` to the imports in `pipeline_test.go`.

- [ ] **Step 2: Run tests — verify they fail**

```bash
go test ./internal/pipeline/... 2>&1 | head -10
```

Expected: compilation error (New, Config, Pipeline not found).

- [ ] **Step 3: Create internal/pipeline/pipeline.go**

```go
package pipeline

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"github.com/dsandor/memory/internal/llm"
	"github.com/dsandor/memory/internal/storage"
)

// Config controls when and how the pipeline runs.
type Config struct {
	MinEntries       int
	Interval         time.Duration
	ClusterThreshold float64
}

// Pipeline orchestrates the knowledge analysis pipeline.
type Pipeline struct {
	store storage.AnalysisStore
	llm   llm.Client
	cfg   Config
}

// New creates a new Pipeline.
func New(store storage.AnalysisStore, llmClient llm.Client, cfg Config) *Pipeline {
	return &Pipeline{store: store, llm: llmClient, cfg: cfg}
}

// Start runs the pipeline as a background goroutine until ctx is cancelled.
// It checks the entry count on every tick and runs the pipeline when count >= cfg.MinEntries.
func (p *Pipeline) Start(ctx context.Context) {
	ticker := time.NewTicker(p.cfg.Interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			count, err := p.store.CountEntries(ctx)
			if err != nil || count < p.cfg.MinEntries {
				continue
			}
			if err := p.Run(ctx, "interval"); err != nil {
				log.Printf("pipeline run error: %v", err)
			}
		}
	}
}

// Run executes a single pipeline pass: cluster → score → summarize → detect gaps → snapshot.
func (p *Pipeline) Run(ctx context.Context, trigger string) error {
	runID, err := p.store.StartPipelineRun(ctx, trigger)
	if err != nil {
		return fmt.Errorf("start run: %w", err)
	}

	var runErrs []string
	clustersFound := 0

	entries, err := p.store.ListEntries(ctx, storage.ListFilter{Limit: 1000})
	if err != nil {
		runErrs = append(runErrs, fmt.Sprintf("list entries: %v", err))
		return p.store.FinishPipelineRun(ctx, runID, "failed", 0, 0, runErrs)
	}

	embeddings, err := p.store.GetAllEmbeddings(ctx)
	if err != nil {
		runErrs = append(runErrs, fmt.Sprintf("get embeddings: %v", err))
		return p.store.FinishPipelineRun(ctx, runID, "failed", len(entries), 0, runErrs)
	}

	entryByID := make(map[string]storage.KnowledgeEntry, len(entries))
	domainByID := make(map[string]string, len(entries))
	domainCounts := make(map[string]int)
	for _, e := range entries {
		entryByID[e.ID] = e
		domainByID[e.ID] = e.Domain
		domainCounts[e.Domain]++
	}

	candidates := Cluster(embeddings, domainByID, p.cfg.ClusterThreshold)

	for _, cand := range candidates {
		clusterEntries := make([]storage.KnowledgeEntry, 0, len(cand.EntryIDs))
		for _, id := range cand.EntryIDs {
			if e, ok := entryByID[id]; ok {
				clusterEntries = append(clusterEntries, e)
			}
		}

		summary, err := SummarizeCluster(ctx, p.llm, clusterEntries)
		if err != nil {
			runErrs = append(runErrs, fmt.Sprintf("summarize cluster: %v", err))
			continue
		}

		var totalScore float64
		for _, e := range clusterEntries {
			score, err := ScoreEntry(ctx, p.llm, e)
			if err == nil {
				totalScore += score.Total
			}
		}
		avgScore := 0.0
		if len(clusterEntries) > 0 {
			avgScore = totalScore / float64(len(clusterEntries))
		}

		cluster := storage.Cluster{
			Domain:        cand.Domain,
			Title:         summary.Title,
			Summary:       summary.Summary,
			EntryIDs:      cand.EntryIDs,
			QualityScore:  avgScore,
			PipelineRunID: runID,
		}
		if _, err := p.store.StoreCluster(ctx, cluster); err != nil {
			runErrs = append(runErrs, fmt.Sprintf("store cluster: %v", err))
			continue
		}
		clustersFound++
	}

	gaps, err := DetectGaps(ctx, p.llm, domainCounts)
	if err != nil {
		runErrs = append(runErrs, fmt.Sprintf("detect gaps: %v", err))
		gaps = nil
	}

	latest, _ := p.store.GetLatestSnapshot(ctx)
	version := 1
	if latest != nil {
		version = latest.Version + 1
	}

	type snapshotData struct {
		Gaps []DomainGap `json:"gaps"`
	}
	snapDataJSON, _ := json.Marshal(snapshotData{Gaps: gaps})

	snap := storage.DatasetSnapshot{
		Version:       version,
		ClusterCount:  clustersFound,
		EntryCount:    len(entries),
		Data:          string(snapDataJSON),
		PipelineRunID: runID,
	}
	if _, err := p.store.StoreSnapshot(ctx, snap); err != nil {
		runErrs = append(runErrs, fmt.Sprintf("store snapshot: %v", err))
	}

	status := "complete"
	if len(runErrs) > 0 {
		status = "complete_with_errors"
	}
	return p.store.FinishPipelineRun(ctx, runID, status, len(entries), clustersFound, runErrs)
}
```

- [ ] **Step 4: Run tests**

```bash
go test ./internal/pipeline/... -v 2>&1 | tail -20
```

Expected: all 12 tests PASS (5 cluster + 5 analyze + 2 pipeline).

- [ ] **Step 5: Build check**

```bash
CGO_ENABLED=1 go build ./cmd/server/ && echo "ok"
```

Expected: `ok`

- [ ] **Step 6: Commit**

```bash
git add internal/pipeline/pipeline.go internal/pipeline/pipeline_test.go internal/pipeline/testhelpers_test.go
git commit -m "feat(pipeline): add pipeline orchestrator with clustering, scoring, and snapshots"
```

---

## Task 6: MCP tools, server wiring, and main.go integration

**Files:**
- Create: `internal/mcp/analysis_tools.go`
- Create: `internal/mcp/analysis_tools_test.go`
- Modify: `internal/mcp/server.go`
- Modify: `cmd/server/main.go`

- [ ] **Step 1: Write failing tests**

Create `internal/mcp/analysis_tools_test.go`:

```go
package mcp

import (
	"context"
	"testing"
	"time"

	"github.com/dsandor/memory/internal/storage"
	mcplib "github.com/mark3labs/mcp-go/mcp"
)

type mockAnalysisStore struct {
	mockStore
	clusters []storage.Cluster
	run      *storage.PipelineRun
	snapshot *storage.DatasetSnapshot
}

func (m *mockAnalysisStore) CountEntries(_ context.Context) (int, error)  { return 0, nil }
func (m *mockAnalysisStore) GetAllEmbeddings(_ context.Context) (map[string][]float32, error) {
	return nil, nil
}
func (m *mockAnalysisStore) ListClusters(_ context.Context) ([]storage.Cluster, error) {
	return m.clusters, nil
}
func (m *mockAnalysisStore) StoreCluster(_ context.Context, _ storage.Cluster) (string, error) {
	return "id", nil
}
func (m *mockAnalysisStore) DeleteClustersByRunID(_ context.Context, _ string) error { return nil }
func (m *mockAnalysisStore) StartPipelineRun(_ context.Context, _ string) (string, error) {
	return "id", nil
}
func (m *mockAnalysisStore) FinishPipelineRun(_ context.Context, _, _ string, _, _ int, _ []string) error {
	return nil
}
func (m *mockAnalysisStore) GetLatestPipelineRun(_ context.Context) (*storage.PipelineRun, error) {
	return m.run, nil
}
func (m *mockAnalysisStore) StoreSnapshot(_ context.Context, _ storage.DatasetSnapshot) (string, error) {
	return "id", nil
}
func (m *mockAnalysisStore) GetLatestSnapshot(_ context.Context) (*storage.DatasetSnapshot, error) {
	return m.snapshot, nil
}

func TestHandleClusterList_ReturnsClusters(t *testing.T) {
	store := &mockAnalysisStore{
		clusters: []storage.Cluster{
			{ID: "c1", Title: "Finance Cluster", Summary: "Finance entries", Domain: "finance"},
		},
	}
	handler := HandleClusterList(store)
	result, err := handler(context.Background(), mcplib.CallToolRequest{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Errorf("unexpected tool error")
	}
}

func TestHandleClusterList_Empty(t *testing.T) {
	store := &mockAnalysisStore{}
	handler := HandleClusterList(store)
	result, err := handler(context.Background(), mcplib.CallToolRequest{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Errorf("unexpected tool error")
	}
}

func TestHandleAnalysisStatus_WithData(t *testing.T) {
	now := time.Now()
	store := &mockAnalysisStore{
		run:      &storage.PipelineRun{ID: "r1", Status: "complete", StartedAt: now},
		snapshot: &storage.DatasetSnapshot{ID: "s1", Version: 2, ClusterCount: 3, EntryCount: 15},
	}
	handler := HandleAnalysisStatus(store)
	result, err := handler(context.Background(), mcplib.CallToolRequest{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Errorf("unexpected tool error")
	}
}

func TestHandleAnalysisStatus_NoData(t *testing.T) {
	store := &mockAnalysisStore{}
	handler := HandleAnalysisStatus(store)
	result, err := handler(context.Background(), mcplib.CallToolRequest{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Errorf("unexpected tool error for empty status")
	}
}
```

- [ ] **Step 2: Run tests — verify they fail**

```bash
CGO_ENABLED=1 go test ./internal/mcp/... 2>&1 | head -15
```

Expected: compilation errors (HandleClusterList, HandleAnalysisStatus not found; mockStore embed issue).

- [ ] **Step 3: Create internal/mcp/analysis_tools.go**

```go
package mcp

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/dsandor/memory/internal/storage"
	mcplib "github.com/mark3labs/mcp-go/mcp"
)

// HandleClusterList returns a handler that lists all knowledge clusters.
func HandleClusterList(store storage.AnalysisStore) func(context.Context, mcplib.CallToolRequest) (*mcplib.CallToolResult, error) {
	return func(ctx context.Context, req mcplib.CallToolRequest) (*mcplib.CallToolResult, error) {
		clusters, err := store.ListClusters(ctx)
		if err != nil {
			return mcplib.NewToolResultError(fmt.Sprintf("list clusters: %v", err)), nil
		}
		if clusters == nil {
			clusters = []storage.Cluster{}
		}
		data, _ := json.Marshal(clusters)
		return mcplib.NewToolResultText(string(data)), nil
	}
}

// HandleAnalysisStatus returns a handler that reports pipeline and snapshot status.
func HandleAnalysisStatus(store storage.AnalysisStore) func(context.Context, mcplib.CallToolRequest) (*mcplib.CallToolResult, error) {
	return func(ctx context.Context, req mcplib.CallToolRequest) (*mcplib.CallToolResult, error) {
		run, err := store.GetLatestPipelineRun(ctx)
		if err != nil {
			return mcplib.NewToolResultError(fmt.Sprintf("get pipeline run: %v", err)), nil
		}
		snap, err := store.GetLatestSnapshot(ctx)
		if err != nil {
			return mcplib.NewToolResultError(fmt.Sprintf("get snapshot: %v", err)), nil
		}
		status := map[string]any{
			"pipeline_run":    run,
			"latest_snapshot": snap,
		}
		data, _ := json.Marshal(status)
		return mcplib.NewToolResultText(string(data)), nil
	}
}
```

- [ ] **Step 4: Add RegisterAnalysisTools to server.go**

Add after the closing brace of `NewMCPServer` in `internal/mcp/server.go`:

```go
// RegisterAnalysisTools adds the cluster_list and analysis_status tools to an existing MCP server.
func RegisterAnalysisTools(s *server.MCPServer, store storage.AnalysisStore) {
	s.AddTool(
		mcplib.NewTool("cluster_list",
			mcplib.WithDescription("List knowledge clusters produced by the analysis pipeline, with LLM-generated summaries"),
		),
		HandleClusterList(store),
	)
	s.AddTool(
		mcplib.NewTool("analysis_status",
			mcplib.WithDescription("Show the latest analysis pipeline run status and dataset snapshot info"),
		),
		HandleAnalysisStatus(store),
	)
}
```

- [ ] **Step 5: Run MCP tests**

```bash
CGO_ENABLED=1 go test ./internal/mcp/... -v 2>&1 | tail -20
```

Expected: all tests PASS (6 from Phase 1 + 4 new = 10 total).

- [ ] **Step 6: Update cmd/server/main.go to start the pipeline and register analysis tools**

Replace `cmd/server/main.go` with:

```go
package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/dsandor/memory/internal/config"
	"github.com/dsandor/memory/internal/embedding"
	"github.com/dsandor/memory/internal/llm"
	internalmcp "github.com/dsandor/memory/internal/mcp"
	"github.com/dsandor/memory/internal/pipeline"
	"github.com/dsandor/memory/internal/storage"
	"github.com/mark3labs/mcp-go/server"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("load config: %v", err)
	}

	store, err := storage.NewSQLiteStore(cfg.DBPath, cfg.EmbeddingDim)
	if err != nil {
		log.Fatalf("open storage: %v", err)
	}
	defer store.Close()

	embedder := embedding.NewOllamaEmbedder(cfg.OllamaURL, cfg.OllamaModel)

	mcpServer := internalmcp.NewMCPServer(store, embedder)
	internalmcp.RegisterAnalysisTools(mcpServer, store)

	if cfg.AnthropicAPIKey != "" {
		llmClient := llm.NewAnthropicClient(cfg.AnthropicAPIKey, cfg.AnthropicModel)
		p := pipeline.New(store, llmClient, pipeline.Config{
			MinEntries:       cfg.PipelineMinEntries,
			Interval:         cfg.PipelineInterval,
			ClusterThreshold: cfg.ClusterThreshold,
		})
		ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer cancel()
		go p.Start(ctx)
	}

	if err := server.ServeStdio(mcpServer); err != nil {
		log.Fatalf("serve: %v", err)
	}
}
```

- [ ] **Step 7: Build the complete binary**

```bash
CGO_ENABLED=1 go build -o tribal-knowledge ./cmd/server/ && echo "build ok"
```

Expected: `build ok` with no errors.

- [ ] **Step 8: Run all tests**

```bash
CGO_ENABLED=1 go test ./... 2>&1 | tail -20
```

Expected: all packages PASS. Approximate test count: 6 storage (Phase 1) + 7 analysis storage + 6 config + 3 llm + 5 cluster + 5 analyze + 2 pipeline + 10 mcp = ~44 tests.

- [ ] **Step 9: Commit**

```bash
git add internal/mcp/analysis_tools.go internal/mcp/analysis_tools_test.go internal/mcp/server.go cmd/server/main.go
git commit -m "feat: wire analysis pipeline and MCP tools into server binary"
```

---

## Self-Review

### Spec coverage

| Requirement | Task |
|-------------|------|
| Pipeline runner with interval + threshold trigger | Task 5 (pipeline.go Start()) |
| Cosine similarity clustering | Task 3 (cluster.go) |
| Quality scoring: coherence + specificity via LLM | Task 4 (analyze.go ScoreEntry) |
| LLM cluster summarization (claude-haiku-4-5-20251001) | Task 4 (analyze.go SummarizeCluster) |
| Coverage gap detection | Task 4 (analyze.go DetectGaps) |
| Versioned dataset snapshots | Task 5 (pipeline.go StoreSnapshot) |
| Schema: clusters, pipeline_runs, dataset_snapshots | Task 1 (migrate()) |
| MCP tools: cluster_list, analysis_status | Task 6 |
| Anthropic API integration with retry | Task 2 (anthropic.go) |
| Deduplication (near-duplicate detection) | **DEFERRED** — spec says "LLM-assisted merge"; actual entry deletion deferred to Phase 5 curator workflow. Duplicates surface via clustering (high similarity = same cluster). |

### Placeholder scan

No TBD, TODO, or incomplete steps. All code blocks are complete.

### Type consistency

- `storage.AnalysisStore` is used consistently as the interface for the pipeline and MCP analysis tools
- `storage.Cluster`, `storage.PipelineRun`, `storage.DatasetSnapshot` types are defined in Task 1 and used in Tasks 5 and 6
- `llm.Client` interface defined in Task 2; `mockLLM` in testhelpers_test.go satisfies it
- `pipeline.Config` fields `MinEntries`, `Interval`, `ClusterThreshold` match config fields added in Task 2

---

## Exit Criteria Verification

After completing all tasks:

1. Build succeeds: `CGO_ENABLED=1 go build -o tribal-knowledge ./cmd/server/`
2. All tests pass: `CGO_ENABLED=1 go test ./...`
3. Store 10+ entries via `knowledge_store` MCP tool
4. Set `PIPELINE_MIN_ENTRIES=10` and `PIPELINE_INTERVAL=1m`; after 1 minute the pipeline runs
5. Call `cluster_list` — returns clusters with LLM-generated titles and summaries
6. Call `analysis_status` — shows latest run (status=complete) and snapshot (version=1, cluster_count>0)
