# Resumable Background AI Work — Design

**Date:** 2026-06-11
**Status:** Approved

## Problem

Background AI work must heal from failures — especially with the new selectable Ollama
provider, where transient errors and slow local models make mid-run failures likely.

Current gaps:
1. A pipeline run interrupted by crash/restart leaves its `pipeline_runs` row stuck in
   `running` forever; nothing detects it.
2. No partial progress survives: a run that fails at entry 30 of 50 recomputes all 50
   LLM calls on the next interval run. Healing works but by brute-force recompute.

Already self-healing (unchanged): interval-based full re-runs; auto-tag backfill stage;
weak-signal improvement re-querying each run; request-path AI (enrich_context,
prompt_suggest) degrades per-response and retries on the next call.

## Decisions (user-approved)

| Decision | Choice |
|----------|--------|
| Scope | Background AI work only (pipeline); request path untouched |
| Visibility | Automatic healing only — slog records + existing failed-runs list; no new retry UI |
| Mechanism | Content-hash memoization of per-item LLM results, not stage checkpointing |

## Components

### 1. Stale-run cleanup

New method on `AnalysisStore` (both adapters):

```go
// MarkInterruptedRuns marks every pipeline run still in status "running" as
// failed with an "interrupted" error and a completion timestamp. Called at
// startup: only one process runs the pipeline, so any "running" row at boot
// belongs to a dead process. Returns the number of runs marked.
MarkInterruptedRuns(ctx context.Context) (int, error)
```

- SQLite: `UPDATE pipeline_runs SET status='failed', errors=?, completed_at=CURRENT_TIMESTAMP WHERE status='running'`
  (errors set to `["interrupted by restart"]` — the JSON array column; merging with prior
  errors is unnecessary since the run never finished).
- Postgres mirror with `NOW()`.
- `cmd/server/main.go`: call after store init (near the team backfill); log
  `slog.Info("marked interrupted pipeline runs", "count", n)` when n > 0; failure logs at
  error and does not prevent startup.

### 2. Analysis cache (memoized LLM results)

New table in both adapters (idempotent migration):

```sql
CREATE TABLE IF NOT EXISTS analysis_cache (
    kind       TEXT NOT NULL,             -- "score" | "summary" | "agent"
    key        TEXT NOT NULL,             -- content-derived hash (see below)
    value      TEXT NOT NULL,             -- raw LLM JSON payload
    team_id    TEXT NOT NULL DEFAULT '',
    created_at DATETIME/TIMESTAMPTZ DEFAULT now,
    PRIMARY KEY (kind, key)
)
```

Store methods on `AnalysisStore` (both adapters):

```go
// GetAnalysisCache returns the cached value for (kind, key), or ok=false.
GetAnalysisCache(ctx context.Context, kind, key string) (value string, ok bool, err error)
// PutAnalysisCache upserts the cached value for (kind, key).
PutAnalysisCache(ctx context.Context, kind, key, value, teamID string) error
// PruneAnalysisCache deletes cache rows older than the cutoff. Returns rows deleted.
PruneAnalysisCache(ctx context.Context, olderThan time.Duration) (int, error)
```

### 3. Pipeline integration (`internal/pipeline`)

Wrap the three expensive per-item LLM calls in check-cache → call → write-through:

| Call | kind | key |
|------|------|-----|
| `ScoreEntry` (analyze.go) | `score` | sha256 of entry title+content (reuse the entry content-hash convention) |
| `SummarizeCluster` (analyze.go) | `summary` | sha256 of the cluster's sorted member content hashes joined |
| `agent.Generate` (pipeline generateAgent) | `agent` | same cluster-shaped key |

Semantics:
- Cache hit → unmarshal cached JSON, skip the LLM call (log at debug if logging exists at
  that level; otherwise silent).
- Miss → LLM call; on success store the raw JSON response via PutAnalysisCache (best-effort:
  a cache-write failure logs warn and does not fail the item); on LLM error keep today's
  behavior (item logged + skipped/errored) — the item is simply absent from the cache and
  retries on the next run.
- Keys derive only from content, so entry edits invalidate naturally and the cache is safe
  across teams (identical content yields identical results; team_id column is informational).
- DetectGaps (one cheap call per run), weak-signal improvement, and auto-tag backfill stay
  uncached (already incremental or trivially cheap).

Prune: at the end of each successful `Run`, call `PruneAnalysisCache(ctx, 90*24*time.Hour)`;
log count when > 0; failures log warn only.

### 4. Net behavior

- Kill the server mid-run → restart marks the run failed; the next interval run reuses every
  completed score/summary/agent result and only performs the remaining LLM calls.
- A flaky Ollama call costs one item one cycle, not the whole run.
- Steady-state runs only pay LLM cost for new/edited entries — a meaningful speedup for
  local models, and a cost reduction for Anthropic.

## Non-Goals

- No stage/position checkpointing in pipeline_runs.
- No retry queue, no manual-retry UI.
- No caching of request-path AI (enrich_context, prompt_suggest) or embeddings.

## Testing

- Storage (SQLite, both adapters compiled): MarkInterruptedRuns marks only running rows and
  returns count; analysis cache get/put/overwrite/prune round-trip.
- Pipeline: scoring/summarize/agent each hit the cache on a second run with unchanged
  content (fake LLM call-count assertions); edited content misses; LLM error → no cache
  write → retried next run; prune invoked on successful run.
- main.go wiring compiles; startup log path exercised by storage tests.
- Full `go test ./...` + Vite build (no frontend changes).
