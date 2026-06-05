# Phase 1: Core MCP + Storage Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build a working Go binary with an MCP stdio server, knowledge CRUD tools, SQLite + sqlite-vec vector storage, and an Ollama-backed embedding service — so Claude Desktop can store and semantically retrieve team knowledge entries.

**Architecture:** Single Go binary with layered internal packages: `config` → `storage` → `embedding` → `mcp`. MCP stdio transport is the sole client interface in Phase 1. SQLite + sqlite-vec provides zero-dependency vector search. CGO is required (mattn/go-sqlite3 + sqlite-vec extension).

**Tech Stack:** Go 1.24, `github.com/mark3labs/mcp-go` v0.54.1, `github.com/mattn/go-sqlite3` v1.14.x (CGO), `github.com/asg017/sqlite-vec-go-bindings/cgo` v0.1.6, `github.com/google/uuid`, Ollama HTTP API (default model: `nomic-embed-text`, 768 dims)

---

## File Structure

```
go.mod
go.sum
cmd/server/main.go                     Entry point: load config, wire deps, serve stdio
internal/config/config.go              Env var loading with defaults
internal/config/config_test.go         Config loading tests
internal/storage/storage.go            Store interface + KnowledgeEntry/SearchResult/ListFilter types
internal/storage/sqlite.go             SQLiteStore implementation (CGO, sqlite-vec)
internal/storage/storage_test.go       Storage CRUD + search tests (temp DB per test)
internal/embedding/embedder.go         Embedder interface
internal/embedding/ollama.go           OllamaEmbedder: HTTP POST to Ollama /api/embeddings
internal/embedding/ollama_test.go      Tests using httptest.NewServer mock
internal/mcp/server.go                 Builds and returns configured *server.MCPServer
internal/mcp/tools.go                  Tool handler functions (one per MCP tool)
internal/mcp/tools_test.go             Tool handler tests with in-memory mock Store + Embedder
docs/mcp-config.md                     Claude Desktop config snippet + env var reference
```

---

### Task 1: Go Module + Config

**Files:**
- Create: `go.mod`
- Create: `internal/config/config.go`
- Create: `internal/config/config_test.go`

- [ ] **Step 1: Initialize the Go module**

Run from the project root `/Users/dsandor/Projects/memory`:

```bash
go mod init github.com/dsandor/memory
```

Expected: `go.mod` created containing `module github.com/dsandor/memory` and a `go 1.24` line. No errors.

- [ ] **Step 2: Write the failing config test**

Create `internal/config/config_test.go`:

```go
package config_test

import (
	"os"
	"testing"

	"github.com/dsandor/memory/internal/config"
)

func TestLoad_Defaults(t *testing.T) {
	os.Unsetenv("DATABASE_PATH")
	os.Unsetenv("OLLAMA_URL")
	os.Unsetenv("OLLAMA_MODEL")
	os.Unsetenv("TEAM_ID")
	os.Unsetenv("EMBEDDING_DIM")

	cfg := config.Load()

	if cfg.DBPath != "knowledge.db" {
		t.Errorf("DBPath: got %q, want %q", cfg.DBPath, "knowledge.db")
	}
	if cfg.OllamaURL != "http://localhost:11434" {
		t.Errorf("OllamaURL: got %q, want %q", cfg.OllamaURL, "http://localhost:11434")
	}
	if cfg.OllamaModel != "nomic-embed-text" {
		t.Errorf("OllamaModel: got %q, want %q", cfg.OllamaModel, "nomic-embed-text")
	}
	if cfg.TeamID != "default" {
		t.Errorf("TeamID: got %q, want %q", cfg.TeamID, "default")
	}
	if cfg.EmbeddingDim != 768 {
		t.Errorf("EmbeddingDim: got %d, want 768", cfg.EmbeddingDim)
	}
}

func TestLoad_EnvOverrides(t *testing.T) {
	t.Setenv("DATABASE_PATH", "/tmp/test.db")
	t.Setenv("OLLAMA_URL", "http://myollama:11434")
	t.Setenv("OLLAMA_MODEL", "mxbai-embed-large")
	t.Setenv("TEAM_ID", "acme")
	t.Setenv("EMBEDDING_DIM", "1024")

	cfg := config.Load()

	if cfg.DBPath != "/tmp/test.db" {
		t.Errorf("DBPath: got %q, want %q", cfg.DBPath, "/tmp/test.db")
	}
	if cfg.OllamaURL != "http://myollama:11434" {
		t.Errorf("OllamaURL: got %q, want %q", cfg.OllamaURL, "http://myollama:11434")
	}
	if cfg.OllamaModel != "mxbai-embed-large" {
		t.Errorf("OllamaModel: got %q, want %q", cfg.OllamaModel, "mxbai-embed-large")
	}
	if cfg.TeamID != "acme" {
		t.Errorf("TeamID: got %q, want %q", cfg.TeamID, "acme")
	}
	if cfg.EmbeddingDim != 1024 {
		t.Errorf("EmbeddingDim: got %d, want 1024", cfg.EmbeddingDim)
	}
}
```

- [ ] **Step 3: Run to confirm failure**

```bash
go test ./internal/config/... -v
```

Expected: compile error — `package config` is not found yet.

- [ ] **Step 4: Implement config.go**

Create `internal/config/config.go`:

```go
package config

import (
	"os"
	"strconv"
)

type Config struct {
	DBPath       string
	OllamaURL    string
	OllamaModel  string
	TeamID       string
	EmbeddingDim int
}

func Load() Config {
	dim := 768
	if v := os.Getenv("EMBEDDING_DIM"); v != "" {
		if parsed, err := strconv.Atoi(v); err == nil {
			dim = parsed
		}
	}
	return Config{
		DBPath:       envOrDefault("DATABASE_PATH", "knowledge.db"),
		OllamaURL:    envOrDefault("OLLAMA_URL", "http://localhost:11434"),
		OllamaModel:  envOrDefault("OLLAMA_MODEL", "nomic-embed-text"),
		TeamID:       envOrDefault("TEAM_ID", "default"),
		EmbeddingDim: dim,
	}
}

func envOrDefault(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
```

