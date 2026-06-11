# Team Data-Isolation Audit & Fix — Design

**Date:** 2026-06-10
**Status:** Approved

## Problem

The Phase 5 multi-tenant model stores `team_id` on knowledge entries and enforces it in some
places (export, batch approve/reject, analytics, activity, trending) but not others. An audit
found:

- **Critical (IDOR):** `GET/PUT/DELETE /api/knowledge/{id}`, rate, approve, reject act purely
  on the entry ID with no team ownership check — any authenticated user can read or mutate
  any team's entries.
- **High:** `GET /api/knowledge` (list/search), `/api/stats`, `/api/clusters`, `/api/datasets`,
  `/api/agents` (+ get/publish/export/refactor), `/api/pipeline/status|runs` return global data.
- **Structural:** `Cluster`/`Agent` structs have no `TeamID` field (columns exist, always
  empty, never stamped by the pipeline); `dataset_snapshots`/`pipeline_runs` have no team
  column at all.
- **MCP:** `knowledge_list`, `knowledge_get`, `knowledge_delete`, `knowledge_rate`,
  `knowledge_search` are unscoped; `knowledge_store` never sets `entry.TeamID` (bug — MCP
  entries land with empty `team_id`).

## Decisions (user-approved)

| Decision | Choice |
|----------|--------|
| Scope | Everything: knowledge endpoints + clusters/agents/datasets/pipeline/stats + MCP tools |
| Superadmin & empty-TeamID contexts (dev bypass, stdio MCP) | Full cross-team access |
| Legacy rows with empty `team_id` | Pass all checks (visible to everyone) + single-team-only backfill |
| Cross-team by-ID access | **403 Forbidden** |
| Enforcement point | Handler-level fetch-then-check with one central policy function |

## Access Policy

New function in `internal/auth`:

```go
// CanAccess reports whether the caller may act on a resource owned by
// resourceTeam. Superadmins, contexts without a team (dev bypass, stdio MCP),
// and team-less legacy resources are always allowed.
func CanAccess(tc TeamContext, resourceTeam string) bool {
	return tc.Role == "superadmin" || tc.TeamID == "" || resourceTeam == "" ||
		tc.TeamID == resourceTeam
}
```

- Web denial: `writeError(w, 403, "forbidden", "entry belongs to another team")` (message
  adapted per resource type).
- MCP denial: `mcplib.NewToolResultError("forbidden: resource belongs to another team")`.
- Unit table tests cover all four allow branches and the deny branch.

## Web Handlers — Knowledge (critical)

`internal/web/handlers.go`:

- `handleKnowledgeList`: add `TeamID: tc.TeamID` to the `ListFilter` (requires resolving
  `tc := auth.GetTeamContext(r.Context())`).
- `handleKnowledgeGet`, `handleKnowledgeRate`, `handleKnowledgeUpdate`,
  `handleKnowledgeDelete`, `handleKnowledgeApprove`, `handleKnowledgeReject`: fetch the entry
  (`GetEntry`), then `if !auth.CanAccess(tc, entry.TeamID) { 403 }`, then act. Update already
  fetches; the others gain the fetch. (This mirrors the existing batch-approve/reject pattern
  in `batch_handlers.go`.)
- `handleAgentRefactor` (`handlers.go:~335-343`): pass `TeamID: tc.TeamID` in its
  `ListEntries` filter (it already resolves `tc`).

## Storage — Team on Clusters/Agents/Snapshots/Runs

- Structs gain `TeamID string`: `Cluster`, `DatasetSnapshot`, `PipelineRun`
  (`internal/storage/storage.go`), `Agent` (`internal/storage/agents.go`).
- Migrations (idempotent, both adapters): `dataset_snapshots` and `pipeline_runs` gain
  `team_id TEXT NOT NULL DEFAULT ''` (clusters/agents/agent_versions columns already exist).
- Read/write paths for clusters, agents, snapshots, pipeline runs include `team_id`
  (INSERT columns + SELECT lists + scans), mirroring how entries handle `team_id`.
- Signature changes (empty teamID = no filter, consistent with `ListEntries`):
  - `ListClusters(ctx, teamID string)`
  - `ListAgents(ctx, teamID string)` (AgentStore)
  - `ListSnapshots(ctx, teamID string)`
  - `ListPipelineRuns(ctx, teamID string, limit int)`
  - `GetLatestPipelineRun(ctx, teamID string)`
  - `CountEntries(ctx, teamID string)`
