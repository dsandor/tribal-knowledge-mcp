# Resumable Background AI Work Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

> **PROJECT RULE — NO GIT COMMITS.** Never run `git add`/`git commit`. Every task ends at "tests pass".

**Goal:** Background AI work heals from failures: interrupted pipeline runs are marked failed at startup, and per-item LLM results (scores, summaries, agent generations) are memoized by content hash so the next run resumes instead of recomputing.

**Architecture:** `MarkInterruptedRuns` flips stale `running` rows to `failed` at boot. An `analysis_cache(kind, key, value)` table memoizes the three expensive per-item LLM calls; cache keys derive from content hashes so edits invalidate naturally; LLM failures simply aren't cached and retry on the next interval run. Pruned (90 days) at the end of successful runs.

**Tech Stack:** Go, SQLite/Postgres dual adapters.

**Spec:** `docs/superpowers/specs/2026-06-11-resumable-pipeline-design.md`

**Key facts:**
- `AnalysisStore` interface: `internal/storage/storage.go:139+`. Pipeline-run methods in `internal/storage/analysis.go` (SQLite) / `postgres_analysis.go` (Postgres). Migrations: SQLite tables in `sqlite.go` `migrate()`; Postgres in `postgres_analysis.go` `migrateAnalysis`.
- Pipeline: `internal/pipeline/pipeline.go` `Run()`; per-item LLM calls: `ScoreEntry`/`SummarizeCluster` in `internal/pipeline/analyze.go` (signatures `(ctx, client, entry) (QualityScore, error)` / `(ctx, client, entries) (SummarizeResult, error)`); agent generation in `generateAgent` (~:502) calling `agent.Generate`.
- Content-hash convention: sha256 hex of title+content (`storage.sha256Hex` is unexported; `internal/mcp.contentHash` mirrors it — the pipeline adds its own small helper).
- `cmd/server/main.go`: startup section near the team backfill + `MarkInterruptedRuns` wiring point.
- Test fakes: `internal/pipeline/testhelpers_test.go` `mockAnalysisStore` (will need the three new methods); web/mcp mocks embed stores satisfying AnalysisStore where applicable — `go build` will list breakages.
- `mockLLM` in pipeline tests records calls (check; if it lacks a call counter, add one in the test file).

---

### Task 1: Storage — `MarkInterruptedRuns` + `analysis_cache` table & methods

**Files:**
- Modify: `internal/storage/storage.go` (AnalysisStore interface), `internal/storage/sqlite.go` (migration), `internal/storage/analysis.go`, `internal/storage/postgres_analysis.go`
- Test: `internal/storage/resumability_test.go` (new; reuse `newTestStoreInternal`)

- [ ] **Step 1: Failing tests**

