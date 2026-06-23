# Multi-team Support + Superadmin Assignment Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.
>
> **PROJECT POLICY — DO NOT COMMIT.** This repository's owner commits all code manually. Every task ends at a **verification checkpoint** (build + tests green), NOT a `git commit`. Never run `git commit`/`git push`. After verifying, leave changes in the working tree and report.
>
> **Build/test commands** (run from repo root unless noted):
> - Go build: `go build ./...`
> - Go tests for a package: `go test ./internal/<pkg>/`
> - Go vet: `go vet ./internal/<pkg>/`
> - Web build (from `web/`): `npm run build`
> - cgo/sqlite deprecation warnings on macOS are expected noise; ignore lines matching `deprecated|sqlite3.h|cgo-gcc`.

**Goal:** Let a user belong to multiple teams (default team from the API key/home team, overridable per request via `X-Team-Id` / a web switcher), let superadmins be assigned from the UI, and let superadmins move/copy knowledge across teams.

**Architecture:** Keep `users.team_id` as the home/default team; add a `team_members` join table for extra memberships (global role, not per-team). Resolve an "active team" per request in a new `ActiveTeamMiddleware` chained after `RequireAuth` (web + MCP-HTTP); downstream reads/writes use helper methods on `TeamContext`. UI gains a team switcher in a user-avatar menu, role-gated nav, and superadmin bulk move/copy.

**Tech Stack:** Go (chi router, database/sql, SQLite + Postgres), `mark3labs/mcp-go`, React + TypeScript + MUI (Vite).

**Spec:** `docs/superpowers/specs/2026-06-23-multi-team-support-design.md`

---

## File Structure

**Phase 1**
- `internal/auth/middleware.go` — add `TeamContext.ListScopeTeamID()`.
- `internal/auth/middleware_test.go` — tests for the helper.
- `internal/web/handlers.go`, `internal/mcp/tools.go` — use `ListScopeTeamID()` in list/count/search read scoping.
- `internal/web/admin_handlers.go` — superadmin grant rule in `handleSetUserRole`.
- `web/src/pages/Users.tsx` (and/or the user editor component) — superadmin dropdown option (role-gated).
- `web/src/pages/Teams.tsx` (`/admin/teams`) — team ID + copy button.

**Phase 2 — backend**
- `internal/storage/teams.go` — `team_members` types, `TeamStore` methods, `User`/`Team` reuse.
- `internal/storage/sqlite.go`, `internal/storage/postgres_teams.go` — schema migration + backfill for `team_members`.
- `internal/storage/teams_sqlite.go`, `internal/storage/postgres_teams.go` — membership method impls; `ReassignEntriesTeam`.
- `internal/storage/teams_test.go` — membership + reassign tests.
- `internal/auth/middleware.go` — `ActiveTeamMiddleware`, `TeamContext.ActiveTeamExplicit`, `WriteTargetTeamID()`, extend `ListScopeTeamID()`.
- `internal/web/server.go` — chain `ActiveTeamMiddleware`; new routes; role-gate nothing server-side beyond existing.
- `internal/mcp/remote.go` — chain `ActiveTeamMiddleware`.
- `internal/web/team_handlers.go` (new) — `handleMyTeams`, `handleAddMembership`, `handleRemoveMembership`.
- `internal/web/knowledge_move_handlers.go` (new) — `handleMoveKnowledge`, `handleCopyKnowledge`.
- `internal/sharing/sharing.go` — factor a token-free `copyEntryToTeam` core out of `Import`.
- `internal/web/handlers.go`, `internal/mcp/tools.go` — writes use `WriteTargetTeamID()`.

**Phase 2 — frontend**
- `web/src/lib/api.ts` — inject `X-Team-Id`; `getMyTeams`, membership + move/copy calls; `getMe`.
- `web/src/components/Layout.tsx` — nav reorg, avatar menu, switcher, role-gating.
- `web/src/components/UserMenu.tsx` (new) — avatar popout (profile header, switcher, links, sign out).
- `web/src/pages/Users.tsx` — multi-team membership editor.
- `web/src/pages/Knowledge.tsx` — multi-select + bulk move/copy actions (superadmin).

---

# PHASE 1 — Superadmin assignment + read-scoping fix

## Task 1: `ListScopeTeamID()` helper (Phase 1 form)

**Files:**
- Modify: `internal/auth/middleware.go`
- Test: `internal/auth/middleware_test.go`

- [ ] **Step 1: Write the failing test**

Add to `internal/auth/middleware_test.go` (create file if absent, `package auth`):

```go
package auth

import "testing"

func TestListScopeTeamID(t *testing.T) {
	cases := []struct {
		name string
		tc   TeamContext
		want string
	}{
		{"member scoped to own team", TeamContext{Role: "member", TeamID: "t1"}, "t1"},
		{"admin scoped to own team", TeamContext{Role: "admin", TeamID: "t1"}, "t1"},
		{"superadmin sees all", TeamContext{Role: "superadmin", TeamID: "t1"}, ""},
		{"superadmin empty stays empty", TeamContext{Role: "superadmin"}, ""},
	}
	for _, c := range cases {
		if got := c.tc.ListScopeTeamID(); got != c.want {
			t.Errorf("%s: ListScopeTeamID() = %q, want %q", c.name, got, c.want)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/auth/ -run TestListScopeTeamID`
Expected: build failure — `tc.ListScopeTeamID undefined`.

- [ ] **Step 3: Implement the helper**

Add to `internal/auth/middleware.go` near `EffectiveActorID`:

```go
// ListScopeTeamID returns the team id to use when scoping list/count/search
// reads. Superadmins see all teams (empty filter); everyone else is scoped to
// their resolved team. Task 7 extends this for explicit active-team overrides.
func (tc TeamContext) ListScopeTeamID() string {
	if tc.Role == "superadmin" {
		return ""
	}
	return tc.TeamID
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/auth/ -run TestListScopeTeamID`
Expected: PASS.

- [ ] **Step 5: Use the helper in read paths**

In `internal/web/handlers.go`, replace `tc.TeamID` with `tc.ListScopeTeamID()` in the list/count/search read scoping ONLY (not writes, not per-record `CanAccess`). Known callsites to update (verify each is a read-scope use): the `ListFilter{TeamID: tc.TeamID}` constructions in `handleKnowledgeList` (~`handlers.go:130-138`) and the list near `handlers.go:439`; `CountEntries`, `ListClusters`, `ListAgents`, `GetLatestPipelineRun`, `ListSnapshots` calls that pass `tc.TeamID` (~`handlers.go:62-77,213,226,290,569`). In `internal/mcp/tools.go`, the list filter derived for `HandleKnowledgeList` (~`tools.go:194-200`).

Grep to find them all:

```bash
grep -n "tc.TeamID" internal/web/handlers.go internal/mcp/tools.go
```

For each that feeds a list/count/search filter, change to `tc.ListScopeTeamID()`. Leave write paths (`handleKnowledgeStore` `Team`/`TeamID`) and `CanAccess(tc, ...)` calls unchanged.

- [ ] **Step 6: Verify build + existing tests**

Run: `go build ./...` then `go test ./internal/web/ ./internal/mcp/ ./internal/auth/`
Expected: all PASS. **Checkpoint — do not commit.**

---

## Task 2: Grant superadmin (backend)

**Files:**
- Modify: `internal/web/admin_handlers.go` (`handleSetUserRole`, `validAdminRole`)
- Test: `internal/web/admin_handlers_test.go`

- [ ] **Step 1: Read current handler**

Read `internal/web/admin_handlers.go` around `handleSetUserRole` and the `validAdminRole` map (~`admin_handlers.go:438-500`) to match the existing request/guard shape (it decodes `{role}`, checks `validAdminRole`, and enforces "can't grant ≥ your own role").

- [ ] **Step 2: Write the failing test**

Add to `internal/web/admin_handlers_test.go` following the existing `newAdminTestServer`/`deleteTeamTrackingStore` patterns. Two cases: a superadmin actor CAN set role `superadmin`; an admin actor CANNOT (403/400). Use the existing test harness for building an authed request with a given actor role. Example skeleton (adapt to the harness in that file):

```go
func TestSetUserRole_SuperadminGrant(t *testing.T) {
	// actor = superadmin -> setting role "superadmin" on a target succeeds (2xx)
	// actor = admin      -> setting role "superadmin" is rejected (403)
	// Use the same server/store setup as the other admin_handlers_test.go tests,
	// injecting the actor's TeamContext via auth.WithTestTeamContext.
}
```

(Read the top of `admin_handlers_test.go` to reuse its request helper and store mock; assert on response status.)

- [ ] **Step 3: Run test to verify it fails**

Run: `go test ./internal/web/ -run TestSetUserRole_SuperadminGrant`
Expected: FAIL — admin path currently rejects `superadmin` as invalid, but the superadmin path also rejects it (not yet allowed) → the success case fails.

- [ ] **Step 4: Implement the grant rule**

In `handleSetUserRole`: after decoding the requested role and resolving the actor `tc := auth.GetTeamContext(r.Context())`, add:

```go
if body.Role == "superadmin" {
	if tc.Role != "superadmin" {
		writeError(w, 403, "forbidden", "only a superadmin can grant the superadmin role")
		return
	}
} else if !validAdminRole[body.Role] {
	writeError(w, 400, "bad_request", "invalid role")
	return
}
```

Keep the existing "cannot grant a role greater than or equal to your own" guard for the non-superadmin actor path (an admin still cannot grant admin/superadmin). Ensure the guard does not block a superadmin granting superadmin. Do NOT add `superadmin` to `validAdminRole` (keep that map for the admin-grantable set); the superadmin value is handled by the branch above.

- [ ] **Step 5: Run test to verify it passes**

Run: `go test ./internal/web/ -run TestSetUserRole_SuperadminGrant`
Expected: PASS.

- [ ] **Step 6: Verify build + package tests**

Run: `go build ./...` then `go test ./internal/web/`
Expected: PASS. **Checkpoint — do not commit.**

---

## Task 3: Superadmin option in the role dropdown (UI)

**Files:**
- Modify: `web/src/pages/Users.tsx` (role `<Select>` / editor)
- Modify: `web/src/lib/api.ts` (ensure `getMe()` exposes role; add if missing)

- [ ] **Step 1: Confirm role source**

Read `web/src/pages/Users.tsx` and `web/src/lib/api.ts`. Confirm how the current user's role is known to the page. If `getMe()` doesn't return `role`, add it (the `/api/me` endpoint already returns `role` — see `auth_handlers.go handleMe`).

- [ ] **Step 2: Add the gated option**

In the role select used by the user editor, render a `superadmin` option **only** when the logged-in user's role is `superadmin`:

```tsx
{myRole === 'superadmin' && <MenuItem value="superadmin">superadmin</MenuItem>}
```

Keep existing options (`member`, `curator`, `admin`). The PUT call to `/api/users/{id}/role` is unchanged; the backend (Task 2) enforces authorization.

- [ ] **Step 3: Verify web build**

