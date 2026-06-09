# Embedded Web UI — Phase 4 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build a React SPA compiled by Vite and embedded in the Go binary via `//go:embed` that surfaces all knowledge entries, clusters, dataset snapshots, and agents with full download flows.

**Architecture:** A new `internal/web` package exports `NewServer(fs.FS, AllStore) *Server` — a thin `net/http` mux over existing storage interfaces. The React SPA in `web/` uses Vite with `outDir: '../internal/web/dist'` so built assets land where `//go:embed all:dist` picks them up. Two minor storage additions (`RateEntry`, `ListSnapshots`, `Offset`/`Search` on `ListFilter`) close the gaps the UI requires. A single change to `cmd/server/main.go` starts the HTTP server alongside the existing MCP stdio transport.

**Tech Stack:** Go stdlib `net/http`, `archive/zip`, `encoding/csv`; React 18, TypeScript 5, Vite 5, Tailwind CSS v4, shadcn/ui, lucide-react, React Router v6.

---

## File Map

### New files

| File | Purpose |
|------|---------|
| `internal/web/server.go` | `AllStore` interface, `Server` struct, `NewServer`, route wiring |
| `internal/web/handlers.go` | All REST API handler methods on `*Server` |
| `internal/web/server_test.go` | HTTP handler tests using `httptest` |
| `internal/web/embed.go` | `//go:embed all:dist` + exported `DistFS embed.FS` |
| `internal/web/dist/index.html` | Placeholder so `go build` succeeds before first Vite run |
| `web/package.json` | Vite + React + TypeScript project manifest |
| `web/vite.config.ts` | Vite config: outDir `../internal/web/dist`, dev proxy `/api→:8080` |
| `web/tsconfig.json` | TypeScript config with `@/` path alias |
| `web/tsconfig.node.json` | TypeScript config for Vite node scripts |
| `web/index.html` | Vite entry HTML |
| `web/components.json` | shadcn/ui config |
| `web/src/main.tsx` | React entry point |
| `web/src/App.tsx` | React Router setup |
| `web/src/index.css` | Tailwind directives + CSS variables |
| `web/src/lib/utils.ts` | `cn()` helper (shadcn requirement) |
| `web/src/lib/api.ts` | Typed fetch wrappers for all REST endpoints |
| `web/src/components/Layout.tsx` | Sidebar navigation shell |
| `web/src/pages/Dashboard.tsx` | Stats cards + pipeline status |
| `web/src/pages/KnowledgeBrowser.tsx` | Paginated list with search + type/domain filters |
| `web/src/pages/KnowledgeDetail.tsx` | Entry detail + inline rating |
| `web/src/pages/Clusters.tsx` | Cluster cards with summaries |
| `web/src/pages/Datasets.tsx` | Snapshot history + JSON/CSV export |
| `web/src/pages/Agents.tsx` | Agent list + bulk ZIP export |
| `web/src/pages/AgentDetail.tsx` | Agent detail + publish + per-format export + version history |
| `Makefile` | `make web` and `make build` targets |

### Modified files

| File | Change |
|------|--------|
| `internal/storage/storage.go` | Add `Offset int`, `Search string` to `ListFilter`; add `RateEntry` to `Store`; add `ListSnapshots` to `AnalysisStore` |
| `internal/storage/sqlite.go` | Implement `RateEntry`; extend `ListEntries` for `Offset`/`Search` |
| `internal/storage/analysis.go` | Implement `ListSnapshots` |
| `internal/config/config.go` | Add `HTTPAddr string` (default `":8080"`) |
| `internal/config/config_test.go` | Test `HTTPAddr` default |
| `cmd/server/main.go` | Start HTTP server in goroutine; pass `fs.Sub(web.DistFS, "dist")` as static FS |

---

## Task 1: Storage extensions — RateEntry, ListSnapshots, ListFilter pagination

**Files:**
- Modify: `internal/storage/storage.go`
- Modify: `internal/storage/sqlite.go`
- Modify: `internal/storage/analysis.go`
- Modify: `internal/storage/storage_test.go`
- Modify: `internal/storage/analysis_test.go`

- [ ] **Step 1: Write failing tests**

Append to `internal/storage/storage_test.go`:

```go
func TestRateEntry(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	id := storeTestEntry(t, s, "Rate me", "rate content")

	if err := s.RateEntry(ctx, id, 4.5); err != nil {
		t.Fatalf("RateEntry: %v", err)
	}
	e, err := s.GetEntry(ctx, id)
	if err != nil {
		t.Fatalf("GetEntry: %v", err)
	}
	if e.Rating != 4.5 {
		t.Errorf("Rating = %v, want 4.5", e.Rating)
	}
}

func TestRateEntry_NotFound(t *testing.T) {
	s := newTestStore(t)
	err := s.RateEntry(context.Background(), "nonexistent-id", 3.0)
	if err == nil {
		t.Fatal("expected error for missing entry")
	}
}

func TestListEntries_OffsetAndSearch(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	for _, title := range []string{"Alpha analytics", "Beta reporting", "Gamma forecast"} {
		storeTestEntry(t, s, title, "content about "+title)
	}

	// Test offset
	all, _ := s.ListEntries(ctx, ListFilter{Limit: 10})
	second, _ := s.ListEntries(ctx, ListFilter{Limit: 10, Offset: 1})
	if len(second) != len(all)-1 {
		t.Errorf("offset 1: want %d entries, got %d", len(all)-1, len(second))
	}

	// Test search by title
	results, err := s.ListEntries(ctx, ListFilter{Limit: 10, Search: "Alpha"})
	if err != nil {
		t.Fatalf("ListEntries search: %v", err)
	}
	if len(results) != 1 {
		t.Errorf("search 'Alpha': want 1 result, got %d", len(results))
	}
	if results[0].Title != "Alpha analytics" {
		t.Errorf("wrong entry: %q", results[0].Title)
	}
}
```

Append to `internal/storage/analysis_test.go`:

```go
func TestListSnapshots(t *testing.T) {
	s := newTestAnalysisStore(t)
	ctx := context.Background()

	for _, v := range []int{1, 2, 3} {
		_, err := s.StoreSnapshot(ctx, storage.DatasetSnapshot{
			Version:      v,
			ClusterCount: v * 2,
			EntryCount:   v * 5,
		})
		if err != nil {
			t.Fatalf("StoreSnapshot v%d: %v", v, err)
		}
	}

	snaps, err := s.ListSnapshots(ctx)
	if err != nil {
		t.Fatalf("ListSnapshots: %v", err)
	}
	if len(snaps) != 3 {
		t.Fatalf("want 3 snapshots, got %d", len(snaps))
	}
	if snaps[0].Version < snaps[1].Version {
		t.Errorf("expected descending version order, got %d then %d", snaps[0].Version, snaps[1].Version)
	}
}
```

- [ ] **Step 2: Run — verify they fail**

```bash
cd /Users/dsandor/Projects/memory && CGO_ENABLED=1 go test ./internal/storage/... 2>&1 | head -15
```

Expected: compilation errors (`RateEntry`, `Offset`, `Search`, `ListSnapshots` undefined).

- [ ] **Step 3: Extend storage.go interfaces**

In `internal/storage/storage.go`, replace the `ListFilter` struct:

```go
type ListFilter struct {
	Domain string
	Type   KnowledgeType
	// Limit is the maximum number of entries to return. Zero means default (50).
	Limit  int
	// Offset skips this many entries (for pagination).
	Offset int
	// Search filters by substring match on Title or Content (case-insensitive). Empty means no filter.
	Search string
}
```

Add `RateEntry` to the `Store` interface after `SearchSimilar`:

```go
// RateEntry updates the rating for an existing entry.
// Returns ErrNotFound if the entry does not exist.
RateEntry(ctx context.Context, id string, rating float64) error
```

Add `ListSnapshots` to the `AnalysisStore` interface after `GetLatestSnapshot`:

```go
// ListSnapshots returns all dataset snapshots ordered by version descending.
ListSnapshots(ctx context.Context) ([]DatasetSnapshot, error)
```

- [ ] **Step 4: Implement RateEntry in sqlite.go**

In `internal/storage/sqlite.go`, add after the `Close()` method:

```go
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
```

- [ ] **Step 5: Extend ListEntries in sqlite.go to handle Offset and Search**

In `internal/storage/sqlite.go`, replace the `ListEntries` method:

```go
func (s *SQLiteStore) ListEntries(ctx context.Context, filter ListFilter) ([]KnowledgeEntry, error) {
	limit := filter.Limit
	if limit == 0 {
		limit = 50
	}

	query := `SELECT id, type, title, content, description, domain, tags, author, team,
	                 created_at, updated_at, version, rating, usage_count
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
		query += " AND (title LIKE ? OR content LIKE ?)"
		pattern := "%" + filter.Search + "%"
		args = append(args, pattern, pattern)
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
```

- [ ] **Step 6: Implement ListSnapshots in analysis.go**

In `internal/storage/analysis.go`, add after `GetLatestSnapshot`:

```go
func (s *SQLiteStore) ListSnapshots(ctx context.Context) ([]DatasetSnapshot, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, version, cluster_count, entry_count, data, pipeline_run_id, created_at
		FROM dataset_snapshots
		ORDER BY version DESC, created_at DESC
	`)
	if err != nil {
		return nil, fmt.Errorf("list snapshots: %w", err)
	}
	defer rows.Close()

	var snaps []DatasetSnapshot
	for rows.Next() {
		var snap DatasetSnapshot
		var createdAt string
		if err := rows.Scan(
			&snap.ID, &snap.Version, &snap.ClusterCount, &snap.EntryCount,
			&snap.Data, &snap.PipelineRunID, &createdAt,
		); err != nil {
			return nil, fmt.Errorf("scan snapshot: %w", err)
		}
		snap.CreatedAt = parseTimestamp(createdAt)
		snaps = append(snaps, snap)
	}
	return snaps, rows.Err()
}
```

- [ ] **Step 7: Run all storage tests**

```bash
cd /Users/dsandor/Projects/memory && CGO_ENABLED=1 go test ./internal/storage/... -v 2>&1 | grep -E "^--- |^ok|FAIL"
```

Expected: all PASS, no FAIL.

- [ ] **Step 8: Build check**

```bash
cd /Users/dsandor/Projects/memory && CGO_ENABLED=1 go build ./cmd/server/ && echo "ok"
```

Expected: `ok`

---

## Task 2: Go HTTP server + REST API

**Files:**
- Create: `internal/web/dist/index.html`
- Create: `internal/web/server.go`
- Create: `internal/web/handlers.go`

- [ ] **Step 1: Create placeholder dist/index.html**

Create `internal/web/dist/index.html`:

```html
<!DOCTYPE html>
<html lang="en">
<head><meta charset="UTF-8"><title>Tribal Knowledge</title></head>
<body><p>Run <code>make web</code> to build the UI.</p></body>
</html>
```

- [ ] **Step 2: Create internal/web/server.go**

```go
package web

import (
	"io/fs"
	"net/http"
	"strings"

	"github.com/dsandor/memory/internal/storage"
)

// AllStore is the storage interface required by the HTTP API.
// *storage.SQLiteStore satisfies this — it implements AgentStore which
// transitively includes AnalysisStore (ListSnapshots) and Store (RateEntry).
type AllStore interface {
	storage.AgentStore
}

// Server wraps an http.ServeMux with REST API routes and SPA static serving.
type Server struct {
	store    AllStore
	mux      *http.ServeMux
	staticFS fs.FS
}

// NewServer wires all routes and returns a ready Server.
// staticFS should be the built React dist (typically fs.Sub of the embedded FS).
func NewServer(staticFS fs.FS, store AllStore) *Server {
	s := &Server{
		store:    store,
		mux:      http.NewServeMux(),
		staticFS: staticFS,
	}
	s.routes()
	return s
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mux.ServeHTTP(w, r)
}

func (s *Server) routes() {
	s.mux.HandleFunc("GET /api/stats", s.handleStats)
	s.mux.HandleFunc("GET /api/knowledge", s.handleKnowledgeList)
	s.mux.HandleFunc("GET /api/knowledge/{id}", s.handleKnowledgeGet)
	s.mux.HandleFunc("PUT /api/knowledge/{id}/rate", s.handleKnowledgeRate)
	s.mux.HandleFunc("GET /api/clusters", s.handleClusterList)
	s.mux.HandleFunc("GET /api/datasets", s.handleDatasetList)
	s.mux.HandleFunc("GET /api/datasets/{id}/export", s.handleDatasetExport)
	s.mux.HandleFunc("GET /api/agents/bulk-export", s.handleAgentBulkExport)
	s.mux.HandleFunc("GET /api/agents/{id}", s.handleAgentGet)
	s.mux.HandleFunc("GET /api/agents", s.handleAgentList)
	s.mux.HandleFunc("PUT /api/agents/{id}/publish", s.handleAgentPublish)
	s.mux.HandleFunc("GET /api/agents/{id}/export", s.handleAgentExport)
	s.mux.HandleFunc("GET /api/pipeline/status", s.handlePipelineStatus)
	s.mux.HandleFunc("/", s.handleStatic)
}

func (s *Server) handleStatic(w http.ResponseWriter, r *http.Request) {
	if strings.HasPrefix(r.URL.Path, "/api/") {
		http.NotFound(w, r)
		return
	}
	path := strings.TrimPrefix(r.URL.Path, "/")
	if path == "" {
		path = "index.html"
	}
	_, err := s.staticFS.Open(path)
	if err != nil {
		// SPA fallback: any unknown path → index.html for client-side routing
		http.ServeFileFS(w, r, s.staticFS, "index.html")
		return
	}
	http.FileServerFS(s.staticFS).ServeHTTP(w, r)
}
```

- [ ] **Step 3: Create internal/web/handlers.go**

```go
package web

import (
	"archive/zip"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"

	agentpkg "github.com/dsandor/memory/internal/agent"
	"github.com/dsandor/memory/internal/storage"
)

// StatsResponse is the JSON shape returned by GET /api/stats.
type StatsResponse struct {
	KnowledgeCount int     `json:"knowledge_count"`
	ClusterCount   int     `json:"cluster_count"`
	AgentCount     int     `json:"agent_count"`
	PipelineStatus string  `json:"pipeline_status"`
	PipelineLastRun *string `json:"pipeline_last_run"`
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, code int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": msg})
}

func (s *Server) handleStats(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	count, err := s.store.CountEntries(ctx)
	if err != nil {
		writeError(w, 500, fmt.Sprintf("count entries: %v", err))
		return
	}
	clusters, err := s.store.ListClusters(ctx)
	if err != nil {
		writeError(w, 500, fmt.Sprintf("list clusters: %v", err))
		return
	}
	agents, err := s.store.ListAgents(ctx)
	if err != nil {
		writeError(w, 500, fmt.Sprintf("list agents: %v", err))
		return
	}
	run, err := s.store.GetLatestPipelineRun(ctx)
	if err != nil {
		writeError(w, 500, fmt.Sprintf("get pipeline run: %v", err))
		return
	}

	resp := StatsResponse{
		KnowledgeCount: count,
		ClusterCount:   len(clusters),
		AgentCount:     len(agents),
		PipelineStatus: "idle",
	}
	if run != nil {
		resp.PipelineStatus = run.Status
		t := run.StartedAt.Format("2006-01-02T15:04:05Z")
		resp.PipelineLastRun = &t
	}
	writeJSON(w, resp)
}

func (s *Server) handleKnowledgeList(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	limit, _ := strconv.Atoi(q.Get("limit"))
	if limit == 0 {
		limit = 20
	}
	offset, _ := strconv.Atoi(q.Get("offset"))

	filter := storage.ListFilter{
		Domain: q.Get("domain"),
		Type:   storage.KnowledgeType(q.Get("type")),
		Limit:  limit,
		Offset: offset,
		Search: q.Get("search"),
	}
	entries, err := s.store.ListEntries(r.Context(), filter)
	if err != nil {
		writeError(w, 500, fmt.Sprintf("list entries: %v", err))
		return
	}
	if entries == nil {
		entries = []storage.KnowledgeEntry{}
	}
	writeJSON(w, entries)
}

func (s *Server) handleKnowledgeGet(w http.ResponseWriter, r *http.Request) {
	entry, err := s.store.GetEntry(r.Context(), r.PathValue("id"))
	if err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			writeError(w, 404, "entry not found")
			return
		}
		writeError(w, 500, fmt.Sprintf("get entry: %v", err))
		return
	}
	writeJSON(w, entry)
}

func (s *Server) handleKnowledgeRate(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Rating float64 `json:"rating"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, 400, "invalid JSON body")
		return
	}
	if body.Rating < 0 || body.Rating > 5 {
		writeError(w, 400, "rating must be between 0 and 5")
		return
	}
	if err := s.store.RateEntry(r.Context(), r.PathValue("id"), body.Rating); err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			writeError(w, 404, "entry not found")
			return
		}
		writeError(w, 500, fmt.Sprintf("rate entry: %v", err))
		return
	}
	writeJSON(w, map[string]any{"ok": true})
}

func (s *Server) handleClusterList(w http.ResponseWriter, r *http.Request) {
	clusters, err := s.store.ListClusters(r.Context())
	if err != nil {
		writeError(w, 500, fmt.Sprintf("list clusters: %v", err))
		return
	}
	if clusters == nil {
		clusters = []storage.Cluster{}
	}
	writeJSON(w, clusters)
}

func (s *Server) handleDatasetList(w http.ResponseWriter, r *http.Request) {
	snaps, err := s.store.ListSnapshots(r.Context())
	if err != nil {
		writeError(w, 500, fmt.Sprintf("list snapshots: %v", err))
		return
	}
	if snaps == nil {
		snaps = []storage.DatasetSnapshot{}
	}
	writeJSON(w, snaps)
}

func (s *Server) handleDatasetExport(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	format := r.URL.Query().Get("format")
	if format == "" {
		format = "json"
	}

	snaps, err := s.store.ListSnapshots(r.Context())
	if err != nil {
		writeError(w, 500, fmt.Sprintf("list snapshots: %v", err))
		return
	}
	var snap *storage.DatasetSnapshot
	for i := range snaps {
		if snaps[i].ID == id {
			snap = &snaps[i]
			break
		}
	}
	if snap == nil {
		writeError(w, 404, "snapshot not found")
		return
	}

	switch format {
	case "json":
		w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="snapshot-v%d.json"`, snap.Version))
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, snap.Data)
	case "csv":
		w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="snapshot-v%d.csv"`, snap.Version))
		w.Header().Set("Content-Type", "text/csv")
		cw := csv.NewWriter(w)
		_ = cw.Write([]string{"id", "version", "cluster_count", "entry_count", "pipeline_run_id", "created_at"})
		_ = cw.Write([]string{
			snap.ID, strconv.Itoa(snap.Version),
			strconv.Itoa(snap.ClusterCount), strconv.Itoa(snap.EntryCount),
			snap.PipelineRunID, snap.CreatedAt.Format("2006-01-02T15:04:05Z"),
		})
		cw.Flush()
	default:
		writeError(w, 400, "format must be json or csv")
	}
}

func (s *Server) handleAgentList(w http.ResponseWriter, r *http.Request) {
	agents, err := s.store.ListAgents(r.Context())
	if err != nil {
		writeError(w, 500, fmt.Sprintf("list agents: %v", err))
		return
	}
	if agents == nil {
		agents = []storage.Agent{}
	}
	writeJSON(w, agents)
}

func (s *Server) handleAgentGet(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := r.PathValue("id")

	a, err := s.store.GetAgent(ctx, id)
	if err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			writeError(w, 404, "agent not found")
			return
		}
		writeError(w, 500, fmt.Sprintf("get agent: %v", err))
		return
	}
	versions, err := s.store.ListAgentVersions(ctx, id)
	if err != nil || versions == nil {
		versions = []storage.AgentVersion{}
	}
	writeJSON(w, map[string]any{"agent": a, "versions": versions})
}