```go
package storage

import (
	"context"
	"testing"
	"time"
)

func TestMarkInterruptedRuns(t *testing.T) {
	s := newTestStoreInternal(t)
	ctx := context.Background()

	runID, err := s.StartPipelineRun(ctx, "interval", "t1")
	if err != nil {
		t.Fatalf("start run: %v", err)
	}
	doneID, err := s.StartPipelineRun(ctx, "manual", "t1")
	if err != nil {
		t.Fatalf("start done run: %v", err)
	}
	if err := s.FinishPipelineRun(ctx, doneID, "completed", 5, 2, nil); err != nil {
		t.Fatalf("finish run: %v", err)
	}

	n, err := s.MarkInterruptedRuns(ctx)
	if err != nil {
		t.Fatalf("mark interrupted: %v", err)
	}
	if n != 1 {
		t.Fatalf("marked %d runs, want 1", n)
	}

	runs, err := s.ListPipelineRuns(ctx, "t1", 10)
	if err != nil {
		t.Fatalf("list runs: %v", err)
	}
	for _, r := range runs {
		switch r.ID {
		case runID:
			if r.Status != "failed" {
				t.Errorf("interrupted run status = %q, want failed", r.Status)
			}
			if len(r.Errors) == 0 || r.Errors[0] != "interrupted by restart" {
				t.Errorf("interrupted run errors = %v, want [interrupted by restart]", r.Errors)
			}
			if r.CompletedAt == nil {
				t.Error("interrupted run has nil CompletedAt")
			}
		case doneID:
			if r.Status != "completed" {
				t.Errorf("completed run mutated to %q", r.Status)
			}
		}
	}

	// Idempotent: nothing left to mark.
	n2, err := s.MarkInterruptedRuns(ctx)
	if err != nil || n2 != 0 {
		t.Fatalf("second mark = %d err=%v, want 0 nil", n2, err)
	}
}

func TestAnalysisCacheRoundTrip(t *testing.T) {
	s := newTestStoreInternal(t)
	ctx := context.Background()

	if _, ok, err := s.GetAnalysisCache(ctx, "score", "k1"); err != nil || ok {
		t.Fatalf("expected miss, got ok=%v err=%v", ok, err)
	}
	if err := s.PutAnalysisCache(ctx, "score", "k1", `{"coherence":0.8}`, "t1"); err != nil {
		t.Fatalf("put: %v", err)
	}
	v, ok, err := s.GetAnalysisCache(ctx, "score", "k1")
	if err != nil || !ok || v != `{"coherence":0.8}` {
		t.Fatalf("get = %q ok=%v err=%v", v, ok, err)
	}
	// Upsert overwrites.
	if err := s.PutAnalysisCache(ctx, "score", "k1", `{"coherence":0.9}`, "t1"); err != nil {
		t.Fatalf("overwrite: %v", err)
	}
	v, _, _ = s.GetAnalysisCache(ctx, "score", "k1")
	if v != `{"coherence":0.9}` {
		t.Fatalf("after overwrite got %q", v)
	}
	// Kinds are namespaced.
	if _, ok, _ := s.GetAnalysisCache(ctx, "summary", "k1"); ok {
		t.Fatal("kind namespacing broken")
	}
	// Prune with zero cutoff deletes everything (created_at < now).
	time.Sleep(1100 * time.Millisecond) // ensure created_at is in the past at second precision
	deleted, err := s.PruneAnalysisCache(ctx, 0)
	if err != nil || deleted != 1 {
		t.Fatalf("prune = %d err=%v, want 1", deleted, err)
	}
	if _, ok, _ := s.GetAnalysisCache(ctx, "score", "k1"); ok {
		t.Fatal("entry survived prune")
	}
}
```

- [ ] **Step 2:** `cd /Users/dsandor/Projects/memory && go test ./internal/storage/ -run 'Interrupted|AnalysisCache' -v` — compile FAIL.

- [ ] **Step 3: Implement.**

Interface (`storage.go`, AnalysisStore):
```go
	// MarkInterruptedRuns marks every pipeline run still in status "running"
	// as failed with an "interrupted by restart" error. Called at startup —
	// only one process runs the pipeline, so any running row at boot is dead.
	// Returns the number of runs marked.
	MarkInterruptedRuns(ctx context.Context) (int, error)
	// GetAnalysisCache returns the cached LLM result for (kind, key).
	GetAnalysisCache(ctx context.Context, kind, key string) (value string, ok bool, err error)
	// PutAnalysisCache upserts the cached LLM result for (kind, key).
	PutAnalysisCache(ctx context.Context, kind, key, value, teamID string) error
	// PruneAnalysisCache deletes cache rows older than olderThan. Returns rows deleted.
	PruneAnalysisCache(ctx context.Context, olderThan time.Duration) (int, error)
```
(`storage.go` already imports `time`.)

SQLite migration (in `migrate()`, after dataset_snapshots/pipeline_runs creation):
```go
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
```

