# Team Deletion with Data Migration Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

> **PROJECT RULE — NO GIT COMMITS.** Never run `git add`/`git commit`. Every task ends at "tests pass".

**Goal:** Admins can delete a team from the UI; teams with data require picking a target team and everything is transactionally migrated there first.

**Architecture:** Existing `DELETE /api/admin/teams/{id}` gains `?migrate_to=`; a 409-with-counts response drives the UI's migration dialog. New `TeamDataCounts` + `DeleteTeamMigrate` storage methods on both adapters; agents with domain conflicts in the target are deleted (pipeline-regenerable) and reported as skipped.

**Spec:** `docs/superpowers/specs/2026-06-11-team-deletion-design.md` (read it in full — semantics table + migration list are authoritative).

**Key facts:**
- Route: `internal/web/server.go:249`; handler `internal/web/admin_handlers.go:95-101` (bare passthrough today). Superadmin-gated route group.
- `DeleteTeam` impls: `internal/storage/teams_sqlite.go:106`, `postgres_teams.go:207`. TeamStore interface: `internal/storage/teams.go:~100-110`. Existing `TestDeleteTeam` at `teams_test.go:91`.
- team_id-bearing tables: users, api_keys, entries, clusters, agents, agent_versions, rules, dataset_snapshots, pipeline_runs, feed_activity, activity_log, team_settings. Agents have UNIQUE(domain, team_id) — `idx_agents_domain_team` (PG) / table constraint (SQLite).
- web error helper `writeError(w, code, errCode, msg)`; for the 409 the handler writes a custom JSON body (counts) — follow writeJSON.
- UI: `web/src/pages/AdminTeams.tsx` (read fully; uses fetchTeams/createTeam/updateTeam/setTeamEnabled from api.ts); MUI Dialog patterns exist in KnowledgeDetail.tsx (delete confirm).
- Tests: storage `teams_test.go` fixtures (`newTestStoreInternal`); web handler tests in `server_test.go`/`admin` tests — find how admin routes are tested (superadmin auth fixture).

---

### Task 1: Storage — TeamDataCounts + DeleteTeamMigrate (+ DeleteTeam settings cleanup)

**Files:**
- Modify: `internal/storage/teams.go` (types + interface), `internal/storage/teams_sqlite.go`, `internal/storage/postgres_teams.go`
- Test: extend `internal/storage/teams_test.go`

- [ ] **Step 1: Failing tests** (real Go, reusing `newTestStoreInternal`; seed via existing helpers — CreateTeam, UpsertUser/AssignUserToTeam (read what exists), StoreEntry, StoreCluster, UpsertAgent, rule-create method, StartPipelineRun, StoreSnapshot):

```go
// TestTeamDataCounts: team t1 with 1 user, 1 entry, 1 cluster, 1 agent, 1 rule →
// counts match exactly; empty team t2 → all zeros.
// TestDeleteTeamMigrate: seed t1 with one of EVERY category (user, api key if a
// create-key method exists — read the store; entries, cluster, agent domain "d1",
// agent_versions row, rule, snapshot, pipeline run) and target t2 with an agent of
// the SAME domain "d1". DeleteTeamMigrate(t1, t2):
//   - summary{Users:1, Entries:1, Clusters:1, Agents:0, AgentsSkipped:1, Rules:1} (APIKeys per seed)
//   - t1 gone from ListTeams; t1's team_settings row gone
//   - every migrated record now has team_id t2 (spot-check entry/cluster/rule/user)
//   - t2's own agent for "d1" untouched (same ID/system prompt); t1's conflicting agent
//     AND its versions deleted
// TestDeleteTeamMigrateValidation: target == source → error; unknown target → error;
// unknown source → ErrNotFound.
// TestDeleteTeamCleansSettings: empty team with a settings row → DeleteTeam → team
// AND settings row gone.
```

- [ ] **Step 2:** Run — compile FAIL.

- [ ] **Step 3: Implement.** Types + interface methods per spec. SQLite `DeleteTeamMigrate`: one transaction —
```go
// for each table in the migration list: UPDATE <t> SET team_id = ? WHERE team_id = ?
// agents first need the conflict pass:
//   DELETE FROM agent_versions WHERE team_id = ? AND agent_id IN (
//     SELECT a.id FROM agents a WHERE a.team_id = ?
//       AND EXISTS (SELECT 1 FROM agents b WHERE b.team_id = ? AND b.domain = a.domain))
//   DELETE FROM agents WHERE team_id = ? AND domain IN (SELECT domain FROM agents WHERE team_id = ?)
//   (capture skipped count from RowsAffected of the agents DELETE)
// then UPDATE agents/agent_versions SET team_id=target WHERE team_id=source
// DELETE FROM team_settings WHERE team_id = source
// DELETE FROM teams WHERE id = source (RowsAffected 0 → ErrNotFound, rollback)
```
Validate source≠target and target exists (SELECT 1 FROM teams WHERE id=?) up front. Counts for the summary come from each UPDATE's RowsAffected (users, api_keys, entries, clusters, rules; agents = moved count). Postgres mirrors with $n. `TeamDataCounts`: one query per table or a single multi-subselect — keep simple per-table COUNT. `DeleteTeam`: add `DELETE FROM team_settings WHERE team_id=?` before the team delete (both adapters; transactional or sequential — match existing style; FK failure from users/keys remains the backstop).

