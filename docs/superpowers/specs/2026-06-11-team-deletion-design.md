# Team Deletion with Data Migration — Design

**Date:** 2026-06-11
**Status:** Approved

## Problem

Teams can be created, renamed, and enabled/disabled but not deleted from the UI. The backend
`DELETE /api/admin/teams/{id}` exists but is a bare `DELETE FROM teams`: with FKs enforced it
500s when users/keys exist, would orphan entries/agents otherwise, and no UI calls it.

## Decision (user-approved)

Deleting a team with data prompts the admin to pick another team; all data is migrated to
the target, then the team row is removed.

## API

`DELETE /api/admin/teams/{id}` (existing superadmin route) gains optional `?migrate_to=<teamID>`:

| Case | Behavior |
|------|----------|
| No `migrate_to`, team empty | Delete team + its `team_settings` row → 200 `{ok:true}` |
| No `migrate_to`, team has data | **409** `{error:"team_not_empty", counts:{users, api_keys, entries, clusters, agents, rules}}` |
| `migrate_to` valid (exists, ≠ source) | Transactional migrate + delete → 200 with `TeamMigrationSummary` JSON |
| `migrate_to` == source or unknown | 400 |
| Source team unknown | 404 |

## Migration semantics

Re-stamp `team_id` to the target on: `users`, `api_keys`, `entries`, `clusters`, `agents`,
`agent_versions`, `rules`, `dataset_snapshots`, `pipeline_runs`, `feed_activity`,
`activity_log`. Entry-linked tables (usage_events, outcome_ratings) follow entries
automatically (keyed by entry_id).

- **Agent domain conflicts:** agents are UNIQUE(domain, team_id). A source agent whose
  domain already exists in the target is DELETED (with its agent_versions) instead of moved
  — agents are pipeline-regenerable. Reported as `agents_skipped` in the summary.
- **team_settings:** the source row is deleted; the target's settings are never touched.
- All of the above + the team-row delete happen in ONE transaction per adapter; any failure
  leaves the source team fully intact.

## Storage

```go
// TeamDataCounts reports how many team-owned records exist for id.
type TeamDataCounts struct {
	Users, APIKeys, Entries, Clusters, Agents, Rules int
}
TeamDataCounts(ctx context.Context, id string) (TeamDataCounts, error)

// TeamMigrationSummary reports what DeleteTeamMigrate moved or skipped.
type TeamMigrationSummary struct {
	Users, APIKeys, Entries, Clusters, Agents, AgentsSkipped, Rules int
}
DeleteTeamMigrate(ctx context.Context, id, targetID string) (TeamMigrationSummary, error)
```

Both on the TeamStore interface, both adapters. `DeleteTeam(ctx, id)` keeps its signature
but additionally deletes the `team_settings` row (still errors via FK if users/keys exist —
the handler guards with TeamDataCounts first, so the FK error is a backstop, not UX).

## UI (AdminTeams.tsx)

- Delete (trash) icon per team row.
- Click → call DELETE without `migrate_to`.
  - 200 → refresh list.
  - 409 → dialog showing the counts and a "Migrate data to" Select of the OTHER teams,
    confirm button labeled with the consequence ("Move 3 users, 35 entries … to <team>,
    then delete <team>"); confirm re-calls DELETE with `migrate_to`; success → refresh +
    show the summary (snackbar or inline).
- Empty team → simple confirm dialog before the first DELETE ("Delete team <name>? This
  cannot be undone.") — i.e. always confirm before any DELETE call; the 409 path then
  upgrades the dialog to migration mode.
- Errors render in the dialog.
- `api.ts`: `deleteTeam(id, migrateTo?)` returning the parsed response; 409 surfaced with
  its counts (not thrown as a generic error).

## Non-Goals

- No cascade-delete option.
- No undo.
- No migration of the analysis cache (content-addressed; team_id informational).

## Testing

- Storage (SQLite; Postgres compiles): TeamDataCounts accuracy; DeleteTeamMigrate moves
  every category (seed one of each), agent domain conflict → skipped+deleted (target's
  agent untouched), summary counts correct, source team gone, transactional failure leaves
  source intact (force one with a closed/canceled ctx or a conflicting constraint);
  DeleteTeam cleans settings row.
- Web: 409 with counts; migrate happy path returns summary; self-target 400; unknown
  target 400; unknown team 404.
- UI: Vite build clean.