Run (from `web/`): `npm run build`
Expected: build succeeds. Manually sanity-check the dropdown shows `superadmin` only for a superadmin viewer. **Checkpoint — do not commit.**

---

## Task 4: Team ID + copy button on Teams page (UI)

**Files:**
- Modify: `web/src/pages/Teams.tsx`

- [ ] **Step 1: Add an ID cell with copy**

In the teams list/table, render each team's `id` (monospace) with a copy button using `navigator.clipboard.writeText(team.id)` and a brief "Copied" affordance. Follow the existing copy pattern if one exists (e.g. `APIKeys.tsx` copies raw keys — reuse its component/util if present).

- [ ] **Step 2: Verify web build**

Run (from `web/`): `npm run build`
Expected: success. **Checkpoint — do not commit.**

---

# PHASE 2 — Multi-team membership, routing, UI, move/copy

## Task 5: `team_members` schema + backfill migration

**Files:**
- Modify: `internal/storage/sqlite.go` (schema/migration block)
- Modify: `internal/storage/postgres_teams.go` (migration list `migrateTeams`)
- Test: covered by Task 6 (membership methods rely on the table)

- [ ] **Step 1: Add the SQLite table + backfill**

In `internal/storage/sqlite.go` where the other `CREATE TABLE IF NOT EXISTS` statements live (near the `users` table ~line 261), add:

```sql
CREATE TABLE IF NOT EXISTS team_members (
	user_id    TEXT NOT NULL,
	team_id    TEXT NOT NULL,
	created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
	PRIMARY KEY (user_id, team_id)
);
```

After table creation, backfill home memberships idempotently:

```sql
INSERT OR IGNORE INTO team_members (user_id, team_id)
SELECT id, team_id FROM users WHERE team_id IS NOT NULL AND team_id <> '';
```

- [ ] **Step 2: Add the Postgres table + backfill**

In `internal/storage/postgres_teams.go` `migrateTeams`, add entries to the migration list (same `{name, sql}` shape used at ~line 41-42):

```go
{"team_members", `CREATE TABLE IF NOT EXISTS team_members (
	user_id    TEXT NOT NULL,
	team_id    TEXT NOT NULL,
	created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
	PRIMARY KEY (user_id, team_id)
)`},
{"team_members_backfill", `INSERT INTO team_members (user_id, team_id)
	SELECT id, team_id FROM users WHERE team_id <> ''
	ON CONFLICT DO NOTHING`},
```

- [ ] **Step 3: Verify build + store tests still pass**

Run: `go build ./...` then `go test ./internal/storage/`
Expected: PASS (new table created on open). **Checkpoint — do not commit.**

---

## Task 6: Membership storage methods

**Files:**
- Modify: `internal/storage/teams.go` (`TeamStore` interface)
- Modify: `internal/storage/teams_sqlite.go`, `internal/storage/postgres_teams.go`
- Modify: `internal/web/server_test.go` (mockStore stubs)
- Test: `internal/storage/teams_test.go`

- [ ] **Step 1: Write the failing test**

Add to `internal/storage/teams_test.go`:

```go
func TestTeamMemberships(t *testing.T) {
	s := newTestStoreInternal(t)
	ctx := context.Background()
	tA, _ := s.CreateTeam(ctx, Team{Name: "A", Enabled: true})
	tB, _ := s.CreateTeam(ctx, Team{Name: "B", Enabled: true})
	uid, _ := s.UpsertUser(ctx, User{Email: "u@x.com", Role: "member", TeamID: tA})

	// Home team counts as a membership even without a team_members row.
	if ok, _ := s.IsTeamMember(ctx, uid, tA); !ok {
		t.Fatal("home team should count as membership")
	}
	if ok, _ := s.IsTeamMember(ctx, uid, tB); ok {
		t.Fatal("not a member of B yet")
	}

	if err := s.AddTeamMember(ctx, uid, tB); err != nil {
		t.Fatalf("AddTeamMember: %v", err)
	}
	if ok, _ := s.IsTeamMember(ctx, uid, tB); !ok {
		t.Fatal("should be a member of B after add")
	}

	teams, _ := s.ListUserTeams(ctx, uid)
	if len(teams) != 2 {
		t.Fatalf("want 2 teams (home + B), got %d", len(teams))
	}

	// Cannot remove the home team.
	if err := s.RemoveTeamMember(ctx, uid, tA); err == nil {
		t.Fatal("removing home team should error")
	}
	// Can remove an added team.
	if err := s.RemoveTeamMember(ctx, uid, tB); err != nil {
		t.Fatalf("RemoveTeamMember: %v", err)
	}
	if ok, _ := s.IsTeamMember(ctx, uid, tB); ok {
		t.Fatal("should not be a member of B after remove")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/storage/ -run TestTeamMemberships`
Expected: build failure — methods undefined.

- [ ] **Step 3: Add interface methods**

In `internal/storage/teams.go` `TeamStore`, after `ResolveTeamByEmail`:

```go
	// Team memberships (multi-team). The user's home team (users.team_id) is an
	// implicit membership and cannot be removed via RemoveTeamMember.
	AddTeamMember(ctx context.Context, userID, teamID string) error
	RemoveTeamMember(ctx context.Context, userID, teamID string) error
	ListUserTeams(ctx context.Context, userID string) ([]Team, error)
	IsTeamMember(ctx context.Context, userID, teamID string) (bool, error)
```

- [ ] **Step 4: Implement for SQLite**

In `internal/storage/teams_sqlite.go`:

```go
func (s *SQLiteStore) AddTeamMember(ctx context.Context, userID, teamID string) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT OR IGNORE INTO team_members (user_id, team_id) VALUES (?, ?)`, userID, teamID)
	if err != nil {
		return fmt.Errorf("add team member: %w", err)
	}
	return nil
}

func (s *SQLiteStore) RemoveTeamMember(ctx context.Context, userID, teamID string) error {
	u, err := s.GetUserByID(ctx, userID)
	if err != nil {
		return err
	}
	if u.TeamID == teamID {
		return fmt.Errorf("cannot remove home team membership: %w", ErrInvalid)
	}
	_, err = s.db.ExecContext(ctx,
		`DELETE FROM team_members WHERE user_id = ? AND team_id = ?`, userID, teamID)
	if err != nil {
		return fmt.Errorf("remove team member: %w", err)
	}
	return nil
}

func (s *SQLiteStore) IsTeamMember(ctx context.Context, userID, teamID string) (bool, error) {
	u, err := s.GetUserByID(ctx, userID)
	if err == nil && u.TeamID == teamID && teamID != "" {
		return true, nil
	}
	var one int
	err = s.db.QueryRowContext(ctx,
		`SELECT 1 FROM team_members WHERE user_id = ? AND team_id = ?`, userID, teamID).Scan(&one)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("is team member: %w", err)
	}
	return true, nil
}

func (s *SQLiteStore) ListUserTeams(ctx context.Context, userID string) ([]Team, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT DISTINCT t.id, t.name, COALESCE(t.enabled,1)
		FROM teams t
		WHERE t.id = (SELECT team_id FROM users WHERE id = ?)
		   OR t.id IN (SELECT team_id FROM team_members WHERE user_id = ?)
		ORDER BY t.name`, userID, userID)
	if err != nil {
		return nil, fmt.Errorf("list user teams: %w", err)
	}
	defer rows.Close()
	var out []Team
	for rows.Next() {
		var t Team
		var enabled int
		if err := rows.Scan(&t.ID, &t.Name, &enabled); err != nil {
			return nil, fmt.Errorf("scan user team: %w", err)
		}
		t.Enabled = enabled != 0
		out = append(out, t)
	}
	return out, rows.Err()
}
```

If `ErrInvalid` does not exist in the storage package, use the existing error convention (grep `var Err` in `internal/storage`); reuse an existing sentinel or add `ErrInvalid = errors.New("invalid argument")` alongside `ErrNotFound`.

- [ ] **Step 5: Implement for Postgres**

Mirror in `internal/storage/postgres_teams.go` using `$1,$2` placeholders, `ON CONFLICT DO NOTHING` for insert, and `t.enabled` boolean scan (Postgres `teams.enabled` is a real bool — match the existing `scanTeam`/ListTeams pattern in that file for column names and types).

- [ ] **Step 6: Add mockStore stubs**

In `internal/web/server_test.go` add no-op stubs so `mockStore` still satisfies the interface (all other web test stores embed it):

```go
func (m *mockStore) AddTeamMember(_ context.Context, _, _ string) error    { return nil }
func (m *mockStore) RemoveTeamMember(_ context.Context, _, _ string) error { return nil }
func (m *mockStore) ListUserTeams(_ context.Context, _ string) ([]storage.Team, error) {
	return nil, nil
}
func (m *mockStore) IsTeamMember(_ context.Context, _, _ string) (bool, error) { return false, nil }
```

- [ ] **Step 7: Run test + build**

Run: `go test ./internal/storage/ -run TestTeamMemberships` then `go build ./...` then `go test ./internal/...`
Expected: all PASS. **Checkpoint — do not commit.**

---

## Task 7: ActiveTeamMiddleware + TeamContext helpers

**Files:**
- Modify: `internal/auth/middleware.go` (add `ActiveTeamExplicit`, `WriteTargetTeamID`, extend `ListScopeTeamID`, add `ActiveTeamMiddleware`)
- Modify: `internal/web/server.go` (chain middleware)
- Modify: `internal/mcp/remote.go` (chain middleware)
- Test: `internal/auth/middleware_test.go`

- [ ] **Step 1: Write the failing test**

Add to `internal/auth/middleware_test.go`. Use a tiny fake store implementing the membership lookup the middleware needs. Define a minimal interface in middleware.go (Step 3) — call it `MembershipStore` with `IsTeamMember(ctx, userID, teamID) (bool, error)`.

```go
type fakeMembers struct{ member map[string]bool }

func (f fakeMembers) IsTeamMember(_ context.Context, userID, teamID string) (bool, error) {
	return f.member[userID+"|"+teamID], nil
}

func TestActiveTeamMiddleware(t *testing.T) {
	store := fakeMembers{member: map[string]bool{"u1|tB": true}}
	mw := ActiveTeamMiddleware(store)

	run := func(tc TeamContext, header string) (int, string) {
		var gotTeam string
		h := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			gotTeam = GetTeamContext(r.Context()).TeamID
		}))
		req := httptest.NewRequest("GET", "/", nil)
		if header != "" {
			req.Header.Set("X-Team-Id", header)
		}
		req = req.WithContext(WithTestTeamContext(req.Context(), tc))
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec.Code, gotTeam
	}

	// member overriding to a team they belong to
	if code, team := run(TeamContext{UserID: "u1", TeamID: "tA", Role: "member"}, "tB"); code != 200 || team != "tB" {
		t.Errorf("member->tB: code=%d team=%q", code, team)
	}
	// member overriding to a team they do NOT belong to => 403
	if code, _ := run(TeamContext{UserID: "u1", TeamID: "tA", Role: "member"}, "tC"); code != 403 {
		t.Errorf("member->tC: want 403, got %d", code)
	}
	// superadmin can override anywhere
	if code, team := run(TeamContext{UserID: "s1", Role: "superadmin"}, "tZ"); code != 200 || team != "tZ" {
		t.Errorf("superadmin->tZ: code=%d team=%q", code, team)
	}
	// plain team key (no user) cannot switch to a foreign team => 403
	if code, _ := run(TeamContext{KeyID: "k1", KeyType: "team", TeamID: "tA", Role: "member"}, "tB"); code != 403 {
		t.Errorf("teamkey->tB: want 403, got %d", code)
	}
	// no header => unchanged
	if code, team := run(TeamContext{UserID: "u1", TeamID: "tA", Role: "member"}, ""); code != 200 || team != "tA" {
		t.Errorf("no header: code=%d team=%q", code, team)
	}
}
```

Add imports `net/http`, `net/http/httptest`, `context` to the test file as needed.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/auth/ -run TestActiveTeamMiddleware`
Expected: build failure — `ActiveTeamMiddleware`/`MembershipStore` undefined.

- [ ] **Step 3: Implement helpers + middleware**

In `internal/auth/middleware.go`:

```go
// add to TeamContext struct:
//   ActiveTeamExplicit bool // an X-Team-Id override was applied this request

// MembershipStore is the minimal lookup ActiveTeamMiddleware needs.
type MembershipStore interface {
	IsTeamMember(ctx context.Context, userID, teamID string) (bool, error)
}

// ActiveTeamMiddleware honors an X-Team-Id header to pin the request to another
// team. Must run AFTER RequireAuth. superadmin may pin to any team; a user
// identity may pin to any team they belong to; a plain team key may only pin to
// its own team. Invalid/forbidden overrides return 403.
func ActiveTeamMiddleware(store MembershipStore) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			target := strings.TrimSpace(r.Header.Get("X-Team-Id"))
			if target == "" {
				next.ServeHTTP(w, r)
				return
			}
			tc := GetTeamContext(r.Context())
			allowed := false
			switch {
			case tc.Role == "superadmin":
				allowed = true
			case tc.UserID != "":
				ok, err := store.IsTeamMember(r.Context(), tc.UserID, target)
				allowed = err == nil && ok
			default: // plain team key
				allowed = target == tc.TeamID
			}
			if !allowed {
				writeForbidden(w)
				return
			}
			tc.TeamID = target
			tc.ActiveTeamExplicit = true
			next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), contextKey{}, tc)))
		})
	}
}
```

Extend `ListScopeTeamID` and add `WriteTargetTeamID`:

```go
func (tc TeamContext) ListScopeTeamID() string {
	if tc.ActiveTeamExplicit {
		return tc.TeamID
	}
	if tc.Role == "superadmin" {
		return ""
	}
	return tc.TeamID
}