- [ ] **Step 4:** Fix fakes if any mock implements TeamStore (`go build ./...` will say — web mockStore implements AllStore: add no-ops returning zero values). `go build ./... && go test ./... 2>&1 | tail -4` — ALL pass.

---

### Task 2: Handler — guarded delete + migrate

**Files:**
- Modify: `internal/web/admin_handlers.go` (`handleDeleteTeam` :95)
- Test: extend the web admin tests

- [ ] **Step 1: Failing tests** (use the superadmin auth fixture the existing admin-route tests use):
```go
// TestDeleteTeamEmptyOK: empty team → 200 {ok:true}, team gone.
// TestDeleteTeamWithDataReturns409Counts: team with 1 user + 2 entries → 409, body
//   {"error":"team_not_empty","counts":{"users":1,...,"entries":2,...}}, team NOT deleted.
// TestDeleteTeamMigrateHappyPath: ?migrate_to=t2 → 200 with summary JSON; store records
//   the DeleteTeamMigrate call (extend mock).
// TestDeleteTeamMigrateSelfTarget400 / UnknownTarget400 / UnknownTeam404.
```

- [ ] **Step 2:** Run — FAIL.

- [ ] **Step 3: Implement** `handleDeleteTeam`:
```go
	id := chi.URLParam(r, "id")
	target := r.URL.Query().Get("migrate_to")
	if target == "" {
		counts, err := s.store.TeamDataCounts(r.Context(), id)
		// ErrNotFound → 404; other err → 500
		if counts != (storage.TeamDataCounts{}) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(409)
			_ = json.NewEncoder(w).Encode(map[string]any{"error": "team_not_empty", "counts": counts})
			return
		}
		// empty → existing DeleteTeam path (404/500 handling as today) → {ok:true}
	}
	if target == id { writeError(w, 400, "bad_request", "migrate_to must be a different team"); return }
	summary, err := s.store.DeleteTeamMigrate(r.Context(), id, target)
	// ErrNotFound → 404 (source) / 400 if the impl distinguishes bad target — map impl
	// errors: define a sentinel or match the error text the impl returns for unknown target → 400.
	writeJSON(w, summary)
```
Give `TeamDataCounts` JSON tags (lowercase) in Task 1 so the 409 body keys are `users`, `api_keys`, `entries`, `clusters`, `agents`, `rules`; `TeamMigrationSummary` likewise (`agents_skipped`). Adjust Task 1 if not already done.

- [ ] **Step 4:** `go test ./internal/web/ 2>&1 | tail -2` + `go build ./...` — pass.

---

### Task 3: UI — delete button + migration dialog

**Files:**
- Modify: `web/src/lib/api.ts`, `web/src/pages/AdminTeams.tsx`

- [ ] **Step 1: api.ts**
```ts
export interface TeamDataCounts { users: number; api_keys: number; entries: number; clusters: number; agents: number; rules: number }
export interface TeamDeleteResult { ok?: boolean; needsMigration?: boolean; counts?: TeamDataCounts; summary?: Record<string, number> }

export async function deleteTeam(id: string, migrateTo?: string): Promise<TeamDeleteResult> {
  const q = migrateTo ? `?migrate_to=${encodeURIComponent(migrateTo)}` : ''
  const r = await apiFetch(`/api/admin/teams/${id}${q}`, { method: 'DELETE' })
  if (r.status === 409) {
    const body = await r.json()
    return { needsMigration: true, counts: body.counts }
  }
  if (!r.ok) {
    const err = await r.json().catch(() => ({}))
    throw new Error((err as { message?: string }).message ?? 'delete team failed')
  }
  return { ok: true, summary: await r.json().catch(() => undefined) }
}
```

- [ ] **Step 2: AdminTeams.tsx** (read the page first; follow its list-row and dialog idioms):
- Trash IconButton per row (lucide `Trash2`, matching KnowledgeDetail's usage).
- Click → confirm Dialog ("Delete team <name>? This cannot be undone.") → on confirm call `deleteTeam(id)`.
  - `{ok}` → close, refresh, optional snackbar.
  - `{needsMigration, counts}` → dialog switches to migration mode: render the non-zero counts ("3 users, 35 entries, 1 agent"), a Select of OTHER teams ("Migrate data to"), and a confirm button "Move data to <team> and delete" disabled until a target is chosen → `deleteTeam(id, target)` → close/refresh; show summary (agents_skipped surfaced as "N agents skipped — domain already exists in target").
  - Errors render inside the dialog (Alert).
- State must handle: dialog open/team, mode (confirm|migrate), counts, target, error, busy.

- [ ] **Step 3:** `cd /Users/dsandor/Projects/memory/web && npm run build` — clean.

---

### Task 4: Final verification

- [ ] **Step 1:** `go build ./... && go test ./...` — all pass.
- [ ] **Step 2:** `cd web && npm run build` — clean.
- [ ] **Step 3:** Behavior smoke against a THROWAWAY copy of knowledge.db (./cmd/<name>-tmp pattern; NEVER the real DB): open store, ListTeams (expect 3), pick the duplicate "Review Writers" with 0 entries, call TeamDataCounts (expect zeros or low counts), DeleteTeam or DeleteTeamMigrate to the other Review Writers, re-ListTeams (expect 2). Print OK line; clean up.
- [ ] **Step 4:** Report. **Do not commit.**
