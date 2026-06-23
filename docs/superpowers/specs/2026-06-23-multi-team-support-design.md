# Multi-team support + superadmin assignment — design

**Date:** 2026-06-23
**Status:** Approved (pending spec review)

## Overview

Today a user belongs to exactly one team (`users.team_id`), reads/writes are
scoped to that single team, and `superadmin` is grantable only via the
`SUPERADMIN_KEY` bootstrap API key. This spec adds:

1. The ability to grant the `superadmin` role to a user from the UI (and fixes a
   latent bug that scopes superadmin *users* to their home team in list views).
2. Multi-team membership: a user can belong to several teams. An API key sets the
   default team; an `X-Team-Id` header (MCP) or a web team-switcher pins a
   request to another team the user belongs to. Team IDs are visible and
   copyable in the UI. The left nav is reorganized with a user-avatar menu.

Delivered as **one spec, two sequential phases**. Phase 1 is independently
shippable and unblocks superadmin assignment immediately.

## Decisions (locked)

- **Membership storage:** keep `users.team_id` as the default/home team; add a
  `team_members` join table for additional memberships; backfill home teams.
- **Role scope:** global role per user (one role across all their teams);
  `team_members` defines access only. Per-team roles are out of scope.
- **`X-Team-Id` authorization:** sessions and user-scoped API keys may pin to any
  team the user is a member of; plain team keys stay single-team; superadmin may
  pin to any team. Non-member → 403.
- **Granting superadmin:** only an existing superadmin may grant/revoke it.
- **Web switcher:** yes — the web UI gets an active-team switcher.
- **Nav reorg:** Settings moves to the bottom (above the avatar); a user-avatar
  menu holds an inline profile header, the team switcher, My Visibility, My API
  Keys, and Sign out. Admin-only nav items are gated by role.
- **Writes in see-all mode:** fall back to the actor's home team (not an error).
- **Cross-team knowledge:** superadmin can both **move** (reassign, single
  target) and **copy** (duplicate into multiple targets) knowledge items.
- **Membership management:** superadmin manages any team; an admin manages only
  teams they belong to.

---

## Phase 1 — Superadmin assignment + read-scoping fix

### 1.1 Grant superadmin

- `handleSetUserRole` (`internal/web/admin_handlers.go`, route `PUT
  /api/users/{id}/role`, `server.go:269`, `RequireAdmin`): permit the target
  value `"superadmin"` **only when the actor's role is `superadmin`**. Keep the
  existing "cannot grant a role ≥ your own" guard for admins. Extend
  `validAdminRole` (`admin_handlers.go:500`) so `superadmin` is accepted only on
  the superadmin-actor path.
- Demotion from superadmin likewise requires a superadmin actor.

### 1.2 Fix superadmin list-scoping

Today list/count/search read paths pass `tc.TeamID` straight into the SQL filter
(`handlers.go:62-138`, `tools.go:199`). Superadmin works in lists only because
superadmin *keys* have an empty `TeamID`. A superadmin *user* has a non-empty
home `team_id` and would be wrongly scoped.

- Add `TeamContext.ListScopeTeamID()` to `internal/auth/middleware.go`:
  - Phase 1 behavior: returns `""` (see-all) when `Role == "superadmin"`, else
    `tc.TeamID`.
  - Phase 2 extends it (see 2.3).
- Replace raw `tc.TeamID` with `tc.ListScopeTeamID()` in the read/list/count/
  search callsites (`handlers.go` list/count/search; `mcp/tools.go`
  `resolveActorTeam`-derived list filters). Writes and per-record `CanAccess`
  checks are unchanged.

### 1.3 UI (Phase 1)

- User-editor role dropdown gains **superadmin**, rendered only when the logged-in
  user is a superadmin (from `GET /api/me`).
- Teams admin page (`/admin/teams`) shows each **team ID with a copy button**
  (read-only; existing `GET /api/admin/teams`).

---

## Phase 2 — Multi-team membership + routing + UI

### 2.1 Data model & migration

New join table (both SQLite and Postgres):

```sql
CREATE TABLE team_members (
  user_id    TEXT NOT NULL,
  team_id    TEXT NOT NULL,
  created_at TIMESTAMP/DATETIME DEFAULT now,
  PRIMARY KEY (user_id, team_id)
);
```

- `users.team_id` remains the home/default team (unchanged semantics).
- **Backfill migration:** for every user with non-empty `team_id`, insert
  `(user_id, team_id)` into `team_members` (idempotent — `INSERT ... ON CONFLICT
  DO NOTHING` / `INSERT OR IGNORE`).