// WriteTargetTeamID returns the team a new record should be written to. Prefers
// an explicit active team, then the resolved team, then the user's home team.
// homeTeamID is the caller's users.team_id (pass "" if unknown / not a user).
func (tc TeamContext) WriteTargetTeamID(homeTeamID string) string {
	if tc.TeamID != "" {
		return tc.TeamID
	}
	return homeTeamID
}
```

- [ ] **Step 4: Run helper + middleware tests**

Run: `go test ./internal/auth/`
Expected: PASS (Task 1 test still passes — non-explicit superadmin still returns "").

- [ ] **Step 5: Chain the middleware (web)**

In `internal/web/server.go`, in each authenticated route group that currently uses `authMW` (`r.Use(authMW)`), add `r.Use(auth.ActiveTeamMiddleware(s.store))` immediately after. The `s.store` satisfies `MembershipStore` via Task 6. (Public/login groups: leave unchanged.)

- [ ] **Step 6: Chain the middleware (MCP HTTP)**

In `internal/mcp/remote.go` where `auth.RequireAuth(authStore)` wraps the handler (~`remote.go:25`), wrap the result with `auth.ActiveTeamMiddleware(store)` so the order is RequireAuth → ActiveTeamMiddleware → MCP handler. Ensure `store` passed to `StartRemoteMCP` satisfies `MembershipStore` (it does — it's the full store).

- [ ] **Step 7: Verify build + tests**

Run: `go build ./...` then `go test ./internal/auth/ ./internal/web/ ./internal/mcp/`
Expected: PASS. **Checkpoint — do not commit.**

---

## Task 8: `GET /api/me/teams`

**Files:**
- Create: `internal/web/team_handlers.go`
- Modify: `internal/web/server.go` (route)
- Test: `internal/web/team_handlers_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/web/team_handlers_test.go`. Configure `mockStore` (extend it if needed with a `userTeams []storage.Team` field returned by `ListUserTeams`) so a session/user caller gets their teams. Assert `GET /api/me/teams` returns 200 and a JSON array containing the team ids, plus the `active_team` field. Follow the request-building pattern in `server_test.go`.

- [ ] **Step 2: Run to verify fail**

Run: `go test ./internal/web/ -run TestMyTeams`
Expected: 404/handler-undefined failure.

- [ ] **Step 3: Implement handler**

In `internal/web/team_handlers.go`:

```go
package web

import (
	"net/http"

	"github.com/dsandor/memory/internal/auth"
)