- [ ] **Step 5: Run tests**

```bash
go test ./internal/config/... -v
```

Expected: PASS — both `TestLoad_Defaults` and `TestLoad_EnvOverrides` pass.

- [ ] **Step 6: Commit**

```bash
git add go.mod internal/config/
git commit -m "feat: go module scaffold and env-based config loading"
```

---

### Task 2: Storage Types + Interface

**Files:**
- Create: `internal/storage/storage.go`

Pure type definitions. No tests needed — this is the contract other tasks depend on.

- [ ] **Step 1: Create storage.go**

Create `internal/storage/storage.go`:

```go
package storage

import (
	"context"
	"time"
)

type KnowledgeType string

const (
	KTPrompt      KnowledgeType = "prompt"
	KTPattern     KnowledgeType = "pattern"
	KTWorkflow    KnowledgeType = "workflow"
	KTDomainFact  KnowledgeType = "domain_fact"
	KTAntiPattern KnowledgeType = "anti_pattern"
)

type KnowledgeEntry struct {
	ID          string
	Type        KnowledgeType
	Title       string
	Content     string
	Description string
	Domain      string
	Tags        []string
	Author      string
	Team        string
	CreatedAt   time.Time
	UpdatedAt   time.Time
	Version     int
}

type SearchResult struct {
	Entry    KnowledgeEntry
	Score    float64
}

type ListFilter struct {
	Domain string
	Type   KnowledgeType
	Limit  int
}

type Store interface {
	StoreEntry(ctx context.Context, entry KnowledgeEntry, embedding []float32) error
	GetEntry(ctx context.Context, id string) (*KnowledgeEntry, error)
	ListEntries(ctx context.Context, filter ListFilter) ([]KnowledgeEntry, error)
	DeleteEntry(ctx context.Context, id string) error
	SearchSimilar(ctx context.Context, embedding []float32, topK int) ([]SearchResult, error)
	Close() error
}
```

- [ ] **Step 2: Verify it compiles**

```bash
go build ./internal/storage/...
```

Expected: success (no output).

- [ ] **Step 3: Commit**

```bash
git add internal/storage/storage.go
git commit -m "feat: storage types and Store interface"
```

---

### Task 3: SQLite Store — Schema, CRUD, Vector Search

**Files:**
- Create: `internal/storage/sqlite.go`
- Create: `internal/storage/storage_test.go`

**CGO requirement:** CGO must be enabled (it is by default on macOS). Verify: `go env CGO_ENABLED` must print `1`. Xcode command-line tools must be installed (`xcode-select --install` if not).

- [ ] **Step 1: Fetch dependencies**

```bash
go get github.com/mattn/go-sqlite3@v1.14.45
go get github.com/asg017/sqlite-vec-go-bindings/cgo@v0.1.6
go get github.com/google/uuid@latest
```

Expected: all three appear in `go.mod`. No errors.

- [ ] **Step 2: Write the failing storage tests**

Create `internal/storage/storage_test.go`:

```go
package storage_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/dsandor/memory/internal/storage"
)

// newTestStore opens a temp SQLite DB with embeddingDim=4 for fast test vectors.
func newTestStore(t *testing.T) storage.Store {
	t.Helper()
	f, err := os.CreateTemp("", "knowledge-test-*.db")
	if err != nil {
		t.Fatalf("create temp db: %v", err)
	}
	f.Close()
	path := f.Name()
	t.Cleanup(func() { os.Remove(path) })

	store, err := storage.NewSQLiteStore(path, 4)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { store.Close() })
	return store
}

func sampleEntry() storage.KnowledgeEntry {
	return storage.KnowledgeEntry{
		Type:        storage.KTPrompt,
		Title:       "Earnings Summary Prompt",
		Content:     "Summarize the earnings call transcript focusing on guidance and surprises.",
		Description: "Use for Q earnings summaries",
		Domain:      "finance",
		Tags:        []string{"earnings", "summary"},
		Author:      "alice",
		Team:        "analysts",
	}
}

func TestStoreAndGet(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)

	entry := sampleEntry()
	emb := []float32{0.1, 0.2, 0.3, 0.4}

	if err := store.StoreEntry(ctx, entry, emb); err != nil {
		t.Fatalf("StoreEntry: %v", err)
	}

	entries, err := store.ListEntries(ctx, storage.ListFilter{Limit: 10})
	if err != nil {
		t.Fatalf("ListEntries: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("want 1 entry, got %d", len(entries))
	}

	got, err := store.GetEntry(ctx, entries[0].ID)
	if err != nil {
		t.Fatalf("GetEntry: %v", err)
	}
	if got.Title != entry.Title {
		t.Errorf("Title: got %q, want %q", got.Title, entry.Title)
	}
	if got.Domain != "finance" {
		t.Errorf("Domain: got %q, want %q", got.Domain, "finance")
	}
	if len(got.Tags) != 2 {
		t.Errorf("Tags: got %v, want 2 tags", got.Tags)
	}
	if got.Version != 1 {
		t.Errorf("Version: got %d, want 1", got.Version)
	}
	if got.CreatedAt.IsZero() {
		t.Error("CreatedAt should not be zero")
	}
}

func TestGetEntry_NotFound(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)

	_, err := store.GetEntry(ctx, "nonexistent-id")
	if err == nil {
		t.Fatal("expected error for missing entry, got nil")
	}
}

func TestDeleteEntry(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)

	emb := []float32{0.1, 0.2, 0.3, 0.4}
	if err := store.StoreEntry(ctx, sampleEntry(), emb); err != nil {
		t.Fatalf("StoreEntry: %v", err)
	}

	entries, _ := store.ListEntries(ctx, storage.ListFilter{Limit: 10})
	id := entries[0].ID

	if err := store.DeleteEntry(ctx, id); err != nil {
		t.Fatalf("DeleteEntry: %v", err)
	}

	remaining, _ := store.ListEntries(ctx, storage.ListFilter{Limit: 10})
	if len(remaining) != 0 {
		t.Errorf("expected 0 entries after delete, got %d", len(remaining))
	}
}

func TestListEntries_DomainFilter(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)

	emb := []float32{0.1, 0.2, 0.3, 0.4}

	financeEntry := sampleEntry()
	financeEntry.Domain = "finance"
	techEntry := sampleEntry()
	techEntry.Domain = "tech"

	store.StoreEntry(ctx, financeEntry, emb)
	store.StoreEntry(ctx, techEntry, emb)

	all, _ := store.ListEntries(ctx, storage.ListFilter{Limit: 10})
	if len(all) != 2 {
		t.Fatalf("want 2 total, got %d", len(all))
	}

	filtered, _ := store.ListEntries(ctx, storage.ListFilter{Domain: "finance", Limit: 10})
	if len(filtered) != 1 {
		t.Fatalf("want 1 finance entry, got %d", len(filtered))
	}
	if filtered[0].Domain != "finance" {
		t.Errorf("Domain: got %q, want finance", filtered[0].Domain)
	}
}

func TestSearchSimilar(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)

	// Three orthogonal-ish 4-dim vectors
	north := []float32{1.0, 0.0, 0.0, 0.0}
	east := []float32{0.0, 1.0, 0.0, 0.0}
	nearNorth := []float32{0.9, 0.1, 0.0, 0.0}

	e1 := sampleEntry(); e1.Title = "North"
	e2 := sampleEntry(); e2.Title = "East"
	e3 := sampleEntry(); e3.Title = "NearNorth"

	store.StoreEntry(ctx, e1, north)
	store.StoreEntry(ctx, e2, east)
	store.StoreEntry(ctx, e3, nearNorth)

	query := []float32{0.95, 0.05, 0.0, 0.0}
	results, err := store.SearchSimilar(ctx, query, 2)
	if err != nil {
		t.Fatalf("SearchSimilar: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("want 2 results, got %d", len(results))
	}
	// East is orthogonal to query — must not appear in top-2
	for _, r := range results {
		if r.Entry.Title == "East" {
			t.Error("East should not appear in top-2 results for a northward query")
		}
	}
}

func TestEntryTimestamps(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)

	before := time.Now().UTC().Truncate(time.Second)
	store.StoreEntry(ctx, sampleEntry(), []float32{0.1, 0.2, 0.3, 0.4})
	after := time.Now().UTC().Add(time.Second)

	entries, _ := store.ListEntries(ctx, storage.ListFilter{Limit: 10})
	ts := entries[0].CreatedAt.UTC()
	if ts.Before(before) || ts.After(after) {
		t.Errorf("CreatedAt %v outside expected range [%v, %v]", ts, before, after)
	}
}
```

- [ ] **Step 3: Run to confirm failure**

```bash
CGO_ENABLED=1 go test ./internal/storage/... -v 2>&1 | head -10
```

Expected: compile error — `storage.NewSQLiteStore` is undefined.

- [ ] **Step 4: Implement sqlite.go**

Create `internal/storage/sqlite.go`:

```go
package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
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

func (s *SQLiteStore) StoreEntry(ctx context.Context, entry KnowledgeEntry, embedding []float32) error {
	if len(embedding) != s.embeddingDim {
		return fmt.Errorf("embedding dim mismatch: got %d, want %d", len(embedding), s.embeddingDim)
	}

	entry.ID = uuid.NewString()

	tagsJSON, err := json.Marshal(entry.Tags)
	if err != nil {
		return fmt.Errorf("marshal tags: %w", err)
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	res, err := tx.ExecContext(ctx, `
		INSERT INTO entries (id, type, title, content, description, domain, tags, author, team)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, entry.ID, string(entry.Type), entry.Title, entry.Content,
		entry.Description, entry.Domain, string(tagsJSON), entry.Author, entry.Team)
	if err != nil {
		return fmt.Errorf("insert entry: %w", err)
	}

	rowID, err := res.LastInsertId()
	if err != nil {
		return fmt.Errorf("last insert id: %w", err)
	}

	blob, err := vec.SerializeFloat32(embedding)
	if err != nil {
		return fmt.Errorf("serialize embedding: %w", err)
	}

	_, err = tx.ExecContext(ctx, `INSERT INTO vec_entries (rowid, embedding) VALUES (?, ?)`, rowID, blob)
	if err != nil {
		return fmt.Errorf("insert vec_entries: %w", err)
	}

	return tx.Commit()
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
			return nil, fmt.Errorf("entry %q not found", id)
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
			return nil, err
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
			return fmt.Errorf("entry %q not found", id)
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

	results := make([]SearchResult, 0, len(rowResults))
	for _, r := range rowResults {
		row := s.db.QueryRowContext(ctx, `
			SELECT id, type, title, content, description, domain, tags, author, team,
			       created_at, updated_at, version
			FROM entries WHERE rowid = ?
		`, r.rowID)
		entry, err := scanEntry(row)
		if err != nil {
			continue
		}
		results = append(results, SearchResult{Entry: *entry, Score: r.distance})
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

	e.CreatedAt, _ = time.Parse("2006-01-02 15:04:05", createdAt)
	e.UpdatedAt, _ = time.Parse("2006-01-02 15:04:05", updatedAt)
	return &e, nil
}
```

- [ ] **Step 5: Run storage tests**

```bash
CGO_ENABLED=1 go test ./internal/storage/... -v
```

Expected: all 6 tests PASS: `TestStoreAndGet`, `TestGetEntry_NotFound`, `TestDeleteEntry`, `TestListEntries_DomainFilter`, `TestSearchSimilar`, `TestEntryTimestamps`.

If `TestSearchSimilar` fails with a sqlite-vec query error, check that sqlite-vec was loaded by running `CGO_ENABLED=1 go test ./internal/storage/... -v -run TestStoreAndGet` first to isolate the issue.

- [ ] **Step 6: Commit**

```bash
git add internal/storage/ go.mod go.sum
git commit -m "feat: SQLite + sqlite-vec storage with CRUD and KNN semantic search"
```

---

### Task 4: Ollama Embedding Service

**Files:**
- Create: `internal/embedding/embedder.go`
- Create: `internal/embedding/ollama.go`
- Create: `internal/embedding/ollama_test.go`

- [ ] **Step 1: Write the interface and failing test**

Create `internal/embedding/embedder.go`:

```go
package embedding

