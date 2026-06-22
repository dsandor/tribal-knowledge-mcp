# Per-User Knowledge Visibility & Cross-Team Sharing — Design

**Date:** 2026-06-22
**Status:** Approved — implementing
**Author:** brainstormed with David Sandor

## Problem

All team knowledge is visible to every team member. A user wants to *subtract*
knowledge that doesn't fit their workflow (e.g. ignore a teammate's git-workflow
entries) without deleting it for everyone. Separately, users want to share an
item with another team, who can then import a copy into their own knowledge base.

## Goals & Decisions

1. **Per-user visibility (opt-out / suppression).** Default: a user sees all
   team knowledge. The user subtracts via suppression rules of three kinds:
   `item` (hide one entry), `author` (mute an author), `tag`/`domain` (mute a
   topic). A user's **own** authored entries are never hidden by their rules.
2. **Filtering applies to retrieval only** — `knowledge_search`,
   `enrich_context`, `knowledge_list` (MCP), and the web knowledge browser.
3. **Identity:** only **user-scoped tokens** (carry `user_id`) are filtered;
   shared **team tokens** see the full unfiltered team view.
4. **Sharing:** a **single-use** share token. Within a team it's just a deep
   link. **Cross-team**, the recipient (authenticated, any team) imports a
   **copy** into their team as a **`pending`** entry (their curator queue),
   re-chunked/re-embedded under their team's config, original author attributed.
   The token is burned on import.

## Non-Goals

- Ongoing cross-team read access (we copy, not grant).
- Per-user filtering for team (shared) tokens (no single user identity).
- Reusing `BulkImport` for cross-team import — it stores no embeddings, so the
  copy would not be searchable. Use the chunk+embed `StoreEntryChunked` path.

## Architecture

### Phase 0 — User identity in token context (foundational, tiny)

`internal/auth/middleware.go` bearer branch (~line 106) already has `key.UserID`
loaded by `GetAPIKeyByHash` but drops it. Add `UserID: key.UserID` (and a
`KeyType` field on `TeamContext` set to `key.KeyType`) so MCP tool handlers can
identify the calling user for user-scoped tokens. Session requests already set
`UserID`. Team keys have empty `UserID` → naturally unfiltered.

### Phase 1 — Per-user visibility

**Data model** (new table, SQLite + Postgres):
```sql
CREATE TABLE user_visibility_rules (
  id         TEXT PRIMARY KEY,
  user_id    TEXT NOT NULL,
  rule_type  TEXT NOT NULL,           -- 'item' | 'author' | 'tag' | 'domain'
  value      TEXT NOT NULL,           -- entry id, author string, tag, or domain
  created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
  UNIQUE(user_id, rule_type, value)
);
CREATE INDEX idx_uvr_user ON user_visibility_rules(user_id);
```

**Storage methods** (on `Store`): `AddVisibilityRule(ctx, rule)`,
`DeleteVisibilityRule(ctx, userID, ruleType, value)`,
`ListVisibilityRules(ctx, userID) ([]VisibilityRule, error)`.

**Visibility helper** (`internal/visibility/visibility.go`): given a user's rule
set and a slice of entries, return the entries that remain visible. Pure +
unit-testable. An entry E is hidden for user U iff E.Author != U's identity AND
(rule(item, E.ID) ∨ rule(author, E.Author) ∨ rule(domain, E.Domain) ∨
∃ t ∈ E.Tags∪E.AutoTags: rule(tag, t)). Own-entry exemption uses the user's
author string (resolve via `GetUserByID` → author = user email/name as stored on
entries; match the convention `HandleKnowledgeStore` uses for `Author`).

**Retrieval wiring** (only when `tc.UserID != ""`):
- `knowledge_search` — `internal/mcp/knowledge_tools.go:98` (after team filter,
  before topK truncation; handler already over-fetches 2×).
- `enrich_context` — `internal/mcp/enrich_context.go:142` (after team filter,
  before wantK slice; over-fetches 2×).
- `knowledge_list` (MCP) — `internal/mcp/tools.go:201`→206 (filter the
  `[]KnowledgeEntry` before marshal).
