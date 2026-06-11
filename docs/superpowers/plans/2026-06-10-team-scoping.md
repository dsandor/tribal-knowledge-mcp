# Team Data-Isolation Enforcement Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

> **PROJECT RULE — NO GIT COMMITS.** Never run `git add`/`git commit`. Every task ends at "tests pass".

**Goal:** Enforce team data isolation across all web REST handlers, MCP tools, and pipeline artifacts via one central access policy, with a single-team backfill for legacy rows.

**Architecture:** A central `auth.CanAccess(tc, resourceTeam)` policy (superadmin / empty-team caller / empty-team resource always allowed; otherwise IDs must match). By-ID handlers fetch-then-check and return **403** on denial. List/count store methods gain a `teamID` filter param (empty = unfiltered, matching `ListFilter.TeamID` semantics). `Cluster`/`Agent`/`DatasetSnapshot`/`PipelineRun` gain `TeamID` stamped by the pipeline. Startup backfill stamps empty `team_id` rows only when exactly one team exists.

**Tech Stack:** Go (chi, mcp-go), SQLite + Postgres dual adapters.

**Spec:** `docs/superpowers/specs/2026-06-10-team-scoping-design.md`

**Verification:** `cd /Users/dsandor/Projects/memory && go build ./... && go test ./...` plus `cd web && npm run build`.

**Key existing facts:**
- `auth.TeamContext{TeamID, KeyID, UserID, Role, Display}` (`internal/auth/middleware.go:16`); roles member<curator<admin<superadmin; dev bypass injects `Role:"superadmin"` with empty TeamID.
- The batch handlers (`internal/web/batch_handlers.go:17-147`) already do fetch-then-team-check — mirror their style.
- `writeError(w, code, errCode, msg)` is the standard error helper.
- Store signatures being changed (current → new):
  - `CountEntries(ctx)` → `CountEntries(ctx, teamID string)` — sqlite `internal/storage/analysis.go:16`, pg `postgres_analysis.go:69`
  - `ListClusters(ctx)` → `(ctx, teamID string)` — `analysis.go:68`, `postgres_analysis.go:102`
  - `StartPipelineRun(ctx, trigger)` → `(ctx, trigger, teamID string)` — `analysis.go:130`, `postgres_analysis.go:164`
  - `GetLatestPipelineRun(ctx)` → `(ctx, teamID string)` — `analysis.go:170`, `postgres_analysis.go:200`
  - `ListPipelineRuns(ctx, limit)` → `(ctx, teamID string, limit int)` — `analysis.go:204`, `postgres_analysis.go:234`
  - `ListSnapshots(ctx)` → `(ctx, teamID string)` — `analysis.go:282`, `postgres_analysis.go:312`
  - `ListAgents(ctx)` → `(ctx, teamID string)` — `agents_sqlite.go:88`, `postgres_agents.go:135`
  - Interfaces: `AnalysisStore` (`storage.go:132`), `AgentStore` (`agents.go:41`).
- `clusters`/`agents`/`agent_versions` already have `team_id TEXT NOT NULL DEFAULT ''` columns (sqlite.go:271-273 alters; postgres mirrors); `dataset_snapshots` and `pipeline_runs` do NOT — new columns needed.
- `ListTeams(ctx) ([]Team, error)` exists on both adapters (`teams_sqlite.go:51`, `postgres_teams.go:147`).
- MCP: `resolveActorTeam(ctx) (teamID string, actor live.ActorRef)` (`internal/mcp/live_producer.go:15`) wraps `auth.GetTeamContext`.

---

### Task 1: `auth.CanAccess` policy

**Files:**
- Modify: `internal/auth/middleware.go` (append near `InjectSuperadmin`)
- Test: `internal/auth/canaccess_test.go` (new)

- [ ] **Step 1: Write the failing test**