func (s *Server) handleAgentPublish(w http.ResponseWriter, r *http.Request) {
	if err := s.store.PublishAgent(r.Context(), r.PathValue("id")); err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			writeError(w, 404, "agent not found")
			return
		}
		writeError(w, 500, fmt.Sprintf("publish agent: %v", err))
		return
	}
	writeJSON(w, map[string]any{"ok": true})
}

func (s *Server) handleAgentExport(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := r.PathValue("id")
	format := r.URL.Query().Get("format")
	if format == "" {
		format = "md"
	}

	a, err := s.store.GetAgent(ctx, id)
	if err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			writeError(w, 404, "agent not found")
			return
		}
		writeError(w, 500, fmt.Sprintf("get agent: %v", err))
		return
	}

	var contentType, ext string
	switch format {
	case "md":
		contentType, ext = "text/markdown", "md"
	case "txt":
		contentType, ext = "text/plain", "txt"
	case "json":
		contentType, ext = "application/json", "json"
	default:
		writeError(w, 400, "format must be md, txt, or json")
		return
	}
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s-agent.%s"`, a.Domain, ext))
	fmt.Fprint(w, agentpkg.Export(a, format))
}

func (s *Server) handleAgentBulkExport(w http.ResponseWriter, r *http.Request) {
	agents, err := s.store.ListAgents(r.Context())
	if err != nil {
		writeError(w, 500, fmt.Sprintf("list agents: %v", err))
		return
	}

	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", `attachment; filename="agents-export.zip"`)

	zw := zip.NewWriter(w)
	for i := range agents {
		for _, format := range []string{"md", "txt", "json"} {
			f, err := zw.Create(fmt.Sprintf("%s/agent.%s", agents[i].Domain, format))
			if err != nil {
				continue
			}
			fmt.Fprint(f, agentpkg.Export(&agents[i], format))
		}
	}
	_ = zw.Close()
}

func (s *Server) handlePipelineStatus(w http.ResponseWriter, r *http.Request) {
	run, err := s.store.GetLatestPipelineRun(r.Context())
	if err != nil {
		writeError(w, 500, fmt.Sprintf("get pipeline run: %v", err))
		return
	}
	if run == nil {
		writeJSON(w, map[string]any{"status": "idle"})
		return
	}
	writeJSON(w, run)
}
```

- [ ] **Step 4: Build check**

```bash
cd /Users/dsandor/Projects/memory && CGO_ENABLED=1 go build ./internal/web/ && echo "ok"
```

Expected: `ok`

---

## Task 3: HTTP server tests

**Files:**
- Create: `internal/web/server_test.go`

- [ ] **Step 1: Create server_test.go**

```go
package web_test

import (
	"context"
	"encoding/json"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/dsandor/memory/internal/storage"
	"github.com/dsandor/memory/internal/web"
)

// mockStore implements web.AllStore with configurable return values.
type mockStore struct {
	entries  []storage.KnowledgeEntry
	clusters []storage.Cluster
	agents   []storage.Agent
	versions []storage.AgentVersion
	snaps    []storage.DatasetSnapshot
	run      *storage.PipelineRun
}

func (m *mockStore) CountEntries(_ context.Context) (int, error) { return len(m.entries), nil }
func (m *mockStore) GetAllEmbeddings(_ context.Context) (map[string][]float32, error) {
	return map[string][]float32{}, nil
}
func (m *mockStore) ListClusters(_ context.Context) ([]storage.Cluster, error) {
	return m.clusters, nil
}
func (m *mockStore) StoreCluster(_ context.Context, c storage.Cluster) (string, error) { return "x", nil }
func (m *mockStore) DeleteClustersByRunID(_ context.Context, _ string) error           { return nil }
func (m *mockStore) StartPipelineRun(_ context.Context, _ string) (string, error)      { return "x", nil }
func (m *mockStore) FinishPipelineRun(_ context.Context, _, _ string, _, _ int, _ []string) error {
	return nil
}
func (m *mockStore) GetLatestPipelineRun(_ context.Context) (*storage.PipelineRun, error) {
	return m.run, nil
}
func (m *mockStore) StoreSnapshot(_ context.Context, _ storage.DatasetSnapshot) (string, error) {
	return "x", nil
}
func (m *mockStore) GetLatestSnapshot(_ context.Context) (*storage.DatasetSnapshot, error) {
	if len(m.snaps) == 0 {
		return nil, nil
	}
	return &m.snaps[0], nil
}
func (m *mockStore) ListSnapshots(_ context.Context) ([]storage.DatasetSnapshot, error) {
	return m.snaps, nil
}
func (m *mockStore) StoreEntry(_ context.Context, _ storage.KnowledgeEntry, _ []float32) (string, error) {
	return "x", nil
}
func (m *mockStore) GetEntry(_ context.Context, id string) (*storage.KnowledgeEntry, error) {
	for i := range m.entries {
		if m.entries[i].ID == id {
			return &m.entries[i], nil
		}
	}
	return nil, storage.ErrNotFound
}
func (m *mockStore) ListEntries(_ context.Context, _ storage.ListFilter) ([]storage.KnowledgeEntry, error) {
	return m.entries, nil
}
func (m *mockStore) DeleteEntry(_ context.Context, _ string) error { return nil }
func (m *mockStore) SearchSimilar(_ context.Context, _ []float32, _ int) ([]storage.SearchResult, error) {
	return nil, nil
}
func (m *mockStore) RateEntry(_ context.Context, id string, _ float64) error {
	for i := range m.entries {
		if m.entries[i].ID == id {
			return nil
		}
	}
	return storage.ErrNotFound
}
func (m *mockStore) Close() error { return nil }
func (m *mockStore) UpsertAgent(_ context.Context, _ storage.Agent) (string, error) { return "x", nil }
func (m *mockStore) GetAgent(_ context.Context, id string) (*storage.Agent, error) {
	for i := range m.agents {
		if m.agents[i].ID == id {
			return &m.agents[i], nil
		}
	}
	return nil, storage.ErrNotFound
}
func (m *mockStore) GetAgentByDomain(_ context.Context, _ string) (*storage.Agent, error) {
	return nil, storage.ErrNotFound
}
func (m *mockStore) ListAgents(_ context.Context) ([]storage.Agent, error) { return m.agents, nil }
func (m *mockStore) PublishAgent(_ context.Context, id string) error {
	for i := range m.agents {
		if m.agents[i].ID == id {
			m.agents[i].Status = storage.AgentStatusPublished
			return nil
		}
	}
	return storage.ErrNotFound
}
func (m *mockStore) StoreAgentVersion(_ context.Context, _ storage.AgentVersion) error { return nil }
func (m *mockStore) ListAgentVersions(_ context.Context, _ string) ([]storage.AgentVersion, error) {
	return m.versions, nil
}

func newTestServer(t *testing.T, store *mockStore) *web.Server {
	t.Helper()
	staticFS := fstest.MapFS{
		"index.html": {Data: []byte("<html><body>app</body></html>"), Mode: 0444, ModTime: time.Now()},
	}
	return web.NewServer(staticFS, store)
}

func TestHandleStats(t *testing.T) {
	store := &mockStore{
		entries:  []storage.KnowledgeEntry{{ID: "e1"}, {ID: "e2"}},
		clusters: []storage.Cluster{{ID: "c1"}},
		agents:   []storage.Agent{{ID: "a1"}},
	}
	srv := newTestServer(t, store)

	req := httptest.NewRequest("GET", "/api/stats", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", w.Code, w.Body.String())
	}
	var stats web.StatsResponse
	if err := json.NewDecoder(w.Body).Decode(&stats); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if stats.KnowledgeCount != 2 {
		t.Errorf("KnowledgeCount = %d, want 2", stats.KnowledgeCount)
	}
	if stats.ClusterCount != 1 {
		t.Errorf("ClusterCount = %d, want 1", stats.ClusterCount)
	}
	if stats.AgentCount != 1 {
		t.Errorf("AgentCount = %d, want 1", stats.AgentCount)
	}
	if stats.PipelineStatus != "idle" {
		t.Errorf("PipelineStatus = %q, want idle", stats.PipelineStatus)
	}
}

func TestHandleKnowledgeList(t *testing.T) {
	store := &mockStore{
		entries: []storage.KnowledgeEntry{
			{ID: "e1", Title: "Entry One", Type: "prompt"},
			{ID: "e2", Title: "Entry Two", Type: "pattern"},
		},
	}
	srv := newTestServer(t, store)

	req := httptest.NewRequest("GET", "/api/knowledge?limit=10", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("want 200, got %d", w.Code)
	}
	var entries []storage.KnowledgeEntry
	if err := json.NewDecoder(w.Body).Decode(&entries); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(entries) != 2 {
		t.Errorf("want 2 entries, got %d", len(entries))
	}
}

func TestHandleKnowledgeGet_Found(t *testing.T) {
	store := &mockStore{
		entries: []storage.KnowledgeEntry{{ID: "e1", Title: "Found Entry"}},
	}
	srv := newTestServer(t, store)

	req := httptest.NewRequest("GET", "/api/knowledge/e1", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("want 200, got %d", w.Code)
	}
	var entry storage.KnowledgeEntry
	if err := json.NewDecoder(w.Body).Decode(&entry); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if entry.Title != "Found Entry" {
		t.Errorf("title = %q, want Found Entry", entry.Title)
	}
}