- A user's effective membership set = union of home team and `team_members` rows.
  The home-team membership is implicit and cannot be removed via the membership
  API (reassign home first).

Storage methods on `TeamStore` (`internal/storage/teams.go`), implemented for
both engines:

- `AddTeamMember(ctx, userID, teamID) error`
- `RemoveTeamMember(ctx, userID, teamID) error` (no-op/400 on home team)
- `ListUserTeams(ctx, userID) ([]Team, error)` (union, includes home; each with
  id + name; sorted)
- `IsTeamMember(ctx, userID, teamID) (bool, error)` (true if home team or a
  `team_members` row)

### 2.2 Effective-team resolution (`ActiveTeamMiddleware`)

A new middleware in `internal/auth`, chained **after** `RequireAuth` in both the
web router (`internal/web/server.go`) and the MCP-HTTP chain
(`internal/mcp/remote.go:25`). stdio MCP has no headers and is unaffected.

Algorithm, given the resolved `TeamContext tc` and header `X-Team-Id`:

1. If `X-Team-Id` is absent/empty → no change (default active team is `tc.TeamID`,
   or see-all for superadmin via `ListScopeTeamID`).
2. If present (`target`):
   - `tc.Role == "superadmin"` → allow; set `tc.TeamID = target`.
   - User identity (`tc.UserID != ""`, i.e. session or user-scoped key) → allow
     iff `IsTeamMember(tc.UserID, target)`; else **403** "not a member of the
     requested team".
   - Plain team key (`tc.UserID == ""`, non-superadmin) → allow only if `target
     == tc.TeamID`; else **403**.
   - Unknown team id → **403** (treated as non-membership; no team enumeration).
3. On success re-inject the updated `tc`. A sentinel records that an explicit
   active team was chosen (for `ListScopeTeamID`, see 2.3).

### 2.3 ListScopeTeamID (Phase 2 form)

`TeamContext` gains an `ActiveTeamExplicit bool` (set by the middleware when an
`X-Team-Id` override was applied). `ListScopeTeamID()` becomes:

- explicit active team set → return `tc.TeamID` (scopes even a superadmin to the
  chosen team);
- else `Role == "superadmin"` → return `""` (see-all);
- else return `tc.TeamID`.

### 2.4 Writes with no concrete team

A write (store knowledge, etc.) needs a concrete team. If the resolved scope is
empty (superadmin in see-all mode with no `X-Team-Id`), write handlers **fall
back to the actor's home team** (`users.team_id`; for a superadmin *key* with no
home team the existing team-less behavior remains). Reads in see-all mode are
unaffected. A `WriteTargetTeamID()` helper on `TeamContext` returns: explicit
active team → that; else `tc.TeamID` if non-empty; else the home team.

### 2.5 Endpoints

- `GET /api/me/teams` — any authenticated caller. Returns the teams the caller
  can act in (`ListUserTeams` for a user identity; the single key team for a
  plain team key; all teams for superadmin), each `{id, name}`, plus which is the
  current active team. Powers the switcher, the My-Teams list, and copy buttons.
- Membership management (route gated `RequireAdmin`):
  - `POST /api/admin/users/{id}/teams` body `{team_id}` → `AddTeamMember`.
  - `DELETE /api/admin/users/{id}/teams/{teamId}` → `RemoveTeamMember`.
  - Authorization inside the handler: **superadmin** may manage membership for
    any team; an **admin** may manage membership only for teams they themselves
    belong to (target `team_id` ∈ the actor's effective teams — `IsTeamMember`
    for a user identity, or `== key.TeamID` for a team key). Otherwise 403.

### 2.6 UI (Phase 2)

**Active team (client):** `apiFetch` (`web/src/lib/api.ts:4`) injects
`X-Team-Id: <localStorage['tkm_active_team']>` alongside `Authorization`, when
set. Switching teams updates the stored value and refetches (full reload or query
invalidation). Empty selection = home team / see-all (superadmin).

**Left-nav reorg (`web/src/components/Layout.tsx`):**

```
🧠 Tribal Knowledge
─────────────
Dashboard
Knowledge … (main nav)
Agents / Analytics / Pending Queue …
Users / Teams / All Users / Auth Config   ← admin-only, role-gated
   (flex space)
─────────────
⚙ Settings               ← moved here, above the avatar
─────────────
🟣 David Sandor  ▾        ← user-avatar button (replaces "Sign Out")
```

(My Visibility and My API Keys are removed from the flat nav and live in the
avatar menu instead.)