```go
package auth

import "testing"

func TestCanAccess(t *testing.T) {
	cases := []struct {
		name string
		tc   TeamContext
		team string
		want bool
	}{
		{"same team", TeamContext{TeamID: "t1", Role: "member"}, "t1", true},
		{"different team", TeamContext{TeamID: "t1", Role: "member"}, "t2", false},
		{"superadmin cross-team", TeamContext{TeamID: "t1", Role: "superadmin"}, "t2", true},
		{"empty caller team (dev bypass, stdio MCP)", TeamContext{Role: "superadmin"}, "t2", true},
		{"empty caller team plain", TeamContext{}, "t2", true},
		{"legacy resource without team", TeamContext{TeamID: "t1", Role: "member"}, "", true},
		{"curator different team", TeamContext{TeamID: "t1", Role: "curator"}, "t2", false},
		{"admin different team", TeamContext{TeamID: "t1", Role: "admin"}, "t2", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := CanAccess(c.tc, c.team); got != c.want {
				t.Errorf("CanAccess(%+v, %q) = %v, want %v", c.tc, c.team, got, c.want)
			}
		})
	}
}
```

- [ ] **Step 2: Run** `cd /Users/dsandor/Projects/memory && go test ./internal/auth/ -run CanAccess -v` — FAIL (`undefined: CanAccess`).

- [ ] **Step 3: Implement** in `internal/auth/middleware.go`:

```go
// CanAccess reports whether the caller may act on a resource owned by
// resourceTeam. Superadmins, contexts without a team (dev bypass, stdio MCP),
// and team-less legacy resources are always allowed.
func CanAccess(tc TeamContext, resourceTeam string) bool {
	return tc.Role == "superadmin" || tc.TeamID == "" || resourceTeam == "" ||
		tc.TeamID == resourceTeam
}
```

- [ ] **Step 4: Run** `go test ./internal/auth/ -v` — all PASS.

---

### Task 2: Web knowledge handlers — list scoping + by-ID ownership checks

**Files:**
- Modify: `internal/web/handlers.go` (`handleKnowledgeList` :90, `handleKnowledgeGet` :119, `handleKnowledgeRate` :132, `handleKnowledgeUpdate` :487, `handleKnowledgeDelete` :531, `handleKnowledgeApprove` :543, `handleKnowledgeReject` :555, `handleAgentRefactor` ~:335-343 — line numbers approximate, locate by function name)
- Test: extend `internal/web/server_test.go`

- [ ] **Step 1: Write failing tests**

Study the existing test-server setup in `server_test.go` (mockStore, request helpers, how auth context is provided — check whether tests use dev bypass or inject contexts; there is a `lastFilter` recorder on mockStore from earlier work). Add:

```go
// TestKnowledgeListScopedByTeam: GET /api/knowledge as a team-"t1" caller →
// mockStore.lastFilter.TeamID == "t1".
//
// TestKnowledgeGetForbiddenCrossTeam: mockStore returns an entry with TeamID
// "t2"; GET /api/knowledge/{id} as a team-"t1" member → 403 with error code
// "forbidden".
//
// TestKnowledgeUpdateForbiddenCrossTeam: PUT /api/knowledge/{id} (body
// {"title":"x"}) against a t2-owned entry as t1 member → 403, store's
// UpdateEntry NOT called.
//
// TestKnowledgeDeleteForbiddenCrossTeam / TestKnowledgeRateForbiddenCrossTeam /
// TestKnowledgeApproveForbiddenCrossTeam / TestKnowledgeRejectForbiddenCrossTeam:
// same shape → 403, mutation not called.
//
// TestKnowledgeGetSameTeamOK: t1 caller + t1 entry → 200.
// TestKnowledgeGetLegacyEmptyTeamOK: t1 caller + entry with TeamID "" → 200.
```

How to give the request a team context: read how existing scoped-handler tests (export/analytics/batch in this package) provide auth — reuse exactly that mechanism (API key fixture or direct context injection middleware). If the test server runs with dev bypass (superadmin, empty team), these tests need the real auth path or a context-injecting wrapper — follow whatever `batch_handlers` tests do, since batch approve/reject already enforce team checks and have tests.