SQLite methods (`analysis.go`):
```go
func (s *SQLiteStore) MarkInterruptedRuns(ctx context.Context) (int, error) {
	res, err := s.db.ExecContext(ctx,
		`UPDATE pipeline_runs SET status='failed', errors=?, completed_at=CURRENT_TIMESTAMP WHERE status='running'`,
		`["interrupted by restart"]`,
	)
	if err != nil {
		return 0, fmt.Errorf("mark interrupted runs: %w", err)
	}
	n, _ := res.RowsAffected()
	return int(n), nil
}

func (s *SQLiteStore) GetAnalysisCache(ctx context.Context, kind, key string) (string, bool, error) {
	var value string
	err := s.db.QueryRowContext(ctx,
		`SELECT value FROM analysis_cache WHERE kind = ? AND key = ?`, kind, key).Scan(&value)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("get analysis cache: %w", err)
	}
	return value, true, nil
}

func (s *SQLiteStore) PutAnalysisCache(ctx context.Context, kind, key, value, teamID string) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO analysis_cache (kind, key, value, team_id) VALUES (?, ?, ?, ?)
		ON CONFLICT(kind, key) DO UPDATE SET value=excluded.value, team_id=excluded.team_id, created_at=CURRENT_TIMESTAMP
	`, kind, key, value, teamID)
	if err != nil {
		return fmt.Errorf("put analysis cache: %w", err)
	}
	return nil
}

func (s *SQLiteStore) PruneAnalysisCache(ctx context.Context, olderThan time.Duration) (int, error) {
	res, err := s.db.ExecContext(ctx,
		`DELETE FROM analysis_cache WHERE created_at < datetime('now', ?)`,
		fmt.Sprintf("-%d seconds", int(olderThan.Seconds())),
	)
	if err != nil {
		return 0, fmt.Errorf("prune analysis cache: %w", err)
	}
	n, _ := res.RowsAffected()
	return int(n), nil
}
```
(Check `analysis.go`'s imports — add `database/sql`, `errors`, `time` as needed.)

Postgres (`postgres_analysis.go`): table via `CREATE TABLE IF NOT EXISTS analysis_cache (... created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(), PRIMARY KEY (kind, key))` in `migrateAnalysis`; methods mirrored with `$n` placeholders, `ON CONFLICT (kind, key) DO UPDATE`, `NOW()`, and prune via `created_at < NOW() - $1::interval` passing `fmt.Sprintf("%d seconds", int(olderThan.Seconds()))` (or `NOW() - make_interval(secs => $1)` with float seconds — pick one and keep it parameterized).

- [ ] **Step 4:** Fix fakes: `go build ./... 2>&1 | head -20` — add to `mockAnalysisStore` (pipeline), web `mockStore`, mcp mocks as the compiler demands:
```go
func (m *mockAnalysisStore) MarkInterruptedRuns(_ context.Context) (int, error) { return 0, nil }
func (m *mockAnalysisStore) GetAnalysisCache(_ context.Context, _, _ string) (string, bool, error) {
	return "", false, nil
}
func (m *mockAnalysisStore) PutAnalysisCache(_ context.Context, _, _, _, _ string) error { return nil }
func (m *mockAnalysisStore) PruneAnalysisCache(_ context.Context, _ time.Duration) (int, error) {
	return 0, nil
}
```

- [ ] **Step 5:** `go build ./... && go test ./internal/storage/ -run 'Interrupted|AnalysisCache' -v` — PASS; then `go test ./...` — all PASS.

---

### Task 2: Pipeline memoization

**Files:**
- Create: `internal/pipeline/cache.go`
- Modify: `internal/pipeline/pipeline.go` (Run: score loop, summarize call, generateAgent, end-of-run prune)
- Test: `internal/pipeline/cache_test.go` (new)

- [ ] **Step 1: Failing tests.** Read `testhelpers_test.go` first: make `mockAnalysisStore`'s cache methods functional for these tests by adding a real in-memory map implementation on a small wrapper (or change the no-ops to a map — keep other tests passing; a map starting nil and lazily initialized keeps the zero value usable). Ensure `mockLLM` counts Complete calls (add a counter field if missing).

```go
// TestScoreEntryCachedSkipsLLMOnSecondCall: cachedScoreEntry with same entry twice →
// LLM Complete called exactly once; both results equal.
// TestScoreEntryCachedMissOnEditedContent: change entry.Content → second call hits LLM again.
// TestSummarizeClusterCachedKeyOrderIndependent: same member entries in different slice
// order → one LLM call total (sorted member hashes).
// TestCacheLLMErrorNotCached: LLM returns error → no Put; next call retries LLM.
// TestCacheWriteFailureNonFatal: store whose PutAnalysisCache errors → cachedScoreEntry
// still returns the LLM result.
// TestRunPrunesCache: drive the existing minimal Run() fixture → store records that
// PruneAnalysisCache was invoked with 90*24h (add an invoked field to the mock).
```

Write these as real Go tests against the helpers defined in Step 3 (signatures below) — full code, using the package's existing fixtures.

- [ ] **Step 2:** `go test ./internal/pipeline/ -run Cache -v` — FAIL (undefined helpers).

- [ ] **Step 3: Implement `internal/pipeline/cache.go`:**

```go
package pipeline

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"log/slog"
	"sort"
	"strings"

	"github.com/dsandor/memory/internal/llm"
	"github.com/dsandor/memory/internal/storage"
)