func TestHandleKnowledgeGet_NotFound(t *testing.T) {
	srv := newTestServer(t, &mockStore{})
	req := httptest.NewRequest("GET", "/api/knowledge/missing", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != 404 {
		t.Errorf("want 404, got %d", w.Code)
	}
}

func TestHandleKnowledgeRate(t *testing.T) {
	store := &mockStore{
		entries: []storage.KnowledgeEntry{{ID: "e1"}},
	}
	srv := newTestServer(t, store)

	body := strings.NewReader(`{"rating":4.5}`)
	req := httptest.NewRequest("PUT", "/api/knowledge/e1/rate", body)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("want 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestHandleKnowledgeRate_InvalidRating(t *testing.T) {
	store := &mockStore{entries: []storage.KnowledgeEntry{{ID: "e1"}}}
	srv := newTestServer(t, store)
	body := strings.NewReader(`{"rating":9.9}`)
	req := httptest.NewRequest("PUT", "/api/knowledge/e1/rate", body)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != 400 {
		t.Errorf("want 400, got %d", w.Code)
	}
}

func TestHandleAgentList(t *testing.T) {
	store := &mockStore{
		agents: []storage.Agent{
			{ID: "a1", Domain: "finance", Status: storage.AgentStatusPublished},
		},
	}
	srv := newTestServer(t, store)
	req := httptest.NewRequest("GET", "/api/agents", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("want 200, got %d", w.Code)
	}
	var agents []storage.Agent
	if err := json.NewDecoder(w.Body).Decode(&agents); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(agents) != 1 {
		t.Errorf("want 1 agent, got %d", len(agents))
	}
}

func TestHandleAgentGet_Found(t *testing.T) {
	store := &mockStore{
		agents: []storage.Agent{{ID: "a1", Domain: "finance"}},
	}
	srv := newTestServer(t, store)
	req := httptest.NewRequest("GET", "/api/agents/a1", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("want 200, got %d", w.Code)
	}
}

func TestHandleAgentPublish(t *testing.T) {
	store := &mockStore{
		agents: []storage.Agent{{ID: "a1", Domain: "finance", Status: storage.AgentStatusDraft}},
	}
	srv := newTestServer(t, store)
	req := httptest.NewRequest("PUT", "/api/agents/a1/publish", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("want 200, got %d: %s", w.Code, w.Body.String())
	}
	if store.agents[0].Status != storage.AgentStatusPublished {
		t.Errorf("agent status = %q, want published", store.agents[0].Status)
	}
}

func TestHandleAgentExport_MD(t *testing.T) {
	store := &mockStore{
		agents: []storage.Agent{{ID: "a1", Domain: "finance", SystemPrompt: "You are a finance agent."}},
	}
	srv := newTestServer(t, store)
	req := httptest.NewRequest("GET", "/api/agents/a1/export?format=md", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("want 200, got %d", w.Code)
	}
	if ct := w.Header().Get("Content-Type"); ct != "text/markdown" {
		t.Errorf("Content-Type = %q, want text/markdown", ct)
	}
	if !strings.Contains(w.Body.String(), "finance") {
		t.Errorf("export body missing domain name")
	}
}

func TestHandleStaticFallback(t *testing.T) {
	srv := newTestServer(t, &mockStore{})
	req := httptest.NewRequest("GET", "/agents/some-id", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("SPA fallback: want 200, got %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "app") {
		t.Errorf("SPA fallback should serve index.html content")
	}
}

func TestHandleDatasetList(t *testing.T) {
	store := &mockStore{
		snaps: []storage.DatasetSnapshot{{ID: "s1", Version: 1, ClusterCount: 3, EntryCount: 10}},
	}
	srv := newTestServer(t, store)
	req := httptest.NewRequest("GET", "/api/datasets", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("want 200, got %d", w.Code)
	}
	var snaps []storage.DatasetSnapshot
	if err := json.NewDecoder(w.Body).Decode(&snaps); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(snaps) != 1 {
		t.Errorf("want 1 snapshot, got %d", len(snaps))
	}
}
```

- [ ] **Step 2: Run tests**

```bash
cd /Users/dsandor/Projects/memory && CGO_ENABLED=1 go test ./internal/web/... -v 2>&1 | grep -E "^--- |^ok|FAIL"
```

Expected: all PASS, no FAIL.

- [ ] **Step 3: Full suite check**

```bash
cd /Users/dsandor/Projects/memory && CGO_ENABLED=1 go test ./... 2>&1 | grep -E "^ok|FAIL"
```

Expected: all packages `ok`.

---

## Task 4: Config + main.go changes

**Files:**
- Modify: `internal/config/config.go`
- Modify: `internal/config/config_test.go`
- Modify: `cmd/server/main.go`
- Create: `internal/web/embed.go`

- [ ] **Step 1: Add HTTPAddr to config**

In `internal/config/config.go`, add `HTTPAddr string` to the `Config` struct after `ClusterThreshold`:

```go
HTTPAddr string
```

In `Load()`, add to the return struct:

```go
HTTPAddr: envOrDefault("HTTP_ADDR", ":8080"),
```

- [ ] **Step 2: Add config test for HTTPAddr**

In `internal/config/config_test.go`, add:

```go
func TestHTTPAddrDefault(t *testing.T) {
	os.Unsetenv("HTTP_ADDR")
	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.HTTPAddr != ":8080" {
		t.Errorf("HTTPAddr = %q, want :8080", cfg.HTTPAddr)
	}
}
```

- [ ] **Step 3: Run config tests**

```bash
cd /Users/dsandor/Projects/memory && CGO_ENABLED=1 go test ./internal/config/... -v 2>&1 | grep -E "^--- |^ok|FAIL"
```

Expected: all PASS.

- [ ] **Step 4: Create internal/web/embed.go**

```go
package web

import "embed"

//go:embed all:dist
var DistFS embed.FS
```

- [ ] **Step 5: Update cmd/server/main.go**

Replace the entire `main.go` with:

```go
package main

import (
	"context"
	"io/fs"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/dsandor/memory/internal/config"
	"github.com/dsandor/memory/internal/embedding"
	"github.com/dsandor/memory/internal/llm"
	internalmcp "github.com/dsandor/memory/internal/mcp"
	"github.com/dsandor/memory/internal/pipeline"
	"github.com/dsandor/memory/internal/storage"
	"github.com/dsandor/memory/internal/web"
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

	// HTTP server (serves React SPA + REST API)
	staticFS, err := fs.Sub(web.DistFS, "dist")
	if err != nil {
		log.Fatalf("sub fs: %v", err)
	}
	httpServer := &http.Server{
		Addr:    cfg.HTTPAddr,
		Handler: web.NewServer(staticFS, store),
	}
	go func() {
		log.Printf("HTTP server listening on %s", cfg.HTTPAddr)
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Printf("HTTP server error: %v", err)
		}
	}()

	// MCP server
	mcpServer := internalmcp.NewMCPServer(store, embedder)
	internalmcp.RegisterAnalysisTools(mcpServer, store)
	internalmcp.RegisterRuleTools(mcpServer, store)
	internalmcp.RegisterAgentTools(mcpServer, store)

	if cfg.AnthropicAPIKey != "" {
		llmClient := llm.NewAnthropicClient(cfg.AnthropicAPIKey, cfg.AnthropicModel)
		agentLLMClient := llm.NewAnthropicClient(cfg.AnthropicAPIKey, cfg.AgentModel)

		p := pipeline.New(store, llmClient, pipeline.Config{
			MinEntries:       cfg.PipelineMinEntries,
			Interval:         cfg.PipelineInterval,
			ClusterThreshold: cfg.ClusterThreshold,
		}).WithAgentGeneration(store, agentLLMClient)

		ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer cancel()
		p.Start(ctx)
	}

	if err := server.ServeStdio(mcpServer); err != nil {
		log.Printf("serve: %v", err)
	}
}
```

- [ ] **Step 6: Build check**

```bash
cd /Users/dsandor/Projects/memory && CGO_ENABLED=1 go build ./cmd/server/ && echo "ok"
```

Expected: `ok`

---

## Task 5: React scaffold + Tailwind + shadcn setup

**Files:** All new `web/` project files.

- [ ] **Step 1: Scaffold Vite React TypeScript project**

```bash
cd /Users/dsandor/Projects/memory && npm create vite@latest web -- --template react-ts
```

Expected: creates `web/` directory with React + TypeScript template.

- [ ] **Step 2: Install dependencies**

```bash
cd /Users/dsandor/Projects/memory/web && npm install && npm install react-router-dom && npm install -D tailwindcss @tailwindcss/vite
```

- [ ] **Step 3: Install shadcn/ui dependencies manually**

```bash
cd /Users/dsandor/Projects/memory/web && npm install class-variance-authority clsx tailwind-merge lucide-react @radix-ui/react-slot @radix-ui/react-separator @radix-ui/react-tabs @radix-ui/react-select @radix-ui/react-dialog
```

- [ ] **Step 4: Replace vite.config.ts**

```ts
import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'
import tailwindcss from '@tailwindcss/vite'
import path from 'path'

export default defineConfig({
  plugins: [react(), tailwindcss()],
  resolve: {
    alias: {
      '@': path.resolve(__dirname, './src'),
    },
  },
  server: {
    proxy: {
      '/api': 'http://localhost:8080',
    },
  },
  build: {
    outDir: '../internal/web/dist',
    emptyOutDir: true,
  },
})
```

- [ ] **Step 5: Replace tsconfig.json**

```json
{
  "files": [],
  "references": [
    { "path": "./tsconfig.app.json" },
    { "path": "./tsconfig.node.json" }
  ]
}
```

Create `web/tsconfig.app.json`:

```json
{
  "compilerOptions": {
    "target": "ES2020",
    "useDefineForClassFields": true,
    "lib": ["ES2020", "DOM", "DOM.Iterable"],
    "module": "ESNext",
    "skipLibCheck": true,
    "moduleResolution": "bundler",
    "allowImportingTsExtensions": true,
    "isolatedModules": true,
    "moduleDetection": "force",
    "noEmit": true,
    "jsx": "react-jsx",
    "strict": true,
    "baseUrl": ".",
    "paths": { "@/*": ["./src/*"] }
  },
  "include": ["src"]
}
```

- [ ] **Step 6: Replace src/index.css**

```css
@import "tailwindcss";

:root {
  color-scheme: dark;
}

body {
  background-color: #0f1117;
  color: #e2e8f0;
  font-family: system-ui, -apple-system, sans-serif;
}
```

- [ ] **Step 7: Create src/lib/utils.ts**

```ts
import { clsx, type ClassValue } from 'clsx'
import { twMerge } from 'tailwind-merge'

export function cn(...inputs: ClassValue[]) {
  return twMerge(clsx(inputs))
}
```

- [ ] **Step 8: Create shadcn/ui component files**

Create `web/src/components/ui/button.tsx`:

```tsx
import * as React from 'react'
import { Slot } from '@radix-ui/react-slot'
import { cva, type VariantProps } from 'class-variance-authority'
import { cn } from '@/lib/utils'

const buttonVariants = cva(
  'inline-flex items-center justify-center gap-2 whitespace-nowrap rounded-md text-sm font-medium transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-slate-400 disabled:pointer-events-none disabled:opacity-50',
  {
    variants: {
      variant: {
        default: 'bg-slate-700 text-slate-100 hover:bg-slate-600',
        destructive: 'bg-red-700 text-red-100 hover:bg-red-600',
        outline: 'border border-slate-600 bg-transparent hover:bg-slate-800 text-slate-300',
        ghost: 'hover:bg-slate-800 text-slate-300',
        link: 'text-slate-300 underline-offset-4 hover:underline',
        success: 'bg-emerald-700 text-emerald-100 hover:bg-emerald-600',
      },
      size: {
        default: 'h-9 px-4 py-2',
        sm: 'h-8 rounded-md px-3 text-xs',
        lg: 'h-10 rounded-md px-8',
        icon: 'h-9 w-9',
      },
    },
    defaultVariants: { variant: 'default', size: 'default' },
  }
)

export interface ButtonProps
  extends React.ButtonHTMLAttributes<HTMLButtonElement>,
    VariantProps<typeof buttonVariants> {
  asChild?: boolean
}

const Button = React.forwardRef<HTMLButtonElement, ButtonProps>(
  ({ className, variant, size, asChild = false, ...props }, ref) => {
    const Comp = asChild ? Slot : 'button'
    return <Comp className={cn(buttonVariants({ variant, size, className }))} ref={ref} {...props} />
  }
)
Button.displayName = 'Button'

export { Button, buttonVariants }
```

Create `web/src/components/ui/card.tsx`:

```tsx
import * as React from 'react'
import { cn } from '@/lib/utils'

const Card = React.forwardRef<HTMLDivElement, React.HTMLAttributes<HTMLDivElement>>(
  ({ className, ...props }, ref) => (
    <div ref={ref} className={cn('rounded-lg border border-slate-700 bg-slate-900 text-slate-100 shadow-sm', className)} {...props} />
  )
)
Card.displayName = 'Card'

const CardHeader = React.forwardRef<HTMLDivElement, React.HTMLAttributes<HTMLDivElement>>(
  ({ className, ...props }, ref) => (
    <div ref={ref} className={cn('flex flex-col space-y-1.5 p-6', className)} {...props} />
  )
)
CardHeader.displayName = 'CardHeader'

const CardTitle = React.forwardRef<HTMLParagraphElement, React.HTMLAttributes<HTMLHeadingElement>>(
  ({ className, ...props }, ref) => (
    <h3 ref={ref} className={cn('text-lg font-semibold leading-none tracking-tight', className)} {...props} />
  )
)
CardTitle.displayName = 'CardTitle'

const CardContent = React.forwardRef<HTMLDivElement, React.HTMLAttributes<HTMLDivElement>>(
  ({ className, ...props }, ref) => (
    <div ref={ref} className={cn('p-6 pt-0', className)} {...props} />
  )
)
CardContent.displayName = 'CardContent'

export { Card, CardHeader, CardTitle, CardContent }
```

Create `web/src/components/ui/badge.tsx`:

```tsx
import { cva, type VariantProps } from 'class-variance-authority'
import { cn } from '@/lib/utils'

const badgeVariants = cva(
  'inline-flex items-center rounded-full border px-2.5 py-0.5 text-xs font-semibold transition-colors',
  {
    variants: {
      variant: {
        default: 'border-transparent bg-slate-700 text-slate-100',
        published: 'border-transparent bg-emerald-800 text-emerald-200',
        draft: 'border-transparent bg-amber-800 text-amber-200',
        prompt: 'border-transparent bg-blue-800 text-blue-200',
        pattern: 'border-transparent bg-purple-800 text-purple-200',
        workflow: 'border-transparent bg-cyan-800 text-cyan-200',
        domain_fact: 'border-transparent bg-slate-700 text-slate-200',
        anti_pattern: 'border-transparent bg-red-800 text-red-200',
      },
    },
    defaultVariants: { variant: 'default' },
  }
)

export interface BadgeProps extends React.HTMLAttributes<HTMLDivElement>, VariantProps<typeof badgeVariants> {}

function Badge({ className, variant, ...props }: BadgeProps) {
  return <div className={cn(badgeVariants({ variant }), className)} {...props} />
}

export { Badge, badgeVariants }
```

Create `web/src/components/ui/input.tsx`:

```tsx
import * as React from 'react'
import { cn } from '@/lib/utils'

export interface InputProps extends React.InputHTMLAttributes<HTMLInputElement> {}

const Input = React.forwardRef<HTMLInputElement, InputProps>(
  ({ className, type, ...props }, ref) => (
    <input
      type={type}
      className={cn(
        'flex h-9 w-full rounded-md border border-slate-600 bg-slate-800 px-3 py-1 text-sm text-slate-100 shadow-sm placeholder:text-slate-500 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-slate-400 disabled:cursor-not-allowed disabled:opacity-50',
        className
      )}
      ref={ref}
      {...props}
    />
  )
)
Input.displayName = 'Input'

export { Input }
```

- [ ] **Step 9: Verify TypeScript build succeeds**

```bash
cd /Users/dsandor/Projects/memory/web && npm run build 2>&1 | tail -10
```

Expected: build succeeds, `dist/` appears at `internal/web/dist/`.

---

## Task 6: API client + routing + layout

**Files:**
- Create: `web/src/lib/api.ts`
- Replace: `web/src/App.tsx`
- Create: `web/src/components/Layout.tsx`

- [ ] **Step 1: Create web/src/lib/api.ts**

```ts
const BASE = '/api'

export interface KnowledgeEntry {
  ID: string
  Type: string
  Title: string
  Content: string
  Description: string
  Domain: string
  Tags: string[]
  Author: string
  Team: string
  CreatedAt: string
  UpdatedAt: string
  Version: number
  Rating: number
  UsageCount: number
}

export interface Cluster {
  ID: string
  Domain: string
  Title: string
  Summary: string
  EntryIDs: string[]
  QualityScore: number
  PipelineRunID: string
  CreatedAt: string
}

export interface DatasetSnapshot {
  ID: string
  Version: number
  ClusterCount: number
  EntryCount: number
  Data: string
  PipelineRunID: string
  CreatedAt: string
}

export interface Agent {
  ID: string
  Domain: string
  Version: number
  Status: 'draft' | 'published'
  SystemPrompt: string
  Instructions: string
  AntiPatterns: string
  SourceRefs: string[]
  ClusterID: string
  CreatedAt: string
  UpdatedAt: string
}

export interface AgentVersion {
  ID: string
  AgentID: string
  Version: number
  SystemPrompt: string
  Instructions: string
  AntiPatterns: string
  Changelog: string
  CreatedAt: string
}

export interface Stats {
  knowledge_count: number
  cluster_count: number
  agent_count: number
  pipeline_status: string
  pipeline_last_run: string | null
}

export interface PipelineStatus {
  ID: string
  Status: string
  Trigger: string
  EntriesProcessed: number
  ClustersFound: number
  Errors: string[]
  StartedAt: string
  CompletedAt: string | null
}

async function get<T>(path: string): Promise<T> {
  const r = await fetch(BASE + path)
  if (!r.ok) throw new Error(`${r.status} ${r.statusText}`)
  return r.json()
}

async function put<T>(path: string, body?: unknown): Promise<T> {
  const r = await fetch(BASE + path, {
    method: 'PUT',
    headers: body ? { 'Content-Type': 'application/json' } : {},
    body: body ? JSON.stringify(body) : undefined,
  })
  if (!r.ok) throw new Error(`${r.status} ${r.statusText}`)
  return r.json()
}

export const api = {
  stats: (): Promise<Stats> => get('/stats'),

  knowledge: {
    list: (params: { limit?: number; offset?: number; domain?: string; type?: string; search?: string } = {}): Promise<KnowledgeEntry[]> => {
      const q = new URLSearchParams()
      if (params.limit) q.set('limit', String(params.limit))
      if (params.offset) q.set('offset', String(params.offset))
      if (params.domain) q.set('domain', params.domain)
      if (params.type) q.set('type', params.type)
      if (params.search) q.set('search', params.search)
      return get(`/knowledge?${q}`)
    },
    get: (id: string): Promise<KnowledgeEntry> => get(`/knowledge/${id}`),
    rate: (id: string, rating: number): Promise<{ ok: boolean }> =>
      put(`/knowledge/${id}/rate`, { rating }),
  },

  clusters: {
    list: (): Promise<Cluster[]> => get('/clusters'),
  },

  datasets: {
    list: (): Promise<DatasetSnapshot[]> => get('/datasets'),
    exportUrl: (id: string, format: 'json' | 'csv') => `${BASE}/datasets/${id}/export?format=${format}`,
  },

  agents: {
    list: (): Promise<Agent[]> => get('/agents'),
    get: (id: string): Promise<{ agent: Agent; versions: AgentVersion[] }> => get(`/agents/${id}`),
    publish: (id: string): Promise<{ ok: boolean }> => put(`/agents/${id}/publish`),
    exportUrl: (id: string, format: 'md' | 'txt' | 'json') => `${BASE}/agents/${id}/export?format=${format}`,
    bulkExportUrl: () => `${BASE}/agents/bulk-export`,
  },

  pipeline: {
    status: (): Promise<PipelineStatus | { status: string }> => get('/pipeline/status'),
  },
}
```

- [ ] **Step 2: Replace web/src/App.tsx**

```tsx
import { BrowserRouter, Routes, Route, Navigate } from 'react-router-dom'
import Layout from '@/components/Layout'
import Dashboard from '@/pages/Dashboard'
import KnowledgeBrowser from '@/pages/KnowledgeBrowser'
import KnowledgeDetail from '@/pages/KnowledgeDetail'
import Clusters from '@/pages/Clusters'
import Datasets from '@/pages/Datasets'
import Agents from '@/pages/Agents'
import AgentDetail from '@/pages/AgentDetail'

export default function App() {
  return (
    <BrowserRouter>
      <Routes>
        <Route path="/" element={<Layout />}>
          <Route index element={<Navigate to="/dashboard" replace />} />
          <Route path="dashboard" element={<Dashboard />} />
          <Route path="knowledge" element={<KnowledgeBrowser />} />
          <Route path="knowledge/:id" element={<KnowledgeDetail />} />
          <Route path="clusters" element={<Clusters />} />
          <Route path="datasets" element={<Datasets />} />
          <Route path="agents" element={<Agents />} />
          <Route path="agents/:id" element={<AgentDetail />} />
        </Route>
      </Routes>
    </BrowserRouter>
  )
}
```

- [ ] **Step 3: Create web/src/components/Layout.tsx**

```tsx
import { Link, Outlet, useLocation } from 'react-router-dom'
import { Brain, BookOpen, Network, Database, Bot, LayoutDashboard } from 'lucide-react'
import { cn } from '@/lib/utils'

const nav = [
  { to: '/dashboard', label: 'Dashboard', Icon: LayoutDashboard },
  { to: '/knowledge', label: 'Knowledge', Icon: BookOpen },
  { to: '/clusters', label: 'Clusters', Icon: Network },
  { to: '/datasets', label: 'Datasets', Icon: Database },
  { to: '/agents', label: 'Agents', Icon: Bot },
]

export default function Layout() {
  const { pathname } = useLocation()
  return (
    <div className="flex h-screen overflow-hidden bg-[#0f1117]">
      <aside className="w-56 shrink-0 border-r border-slate-800 bg-slate-950 flex flex-col">
        <div className="flex items-center gap-2 px-5 py-4 border-b border-slate-800">
          <Brain className="h-6 w-6 text-emerald-400" />
          <span className="font-semibold text-slate-100 text-sm">Tribal Knowledge</span>
        </div>
        <nav className="flex-1 px-2 py-3 space-y-0.5">
          {nav.map(({ to, label, Icon }) => (
            <Link
              key={to}
              to={to}
              className={cn(
                'flex items-center gap-3 rounded-md px-3 py-2 text-sm font-medium transition-colors',
                pathname.startsWith(to)
                  ? 'bg-slate-800 text-slate-100'
                  : 'text-slate-400 hover:bg-slate-800 hover:text-slate-200'
              )}
            >
              <Icon className="h-4 w-4 shrink-0" />
              {label}
            </Link>
          ))}
        </nav>
      </aside>
      <main className="flex-1 overflow-auto p-6">
        <Outlet />
      </main>
    </div>
  )
}
```

- [ ] **Step 4: Replace web/src/main.tsx**

```tsx
import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import './index.css'
import App from './App.tsx'

createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <App />
  </StrictMode>,
)
```

- [ ] **Step 5: Verify build**

```bash
cd /Users/dsandor/Projects/memory/web && npm run build 2>&1 | tail -5
```

Expected: build succeeds.

---

## Task 7: Dashboard + Knowledge Browser + Knowledge Detail pages

**Files:**
- Create: `web/src/pages/Dashboard.tsx`
- Create: `web/src/pages/KnowledgeBrowser.tsx`
- Create: `web/src/pages/KnowledgeDetail.tsx`

- [ ] **Step 1: Create web/src/pages/Dashboard.tsx**

```tsx
import { useEffect, useState } from 'react'
import { api, type Stats, type KnowledgeEntry } from '@/lib/api'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Badge } from '@/components/ui/badge'
import { BookOpen, Network, Bot, Activity } from 'lucide-react'
import { Link } from 'react-router-dom'