- All callers updated: web handlers pass `tc.TeamID`; the pipeline passes `p.teamID`;
  MCP `cluster_list`/`analysis_status` pass the resolved team.

## Pipeline Stamping

`internal/pipeline/pipeline.go` and `internal/agent`: every artifact the pipeline writes
gets `TeamID: p.teamID` — clusters (`StoreCluster`), agents (generation path), dataset
snapshots (`StoreSnapshot`), pipeline runs (`StartPipelineRun(ctx, trigger, teamID string)` —
signature gains the teamID argument). Weak-signal improvement drafts already set `TeamID`
(verified).

## Web Handlers — Clusters/Agents/Datasets/Pipeline/Stats

- `handleClusterList`, `handleDatasetList`, `handleAgentList`, `handlePipelineStatus`,
  `handlePipelineRuns`: resolve `tc`, pass `tc.TeamID` to the new signatures.
- `handleClusterGet`, `handleAgentGet`, `handleAgentPublish`, agent export/bulk-export,
  `handleAgentRefactor`, `handleDatasetExport`: fetch, then `CanAccess` check, 403 on deny.
- `handleStats`: `CountEntries(ctx, tc.TeamID)` + team-filtered cluster/agent lists.

## MCP Tools

`internal/mcp`:

- `knowledge_store` (`tools.go`): **bug fix** — set `TeamID: teamID` on the entry literal
  (resolve `teamID, actor := resolveActorTeam(ctx)` before building the entry; it currently
  resolves after the dry-run block — move resolution earlier so the stored entry is stamped).
- `knowledge_list`: `ListFilter{... TeamID: teamID}`.
- `knowledge_get`, `knowledge_delete`, `knowledge_rate`: fetch entry, check
  `auth.CanAccess(auth.GetTeamContext(ctx), entry.TeamID)`, return forbidden tool error on deny.
- `knowledge_search` (`knowledge_tools.go`): post-filter `SearchSimilar` results through the
  same policy before formatting output.
- `cluster_list` / `analysis_status`: pass resolved team to the new list signatures.
- Stdio MCP carries no auth context → empty `TeamID` → full access (per policy). HTTP/SSE
  MCP authenticated by API key is scoped.

## Backfill (single-team only)

New store method on both adapters:

```go
// BackfillTeamID stamps teamID onto rows whose team_id is empty across
// entries, clusters, agents, and agent_versions. Idempotent.
BackfillTeamID(ctx context.Context, teamID string) error
```

`cmd/server/main.go`, after store init: list teams; **iff exactly one team exists**, call
`BackfillTeamID` with that team's ID. Multi-team installs are left untouched (legacy rows
stay empty = visible to all) because ownership cannot be inferred. Logged at info with the
affected row counts (or a simple "backfill applied/skipped" line).

## Error Handling

- 403 (`forbidden`) for cross-team access on all web by-ID routes; 404 stays reserved for
  genuinely missing IDs.
- MCP tools return a tool-level error string, never a protocol error.
- Backfill failures log at error and do not prevent startup.

## Non-Goals

- No frontend changes (UI consumes already-filtered data).
- No changes to analytics/activity/trending/export/batch handlers (already scoped).
- No per-rule or per-settings scoping changes (already scoped).
- No new roles or permission levels.

## Testing

- `internal/auth`: CanAccess table tests.
- `internal/web`: for each by-ID knowledge route — team-A caller vs team-B entry → 403;
  same-team → success; superadmin/empty-team → success. List/stats scoping tests via the
  mock store's recorded filter. Agent/cluster/dataset handler scoping tests.
- `internal/storage`: scoped list tests for clusters/agents/snapshots/runs (SQLite);
  BackfillTeamID test (stamps empty rows only).
- `internal/mcp`: scoped knowledge_list; forbidden knowledge_get/delete/rate; search
  post-filter test; knowledge_store stamps TeamID.
- `internal/pipeline`: stamped TeamID on stored cluster/snapshot/run.
- Full `go test ./...` + clean Vite build (frontend untouched but verified).