// Cache kinds for memoized LLM results.
const (
	cacheKindScore   = "score"
	cacheKindSummary = "summary"
	cacheKindAgent   = "agent"
)

// entryHash returns the content-derived cache key for a single entry,
// mirroring the storage content-hash convention (sha256 of title+content).
func entryHash(e storage.KnowledgeEntry) string {
	h := sha256.Sum256([]byte(e.Title + e.Content))
	return hex.EncodeToString(h[:])
}

// clusterHash returns an order-independent cache key for a set of entries:
// sha256 over the sorted member entry hashes.
func clusterHash(entries []storage.KnowledgeEntry) string {
	hashes := make([]string, len(entries))
	for i, e := range entries {
		hashes[i] = entryHash(e)
	}
	sort.Strings(hashes)
	h := sha256.Sum256([]byte(strings.Join(hashes, "|")))
	return hex.EncodeToString(h[:])
}

// cachedScoreEntry returns the quality score for entry, consulting the
// analysis cache first. LLM failures are returned uncached so the item
// retries on the next run; cache-write failures only log.
func (p *Pipeline) cachedScoreEntry(ctx context.Context, client llm.Client, entry storage.KnowledgeEntry) (QualityScore, error) {
	key := entryHash(entry)
	if raw, ok, err := p.store.GetAnalysisCache(ctx, cacheKindScore, key); err == nil && ok {
		var score QualityScore
		if json.Unmarshal([]byte(raw), &score) == nil {
			score.Total = score.Coherence + score.Specificity
			return score, nil
		}
	}
	score, err := ScoreEntry(ctx, client, entry)
	if err != nil {
		return score, err
	}
	if raw, err := json.Marshal(score); err == nil {
		if err := p.store.PutAnalysisCache(ctx, cacheKindScore, key, string(raw), p.teamID); err != nil {
			slog.Warn("analysis cache: put score", "err", err)
		}
	}
	return score, nil
}