import "context"

type Embedder interface {
	Embed(ctx context.Context, text string) ([]float32, error)
}
```

Create `internal/embedding/ollama_test.go`:

```go
package embedding_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/dsandor/memory/internal/embedding"
)

func TestOllamaEmbedder_Embed(t *testing.T) {
	wantEmbedding := []float32{0.1, 0.2, 0.3}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/embeddings" {
			t.Errorf("unexpected path: %s", r.URL.Path)
			http.Error(w, "not found", 404)
			return
		}
		var body struct {
			Model  string `json:"model"`
			Prompt string `json:"prompt"`
		}
		json.NewDecoder(r.Body).Decode(&body)

		if body.Model != "nomic-embed-text" {
			t.Errorf("model: got %q, want nomic-embed-text", body.Model)
		}
		if body.Prompt != "hello world" {
			t.Errorf("prompt: got %q, want hello world", body.Prompt)
		}
		json.NewEncoder(w).Encode(map[string]any{"embedding": wantEmbedding})
	}))
	defer srv.Close()

	e := embedding.NewOllamaEmbedder(srv.URL, "nomic-embed-text")
	got, err := e.Embed(context.Background(), "hello world")
	if err != nil {
		t.Fatalf("Embed: %v", err)
	}
	if len(got) != len(wantEmbedding) {
		t.Fatalf("len: got %d, want %d", len(got), len(wantEmbedding))
	}
	for i := range wantEmbedding {
		if got[i] != wantEmbedding[i] {
			t.Errorf("[%d]: got %f, want %f", i, got[i], wantEmbedding[i])
		}
	}
}

func TestOllamaEmbedder_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "internal error", 500)
	}))
	defer srv.Close()

	e := embedding.NewOllamaEmbedder(srv.URL, "nomic-embed-text")
	_, err := e.Embed(context.Background(), "hello")
	if err == nil {
		t.Fatal("expected error on 500 response, got nil")
	}
}

func TestOllamaEmbedder_EmptyEmbedding(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"embedding": []float32{}})
	}))
	defer srv.Close()

	e := embedding.NewOllamaEmbedder(srv.URL, "nomic-embed-text")
	_, err := e.Embed(context.Background(), "hello")
	if err == nil {
		t.Fatal("expected error on empty embedding, got nil")
	}
}
```

- [ ] **Step 2: Run to confirm failure**

```bash
go test ./internal/embedding/... -v 2>&1 | head -10
```

Expected: compile error — `embedding.NewOllamaEmbedder` undefined.

- [ ] **Step 3: Implement ollama.go**

Create `internal/embedding/ollama.go`:

```go
package embedding

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
)

type OllamaEmbedder struct {
	url    string
	model  string
	client *http.Client
}

func NewOllamaEmbedder(url, model string) *OllamaEmbedder {
	return &OllamaEmbedder{
		url:    url,
		model:  model,
		client: &http.Client{},
	}
}