Avatar button opens a menu (MUI Menu/Popover) containing, top to bottom:
- **Profile header (inline, not a page):** the user's name, email, role, and home
  team, rendered directly at the top of the popout from `GET /api/me`.
- **Active team** switcher: lists `GET /api/me/teams` with the active one checked;
  superadmin also gets an "All teams" option that clears `X-Team-Id`.
- **My Visibility**, **My API Keys** links.
- **Sign out** (existing `handleSignOut` logic).

Avatar shows display name/initials from `GET /api/me`.

**Nav role-gating:** admin-only items (Users, Teams, All Users, Auth Config) are
shown only to admin/superadmin; the user's role comes from `GET /api/me`. My
Visibility and My API Keys move into the avatar menu (removed from the flat nav).

**User editor:** a multi-team membership editor (multi-select / checkbox list of
teams) alongside the role dropdown, backed by the membership endpoints (2.5).

### 2.7 Move / copy knowledge across teams (superadmin)

Two superadmin-only bulk actions on knowledge entries:

- **Move (reassign):** N selected entries → one target team; changes each entry's
  `team_id` in place (no duplication). Storage: `ReassignEntriesTeam(ctx,
  entryIDs []string, targetTeamID string) error`. Endpoint: `POST
  /api/admin/knowledge/move` body `{entry_ids: [], team_id}`.
- **Copy (duplicate):** N selected entries → one or more target teams; each
  (entry, team) pair becomes a new entry in that team, reusing the existing
  copy logic in `internal/sharing` (`Import` copies an entry into a dest team as
  a new pending entry — factor out the copy core so it can run without a share
  token). Endpoint: `POST /api/admin/knowledge/copy` body `{entry_ids: [],
  team_ids: []}`. Copies land as **pending** in each destination, consistent with
  imported shares.

Both routes are `RequireSuperadmin`. Target team ids are validated to exist.

**UI:** the Knowledge list gains multi-select with a bulk-action bar offering
**Move to team…** (single-team picker) and **Copy to teams…** (multi-team
picker), shown only to superadmins.

---

## Testing

- **Storage:** `team_members` CRUD; `ListUserTeams` union includes home;
  `IsTeamMember` true for home + membership, false otherwise; `RemoveTeamMember`
  refuses the home team; backfill migration inserts one row per existing user and
  is idempotent on re-run.
- **Middleware (`ActiveTeamMiddleware`):** member allowed; non-member 403;
  superadmin any team; plain team key rejected for a foreign team; absent header
  is a no-op.
- **Authorization:** `handleSetUserRole` allows `superadmin` only for a
  superadmin actor; admin attempt → 403.
- **Read scoping:** `ListScopeTeamID` returns "" for see-all superadmin, the
  active team when explicit, `tc.TeamID` otherwise.
- **Writes:** see-all mode write falls back to the home team (`WriteTargetTeamID`).
- **Handlers:** `GET /api/me/teams` shape; membership add/remove with admin
  scoped to own teams (admin adding to a foreign team → 403); superadmin any.
- **Move/copy:** `ReassignEntriesTeam` changes `team_id`; copy creates pending
  entries in each target team; both reject non-superadmin → 403.
- **MCP:** a tool call with `X-Team-Id` scopes to the target team for a member;
  403 for a non-member.

## Risks & edge cases

- **Superadmin write in see-all mode** → handled by the 400 in 2.4.
- **stdio MCP** has no headers → no override; behaves as today (empty team,
  see-all). Documented.
- **Whitelist auto-assignment** continues to drive only the *home* team on OIDC
  login; additional memberships are managed explicitly by a superadmin. Not
  changed in this spec.
- **Backfill** must run before the membership API is used; ordered as a normal
  migration step alongside existing ones (`sqlite.go`, `postgres*.go`).

## Out of scope

- Per-team roles (role stays global per user).
- Per-key allowed-teams lists for plain team keys.
- Auto-adding memberships from whitelist matches.
- Entries belonging to multiple teams simultaneously (the model stays
  single-`TeamID`; cross-team distribution is move or copy, §2.7).

## Phasing summary

- **Phase 1:** 1.1 grant superadmin, 1.2 `ListScopeTeamID` (superadmin form),
  1.3 UI (dropdown + team-ID copy). Shippable alone.
- **Phase 2:** 2.1 model+migration, 2.2 middleware, 2.3 `ListScopeTeamID`
  (explicit-active form), 2.4 write fallback, 2.5 endpoints (membership,
  admin-scoped), 2.6 UI (switcher, nav reorg, avatar menu w/ inline profile,
  membership editor, role-gating), 2.7 move/copy knowledge across teams.