// cachedSummarizeCluster mirrors cachedScoreEntry for cluster summaries.
func (p *Pipeline) cachedSummarizeCluster(ctx context.Context, client llm.Client, entries []storage.KnowledgeEntry) (SummarizeResult, error) {
	key := clusterHash(entries)
	if raw, ok, err := p.store.GetAnalysisCache(ctx, cacheKindSummary, key); err == nil && ok {
		var result SummarizeResult
		if json.Unmarshal([]byte(raw), &result) == nil {
			return result, nil
		}
	}
	result, err := SummarizeCluster(ctx, client, entries)
	if err != nil {
		return result, err
	}
	if raw, err := json.Marshal(result); err == nil {
		if err := p.store.PutAnalysisCache(ctx, cacheKindSummary, key, string(raw), p.teamID); err != nil {
			slog.Warn("analysis cache: put summary", "err", err)
		}
	}
	return result, nil
}
```

For agent generation: read `generateAgent` in pipeline.go and `agent.Generate`'s return type, then add an analogous `cachedGenerateAgent` (same file) keyed by `clusterHash(clusterEntries)` with kind `cacheKindAgent`, marshaling/unmarshaling whatever struct `agent.Generate` returns. Wire `generateAgent` to use it.

Pipeline wiring in `Run()`:
- Replace the direct `ScoreEntry(...)` call with `p.cachedScoreEntry(...)` and `SummarizeCluster(...)` with `p.cachedSummarizeCluster(...)` (read the call sites; behavior on error unchanged).
- At the end of a SUCCESSFUL run (where status "completed" is finalized — read the FinishPipelineRun call sites), add:
```go
	if n, err := p.store.PruneAnalysisCache(ctx, 90*24*time.Hour); err != nil {
		slog.Warn("analysis cache prune failed", "err", err)
	} else if n > 0 {
		slog.Info("analysis cache pruned", "deleted", n)
	}
```

- [ ] **Step 4:** `go test ./internal/pipeline/ -v 2>&1 | grep -E "^(--- FAIL|FAIL|ok)"` — all PASS (existing tests keep passing because mock cache defaults to miss).

---

### Task 3: Startup wiring

**Files:**
- Modify: `cmd/server/main.go` (near the team backfill block)

- [ ] **Step 1: Implement** (match surrounding style/ctx):
```go
	// Any run still "running" at boot belongs to a dead process — mark it
	// failed so the runs list is honest and the next interval run starts clean.
	if n, err := store.MarkInterruptedRuns(bfCtx); err != nil {
		slog.Error("mark interrupted pipeline runs failed", "err", err)
	} else if n > 0 {
		slog.Info("marked interrupted pipeline runs", "count", n)
	}
```
(Reuse the existing context variable near the backfill call; do not fail startup on error.)

- [ ] **Step 2:** `go build ./... && go test ./... 2>&1 | tail -4` — all PASS.

---

### Task 4: Final verification

- [ ] **Step 1:** `go build ./... && go test ./...` — all packages PASS.
- [ ] **Step 2:** `cd web && npm run build` — clean (no frontend changes; regression check).
- [ ] **Step 3:** Migration smoke test against a throwaway DB copy (NEVER the real one):
```bash
cd /Users/dsandor/Projects/memory && cp knowledge.db /tmp/resume-test.db && mkdir -p ./cmd/resumecheck-tmp && cat > ./cmd/resumecheck-tmp/main.go <<'EOF'
package main

import (
	"context"
	"fmt"
	"os"

	"github.com/dsandor/memory/internal/storage"
)

func main() {
	s, err := storage.NewSQLiteStore("/tmp/resume-test.db", 768)
	if err != nil {
		fmt.Println("MIGRATE FAIL:", err)
		os.Exit(1)
	}
	defer s.Close()
	ctx := context.Background()
	n, err := s.MarkInterruptedRuns(ctx)
	if err != nil {
		fmt.Println("MARK FAIL:", err)
		os.Exit(1)
	}
	if err := s.PutAnalysisCache(ctx, "score", "smoke", "{}", ""); err != nil {
		fmt.Println("CACHE PUT FAIL:", err)
		os.Exit(1)
	}
	if _, ok, err := s.GetAnalysisCache(ctx, "score", "smoke"); err != nil || !ok {
		fmt.Println("CACHE GET FAIL:", err)
		os.Exit(1)
	}
	fmt.Printf("OK: analysis_cache migrated, %d stale runs marked\n", n)
}
EOF
go run ./cmd/resumecheck-tmp 2>&1 | grep -v "deprecated\|sqlite3.h\|cgo-gcc\|warning\|^#"; rm -rf ./cmd/resumecheck-tmp /tmp/resume-test.db*
```
Expected: `OK: analysis_cache migrated, N stale runs marked`.
- [ ] **Step 4:** Report. **Do not commit.**