function StatCard({ title, value, icon: Icon, color }: { title: string; value: number | string; icon: React.ElementType; color: string }) {
  return (
    <Card>
      <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
        <CardTitle className="text-sm font-medium text-slate-400">{title}</CardTitle>
        <Icon className={`h-4 w-4 ${color}`} />
      </CardHeader>
      <CardContent>
        <div className="text-2xl font-bold">{value}</div>
      </CardContent>
    </Card>
  )
}

export default function Dashboard() {
  const [stats, setStats] = useState<Stats | null>(null)
  const [recent, setRecent] = useState<KnowledgeEntry[]>([])
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    Promise.all([api.stats(), api.knowledge.list({ limit: 5 })])
      .then(([s, entries]) => { setStats(s); setRecent(entries) })
      .catch(e => setError(e.message))
  }, [])

  if (error) return <p className="text-red-400">Error: {error}</p>
  if (!stats) return <p className="text-slate-500">Loading…</p>

  return (
    <div className="space-y-6">
      <h1 className="text-xl font-semibold text-slate-100">Dashboard</h1>

      <div className="grid gap-4 grid-cols-2 lg:grid-cols-4">
        <StatCard title="Knowledge Entries" value={stats.knowledge_count} icon={BookOpen} color="text-blue-400" />
        <StatCard title="Clusters" value={stats.cluster_count} icon={Network} color="text-purple-400" />
        <StatCard title="Agents" value={stats.agent_count} icon={Bot} color="text-emerald-400" />
        <StatCard title="Pipeline" value={stats.pipeline_status} icon={Activity} color="text-amber-400" />
      </div>

      {stats.pipeline_last_run && (
        <p className="text-xs text-slate-500">
          Last pipeline run: {new Date(stats.pipeline_last_run).toLocaleString()}
        </p>
      )}

      <div>
        <h2 className="text-sm font-semibold text-slate-400 mb-3">Recent Knowledge Entries</h2>
        <div className="space-y-2">
          {recent.map(e => (
            <Link key={e.ID} to={`/knowledge/${e.ID}`} className="block">
              <Card className="hover:border-slate-600 transition-colors">
                <CardContent className="py-3 px-4 flex items-center justify-between">
                  <div>
                    <p className="text-sm font-medium text-slate-200">{e.Title}</p>
                    <p className="text-xs text-slate-500">{e.Domain || 'no domain'} · {new Date(e.CreatedAt).toLocaleDateString()}</p>
                  </div>
                  <Badge variant={e.Type as any}>{e.Type}</Badge>
                </CardContent>
              </Card>
            </Link>
          ))}
        </div>
      </div>
    </div>
  )
}
```

- [ ] **Step 2: Create web/src/pages/KnowledgeBrowser.tsx**

```tsx
import { useEffect, useState } from 'react'
import { Link } from 'react-router-dom'
import { api, type KnowledgeEntry } from '@/lib/api'
import { Card, CardContent } from '@/components/ui/card'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { ChevronLeft, ChevronRight, Search } from 'lucide-react'

