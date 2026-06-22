# Per-User Visibility & Cross-Team Sharing — Implementation Plan

> Executed via superpowers:subagent-driven-development. Spec:
> `docs/superpowers/specs/2026-06-22-user-visibility-and-sharing-design.md`.

**Goal:** Let users suppress (hide/mute) team knowledge per-user, and share items
cross-team via single-use links that the recipient imports as a pending copy.

**Build/test gate (Go server, NOT ./api):** `go build ./...`, `go test ./...`,
web `cd web && npm run build`.

## Phase 0 — User identity in token context
- `internal/auth/middleware.go` bearer branch (~line 106): add `UserID: key.UserID`
  to the `TeamContext`; add a `KeyType string` field to `TeamContext` and set
  `tc.KeyType = key.KeyType`. Test: a request with a user-scoped key exposes
  `GetTeamContext(ctx).UserID`; a team key leaves it empty.

## Phase 1 — Per-user visibility
- **Task 1 (storage):** `VisibilityRule` type + table `user_visibility_rules`
  (SQLite migrate ALTER/CREATE + Postgres mirror); `Store` methods
  `AddVisibilityRule`, `DeleteVisibilityRule`, `ListVisibilityRules`. TDD CRUD +
  uniqueness. Update interface mocks in mcp/web/pipeline tests.
- **Task 2 (visibility helper):** new pkg `internal/visibility` with a pure
  `Filter(rules []storage.VisibilityRule, ownerAuthor string, entries) []…` for
  both `[]KnowledgeEntry` and `[]SearchResult` (or a predicate `Hidden(rule set,
  author, entry) bool`). Own-entry exemption. Full unit tests.
- **Task 3 (retrieval wiring):** apply the filter, only when `tc.UserID != ""`,
  at `knowledge_tools.go:98` (search), `enrich_context.go:142`, `tools.go`
  `HandleKnowledgeList` (after `ListEntries`), and `web/handlers.go`
  `handleKnowledgeList` (after `ListEntries`). Resolve the user's author string
  via `GetUserByID`. Load rules once per call. Tests: hidden entry excluded for
  muting user, present for others/team tokens.
- **Task 4 (MCP management tools):** `RegisterVisibilityTools`: `knowledge_hide`/
  `knowledge_unhide`(entry_id), `knowledge_mute`/`knowledge_unmute`(kind,value),
  `knowledge_visibility`(). Error clearly when `tc.UserID==""`. Register in the
  server constructor alongside other `Register*Tools`. Tests.
- **Task 5 (web API + SPA):** `GET/POST/DELETE /api/visibility` (member group);
  `MyVisibility.tsx` page + route (`App.tsx`) + nav (`Layout.tsx`) + `api.ts`
  helpers; hide/mute control in the knowledge browser. `npm run build` passes.

## Phase 2 — Cross-team sharing & import
- **Task 6 (storage):** `knowledge_shares` table (SQLite + Postgres);
  `Share` type; `CreateShare`, `GetShare`, `MarkShareUsed`, `RevokeShare`. TDD
  single-use semantics (second import fails). Update mocks.
- **Task 7 (share/import service + MCP tools):** `knowledge_share(entry_id)` →
  `{token,url}`; `knowledge_import(share_id)` builds a copy (destination team,
  `Status="pending"`, author preserved + source-team note), re-embeds via dest
  team `Embedder`/`ChunkConfig`, stores via `StoreEntryChunked`, then
  `MarkShareUsed`. Reject used/revoked; same-team import → "already visible".
  Tests: import creates a pending, searchable copy; token burns.
- **Task 8 (web API + public landing + SPA):** `POST /api/knowledge/{id}/share`,
  public `GET /share/{token}` + `GET /api/share/{token}` (register BEFORE auth MW
  in `routes()`), `POST /api/share/{token}/import` (auth). `ShareLanding.tsx`
  public route in `App.tsx` (outside RequireAuth); "Share" action on knowledge
  detail. `npm run build` passes.

## Phase 3 — Verify & docs
- Full `go build ./...` + `go test ./...` + web build; CHANGELOG entry; commit.

## Notes / invariants
- Imported copies MUST set `Status="pending"` explicitly (backend defaults differ).
- Use `StoreEntryChunked` (embeds), never `BulkImport` (no vectors), for imports.
- Visibility filtering is user-token only; team tokens unfiltered.
- A user's own entries are never hidden by their own rules.