func (s *Server) handleMyTeams(w http.ResponseWriter, r *http.Request) {
	tc := auth.GetTeamContext(r.Context())
	type teamDTO struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	var teams []teamDTO
	switch {
	case tc.Role == "superadmin":
		all, _ := s.store.ListTeams(r.Context())
		for _, t := range all {
			teams = append(teams, teamDTO{t.ID, t.Name})
		}
	case tc.UserID != "":
		ut, _ := s.store.ListUserTeams(r.Context(), tc.UserID)
		for _, t := range ut {
			teams = append(teams, teamDTO{t.ID, t.Name})
		}
	default: // plain team key
		if t, _ := s.store.GetTeam(r.Context(), tc.TeamID); t != nil {
			teams = append(teams, teamDTO{t.ID, t.Name})
		}
	}
	writeJSON(w, map[string]any{"teams": teams, "active_team": tc.TeamID})
}
```

- [ ] **Step 4: Register route**

In `internal/web/server.go`, in an authenticated (non-admin) route group: `r.Get("/api/me/teams", s.handleMyTeams)`.

- [ ] **Step 5: Run test + build**

Run: `go test ./internal/web/ -run TestMyTeams` then `go build ./...`
Expected: PASS. **Checkpoint — do not commit.**

---

## Task 9: Membership management endpoints

**Files:**
- Modify: `internal/web/team_handlers.go` (`handleAddMembership`, `handleRemoveMembership`)
- Modify: `internal/web/server.go` (routes under `RequireAdmin`)
- Test: `internal/web/team_handlers_test.go`

- [ ] **Step 1: Write the failing test**

Cases: superadmin adds membership to any team (2xx); admin adds to a team they belong to (2xx); admin adds to a team they do NOT belong to (403). Use `mockStore` with controllable `IsTeamMember`. Assert status + that `AddTeamMember` was invoked (track via a flag in the mock).

- [ ] **Step 2: Run to verify fail**

Run: `go test ./internal/web/ -run TestMembershipEndpoints`
Expected: handler-undefined failure.

- [ ] **Step 3: Implement handlers**

```go
func (s *Server) authorizeTeamManage(r *http.Request, teamID string) bool {
	tc := auth.GetTeamContext(r.Context())
	if tc.Role == "superadmin" {
		return true
	}
	if tc.UserID != "" {
		ok, _ := s.store.IsTeamMember(r.Context(), tc.UserID, teamID)
		return ok
	}
	return teamID == tc.TeamID
}

func (s *Server) handleAddMembership(w http.ResponseWriter, r *http.Request) {
	userID := chi.URLParam(r, "id")
	var body struct{ TeamID string `json:"team_id"` }
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.TeamID == "" {
		writeError(w, 400, "bad_request", "team_id required")
		return
	}
	if !s.authorizeTeamManage(r, body.TeamID) {
		writeForbidden(w)
		return
	}
	if err := s.store.AddTeamMember(r.Context(), userID, body.TeamID); err != nil {
		writeError(w, 500, "internal_error", "add membership failed")
		return
	}
	writeJSON(w, map[string]any{"ok": true})
}