const PAGE_SIZE = 20
const TYPES = ['', 'prompt', 'pattern', 'workflow', 'domain_fact', 'anti_pattern']

export default function KnowledgeBrowser() {
  const [entries, setEntries] = useState<KnowledgeEntry[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [page, setPage] = useState(0)
  const [search, setSearch] = useState('')
  const [searchInput, setSearchInput] = useState('')
  const [domain, setDomain] = useState('')
  const [type, setType] = useState('')

  useEffect(() => {
    setLoading(true)
    api.knowledge
      .list({ limit: PAGE_SIZE, offset: page * PAGE_SIZE, search, domain, type })
      .then(setEntries)
      .catch(e => setError(e.message))
      .finally(() => setLoading(false))
  }, [page, search, domain, type])

  const handleSearch = () => { setSearch(searchInput); setPage(0) }

  if (error) return <p className="text-red-400">Error: {error}</p>

  return (
    <div className="space-y-4">
      <h1 className="text-xl font-semibold text-slate-100">Knowledge Browser</h1>

      <div className="flex gap-2 flex-wrap">
        <div className="flex gap-2 flex-1 min-w-64">
          <Input
            placeholder="Search title or content…"
            value={searchInput}
            onChange={e => setSearchInput(e.target.value)}
            onKeyDown={e => e.key === 'Enter' && handleSearch()}
          />
          <Button size="icon" variant="outline" onClick={handleSearch}><Search className="h-4 w-4" /></Button>
        </div>
        <select
          value={type}
          onChange={e => { setType(e.target.value); setPage(0) }}
          className="h-9 rounded-md border border-slate-600 bg-slate-800 px-3 text-sm text-slate-300"
        >
          {TYPES.map(t => <option key={t} value={t}>{t || 'All types'}</option>)}
        </select>
        <Input
          placeholder="Domain filter"
          value={domain}
          onChange={e => { setDomain(e.target.value); setPage(0) }}
          className="w-40"
        />
      </div>

      {loading ? (
        <p className="text-slate-500">Loading…</p>
      ) : (
        <div className="space-y-2">
          {entries.length === 0 && <p className="text-slate-500">No entries found.</p>}
          {entries.map(e => (
            <Link key={e.ID} to={`/knowledge/${e.ID}`}>
              <Card className="hover:border-slate-600 transition-colors cursor-pointer">
                <CardContent className="py-3 px-4 flex items-start justify-between gap-4">
                  <div className="flex-1 min-w-0">
                    <p className="text-sm font-medium text-slate-200 truncate">{e.Title}</p>
                    <p className="text-xs text-slate-500 mt-0.5">
                      {e.Domain || 'no domain'} · {e.Author || 'unknown'} · ★ {e.Rating.toFixed(1)}
                    </p>
                  </div>
                  <Badge variant={e.Type as any} className="shrink-0">{e.Type}</Badge>
                </CardContent>
              </Card>
            </Link>
          ))}
        </div>
      )}

      <div className="flex items-center gap-3">
        <Button variant="outline" size="sm" onClick={() => setPage(p => Math.max(0, p - 1))} disabled={page === 0}>
          <ChevronLeft className="h-4 w-4" />
        </Button>
        <span className="text-sm text-slate-400">Page {page + 1}</span>
        <Button variant="outline" size="sm" onClick={() => setPage(p => p + 1)} disabled={entries.length < PAGE_SIZE}>
          <ChevronRight className="h-4 w-4" />
        </Button>
      </div>
    </div>
  )
}
```

- [ ] **Step 3: Create web/src/pages/KnowledgeDetail.tsx**

```tsx
import { useEffect, useState } from 'react'
import { useParams, useNavigate } from 'react-router-dom'
import { api, type KnowledgeEntry } from '@/lib/api'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { ArrowLeft, Star } from 'lucide-react'

export default function KnowledgeDetail() {
  const { id } = useParams<{ id: string }>()
  const navigate = useNavigate()
  const [entry, setEntry] = useState<KnowledgeEntry | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [rating, setRating] = useState(0)
  const [ratingSaving, setRatingSaving] = useState(false)

  useEffect(() => {
    if (!id) return
    api.knowledge.get(id)
      .then(e => { setEntry(e); setRating(e.Rating) })
      .catch(e => setError(e.message))
  }, [id])

  const saveRating = async () => {
    if (!id) return
    setRatingSaving(true)
    try {
      await api.knowledge.rate(id, rating)
      setEntry(prev => prev ? { ...prev, Rating: rating } : prev)
    } catch (e: any) {
      setError(e.message)
    } finally {
      setRatingSaving(false)
    }
  }

  if (error) return <p className="text-red-400">Error: {error}</p>
  if (!entry) return <p className="text-slate-500">Loading…</p>

  return (
    <div className="space-y-4 max-w-3xl">
      <div className="flex items-center gap-3">
        <Button variant="ghost" size="sm" onClick={() => navigate(-1)}>
          <ArrowLeft className="h-4 w-4 mr-1" />Back
        </Button>
        <h1 className="text-xl font-semibold text-slate-100 flex-1">{entry.Title}</h1>
        <Badge variant={entry.Type as any}>{entry.Type}</Badge>
      </div>

      <div className="flex gap-2 flex-wrap text-xs text-slate-500">
        {entry.Domain && <span className="bg-slate-800 rounded px-2 py-0.5">{entry.Domain}</span>}
        {entry.Author && <span>{entry.Author}</span>}
        {entry.Tags?.map(t => <span key={t} className="bg-slate-800 rounded px-2 py-0.5">#{t}</span>)}
        <span>{new Date(entry.CreatedAt).toLocaleDateString()}</span>
        <span>{entry.UsageCount} uses</span>
      </div>

      {entry.Description && (
        <Card>
          <CardHeader><CardTitle className="text-sm">Description</CardTitle></CardHeader>
          <CardContent><p className="text-sm text-slate-300">{entry.Description}</p></CardContent>
        </Card>
      )}

      <Card>
        <CardHeader><CardTitle className="text-sm">Content</CardTitle></CardHeader>
        <CardContent>
          <pre className="text-sm text-slate-300 whitespace-pre-wrap font-mono leading-relaxed">{entry.Content}</pre>
        </CardContent>
      </Card>

      <Card>
        <CardHeader><CardTitle className="text-sm">Rating</CardTitle></CardHeader>
        <CardContent className="flex items-center gap-3">
          <div className="flex gap-1">
            {[1, 2, 3, 4, 5].map(n => (
              <button key={n} onClick={() => setRating(n)} className="focus:outline-none">
                <Star className={`h-6 w-6 transition-colors ${n <= rating ? 'text-amber-400 fill-amber-400' : 'text-slate-600'}`} />
              </button>
            ))}
          </div>
          <span className="text-sm text-slate-400">{rating.toFixed(1)}</span>
          <Button size="sm" onClick={saveRating} disabled={ratingSaving}>
            {ratingSaving ? 'Saving…' : 'Save Rating'}
          </Button>
        </CardContent>
      </Card>
    </div>
  )
}
```

- [ ] **Step 4: Verify build**

```bash
cd /Users/dsandor/Projects/memory/web && npm run build 2>&1 | tail -5
```

Expected: build succeeds.

---

## Task 8: Clusters + Datasets pages

**Files:**
- Create: `web/src/pages/Clusters.tsx`
- Create: `web/src/pages/Datasets.tsx`

- [ ] **Step 1: Create web/src/pages/Clusters.tsx**

```tsx
import { useEffect, useState } from 'react'
import { api, type Cluster } from '@/lib/api'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Badge } from '@/components/ui/badge'
import { ChevronDown, ChevronRight, Network } from 'lucide-react'

export default function Clusters() {
  const [clusters, setClusters] = useState<Cluster[]>([])
  const [error, setError] = useState<string | null>(null)
  const [expanded, setExpanded] = useState<Set<string>>(new Set())

  useEffect(() => {
    api.clusters.list()
      .then(setClusters)
      .catch(e => setError(e.message))
  }, [])

  const toggle = (id: string) =>
    setExpanded(prev => { const s = new Set(prev); s.has(id) ? s.delete(id) : s.add(id); return s })

  if (error) return <p className="text-red-400">Error: {error}</p>

  return (
    <div className="space-y-4">
      <div className="flex items-center gap-2">
        <Network className="h-5 w-5 text-purple-400" />
        <h1 className="text-xl font-semibold text-slate-100">Clusters</h1>
        <Badge className="ml-1">{clusters.length}</Badge>
      </div>

      {clusters.length === 0 && <p className="text-slate-500">No clusters yet. Run the analysis pipeline to generate clusters.</p>}

      <div className="space-y-2">
        {clusters.map(c => (
          <Card key={c.ID} className="cursor-pointer hover:border-slate-600 transition-colors" onClick={() => toggle(c.ID)}>
            <CardContent className="py-3 px-4">
              <div className="flex items-center gap-3">
                {expanded.has(c.ID) ? <ChevronDown className="h-4 w-4 text-slate-400 shrink-0" /> : <ChevronRight className="h-4 w-4 text-slate-400 shrink-0" />}
                <div className="flex-1 min-w-0">
                  <p className="text-sm font-medium text-slate-200">{c.Title}</p>
                  <div className="flex gap-3 mt-0.5 text-xs text-slate-500">
                    {c.Domain && <span>{c.Domain}</span>}
                    <span>{c.EntryIDs?.length ?? 0} entries</span>
                    <span>quality {c.QualityScore.toFixed(2)}</span>
                  </div>
                </div>
              </div>
              {expanded.has(c.ID) && c.Summary && (
                <div className="mt-3 pl-7 text-sm text-slate-300 border-t border-slate-800 pt-3">
                  {c.Summary}
                </div>
              )}
            </CardContent>
          </Card>
        ))}
      </div>
    </div>
  )
}
```

- [ ] **Step 2: Create web/src/pages/Datasets.tsx**

```tsx
import { useEffect, useState } from 'react'
import { api, type DatasetSnapshot } from '@/lib/api'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Button } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'
import { Database, Download } from 'lucide-react'

export default function Datasets() {
  const [snaps, setSnaps] = useState<DatasetSnapshot[]>([])
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    api.datasets.list()
      .then(setSnaps)
      .catch(e => setError(e.message))
  }, [])

  const download = (id: string, format: 'json' | 'csv') => {
    window.location.href = api.datasets.exportUrl(id, format)
  }

  if (error) return <p className="text-red-400">Error: {error}</p>

  return (
    <div className="space-y-4">
      <div className="flex items-center gap-2">
        <Database className="h-5 w-5 text-cyan-400" />
        <h1 className="text-xl font-semibold text-slate-100">Datasets</h1>
        <Badge className="ml-1">{snaps.length}</Badge>
      </div>

      {snaps.length === 0 && <p className="text-slate-500">No dataset snapshots yet.</p>}

      <div className="space-y-2">
        {snaps.map(s => (
          <Card key={s.ID}>
            <CardContent className="py-3 px-4 flex items-center justify-between">
              <div>
                <p className="text-sm font-medium text-slate-200">Snapshot v{s.Version}</p>
                <p className="text-xs text-slate-500 mt-0.5">
                  {s.EntryCount} entries · {s.ClusterCount} clusters · {new Date(s.CreatedAt).toLocaleString()}
                </p>
              </div>
              <div className="flex gap-2">
                <Button size="sm" variant="outline" onClick={() => download(s.ID, 'json')}>
                  <Download className="h-3 w-3 mr-1" />JSON
                </Button>
                <Button size="sm" variant="outline" onClick={() => download(s.ID, 'csv')}>
                  <Download className="h-3 w-3 mr-1" />CSV
                </Button>
              </div>
            </CardContent>
          </Card>
        ))}
      </div>
    </div>
  )
}
```

- [ ] **Step 3: Verify build**

```bash
cd /Users/dsandor/Projects/memory/web && npm run build 2>&1 | tail -5
```

Expected: build succeeds.

---

## Task 9: Agents + Agent Detail pages

**Files:**
- Create: `web/src/pages/Agents.tsx`
- Create: `web/src/pages/AgentDetail.tsx`

- [ ] **Step 1: Create web/src/pages/Agents.tsx**

```tsx
import { useEffect, useState } from 'react'
import { Link } from 'react-router-dom'
import { api, type Agent } from '@/lib/api'
import { Card, CardContent } from '@/components/ui/card'
import { Button } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'
import { Bot, Download } from 'lucide-react'