- [ ] **Step 2: Run** `go test ./internal/web/ -run 'CrossTeam|ScopedByTeam|SameTeamOK|LegacyEmptyTeam' -v` — FAIL (handlers don't check).

- [ ] **Step 3: Implement.** Pattern for every by-ID handler (shown for Get; apply identically to Rate/Update/Delete/Approve/Reject — Update already fetches the entry, just add the check after the fetch):

```go
func (s *Server) handleKnowledgeGet(w http.ResponseWriter, r *http.Request) {
	tc := auth.GetTeamContext(r.Context())
	entry, err := s.store.GetEntry(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			writeError(w, 404, "not_found", "entry not found")
			return
		}
		writeError(w, 500, "internal_error", fmt.Sprintf("get entry: %v", err))
		return
	}
	if !auth.CanAccess(tc, entry.TeamID) {
		writeError(w, 403, "forbidden", "entry belongs to another team")
		return
	}
	writeJSON(w, entry)
}
```

For mutation handlers that don't currently fetch (Rate/Delete/Approve/Reject), fetch first, run the check, then perform the existing mutation. Keep the existing 404/500 branches intact.

`handleKnowledgeList`: add at the top `tc := auth.GetTeamContext(r.Context())` and `TeamID: tc.TeamID` in the `ListFilter` literal.

`handleAgentRefactor`: it already has `tc` (~:335); add `TeamID: tc.TeamID` to its `ListEntries` filter (~:343).

- [ ] **Step 4: Run** `go test ./internal/web/ -v 2>&1 | grep -E "^(--- FAIL|FAIL|ok)"` — all PASS. Existing tests that exercised these routes without team context still pass because empty `tc.TeamID` ⇒ `CanAccess` true and `ListFilter.TeamID` "" ⇒ unfiltered. If a pre-existing test DOES set a team and now legitimately gets scoped results, update its expectation and SAY SO in your report.

---

### Task 3: Storage — TeamID on Cluster/Agent/Snapshot/PipelineRun + signature sweep

**Files:**
- Modify: `internal/storage/storage.go` (Cluster :59, DatasetSnapshot :82, PipelineRun :71, AnalysisStore :132), `internal/storage/agents.go` (Agent :15, AgentStore :41)
- Modify: `internal/storage/sqlite.go` (migrate), `internal/storage/analysis.go`, `internal/storage/agents_sqlite.go`
- Modify: `internal/storage/postgres.go` (migrate), `internal/storage/postgres_analysis.go`, `internal/storage/postgres_agents.go`
- Modify (compiler-discovered callers): `internal/web/handlers.go`, `internal/pipeline/pipeline.go`, `internal/mcp/analysis_tools.go`, test fakes, and anything else `go build ./...` flags
- Test: `internal/storage/team_scoping_test.go` (new)

- [ ] **Step 1: Write failing tests** (`internal/storage/team_scoping_test.go`; reuse `newTestStoreInternal` from `teams_test.go`):

```go
package storage

import (
	"context"
	"testing"
)

func TestClusterTeamScoping(t *testing.T) {
	s := newTestStoreInternal(t)
	ctx := context.Background()
	if _, err := s.StoreCluster(ctx, Cluster{Title: "a", TeamID: "t1"}); err != nil {
		t.Fatalf("store cluster t1: %v", err)
	}
	if _, err := s.StoreCluster(ctx, Cluster{Title: "b", TeamID: "t2"}); err != nil {
		t.Fatalf("store cluster t2: %v", err)
	}
	got, err := s.ListClusters(ctx, "t1")
	if err != nil {
		t.Fatalf("list t1: %v", err)
	}
	if len(got) != 1 || got[0].TeamID != "t1" {
		t.Fatalf("ListClusters(t1) = %+v, want 1 cluster with TeamID t1", got)
	}
	all, err := s.ListClusters(ctx, "")
	if err != nil {
		t.Fatalf("list all: %v", err)
	}
	if len(all) != 2 {
		t.Fatalf("ListClusters(\"\") returned %d, want 2", len(all))
	}
}

func TestAgentTeamScoping(t *testing.T) {
	s := newTestStoreInternal(t)
	ctx := context.Background()
	if _, err := s.UpsertAgent(ctx, Agent{Domain: "d1", TeamID: "t1"}); err != nil {
		t.Fatalf("upsert t1: %v", err)
	}
	if _, err := s.UpsertAgent(ctx, Agent{Domain: "d2", TeamID: "t2"}); err != nil {
		t.Fatalf("upsert t2: %v", err)
	}
	got, err := s.ListAgents(ctx, "t1")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(got) != 1 || got[0].TeamID != "t1" {
		t.Fatalf("ListAgents(t1) = %+v, want 1 agent with TeamID t1", got)
	}
}

func TestSnapshotAndRunTeamScoping(t *testing.T) {
	s := newTestStoreInternal(t)
	ctx := context.Background()
	runID, err := s.StartPipelineRun(ctx, "manual", "t1")
	if err != nil {
		t.Fatalf("start run: %v", err)
	}
	_ = runID
	if _, err := s.StartPipelineRun(ctx, "manual", "t2"); err != nil {
		t.Fatalf("start run t2: %v", err)
	}
	runs, err := s.ListPipelineRuns(ctx, "t1", 10)
	if err != nil {
		t.Fatalf("list runs: %v", err)
	}
	if len(runs) != 1 || runs[0].TeamID != "t1" {
		t.Fatalf("ListPipelineRuns(t1) = %+v, want 1 run for t1", runs)
	}
	latest, err := s.GetLatestPipelineRun(ctx, "t1")
	if err != nil || latest == nil || latest.TeamID != "t1" {
		t.Fatalf("GetLatestPipelineRun(t1) = %+v err=%v, want t1 run", latest, err)
	}

	if _, err := s.StoreSnapshot(ctx, DatasetSnapshot{Version: 1, TeamID: "t1"}); err != nil {
		t.Fatalf("store snapshot: %v", err)
	}
	if _, err := s.StoreSnapshot(ctx, DatasetSnapshot{Version: 2, TeamID: "t2"}); err != nil {
		t.Fatalf("store snapshot t2: %v", err)
	}
	snaps, err := s.ListSnapshots(ctx, "t1")
	if err != nil {
		t.Fatalf("list snapshots: %v", err)
	}
	if len(snaps) != 1 || snaps[0].TeamID != "t1" {
		t.Fatalf("ListSnapshots(t1) = %+v, want 1 snapshot for t1", snaps)
	}
}

func TestCountEntriesTeamScoping(t *testing.T) {
	s := newTestStoreInternal(t)
	ctx := context.Background()
	if _, err := s.StoreEntry(ctx, KnowledgeEntry{Type: KTPattern, Title: "a", Content: "c", TeamID: "t1"}, nil); err != nil {
		t.Fatalf("store: %v", err)
	}
	if _, err := s.StoreEntry(ctx, KnowledgeEntry{Type: KTPattern, Title: "b", Content: "c", TeamID: "t2"}, nil); err != nil {
		t.Fatalf("store: %v", err)
	}
	n, err := s.CountEntries(ctx, "t1")
	if err != nil || n != 1 {
		t.Fatalf("CountEntries(t1) = %d err=%v, want 1", n, err)
	}
	all, err := s.CountEntries(ctx, "")
	if err != nil || all != 2 {
		t.Fatalf("CountEntries(\"\") = %d err=%v, want 2", all, err)
	}
}
```

- [ ] **Step 2: Run** `go test ./internal/storage/ -run TeamScoping -v` — compile FAIL (signatures/fields missing).

- [ ] **Step 3: Structs + interfaces.** Add `TeamID string` to `Cluster`, `DatasetSnapshot`, `PipelineRun` (storage.go) and `Agent` (agents.go). Update `AnalysisStore` and `AgentStore` interface methods to the new signatures listed in the plan header.

- [ ] **Step 4: Migrations.** SQLite (`sqlite.go`, in the existing idempotent alter loop alongside the clusters/agents team_id alters at :271-273):
```go
		"ALTER TABLE dataset_snapshots ADD COLUMN team_id TEXT NOT NULL DEFAULT ''",
		"ALTER TABLE pipeline_runs    ADD COLUMN team_id TEXT NOT NULL DEFAULT ''",
```
Postgres (`postgres.go` migrate or `migrateAnalysis`, matching surrounding style):
```go
ALTER TABLE dataset_snapshots ADD COLUMN IF NOT EXISTS team_id TEXT NOT NULL DEFAULT '';
ALTER TABLE pipeline_runs    ADD COLUMN IF NOT EXISTS team_id TEXT NOT NULL DEFAULT '';
```

- [ ] **Step 5: SQLite adapter** (`analysis.go`, `agents_sqlite.go`). For each method:
  - INSERTs (`StoreCluster`, `StoreSnapshot`, `StartPipelineRun`, `UpsertAgent`) gain the `team_id` column + arg from the struct/param.
  - SELECT lists + scans for clusters/agents/snapshots/runs gain `team_id` → `&x.TeamID` (read each function and slot it consistently — last position is fine if you update SELECT and scan together; verify GetAgent/GetAgentByDomain/agent-version paths too).
  - List/count/latest methods gain the `teamID string` param and append `AND team_id = ?` (or `WHERE team_id = ?`) only when `teamID != ""` — copy the conditional-filter style from `ListEntries`.
  - `StartPipelineRun(ctx, trigger, teamID string)` stores team_id on the new run row.

- [ ] **Step 6: Postgres adapter** (`postgres_analysis.go`, `postgres_agents.go`) — mirror Step 5 exactly with `$n` placeholders (use the file's `nextArg`-style or positional conventions as found).

- [ ] **Step 7: Compiler-driven caller sweep.** Run `go build ./... 2>&1 | grep -v warning | head -40` repeatedly and fix every caller:
  - `internal/web/handlers.go`: `handleStats` → `CountEntries(ctx, tc.TeamID)`, `ListClusters(ctx, tc.TeamID)`, `ListAgents(ctx, tc.TeamID)`, `GetLatestPipelineRun(ctx, tc.TeamID)` (resolve `tc` at top); `handleClusterList`/`handleDatasetList`/agent list/pipeline status+runs handlers → pass `tc.TeamID`. (Ownership checks for by-ID routes come in Task 5 — here just make it compile with scoped lists.)
  - `internal/pipeline/pipeline.go`: `CountEntries(ctx, p.teamID)`, `StartPipelineRun(ctx, trigger, p.teamID)`, and anywhere it lists clusters/agents pass `p.teamID`; stamping comes in Task 4.
  - `internal/mcp/analysis_tools.go`: `cluster_list`/`analysis_status` → resolve `teamID, _ := resolveActorTeam(ctx)` and pass it.
  - Test fakes: `mockAnalysisStore` (`internal/pipeline/testhelpers_test.go`), `mockStore` (`internal/web/server_test.go`, `internal/mcp/tools_test.go`) — update method signatures to match; behavior: filter their in-memory slices by teamID when non-empty, else return all (so Task 2/5 tests can assert scoping).
  - `internal/web/export_handlers.go` dataset export path if it calls ListSnapshots.

- [ ] **Step 8: Run** `go build ./... && go test ./...` — everything PASS (including the new TeamScoping tests).

---

### Task 4: Pipeline stamps TeamID on everything it writes

**Files:**
- Modify: `internal/pipeline/pipeline.go` (cluster construction before `StoreCluster` :~243, snapshot construction before `StoreSnapshot` :~302), `internal/agent/generate.go` or wherever the pipeline builds `storage.Agent` (grep `UpsertAgent` callers)
- Test: extend `internal/pipeline/` tests

- [ ] **Step 1: Write failing test.** In the pipeline tests (reuse mock fakes which now record stored values — check how existing tests inspect `mockAnalysisStore` stored clusters/snapshots):

```go
// TestPipelineStampsTeamID: construct Pipeline with teamID "t1" (use the
// option/field path existing tests use — WithWeakSignalImprovement("t1") sets
// p.teamID), run the minimal Run() path the existing pipeline tests use, then
// assert every cluster, snapshot, and pipeline run recorded by the fake store
// has TeamID == "t1". Also assert the agent passed to UpsertAgent (if agent
// generation is enabled in the test) carries TeamID "t1".
```

Adapt to the package's existing Run()-level test harness; if Run() is heavy, test the stamping at the narrowest seam the existing tests use.

- [ ] **Step 2: Run** `go test ./internal/pipeline/ -run StampsTeamID -v` — FAIL (TeamID empty).

- [ ] **Step 3: Implement.** Set `TeamID: p.teamID` on the `Cluster` literal before `StoreCluster`, the `DatasetSnapshot` literal before `StoreSnapshot` (StartPipelineRun already takes teamID from Task 3). For agents: pass `p.teamID` into the generation path so the `storage.Agent` literal carries it (thread a teamID parameter or set after `Generate` returns, matching the call shape in pipeline.go).

- [ ] **Step 4: Run** `go test ./internal/pipeline/ -v` — all PASS.

---

### Task 5: Web cluster/agent/dataset/pipeline by-ID ownership checks

**Files:**
- Modify: `internal/web/handlers.go` (`handleClusterGet`, `handleAgentGet`, `handleAgentPublish`, `handleAgentRefactor`, agent export + bulk-export handlers — locate by name), `internal/web/export_handlers.go` (`handleDatasetExport` if it fetches a snapshot by id)
- Test: extend `internal/web/server_test.go`

- [ ] **Step 1: Write failing tests** (same auth fixture as Task 2):

```go
// TestAgentGetForbiddenCrossTeam: t1 caller, agent with TeamID t2 → 403.
// TestAgentPublishForbiddenCrossTeam: → 403, PublishAgent not called.
// TestClusterGetForbiddenCrossTeam: → 403.
// TestDatasetExportForbiddenCrossTeam: snapshot owned by t2 → 403.
// TestAgentGetLegacyEmptyTeamOK: agent with TeamID "" → 200.
```

- [ ] **Step 2: Run** — FAIL.

- [ ] **Step 3: Implement.** Same fetch-then-check pattern as Task 2, with resource-appropriate messages:
```go
	if !auth.CanAccess(tc, agent.TeamID) {
		writeError(w, 403, "forbidden", "agent belongs to another team")
		return
	}
```
For bulk agent export, filter the exported set to `auth.CanAccess`-passing agents instead of 403ing. For `handleAgentRefactor`, add the check on the fetched agent (its `ListEntries` filter was already scoped in Task 2).

- [ ] **Step 4: Run** `go test ./internal/web/ -v 2>&1 | grep -E "^(--- FAIL|FAIL|ok)"` — all PASS.

---

### Task 6: MCP tool scoping + knowledge_store TeamID bug fix

**Files:**
- Modify: `internal/mcp/tools.go` (`HandleKnowledgeStore`, `HandleKnowledgeGet` :~134, `HandleKnowledgeList` :~151, `HandleKnowledgeDelete` :~169), `internal/mcp/knowledge_tools.go` (`HandleKnowledgeSearch` :38, `HandleKnowledgeRate` :95)
- Test: extend `internal/mcp/tools_test.go` (and the knowledge_tools test file if separate)

- [ ] **Step 1: Write failing tests** (reuse the package's request-building helpers and mockStore; provide team context the way existing MCP tests do — if none set auth context, build one with `auth` context injection used by `resolveActorTeam`/`auth.GetTeamContext`):

```go
// TestKnowledgeStoreStampsTeamID: call knowledge_store with a ctx carrying
// team "t1" → stored entry has TeamID "t1".
// TestKnowledgeListScopedByTeam: ctx team "t1" → ListEntries received
// filter.TeamID "t1".
// TestKnowledgeGetForbidden: ctx team "t1", entry TeamID "t2" → tool result is
// an error containing "forbidden"; same-team and empty-ctx cases succeed.
// TestKnowledgeDeleteForbidden / TestKnowledgeRateForbidden: → forbidden error,
// mutation not called.
// TestKnowledgeSearchFiltersByTeam: SearchSimilar returns entries from t1 and
// t2; ctx team "t1" → output contains only the t1 entry.
```

- [ ] **Step 2: Run** `go test ./internal/mcp/ -run 'StampsTeamID|Scoped|Forbidden|FiltersByTeam' -v` — FAIL.

- [ ] **Step 3: Implement** (import `"github.com/dsandor/memory/internal/auth"` where needed):

`HandleKnowledgeStore`: move `teamID, actor := resolveActorTeam(ctx)` ABOVE the entry literal (it currently sits after the dry-run block) and add `TeamID: teamID` to the `storage.KnowledgeEntry` literal. Keep the single resolveActorTeam call (it's reused for the embedder + live event + async tagger).

`HandleKnowledgeList`: resolve team and set it:
```go
		teamID, _ := resolveActorTeam(ctx)
		filter := storage.ListFilter{
			Domain: req.GetString("domain", ""),
			Type:   storage.KnowledgeType(req.GetString("type", "")),
			Limit:  req.GetInt("limit", 20),
			TeamID: teamID,
		}
```

`HandleKnowledgeGet` / `HandleKnowledgeDelete` / `HandleKnowledgeRate`: fetch entry first, then:
```go
		tc := auth.GetTeamContext(ctx)
		if !auth.CanAccess(tc, entry.TeamID) {
			return mcplib.NewToolResultError("forbidden: entry belongs to another team"), nil
		}
```
(Get already fetches; Delete and Rate gain a `GetEntry` before mutating — on `ErrNotFound` keep the existing error responses.)

`HandleKnowledgeSearch`: after the domain filter block, add:
```go
		tc := auth.GetTeamContext(ctx)
		scoped := results[:0]
		for _, r := range results {
			if auth.CanAccess(tc, r.Entry.TeamID) {
				scoped = append(scoped, r)
			}
		}
		results = scoped
```

- [ ] **Step 4: Run** `go test ./internal/mcp/ -v 2>&1 | grep -E "^(--- FAIL|FAIL|ok)"` — all PASS (existing tests keep passing because empty ctx team ⇒ unscoped).

---

### Task 7: Single-team backfill

**Files:**
- Modify: `internal/storage/storage.go` (Store interface), `internal/storage/sqlite.go`, `internal/storage/postgres.go`, `cmd/server/main.go` (after store init, before server start)
- Test: extend `internal/storage/team_scoping_test.go`

- [ ] **Step 1: Write failing test**

```go
func TestBackfillTeamID(t *testing.T) {
	s := newTestStoreInternal(t)
	ctx := context.Background()
	idEmpty, _ := s.StoreEntry(ctx, KnowledgeEntry{Type: KTPattern, Title: "legacy", Content: "c"}, nil)
	idOwned, _ := s.StoreEntry(ctx, KnowledgeEntry{Type: KTPattern, Title: "owned", Content: "c", TeamID: "t9"}, nil)
	if _, err := s.StoreCluster(ctx, Cluster{Title: "legacy-cluster"}); err != nil {
		t.Fatalf("store cluster: %v", err)
	}

	if err := s.BackfillTeamID(ctx, "t1"); err != nil {
		t.Fatalf("backfill: %v", err)
	}

	e, _ := s.GetEntry(ctx, idEmpty)
	if e.TeamID != "t1" {
		t.Fatalf("legacy entry team = %q, want t1", e.TeamID)
	}
	o, _ := s.GetEntry(ctx, idOwned)
	if o.TeamID != "t9" {
		t.Fatalf("owned entry team = %q, want t9 (untouched)", o.TeamID)
	}
	clusters, _ := s.ListClusters(ctx, "t1")
	if len(clusters) != 1 {
		t.Fatalf("cluster not backfilled: %+v", clusters)
	}
	// Idempotent: second run changes nothing and errors nothing.
	if err := s.BackfillTeamID(ctx, "t1"); err != nil {
		t.Fatalf("second backfill: %v", err)
	}
}
```

- [ ] **Step 2: Run** `go test ./internal/storage/ -run Backfill -v` — FAIL.

- [ ] **Step 3: Implement.** `Store` interface (after `UpdateAutoTags`):
```go
	// BackfillTeamID stamps teamID onto rows whose team_id is empty across
	// entries, clusters, agents, agent_versions, dataset_snapshots, and
	// pipeline_runs. Idempotent; used by single-team deployments at startup.
	BackfillTeamID(ctx context.Context, teamID string) error
```
SQLite:
```go
func (s *SQLiteStore) BackfillTeamID(ctx context.Context, teamID string) error {
	if teamID == "" {
		return nil
	}
	for _, table := range []string{"entries", "clusters", "agents", "agent_versions", "dataset_snapshots", "pipeline_runs"} {
		if _, err := s.db.ExecContext(ctx,
			fmt.Sprintf("UPDATE %s SET team_id = ? WHERE team_id = ''", table), teamID); err != nil {
			return fmt.Errorf("backfill %s: %w", table, err)
		}
	}
	return nil
}
```
Postgres: identical loop with `$1`. Add no-op stubs to every test fake that implements `storage.Store` (compiler will list them).

`cmd/server/main.go` after store init (and after migrations have run — store constructors migrate internally):
```go
	// Single-team deployments converge legacy team-less rows onto the one
	// real team. Multi-team installs are left untouched (ownership unknown).
	if teams, err := store.ListTeams(ctx); err == nil && len(teams) == 1 {
		if err := store.BackfillTeamID(ctx, teams[0].ID); err != nil {
			slog.Error("team backfill failed", "err", err)
		} else {
			slog.Info("team backfill applied", "team", teams[0].ID)
		}
	}
```
(Check `ListTeams`' exact signature and the Team struct's ID field name in `internal/storage/teams.go`; adapt the snippet. Use whatever ctx variable main.go already has. The store variable in main may be typed as an interface — confirm it exposes ListTeams/BackfillTeamID; if main holds separate sqlite/pg variables, place the call where the concrete store is in scope.)

- [ ] **Step 4: Run** `go build ./... && go test ./...` — all PASS.

---

### Task 8: Final verification

- [ ] **Step 1:** `cd /Users/dsandor/Projects/memory && go build ./... && go test ./...` — clean build, all packages PASS.
- [ ] **Step 2:** `cd web && npm run build` — clean (no frontend code changed; validates nothing broke API types it relies on).
- [ ] **Step 3:** Migration + scoping smoke test against a THROWAWAY copy of the dev DB (never the real one):

```bash
cd /Users/dsandor/Projects/memory && cp knowledge.db /tmp/team-scope-test.db && mkdir -p ./cmd/scopecheck-tmp && cat > ./cmd/scopecheck-tmp/main.go <<'EOF'
package main

import (
	"context"
	"fmt"
	"os"

	"github.com/dsandor/memory/internal/storage"
)

func main() {
	s, err := storage.NewSQLiteStore("/tmp/team-scope-test.db", 768)
	if err != nil {
		fmt.Println("MIGRATE FAIL:", err)
		os.Exit(1)
	}
	defer s.Close()
	ctx := context.Background()
	n, err := s.CountEntries(ctx, "")
	if err != nil {
		fmt.Println("COUNT FAIL:", err)
		os.Exit(1)
	}
	if _, err := s.ListClusters(ctx, ""); err != nil {
		fmt.Println("CLUSTERS FAIL:", err)
		os.Exit(1)
	}
	if _, err := s.ListPipelineRuns(ctx, "", 5); err != nil {
		fmt.Println("RUNS FAIL:", err)
		os.Exit(1)
	}
	fmt.Printf("OK: %d entries, team_id columns migrated on snapshots/runs\n", n)
}
EOF
go run ./cmd/scopecheck-tmp 2>&1 | grep -v "deprecated\|sqlite3.h\|cgo-gcc\|warning"; rm -rf ./cmd/scopecheck-tmp /tmp/team-scope-test.db*
```
Expected: `OK: N entries, team_id columns migrated on snapshots/runs`.
- [ ] **Step 4:** Report results. **Do not commit.**