func (s *Server) handleRemoveMembership(w http.ResponseWriter, r *http.Request) {
	userID := chi.URLParam(r, "id")
	teamID := chi.URLParam(r, "teamId")
	if !s.authorizeTeamManage(r, teamID) {
		writeForbidden(w)
		return
	}
	if err := s.store.RemoveTeamMember(r.Context(), userID, teamID); err != nil {
		writeError(w, 400, "bad_request", err.Error())
		return
	}
	writeJSON(w, map[string]any{"ok": true})
}
```

Add the needed imports (`encoding/json`, `github.com/go-chi/chi/v5`) and a `writeForbidden` web-side helper if not present (there is one in `internal/auth`; for web use `writeError(w, 403, "forbidden", "forbidden")`). Match the chi import alias used elsewhere in `server.go`.

- [ ] **Step 4: Register routes**

In `internal/web/server.go` under the `RequireAdmin` group:

```go
r.Post("/api/admin/users/{id}/teams", s.handleAddMembership)
r.Delete("/api/admin/users/{id}/teams/{teamId}", s.handleRemoveMembership)
```

- [ ] **Step 5: Run test + build**

Run: `go test ./internal/web/ -run TestMembershipEndpoints` then `go build ./...`
Expected: PASS. **Checkpoint — do not commit.**

---

## Task 10: Writes use WriteTargetTeamID

**Files:**
- Modify: `internal/web/handlers.go` (`handleKnowledgeStore`)
- Modify: `internal/mcp/tools.go` (`HandleKnowledgeStore` path)
- Test: extend `internal/web` handler tests if a store-write assertion harness exists; otherwise verify by build + manual.

- [ ] **Step 1: Web store write**

In `handleKnowledgeStore` (~`handlers.go:625`), replace the `Team`/`TeamID: tc.TeamID` assignment with the home-team fallback. Resolve home team: if `tc.UserID != ""`, look up `s.store.GetUserByID(ctx, tc.UserID)` for `.TeamID`; else "". Then:

```go
home := ""
if tc.UserID != "" {
	if u, err := s.store.GetUserByID(r.Context(), tc.UserID); err == nil {
		home = u.TeamID
	}
}
target := tc.WriteTargetTeamID(home)
entry.Team = target
entry.TeamID = target
```

- [ ] **Step 2: MCP store write**

In `internal/mcp/tools.go` `HandleKnowledgeStore`, where `teamID, actor := resolveActorTeam(ctx)` then sets `TeamID: teamID`, apply the same fallback via the resolved `TeamContext` (use `auth.GetTeamContext(ctx)` and `WriteTargetTeamID`). Keep stdio behavior (empty team) intact when there is no user/home.

- [ ] **Step 3: Verify build + tests**

Run: `go build ./...` then `go test ./internal/web/ ./internal/mcp/`
Expected: PASS. Manually confirm a superadmin in see-all mode writing an entry lands it in their home team. **Checkpoint — do not commit.**

---

## Task 11: Move knowledge (reassign) — backend

**Files:**
- Modify: `internal/storage/storage.go` or relevant interface file (add `ReassignEntriesTeam`)
- Modify: `internal/storage/sqlite.go`, `internal/storage/postgres.go` (impl)
- Create: `internal/web/knowledge_move_handlers.go` (`handleMoveKnowledge`)
- Modify: `internal/web/server.go` (route under `RequireSuperadmin`)
- Modify: `internal/web/server_test.go` (mock stub)
- Test: `internal/storage/*_test.go` + `internal/web/knowledge_move_handlers_test.go`

- [ ] **Step 1: Write the storage failing test**

In the storage test that has access to entry creation, add a test: store two entries in team A, call `ReassignEntriesTeam(ctx, [id1,id2], "B")`, assert both now have `TeamID == "B"` (via `GetEntry`).

- [ ] **Step 2: Run to verify fail**

Run: `go test ./internal/storage/ -run TestReassignEntriesTeam`
Expected: method undefined.

- [ ] **Step 3: Implement storage method**

Add to the entry `Store` interface and both engines:

```go
// SQLite
func (s *SQLiteStore) ReassignEntriesTeam(ctx context.Context, entryIDs []string, teamID string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck
	stmt, err := tx.PrepareContext(ctx, `UPDATE entries SET team_id = ? WHERE id = ?`)
	if err != nil {
		return err
	}
	defer stmt.Close()
	for _, id := range entryIDs {
		if _, err := stmt.ExecContext(ctx, teamID, id); err != nil {
			return fmt.Errorf("reassign entry %s: %w", id, err)
		}
	}
	return tx.Commit()
}
```

Mirror for Postgres with `$1,$2`. Confirm the entries table/column name (`entries.team_id`) by grepping `team_id` in `sqlite.go`/`postgres.go`.

- [ ] **Step 4: Implement handler + route**

`internal/web/knowledge_move_handlers.go`:

```go
func (s *Server) handleMoveKnowledge(w http.ResponseWriter, r *http.Request) {
	var body struct {
		EntryIDs []string `json:"entry_ids"`
		TeamID   string   `json:"team_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.TeamID == "" || len(body.EntryIDs) == 0 {
		writeError(w, 400, "bad_request", "entry_ids and team_id required")
		return
	}
	if t, _ := s.store.GetTeam(r.Context(), body.TeamID); t == nil {
		writeError(w, 400, "bad_request", "unknown team_id")
		return
	}
	if err := s.store.ReassignEntriesTeam(r.Context(), body.EntryIDs, body.TeamID); err != nil {
		writeError(w, 500, "internal_error", "move failed")
		return
	}
	writeJSON(w, map[string]any{"ok": true, "moved": len(body.EntryIDs)})
}
```

Route in `server.go` under `RequireSuperadmin`: `r.Post("/api/admin/knowledge/move", s.handleMoveKnowledge)`. Add `mockStore.ReassignEntriesTeam` no-op stub in `server_test.go`.

- [ ] **Step 5: Handler test**

In `internal/web/knowledge_move_handlers_test.go`: non-superadmin → 403 (route gating); superadmin with valid body → 200. Reuse `newAdminTestServer`/superadmin context helpers.

- [ ] **Step 6: Verify**

Run: `go test ./internal/storage/ ./internal/web/` then `go build ./...`
Expected: PASS. **Checkpoint — do not commit.**

---

## Task 12: Copy knowledge (duplicate into teams) — backend

**Files:**
- Modify: `internal/sharing/sharing.go` (factor token-free copy core)
- Modify: `internal/web/knowledge_move_handlers.go` (`handleCopyKnowledge`)
- Modify: `internal/web/server.go` (route under `RequireSuperadmin`)
- Test: `internal/sharing/*_test.go` + handler test

- [ ] **Step 1: Read `Import`**

Read `internal/sharing/sharing.go` `Import` (~line 47) to see how it copies an entry into a destination team as a new pending entry (re-embedding via `src *aiconfig.Sources`, status pending, provenance).

- [ ] **Step 2: Factor a token-free copy core (failing test first)**

Write a test in `internal/sharing/sharing_test.go` for a new exported `CopyEntryToTeam(ctx, store, src, entryID, destTeamID, createdBy) (newID string, err error)` that creates a new pending entry in destTeamID from entryID (no share token). Run it (fails — undefined), then implement by extracting the copy body of `Import` into `CopyEntryToTeam` and having `Import` call it after burning the token. Keep `Import` behavior identical.

- [ ] **Step 3: Implement the copy handler + route**

```go
func (s *Server) handleCopyKnowledge(w http.ResponseWriter, r *http.Request) {
	var body struct {
		EntryIDs []string `json:"entry_ids"`
		TeamIDs  []string `json:"team_ids"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || len(body.EntryIDs) == 0 || len(body.TeamIDs) == 0 {
		writeError(w, 400, "bad_request", "entry_ids and team_ids required")
		return
	}
	tc := auth.GetTeamContext(r.Context())
	count := 0
	for _, tid := range body.TeamIDs {
		if t, _ := s.store.GetTeam(r.Context(), tid); t == nil {
			writeError(w, 400, "bad_request", "unknown team_id: "+tid)
			return
		}
		for _, eid := range body.EntryIDs {
			if _, err := sharing.CopyEntryToTeam(r.Context(), s.store, s.aiSrc, eid, tid, tc.EffectiveActorID()); err != nil {
				writeError(w, 500, "internal_error", "copy failed")
				return
			}
			count++
		}
	}
	writeJSON(w, map[string]any{"ok": true, "copied": count})
}
```

Route under `RequireSuperadmin`: `r.Post("/api/admin/knowledge/copy", s.handleCopyKnowledge)`. Confirm `s.aiSrc` is the right field name for AI sources on `Server` (grep `aiSrc` in `server.go`).

- [ ] **Step 4: Handler test**

non-superadmin → 403; superadmin copying one entry into two teams → 200 with `copied == 2`. Use a store mock whose `CopyEntryToTeam`-backing methods (GetEntry/StoreEntryChunked) return success.

- [ ] **Step 5: Verify**

Run: `go test ./internal/sharing/ ./internal/web/` then `go build ./...`
Expected: PASS. **Checkpoint — do not commit.**

---

## Task 13: Client active-team injection (UI)

**Files:**
- Modify: `web/src/lib/api.ts`

- [ ] **Step 1: Inject X-Team-Id**

In `apiFetch` (`api.ts:4`), read `localStorage.getItem('tkm_active_team')` and add `'X-Team-Id': activeTeam` to headers when non-empty (alongside the existing `Authorization`). Add helper exports: `getMyTeams()` → `GET /api/me/teams`; `getMe()` → `GET /api/me` (if not present); `setActiveTeam(id|null)` writes/removes `tkm_active_team`; membership calls `addMembership(userId, teamId)`, `removeMembership(userId, teamId)`; `moveKnowledge(entryIds, teamId)`; `copyKnowledge(entryIds, teamIds)`.

- [ ] **Step 2: Verify web build**

Run (from `web/`): `npm run build`
Expected: success. **Checkpoint — do not commit.**

---

## Task 14: Nav reorg + user-avatar menu + switcher (UI)

**Files:**
- Create: `web/src/components/UserMenu.tsx`
- Modify: `web/src/components/Layout.tsx`

- [ ] **Step 1: Build UserMenu**

`UserMenu.tsx`: an avatar button (MUI `Avatar` + `Menu`). On open, fetch `getMe()` and `getMyTeams()`. Render top-to-bottom: (a) inline profile header (name, email, role, home team); (b) "Active team" section — a list of teams with the active one checked (from `tkm_active_team`, default = first/home); superadmin also gets an "All teams" item that calls `setActiveTeam(null)`; selecting a team calls `setActiveTeam(id)` then `window.location.reload()` (simplest correct refetch); (c) links: My Visibility (`/my-visibility`), My API Keys (`/api-keys`); (d) Sign out (reuse the existing `handleSignOut` logic — move it here or pass as prop).

- [ ] **Step 2: Reorganize Layout**

In `Layout.tsx`: remove `My Visibility` and `API Keys` from the `nav` array; move `Settings` into a bottom group rendered just above the avatar; replace the "Sign Out" `List` block with `<UserMenu />`. Role-gate the admin items (`Users`, `Teams`, `All Users`, `Auth Config`): fetch `getMe()` once in Layout and only render those entries when `role === 'admin' || role === 'superadmin'`.

- [ ] **Step 3: Verify web build**

Run (from `web/`): `npm run build`
Expected: success. Manually verify: switcher lists teams, switching reloads scoped data, admin items hidden for a `member`. **Checkpoint — do not commit.**

---

## Task 15: Multi-team membership editor (UI)

**Files:**
- Modify: `web/src/pages/Users.tsx`

- [ ] **Step 1: Add membership editor**

In the user editor, add a teams multi-select (checkbox list) populated from `getMyTeams()` for the acting admin (or all teams for superadmin via the existing teams list). For an admin, restrict selectable teams to the admin's own teams. On add/remove, call `addMembership`/`removeMembership`. Show the user's current memberships (you may add a `GET /api/admin/users/{id}/teams` if needed, or reuse data already loaded; if a new endpoint is required, add it under `RequireAdmin` returning `ListUserTeams(id)` — keep it minimal).

- [ ] **Step 2: Verify web build**

Run (from `web/`): `npm run build`
Expected: success. **Checkpoint — do not commit.**

---

## Task 16: Knowledge bulk move/copy (UI)

**Files:**
- Modify: `web/src/pages/Knowledge.tsx`

- [ ] **Step 1: Multi-select + bulk bar**

Add row selection (checkboxes) to the Knowledge list. When ≥1 selected AND the viewer is superadmin (`getMe().role === 'superadmin'`), show a bulk-action bar with **Move to team…** (single-team picker dialog → `moveKnowledge(ids, teamId)`) and **Copy to teams…** (multi-team picker dialog → `copyKnowledge(ids, teamIds)`). After success, clear selection and refetch the list. Teams for the pickers come from the teams list (superadmin sees all via existing `/api/admin/teams`).

- [ ] **Step 2: Verify web build**

Run (from `web/`): `npm run build`
Expected: success. Manually verify move reassigns and copy duplicates (as pending) into targets. **Checkpoint — do not commit.**

---

## Final verification (whole feature)

- [ ] `go build ./...` — clean.
- [ ] `go vet ./internal/...` — clean.
- [ ] `go test ./internal/...` — all packages pass.
- [ ] `cd web && npm run build` — succeeds.
- [ ] Manual smoke: assign a user `superadmin` from the UI; superadmin sees all teams' knowledge in lists; a member added to a second team can switch to it (web) and via `X-Team-Id` (MCP); superadmin moves and copies entries across teams.
- [ ] **Leave all changes uncommitted for the repository owner.**