export default function Agents() {
  const [agents, setAgents] = useState<Agent[]>([])
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    api.agents.list()
      .then(setAgents)
      .catch(e => setError(e.message))
  }, [])

  if (error) return <p className="text-red-400">Error: {error}</p>

  return (
    <div className="space-y-4">
      <div className="flex items-center justify-between">
        <div className="flex items-center gap-2">
          <Bot className="h-5 w-5 text-emerald-400" />
          <h1 className="text-xl font-semibold text-slate-100">Agents</h1>
          <Badge className="ml-1">{agents.length}</Badge>
        </div>
        {agents.length > 0 && (
          <a href={api.agents.bulkExportUrl()}>
            <Button variant="outline" size="sm">
              <Download className="h-4 w-4 mr-1" />Bulk Export (ZIP)
            </Button>
          </a>
        )}
      </div>

      {agents.length === 0 && <p className="text-slate-500">No agents generated yet. The pipeline creates agents from clusters.</p>}

      <div className="space-y-2">
        {agents.map(a => (
          <Link key={a.ID} to={`/agents/${a.ID}`}>
            <Card className="hover:border-slate-600 transition-colors cursor-pointer">
              <CardContent className="py-3 px-4 flex items-center justify-between">
                <div>
                  <p className="text-sm font-medium text-slate-200 capitalize">{a.Domain}</p>
                  <p className="text-xs text-slate-500 mt-0.5">
                    v{a.Version} · updated {new Date(a.UpdatedAt).toLocaleDateString()}
                  </p>
                </div>
                <Badge variant={a.Status}>{a.Status}</Badge>
              </CardContent>
            </Card>
          </Link>
        ))}
      </div>
    </div>
  )
}
```

- [ ] **Step 2: Create web/src/pages/AgentDetail.tsx**

```tsx
import { useEffect, useState } from 'react'
import { useParams, useNavigate } from 'react-router-dom'
import { api, type Agent, type AgentVersion } from '@/lib/api'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Button } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'
import { ArrowLeft, Download, CheckCircle } from 'lucide-react'