- web browser — `internal/web/handlers.go:114`→119 (filter before writeJSON).
Load the user's rules once per call; filter in Go. (Over-fetch already present on
the ranked paths covers suppressed drops; list paths filter post-query.)

**Management — MCP tools:** `knowledge_hide(entry_id)` / `knowledge_unhide`,
`knowledge_mute(kind, value)` / `knowledge_unmute(kind, value)`,
`knowledge_visibility()` (list my rules). All resolve the caller via
`auth.GetTeamContext(ctx).UserID`; error clearly if called with a team token
(no user identity).

**Management — web:** `GET/POST/DELETE /api/visibility` (member group) and a
"My Visibility" SPA page (`web/src/pages/MyVisibility.tsx`, route in `App.tsx`,
nav in `Layout.tsx`, API helpers in `api.ts`) listing rules with remove buttons,
plus a hide/mute control on knowledge items in the browser.

### Phase 2 — Cross-team sharing & import

**Data model** (new table):
```sql
CREATE TABLE knowledge_shares (
  id                TEXT PRIMARY KEY,   -- random unguessable token
  entry_id          TEXT NOT NULL,
  source_team_id    TEXT NOT NULL,
  created_by        TEXT NOT NULL,      -- user id
  used_at           DATETIME,           -- set on import (single-use)
  used_by           TEXT,               -- importing user id
  imported_entry_id TEXT,               -- the new copy's id
  revoked_at        DATETIME,
  created_at        DATETIME DEFAULT CURRENT_TIMESTAMP
);
```
Single-use: an import is allowed only when `used_at IS NULL AND revoked_at IS
NULL`; importing sets `used_at/used_by/imported_entry_id`.

**Storage methods:** `CreateShare`, `GetShare(token)`, `MarkShareUsed(...)`,
optionally `RevokeShare`.

**Flow / endpoints:**
- Create: MCP `knowledge_share(entry_id)` and web `POST /api/knowledge/{id}/share`
  → returns `{ token, url: "/share/<token>" }`. Caller must have team access to
  the entry.
- View (public landing): `GET /share/{token}` SPA page + `GET /api/share/{token}`
  returning the shared item's display fields (title, content, type, domain,
  author, source team) and whether it's still importable. Registered BEFORE the
  auth middleware in `routes()` (public), explicitly (SPA fallback 404s `/api/`).
- Import: MCP `knowledge_import(share_id)` and web `POST /api/share/{token}/import`
  (auth required). Builds a `KnowledgeEntry` copy with destination `TeamID/Team`,
  `Status="pending"`, original author/source preserved (e.g. author kept, a note
  or description suffix crediting source team), re-embeds via the destination
  team's embedder + `ChunkConfig`, stores via `StoreEntryChunked`, then
  `MarkShareUsed`. Reject if already used/revoked, or if importing into the same
  team that owns it (deep-link case → just view).
- SPA: `/share/:token` public route → page showing the item + "Import to my team"
  (requires login). Add a "Share" action on knowledge detail that calls the
  create endpoint and surfaces the link.

## Error Handling

- Visibility/sharing MCP tools called with a team token (no `user_id`) → clear
  error: "requires a user-scoped token".
- Import of a used/revoked token → 409/clear tool error.
- Import into the owning team → no-op with a message ("already visible to you").
- Status MUST be set explicitly to `"pending"` on import (backend defaults
  diverge: SQLite→approved, Postgres→pending).

## Testing

- `visibility` helper: item/author/tag/domain matches, own-entry exemption,
  empty rules = all visible, union of tags+auto_tags.
- Storage: rule CRUD + uniqueness; share create/get/mark-used (single-use:
  second import fails); imported entry is `pending` and has chunk vectors.
- Retrieval: a hidden entry is excluded from search/enrich/list for the muting
  user but present for others / for team tokens.
- Import: re-embeds under destination config; original author preserved; token
  burned.
- Web build passes.

## Sequencing

Phase 0 → Phase 1 (ships value alone) → Phase 2. Each phase builds, tests, and
commits independently.