func (e *OllamaEmbedder) Embed(ctx context.Context, text string) ([]float32, error) {
	body, _ := json.Marshal(map[string]string{
		"model":  e.model,
		"prompt": text,
	})

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, e.url+"/api/embeddings", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := e.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("http post: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("ollama status %d", resp.StatusCode)
	}

	var result struct {
		Embedding []float32 `json:"embedding"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}
	if len(result.Embedding) == 0 {
		return nil, fmt.Errorf("ollama returned empty embedding")
	}
	return result.Embedding, nil
}
```

- [ ] **Step 4: Run embedding tests**

```bash
go test ./internal/embedding/... -v
```

Expected: all 3 tests PASS: `TestOllamaEmbedder_Embed`, `TestOllamaEmbedder_ServerError`, `TestOllamaEmbedder_EmptyEmbedding`.

- [ ] **Step 5: Commit**

```bash
git add internal/embedding/
git commit -m "feat: Ollama embedding service with httptest coverage"
```

---

### Task 5: MCP Server + Tool Handlers

**Files:**
- Create: `internal/mcp/tools.go`
- Create: `internal/mcp/server.go`
- Create: `internal/mcp/tools_test.go`

- [ ] **Step 1: Fetch mcp-go dependency**

```bash
go get github.com/mark3labs/mcp-go@v0.54.1
```

Expected: `go.mod` updated with `github.com/mark3labs/mcp-go v0.54.1`. No errors.

- [ ] **Step 2: Write failing tool tests**

Create `internal/mcp/tools_test.go`:

```go
package mcp_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	internalmcp "github.com/dsandor/memory/internal/mcp"
	"github.com/dsandor/memory/internal/storage"
	mcplib "github.com/mark3labs/mcp-go/mcp"
)

// --- mock Store ---

type mockStore struct {
	entries   []storage.KnowledgeEntry
	storeErr  error
	listErr   error
	deleteErr error
}

func (m *mockStore) StoreEntry(_ context.Context, e storage.KnowledgeEntry, _ []float32) error {
	if m.storeErr != nil {
		return m.storeErr
	}
	e.ID = "mock-" + e.Title
	e.CreatedAt = time.Now()
	e.UpdatedAt = time.Now()
	e.Version = 1
	m.entries = append(m.entries, e)
	return nil
}

func (m *mockStore) GetEntry(_ context.Context, id string) (*storage.KnowledgeEntry, error) {
	for _, e := range m.entries {
		if e.ID == id {
			return &e, nil
		}
	}
	return nil, errors.New("not found")
}

func (m *mockStore) ListEntries(_ context.Context, _ storage.ListFilter) ([]storage.KnowledgeEntry, error) {
	if m.listErr != nil {
		return nil, m.listErr
	}
	return m.entries, nil
}

func (m *mockStore) DeleteEntry(_ context.Context, id string) error {
	if m.deleteErr != nil {
		return m.deleteErr
	}
	for i, e := range m.entries {
		if e.ID == id {
			m.entries = append(m.entries[:i], m.entries[i+1:]...)
			return nil
		}
	}
	return errors.New("not found")
}

func (m *mockStore) SearchSimilar(_ context.Context, _ []float32, topK int) ([]storage.SearchResult, error) {
	results := make([]storage.SearchResult, 0, len(m.entries))
	for _, e := range m.entries {
		results = append(results, storage.SearchResult{Entry: e, Score: 0.9})
	}
	if len(results) > topK {
		results = results[:topK]
	}
	return results, nil
}

func (m *mockStore) Close() error { return nil }

// --- mock Embedder ---

type mockEmbedder struct {
	embedding []float32
	err       error
}

func (m *mockEmbedder) Embed(_ context.Context, _ string) ([]float32, error) {
	return m.embedding, m.err
}

// --- helpers ---

func callReq(kv ...any) mcplib.CallToolRequest {
	req := mcplib.CallToolRequest{}
	args := make(map[string]any)
	for i := 0; i+1 < len(kv); i += 2 {
		args[kv[i].(string)] = kv[i+1]
	}
	req.Params.Arguments = args
	return req
}

func textContent(result *mcplib.CallToolResult) string {
	if len(result.Content) == 0 {
		return ""
	}
	tc, ok := result.Content[0].(mcplib.TextContent)
	if !ok {
		return ""
	}
	return tc.Text
}

// --- tests ---

func TestHandleKnowledgeStore_Success(t *testing.T) {
	store := &mockStore{}
	embedder := &mockEmbedder{embedding: []float32{0.1, 0.2}}

	handler := internalmcp.HandleKnowledgeStore(store, embedder)
	req := callReq(
		"title", "Test Prompt",
		"content", "Use bullet points.",
		"type", "prompt",
		"domain", "general",
		"description", "Good for summaries",
		"tags", []any{"clarity", "bullets"},
	)

	result, err := handler(context.Background(), req)
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if result.IsError {
		t.Fatalf("tool returned error: %s", textContent(result))
	}
	if len(store.entries) != 1 {
		t.Fatalf("expected 1 stored entry, got %d", len(store.entries))
	}
	if store.entries[0].Title != "Test Prompt" {
		t.Errorf("title: got %q, want %q", store.entries[0].Title, "Test Prompt")
	}
	if store.entries[0].Domain != "general" {
		t.Errorf("domain: got %q, want general", store.entries[0].Domain)
	}
	if len(store.entries[0].Tags) != 2 {
		t.Errorf("tags: got %v, want 2", store.entries[0].Tags)
	}
}

func TestHandleKnowledgeStore_MissingRequired(t *testing.T) {
	store := &mockStore{}
	embedder := &mockEmbedder{embedding: []float32{0.1}}

	handler := internalmcp.HandleKnowledgeStore(store, embedder)
	req := callReq("title", "No Content") // missing content and type

	result, err := handler(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected IsError=true for missing required fields")
	}
}

func TestHandleKnowledgeGet_Success(t *testing.T) {
	store := &mockStore{}
	embedder := &mockEmbedder{embedding: []float32{0.1}}

	storeHandler := internalmcp.HandleKnowledgeStore(store, embedder)
	storeHandler(context.Background(), callReq("title", "Alpha", "content", "c", "type", "prompt"))

	id := store.entries[0].ID

	getHandler := internalmcp.HandleKnowledgeGet(store)
	result, err := getHandler(context.Background(), callReq("id", id))
	if err != nil {
		t.Fatalf("get handler error: %v", err)
	}
	if result.IsError {
		t.Fatalf("tool returned error: %s", textContent(result))
	}

	var entry map[string]any
	if err := json.Unmarshal([]byte(textContent(result)), &entry); err != nil {
		t.Fatalf("parse result JSON: %v", err)
	}
	if entry["title"] != "Alpha" {
		t.Errorf("title: got %v, want Alpha", entry["title"])
	}
}

func TestHandleKnowledgeGet_MissingID(t *testing.T) {
	store := &mockStore{}
	handler := internalmcp.HandleKnowledgeGet(store)
	result, err := handler(context.Background(), callReq())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected IsError=true when id is missing")
	}
}

func TestHandleKnowledgeList(t *testing.T) {
	store := &mockStore{}
	embedder := &mockEmbedder{embedding: []float32{0.1}}
	storeHandler := internalmcp.HandleKnowledgeStore(store, embedder)

	for _, title := range []string{"A", "B", "C"} {
		storeHandler(context.Background(), callReq("title", title, "content", "c", "type", "prompt"))
	}

	listHandler := internalmcp.HandleKnowledgeList(store)
	result, err := listHandler(context.Background(), callReq("limit", float64(10)))
	if err != nil {
		t.Fatalf("list handler error: %v", err)
	}
	if result.IsError {
		t.Fatalf("tool returned error: %s", textContent(result))
	}

	var entries []map[string]any
	if err := json.Unmarshal([]byte(textContent(result)), &entries); err != nil {
		t.Fatalf("parse result JSON: %v", err)
	}
	if len(entries) != 3 {
		t.Errorf("expected 3 entries, got %d", len(entries))
	}
}

func TestHandleKnowledgeDelete_Success(t *testing.T) {
	store := &mockStore{}
	embedder := &mockEmbedder{embedding: []float32{0.1}}

	storeHandler := internalmcp.HandleKnowledgeStore(store, embedder)
	storeHandler(context.Background(), callReq("title", "ToDelete", "content", "c", "type", "prompt"))

	id := store.entries[0].ID

	deleteHandler := internalmcp.HandleKnowledgeDelete(store)
	result, err := deleteHandler(context.Background(), callReq("id", id))
	if err != nil {
		t.Fatalf("delete handler error: %v", err)
	}
	if result.IsError {
		t.Fatalf("tool returned error: %s", textContent(result))
	}
	if len(store.entries) != 0 {
		t.Errorf("expected 0 entries after delete, got %d", len(store.entries))
	}
}
```

- [ ] **Step 3: Run to confirm failure**

```bash
go test ./internal/mcp/... -v 2>&1 | head -10
```

Expected: compile error — `internalmcp.HandleKnowledgeStore` undefined.

- [ ] **Step 4: Implement tools.go**

Create `internal/mcp/tools.go`:

```go
package mcp

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/dsandor/memory/internal/embedding"
	"github.com/dsandor/memory/internal/storage"
	mcplib "github.com/mark3labs/mcp-go/mcp"
)

func HandleKnowledgeStore(store storage.Store, embedder embedding.Embedder) func(context.Context, mcplib.CallToolRequest) (*mcplib.CallToolResult, error) {
	return func(ctx context.Context, req mcplib.CallToolRequest) (*mcplib.CallToolResult, error) {
		title := req.GetString("title", "")
		content := req.GetString("content", "")
		entryType := req.GetString("type", "")

		if title == "" || content == "" || entryType == "" {
			return mcplib.NewToolResultError("title, content, and type are required"), nil
		}

		entry := storage.KnowledgeEntry{
			Type:        storage.KnowledgeType(entryType),
			Title:       title,
			Content:     content,
			Description: req.GetString("description", ""),
			Domain:      req.GetString("domain", ""),
			Author:      req.GetString("author", ""),
			Team:        req.GetString("team", ""),
			Tags:        tagsFromArgs(req.GetArguments(), "tags"),
		}

		emb, err := embedder.Embed(ctx, content)
		if err != nil {
			return mcplib.NewToolResultError(fmt.Sprintf("embedding failed: %v", err)), nil
		}

		if err := store.StoreEntry(ctx, entry, emb); err != nil {
			return mcplib.NewToolResultError(fmt.Sprintf("store failed: %v", err)), nil
		}

		return mcplib.NewToolResultText("knowledge entry stored successfully"), nil
	}
}

func HandleKnowledgeGet(store storage.Store) func(context.Context, mcplib.CallToolRequest) (*mcplib.CallToolResult, error) {
	return func(ctx context.Context, req mcplib.CallToolRequest) (*mcplib.CallToolResult, error) {
		id := req.GetString("id", "")
		if id == "" {
			return mcplib.NewToolResultError("id is required"), nil
		}

		entry, err := store.GetEntry(ctx, id)
		if err != nil {
			return mcplib.NewToolResultError(fmt.Sprintf("entry not found: %v", err)), nil
		}

		data, _ := json.Marshal(entry)
		return mcplib.NewToolResultText(string(data)), nil
	}
}

func HandleKnowledgeList(store storage.Store) func(context.Context, mcplib.CallToolRequest) (*mcplib.CallToolResult, error) {
	return func(ctx context.Context, req mcplib.CallToolRequest) (*mcplib.CallToolResult, error) {
		filter := storage.ListFilter{
			Domain: req.GetString("domain", ""),
			Type:   storage.KnowledgeType(req.GetString("type", "")),
			Limit:  req.GetInt("limit", 20),
		}

		entries, err := store.ListEntries(ctx, filter)
		if err != nil {
			return mcplib.NewToolResultError(fmt.Sprintf("list failed: %v", err)), nil
		}

		data, _ := json.Marshal(entries)
		return mcplib.NewToolResultText(string(data)), nil
	}
}

func HandleKnowledgeDelete(store storage.Store) func(context.Context, mcplib.CallToolRequest) (*mcplib.CallToolResult, error) {
	return func(ctx context.Context, req mcplib.CallToolRequest) (*mcplib.CallToolResult, error) {
		id := req.GetString("id", "")
		if id == "" {
			return mcplib.NewToolResultError("id is required"), nil
		}

		if err := store.DeleteEntry(ctx, id); err != nil {
			return mcplib.NewToolResultError(fmt.Sprintf("delete failed: %v", err)), nil
		}

		return mcplib.NewToolResultText("entry deleted"), nil
	}
}

func tagsFromArgs(args map[string]any, key string) []string {
	raw, ok := args[key].([]any)
	if !ok {
		return []string{}
	}
	tags := make([]string, 0, len(raw))
	for _, v := range raw {
		if s, ok := v.(string); ok {
			tags = append(tags, s)
		}
	}
	return tags
}
```

- [ ] **Step 5: Implement server.go**

Create `internal/mcp/server.go`:

```go
package mcp

import (
	"github.com/dsandor/memory/internal/embedding"
	"github.com/dsandor/memory/internal/storage"
	mcplib "github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

func NewMCPServer(store storage.Store, embedder embedding.Embedder) *server.MCPServer {
	s := server.NewMCPServer(
		"tribal-knowledge",
		"0.1.0",
		server.WithToolCapabilities(false),
	)

	s.AddTool(
		mcplib.NewTool("knowledge_store",
			mcplib.WithDescription("Store a team knowledge entry (prompt, pattern, workflow, domain fact, or anti-pattern)"),
			mcplib.WithString("title", mcplib.Required(), mcplib.Description("Short descriptive title")),
			mcplib.WithString("content", mcplib.Required(), mcplib.Description("Full content of the knowledge entry")),
			mcplib.WithString("type", mcplib.Required(), mcplib.Description("One of: prompt, pattern, workflow, domain_fact, anti_pattern")),
			mcplib.WithString("domain", mcplib.Description("Domain tag (e.g. finance, legal, engineering)")),
			mcplib.WithString("description", mcplib.Description("When and why to use this entry")),
			mcplib.WithString("author", mcplib.Description("Author identifier")),
			mcplib.WithString("team", mcplib.Description("Team identifier")),
			mcplib.WithArray("tags", mcplib.Description("Additional tags")),
		),
		HandleKnowledgeStore(store, embedder),
	)

	s.AddTool(
		mcplib.NewTool("knowledge_get",
			mcplib.WithDescription("Retrieve a knowledge entry by its UUID"),
			mcplib.WithString("id", mcplib.Required(), mcplib.Description("Entry UUID returned by knowledge_store or knowledge_list")),
		),
		HandleKnowledgeGet(store),
	)

	s.AddTool(
		mcplib.NewTool("knowledge_list",
			mcplib.WithDescription("List knowledge entries with optional domain and type filters"),
			mcplib.WithString("domain", mcplib.Description("Filter by domain")),
			mcplib.WithString("type", mcplib.Description("Filter by type: prompt, pattern, workflow, domain_fact, anti_pattern")),
			mcplib.WithNumber("limit", mcplib.Description("Max entries to return (default 20, max 100)")),
		),
		HandleKnowledgeList(store),
	)

	s.AddTool(
		mcplib.NewTool("knowledge_delete",
			mcplib.WithDescription("Permanently delete a knowledge entry by its UUID"),
			mcplib.WithString("id", mcplib.Required(), mcplib.Description("Entry UUID to delete")),
		),
		HandleKnowledgeDelete(store),
	)

	return s
}
```

- [ ] **Step 6: Run MCP tool tests**

```bash
go test ./internal/mcp/... -v
```

Expected: all 7 tests PASS: `TestHandleKnowledgeStore_Success`, `TestHandleKnowledgeStore_MissingRequired`, `TestHandleKnowledgeGet_Success`, `TestHandleKnowledgeGet_MissingID`, `TestHandleKnowledgeList`, `TestHandleKnowledgeDelete_Success`.

- [ ] **Step 7: Commit**

```bash
git add internal/mcp/ go.mod go.sum
git commit -m "feat: MCP server with knowledge_store/get/list/delete tool handlers"
```

---

### Task 6: Main Entry Point + Build Verification

**Files:**
- Create: `cmd/server/main.go`

- [ ] **Step 1: Write main.go**

Create `cmd/server/main.go`:

```go
package main

import (
	"log"

	"github.com/dsandor/memory/internal/config"
	"github.com/dsandor/memory/internal/embedding"
	internalmcp "github.com/dsandor/memory/internal/mcp"
	"github.com/dsandor/memory/internal/storage"
	"github.com/mark3labs/mcp-go/server"
)

func main() {
	cfg := config.Load()

	store, err := storage.NewSQLiteStore(cfg.DBPath, cfg.EmbeddingDim)
	if err != nil {
		log.Fatalf("open storage: %v", err)
	}
	defer store.Close()

	embedder := embedding.NewOllamaEmbedder(cfg.OllamaURL, cfg.OllamaModel)

	mcpServer := internalmcp.NewMCPServer(store, embedder)

	if err := server.ServeStdio(mcpServer); err != nil {
		log.Fatalf("serve: %v", err)
	}
}
```

- [ ] **Step 2: Build the binary**

```bash
CGO_ENABLED=1 go build -o tribal-knowledge ./cmd/server/
```

Expected: binary `tribal-knowledge` created. No errors or warnings. Binary size will be 10–30 MB.

- [ ] **Step 3: Verify binary**

```bash
/bin/ls -lh tribal-knowledge
file tribal-knowledge
```

Expected: `tribal-knowledge: Mach-O 64-bit executable arm64` (on Apple Silicon) and size 10–30 MB.

- [ ] **Step 4: Run all tests**

```bash
CGO_ENABLED=1 go test ./... -v 2>&1 | tail -30
```

Expected: all tests pass across `config`, `storage`, `embedding`, and `mcp` packages. No failures.

- [ ] **Step 5: Clean up binary**

```bash
rm tribal-knowledge
```

- [ ] **Step 6: Commit**

```bash
git add cmd/
git commit -m "feat: main entry point wiring config, storage, embedding, and MCP server"
```

---

### Task 7: Claude Desktop Config Doc + Update State

**Files:**
- Create: `docs/mcp-config.md`
- Modify: `.planning/STATE.md`

- [ ] **Step 1: Create MCP connection guide**

Create `docs/mcp-config.md`:

```markdown
# Connecting Claude Desktop to the Tribal Knowledge MCP Server

## Build

CGO is required. On macOS, install Xcode command-line tools first:
```bash
xcode-select --install
```

Then build:
```bash
CGO_ENABLED=1 go build -o tribal-knowledge ./cmd/server/
```

## Claude Desktop Config

Add to `~/Library/Application Support/Claude/claude_desktop_config.json`:

```json
{
  "mcpServers": {
    "tribal-knowledge": {
      "command": "/absolute/path/to/tribal-knowledge",
      "env": {
        "DATABASE_PATH": "/Users/you/tribal-knowledge.db",
        "OLLAMA_URL": "http://localhost:11434",
        "OLLAMA_MODEL": "nomic-embed-text",
        "TEAM_ID": "my-team"
      }
    }
  }
}
```

Replace `/absolute/path/to/tribal-knowledge` with the actual build output path.

## Environment Variables

| Variable | Default | Description |
|----------|---------|-------------|
| `DATABASE_PATH` | `knowledge.db` | SQLite database file path (created on first run) |
| `OLLAMA_URL` | `http://localhost:11434` | Ollama server base URL |
| `OLLAMA_MODEL` | `nomic-embed-text` | Embedding model (must produce `EMBEDDING_DIM`-dimensional vectors) |
| `TEAM_ID` | `default` | Team identifier (metadata only in Phase 1) |
| `EMBEDDING_DIM` | `768` | Embedding vector dimensions — must match the model. If changed, delete and recreate the DB. |

## Prerequisites

- Ollama running with the embedding model pulled:
  ```bash
  ollama serve &
  ollama pull nomic-embed-text
  ```
- Go 1.24+ with CGO enabled

## Phase 1 Exit Criteria

Phase 1 is complete when you can:
1. Build the `tribal-knowledge` binary without errors
2. Connect Claude Desktop using the config above (server shows in Claude's tool menu)
3. Call `knowledge_store` to save a prompt entry and receive "stored successfully"
4. Call `knowledge_list` and see the stored entry
5. Call `knowledge_get` with the returned ID and receive the full entry JSON
6. Call `knowledge_delete` with the ID and confirm it no longer appears in `knowledge_list`
```

- [ ] **Step 2: Update STATE.md**

Edit `.planning/STATE.md` to mark Phase 1 complete and Phase 2 as next:

```markdown
# Project State

## Current

- **Phase:** 2
- **Status:** pending
- **Last updated:** 2026-06-05

## Phase Progress

| Phase | Name | Status | Plan |
|-------|------|--------|------|
| 1 | Core MCP + Storage | `complete` | [2026-06-05-phase1-core-mcp-storage.md](../docs/superpowers/plans/2026-06-05-phase1-core-mcp-storage.md) |
| 2 | Knowledge Analysis Pipeline | `pending` | not created |
| 3 | Agent Generation Engine | `pending` | not created |
| 4 | Embedded Web UI | `pending` | not created |
| 5 | REST API + Analytics + Team Model | `pending` | not created |
| 6 | Polish & Developer Experience | `pending` | not created |

## Notes

- Phase 1 complete: working MCP binary with knowledge CRUD and sqlite-vec semantic search
- Phase 2 plan to be created before starting implementation
```

- [ ] **Step 3: Commit**

```bash
git add docs/ .planning/STATE.md
git commit -m "docs: Phase 1 complete — MCP config guide and state updated"
```

---

## Self-Review

### Spec Coverage

Phase 1 ROADMAP deliverables vs. plan tasks:

| Deliverable | Task |
|-------------|------|
| Go module scaffold with `cmd/server/main.go` | Task 1 (module), Task 6 (main.go) |
| SQLite + sqlite-vec schema: `entries`, `embeddings` | Task 3 (`entries` table + `vec_entries` virtual table) |
| Embedding service abstraction (Ollama v1) | Task 4 (Embedder interface + OllamaEmbedder) |
| MCP server over stdio (mark3labs/mcp-go) | Task 5 (server.go + ServeStdio in main.go) |
| `knowledge_store`, `knowledge_get`, `knowledge_list`, `knowledge_delete` tools | Task 5 (tools.go) |
| Basic semantic search (vector similarity, top-K) | Task 3 (`SearchSimilar` using sqlite-vec KNN) |
| Config loading from env vars | Task 1 (config.go) |
| Go `testing` unit tests for storage + embedding layers | Tasks 3, 4, 5 |

**Gap:** ROADMAP mentions `users` and `teams` tables. The plan uses `author` and `team` string fields on `KnowledgeEntry` instead — full user/team schema is deferred to Phase 5 (REST API + Team Model). Phase 1 exit criteria only requires connect → store → retrieve. No gap blocking Phase 1.

### Placeholder Scan

No "TBD", "TODO", "similar to Task N", "add appropriate error handling", or missing code blocks found.

### Type Consistency

- `storage.KnowledgeType` (string alias with constants) used consistently across all tasks
- `storage.Store` interface methods match all call sites in `mcp/tools.go` and `mockStore` in tests
- `embedding.Embedder` interface `Embed(ctx, text)` matches `OllamaEmbedder` and `mockEmbedder`  
- `storage.NewSQLiteStore(path string, embeddingDim int)` matches Task 3 impl and Task 6 usage
- `storage.ListFilter{Domain, Type, Limit}` field names match all usages
- `req.GetString(key, default)` and `req.GetInt(key, default)` are real mcp-go v0.54.1 methods on `CallToolRequest`
- `vec.Auto()` and `vec.SerializeFloat32()` are real exports from `github.com/asg017/sqlite-vec-go-bindings/cgo` v0.1.6 (package name is `vec`)
- `mcplib.NewToolResultText()` and `mcplib.NewToolResultError()` are real exports from `github.com/mark3labs/mcp-go/mcp`
- `result.Content[0].(mcplib.TextContent).Text` is the correct type assertion for mcp-go v0.54.1