export default function AgentDetail() {
  const { id } = useParams<{ id: string }>()
  const navigate = useNavigate()
  const [agent, setAgent] = useState<Agent | null>(null)
  const [versions, setVersions] = useState<AgentVersion[]>([])
  const [error, setError] = useState<string | null>(null)
  const [publishing, setPublishing] = useState(false)

  useEffect(() => {
    if (!id) return
    api.agents.get(id)
      .then(({ agent, versions }) => { setAgent(agent); setVersions(versions) })
      .catch(e => setError(e.message))
  }, [id])

  const publish = async () => {
    if (!id) return
    setPublishing(true)
    try {
      await api.agents.publish(id)
      setAgent(prev => prev ? { ...prev, Status: 'published' } : prev)
    } catch (e: any) {
      setError(e.message)
    } finally {
      setPublishing(false)
    }
  }

  if (error) return <p className="text-red-400">Error: {error}</p>
  if (!agent) return <p className="text-slate-500">Loading…</p>

  return (
    <div className="space-y-4 max-w-3xl">
      <div className="flex items-center gap-3 flex-wrap">
        <Button variant="ghost" size="sm" onClick={() => navigate(-1)}>
          <ArrowLeft className="h-4 w-4 mr-1" />Back
        </Button>
        <h1 className="text-xl font-semibold text-slate-100 capitalize flex-1">{agent.Domain} Agent</h1>
        <Badge variant={agent.Status}>{agent.Status}</Badge>
        <span className="text-xs text-slate-500">v{agent.Version}</span>
      </div>

      <div className="flex gap-2 flex-wrap">
        {agent.Status === 'draft' && (
          <Button variant="success" size="sm" onClick={publish} disabled={publishing}>
            <CheckCircle className="h-4 w-4 mr-1" />
            {publishing ? 'Publishing…' : 'Publish Agent'}
          </Button>
        )}
        {(['md', 'txt', 'json'] as const).map(fmt => (
          <a key={fmt} href={api.agents.exportUrl(agent.ID, fmt)}>
            <Button variant="outline" size="sm">
              <Download className="h-3 w-3 mr-1" />.{fmt}
            </Button>
          </a>
        ))}
      </div>

      <Card>
        <CardHeader><CardTitle className="text-sm">System Prompt</CardTitle></CardHeader>
        <CardContent>
          <pre className="text-sm text-slate-300 whitespace-pre-wrap font-mono leading-relaxed">{agent.SystemPrompt || '—'}</pre>
        </CardContent>
      </Card>

      {agent.Instructions && (
        <Card>
          <CardHeader><CardTitle className="text-sm">Instructions</CardTitle></CardHeader>
          <CardContent>
            <pre className="text-sm text-slate-300 whitespace-pre-wrap font-mono leading-relaxed">{agent.Instructions}</pre>
          </CardContent>
        </Card>
      )}

      {agent.AntiPatterns && (
        <Card>
          <CardHeader><CardTitle className="text-sm">Anti-Patterns</CardTitle></CardHeader>
          <CardContent>
            <pre className="text-sm text-slate-300 whitespace-pre-wrap font-mono leading-relaxed">{agent.AntiPatterns}</pre>
          </CardContent>
        </Card>
      )}

      {agent.SourceRefs && agent.SourceRefs.length > 0 && (
        <Card>
          <CardHeader><CardTitle className="text-sm">Source Knowledge Refs</CardTitle></CardHeader>
          <CardContent>
            <ul className="space-y-1">
              {agent.SourceRefs.map(ref => (
                <li key={ref} className="text-xs text-slate-400 font-mono">{ref}</li>
              ))}
            </ul>
          </CardContent>
        </Card>
      )}

      {versions.length > 0 && (
        <Card>
          <CardHeader><CardTitle className="text-sm">Version History</CardTitle></CardHeader>
          <CardContent className="space-y-3">
            {versions.map(v => (
              <div key={v.ID} className="border-l-2 border-slate-700 pl-3">
                <p className="text-xs font-medium text-slate-300">v{v.Version} — {new Date(v.CreatedAt).toLocaleDateString()}</p>
                {v.Changelog && <p className="text-xs text-slate-500 mt-0.5 whitespace-pre-wrap">{v.Changelog}</p>}
              </div>
            ))}
          </CardContent>
        </Card>
      )}
    </div>
  )
}
```

- [ ] **Step 3: Verify build**

```bash
cd /Users/dsandor/Projects/memory/web && npm run build 2>&1 | tail -5
```

Expected: build succeeds, no TypeScript errors.

---

## Task 10: Makefile + end-to-end build verification

**Files:**
- Create: `Makefile`
- Update: `.planning/STATE.md`

- [ ] **Step 1: Create Makefile**

```makefile
.PHONY: web build clean test

web:
	cd web && npm install && npm run build

build: web
	CGO_ENABLED=1 go build -o tribal-knowledge ./cmd/server/

test:
	CGO_ENABLED=1 go test ./...

clean:
	rm -f tribal-knowledge
	rm -rf internal/web/dist
	mkdir -p internal/web/dist
	echo '<!DOCTYPE html><html><body>Run make web</body></html>' > internal/web/dist/index.html
```

- [ ] **Step 2: Run make build end-to-end**

```bash
cd /Users/dsandor/Projects/memory && make build 2>&1 | tail -15
```

Expected: Vite build succeeds, then `go build` succeeds, `tribal-knowledge` binary is created.

- [ ] **Step 3: Start the binary and verify HTTP server**

```bash
cd /Users/dsandor/Projects/memory && ./tribal-knowledge &
sleep 1
curl -s http://localhost:8080/api/stats | python3 -m json.tool
```

Expected: JSON response with `knowledge_count`, `cluster_count`, `agent_count`, `pipeline_status` fields.

- [ ] **Step 4: Verify SPA fallback works**

```bash
curl -s -o /dev/null -w "%{http_code}" http://localhost:8080/agents/some-fake-id
```

Expected: `200` (SPA fallback serves index.html).

- [ ] **Step 5: Stop the binary**

```bash
kill %1 2>/dev/null || pkill tribal-knowledge
```

- [ ] **Step 6: Run full test suite**

```bash
cd /Users/dsandor/Projects/memory && CGO_ENABLED=1 go test ./... 2>&1 | grep -E "^ok|FAIL"
```

Expected: all packages `ok`, no `FAIL`.

- [ ] **Step 7: Clean up binary**

```bash
rm -f /Users/dsandor/Projects/memory/tribal-knowledge
```

- [ ] **Step 8: Update STATE.md**

Update `.planning/STATE.md`:
- Change Phase 4 status from `pending` to `complete`
- Add the plan path: `[2026-06-06-phase4-embedded-web-ui.md](../docs/superpowers/plans/2026-06-06-phase4-embedded-web-ui.md)`
- Add note: "Phase 4 complete: React SPA embedded in Go binary via go:embed; REST API (stats/knowledge/clusters/datasets/agents); download flows (agent md/txt/json, bulk ZIP, dataset JSON/CSV); SPA fallback routing; dark theme with shadcn/ui"

---

## Self-Review

### Spec coverage

| Requirement | Task |
|-------------|------|
| React + TypeScript + Vite in `web/` | Task 5 |
| shadcn/ui + Tailwind dark theme | Task 5 |
| Dashboard page | Task 7 |
| Knowledge Browser with search/filter/pagination | Task 7 |
| Entry detail view with inline rating | Task 7 |
| Clusters view with entry counts + LLM summaries | Task 8 |
| Datasets view with snapshot history | Task 8 |
| Dataset export JSON/CSV | Task 8 + Task 2 `handleDatasetExport` |
| Agents view with version history + status badges | Task 9 |
| Agent Detail with definition + source refs + version diff + approve/reject | Task 9 |
| Download: single agent (md/txt/json) | Task 2 `handleAgentExport` + Task 9 export buttons |
| Download: bulk ZIP | Task 2 `handleAgentBulkExport` + Task 9 bulk button |
| `go:embed dist/` integration | Task 4 `embed.go` + vite `outDir` in Task 5 |
| `go build` produces single binary | Task 10 |
| `http://localhost:8080` shows live data | Task 10 verification |

### Placeholder scan

No TBD, TODO, or incomplete steps.

### Type consistency

- `AllStore` extends `storage.AgentStore` — `*SQLiteStore` satisfies it, including new `RateEntry` (via `Store`) and `ListSnapshots` (via `AnalysisStore`)
- `StatsResponse` is defined in `handlers.go` and exported; test file uses `web.StatsResponse`
- `api.ts` types match Go JSON output: Go's `json.Encode` uses field names as-is (capitalized) since the storage structs use capitalized fields without json tags — frontend uses `e.ID`, `e.Title` etc. matching exactly
- `Badge` variant props include all agent statuses (`draft`, `published`) and knowledge types
- React Router `useParams<{ id: string }>()` matches route `path="agents/:id"` and `path="knowledge/:id"`
