# Phase 5: REST API + Analytics + Team Model — Design Spec

**Date:** 2026-06-07
**Status:** approved
**Phase:** 5 of 6

---

## Goal

Full multi-tenant federation: every team's knowledge, agents, and rules are isolated. A chi-based REST API enforces auth and team scoping. Analytics surfaces usage, gaps, and contributions. A curator approval queue gates knowledge into the pipeline. OIDC + local auth serve the web UI. API keys serve programmatic and MCP callers. The MCP server runs over stdio and optionally over HTTP/SSE for remote clients.

---

## Role Hierarchy

```
superadmin (team_id=NULL)
  — global: create/disable/delete teams, configure auth provider,
    see all teams' data, issue superadmin API keys
  └─ admin (team-scoped)
       — manage API keys (team + per-user), manage team settings,
         manually assign users, trigger pipeline
       └─ curator (team-scoped)
            — approve/reject pending knowledge entries, publish agents
            └─ member (team-scoped)
                 — read approved knowledge/clusters/agents,
                   store new entries (lands in pending queue)
```

---

## Schema

### New tables

```sql
CREATE TABLE teams (
    id              TEXT PRIMARY KEY,
    name            TEXT NOT NULL,
    domain_patterns TEXT NOT NULL DEFAULT '[]',  -- JSON array of regex strings
    enabled         INTEGER NOT NULL DEFAULT 1,
    created_at      TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE users (
    id               TEXT PRIMARY KEY,
    team_id          TEXT REFERENCES teams(id),   -- NULL until assigned
    email            TEXT NOT NULL UNIQUE,
    name             TEXT NOT NULL DEFAULT '',
    external_id      TEXT NOT NULL DEFAULT '',    -- OIDC subject claim
    password_hash    TEXT NOT NULL DEFAULT '',    -- bcrypt; only used for local auth
    role             TEXT NOT NULL DEFAULT 'member',
    manually_assigned INTEGER NOT NULL DEFAULT 0,
    created_at       TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE sessions (
    id          TEXT PRIMARY KEY,
    user_id     TEXT NOT NULL REFERENCES users(id),
    token_hash  TEXT NOT NULL UNIQUE,
    expires_at  TEXT NOT NULL,
    created_at  TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE api_keys (
    id           TEXT PRIMARY KEY,
    team_id      TEXT REFERENCES teams(id),  -- NULL for superadmin keys
    user_id      TEXT REFERENCES users(id),  -- NULL for team keys
    key_type     TEXT NOT NULL,              -- 'team' | 'user'
    name         TEXT NOT NULL,
    key_hash     TEXT NOT NULL UNIQUE,
    role         TEXT NOT NULL,              -- member|curator|admin|superadmin
    created_at   TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    last_used_at TEXT
);

CREATE TABLE auth_config (
    id               INTEGER PRIMARY KEY CHECK (id = 1),  -- singleton
    provider         TEXT NOT NULL DEFAULT 'local',       -- 'local' | 'oidc'
    oidc_issuer      TEXT NOT NULL DEFAULT '',
    oidc_client_id   TEXT NOT NULL DEFAULT '',
    oidc_redirect_url TEXT NOT NULL DEFAULT '',
    updated_at       TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE team_settings (
    team_id              TEXT PRIMARY KEY REFERENCES teams(id),
    domains              TEXT NOT NULL DEFAULT '[]',  -- JSON array of domain strings
    cluster_threshold    REAL NOT NULL DEFAULT 0.85,
    pipeline_min_entries INTEGER NOT NULL DEFAULT 10,
    agent_model          TEXT NOT NULL DEFAULT 'claude-haiku-4-5-20251001',
    updated_at           TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE activity_log (
    id          TEXT PRIMARY KEY,
    team_id     TEXT,
    key_id      TEXT,
    user_id     TEXT,
    action      TEXT NOT NULL,       -- e.g. 'knowledge.store', 'prompt.enhance'
    entity_type TEXT NOT NULL DEFAULT '',
    entity_id   TEXT NOT NULL DEFAULT '',
    created_at  TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);
```

### Existing table migrations

```sql
ALTER TABLE entries ADD COLUMN team_id TEXT NOT NULL DEFAULT '';
ALTER TABLE entries ADD COLUMN status  TEXT NOT NULL DEFAULT 'pending';
ALTER TABLE clusters ADD COLUMN team_id TEXT NOT NULL DEFAULT '';
ALTER TABLE agents ADD COLUMN team_id TEXT NOT NULL DEFAULT '';
ALTER TABLE agent_versions ADD COLUMN team_id TEXT NOT NULL DEFAULT '';
ALTER TABLE rules ADD COLUMN team_id TEXT NOT NULL DEFAULT '';
```

Existing rows get `team_id = ''` and `status = 'approved'` (already in pipeline) on migration.

---

## Auth Layer — `internal/auth`

### Provider interface

```go
type Provider interface {
    // OIDC path
    AuthURL(state string) string
    Exchange(ctx context.Context, code string) (*UserInfo, error)
    // Local path
    VerifyPassword(ctx context.Context, email, password string) (*UserInfo, error)
}

type UserInfo struct {
    Email      string
    Name       string
    ExternalID string  // OIDC subject claim; empty for local auth
}
```

`LocalProvider` — bcrypt password verification against `users.password_hash` (added to users table for local auth only; column omitted from schema above for brevity, added in migration).

`OIDCProvider` — wraps `github.com/coreos/go-oidc/v3` + `golang.org/x/oauth2`. Configured from `auth_config` row + `OIDC_CLIENT_SECRET` env var. Works with Clerk and Zitadel via OIDC discovery.

### Middleware

```go
type TeamContext struct {
    TeamID string
    KeyID  string   // set for API key requests
    UserID string   // set for session requests
    Role   string   // member|curator|admin|superadmin
}
```

`RequireAuth` — accepts `Authorization: Bearer <key>` (hashes → looks up `api_keys`) or `session` cookie (hashes → looks up `sessions` → loads user). Injects `TeamContext`. Returns 401 if neither present or invalid.

`RequireCurator`, `RequireAdmin`, `RequireSuperadmin` — read role from context, return 403 if insufficient.

### Bootstrap

On startup, if `SUPERADMIN_KEY` env var is set and no superadmin key exists in `api_keys`, the server inserts one with `role=superadmin`, `team_id=NULL`. This is the only way to seed the first superadmin credential.

---

## Storage Layer

### `TeamStore` interface (`internal/storage/teams.go`)

```go
type TeamStore interface {
    // Teams
    CreateTeam(ctx context.Context, t Team) (string, error)
    GetTeam(ctx context.Context, id string) (*Team, error)
    ListTeams(ctx context.Context) ([]Team, error)
    SetTeamEnabled(ctx context.Context, id string, enabled bool) error
    DeleteTeam(ctx context.Context, id string) error

    // Users
    UpsertUser(ctx context.Context, u User) (string, error)
    GetUserByEmail(ctx context.Context, email string) (*User, error)
    GetUserByExternalID(ctx context.Context, externalID string) (*User, error)
    ListUsers(ctx context.Context, teamID string) ([]User, error)
    AssignUserToTeam(ctx context.Context, userID, teamID, role string) error
    ResolveTeamByEmail(ctx context.Context, email string) (*Team, error) // first enabled team whose domain_patterns contains a regex matching email; nil if none

    // API keys
    CreateAPIKey(ctx context.Context, key APIKey) error
    GetAPIKeyByHash(ctx context.Context, hash string) (*APIKey, error)
    ListAPIKeys(ctx context.Context, teamID string) ([]APIKey, error)
    RevokeAPIKey(ctx context.Context, id string) error
    TouchAPIKey(ctx context.Context, id string) error  // async last_used_at update

    // Sessions
    CreateSession(ctx context.Context, s Session) error
    GetSession(ctx context.Context, tokenHash string) (*Session, error)
    DeleteSession(ctx context.Context, tokenHash string) error

    // Team settings
    GetTeamSettings(ctx context.Context, teamID string) (*TeamSettings, error)
    PutTeamSettings(ctx context.Context, s TeamSettings) error

    // Auth config (singleton)
    GetAuthConfig(ctx context.Context) (*AuthConfig, error)
    PutAuthConfig(ctx context.Context, c AuthConfig) error

    // Activity log
    LogActivity(ctx context.Context, e ActivityEntry) error
    QueryActivity(ctx context.Context, teamID string, limit int) ([]ActivityEntry, error)
}

// Logged action values for ActivityEntry.Action:
//   knowledge.store    knowledge.approve  knowledge.reject
//   knowledge.get      knowledge.rate
//   agent.publish      agent.export
//   prompt.enhance     prompt.suggest
//   pipeline.trigger
```

### Existing storage changes

- `ListFilter` gains `TeamID string` and `Status string` fields
- `KnowledgeEntry` gains `Status string` (`pending`|`approved`|`rejected`)
- `Store` interface gains `ApproveEntry(ctx, id string) error` and `RejectEntry(ctx, id string) error`
- All `ListEntries`, `CountEntries`, `SearchSimilar` queries gain `WHERE team_id = ? AND status = 'approved'` when `TeamID` is set
- Pipeline processes only `status = 'approved'` entries

---

## REST API — `internal/web`

### Router (chi)

```
GET  /health                        public

POST /auth/login                    local auth → session cookie
GET  /auth/oidc/login               redirect to OIDC provider
GET  /auth/oidc/callback            exchange code → upsert user → session cookie
POST /auth/logout                   delete session

/api  [RequireAuth]
  GET    /stats
  GET    /knowledge
  POST   /knowledge                 status=pending
  GET    /knowledge/{id}
  PUT    /knowledge/{id}            update entry content (author or curator)
  DELETE /knowledge/{id}            delete entry (author or admin)
  PUT    /knowledge/{id}/rate
  GET    /clusters
  GET    /clusters/{id}             cluster detail with member entries
  GET    /clusters/{id}/summary     LLM-generated cluster summary text
  GET    /datasets
  GET    /datasets/{id}/export
  GET    /agents
  GET    /agents/{id}
  GET    /agents/{id}/export
  GET    /agents/bulk-export
  GET    /agents/domain/{domain}/latest   latest published agent for a domain
  GET    /pipeline/status
  GET    /analytics/usage
  GET    /analytics/gaps
  GET    /analytics/contributions

/api  [RequireCurator]
  PUT  /knowledge/{id}/approve
  PUT  /knowledge/{id}/reject
  PUT  /agents/{id}/publish

/api  [RequireAdmin]
  POST   /pipeline/trigger
  GET    /api-keys
  POST   /api-keys
  DELETE /api-keys/{id}
  GET    /users
  POST   /users
  PUT    /users/{id}/role
  GET    /settings
  PUT    /settings

/api/admin  [RequireSuperadmin]
  GET    /teams
  POST   /teams
  PUT    /teams/{id}/enabled
  DELETE /teams/{id}
  GET    /teams/{id}/users
  GET    /auth-config
  PUT    /auth-config
```

All list handlers read `TeamContext.TeamID` and pass to storage. Superadmin requests may supply `?team_id=` to scope to a specific team.

---

## Analytics

### `GET /api/analytics/usage`
```json
{
  "top_entries": [{ "id", "title", "domain", "score", "usage_count", "rating" }],
  "by_domain":   [{ "domain", "entry_count", "avg_rating", "total_usage" }],
  "heatmap":     [{ "week": "2026-W23", "domain": "finance", "usage": 14 }]
}
```
Score = `rating × usage_count`. Heatmap from `activity_log` grouped by ISO week + domain.

### `GET /api/analytics/gaps`
```json
{
  "gaps": [{ "domain", "entry_count", "threshold", "severity": "low|medium|high" }]
}
```
Domains with `entry_count < pipeline_min_entries`. Severity: `high` < 50% threshold, `medium` < 80%, `low` < 100%.

### `GET /api/analytics/contributions`
```json
{
  "leaderboard": [{ "author", "entry_count", "approved_count",
                    "total_usage", "avg_rating", "score" }]
}
```
Score = `(approved_count × 2) + avg_rating × total_usage`. Ranked descending.

---

## MCP Additions

### Dual transport

| Transport | Enabled | Auth |
|-----------|---------|------|
| stdio | always | `TEAM_ID` env var |
| HTTP/SSE | when `MCP_HTTP_ADDR` set | `Authorization: Bearer <api_key>` |

SSE handler is wrapped in the same chi auth middleware as the REST API.

### New tools completing PROJECT.md plan

**`knowledge_search`** — Semantic vector search via MCP. Input: `query` (string), `domain` (optional), `top_k` (int, default 5). Returns ranked `SearchResult` entries scoped to caller's team. Wraps existing `SearchSimilar` storage method.

**`knowledge_rate`** — Rate a knowledge entry via MCP. Input: `id` (string), `rating` (float 1–5). Calls `RateEntry` + logs `knowledge.rate` activity. Increments `usage_count`.

### New tool: `prompt_suggest`

Input: `prompt` (string), `domain` (optional string).
Finds top-3 semantically similar approved entries in the caller's team. Calls LLM to suggest improvements based on high-rated entries. Returns `suggested_prompt`, `rationale`, `source_entries`.
Requires `ANTHROPIC_API_KEY`; falls back to returning original prompt if not set.

### New MCP resources

| URI | Returns |
|-----|---------|
| `knowledge://team/top` | Top 10 entries by score |
| `knowledge://team/recent` | 10 most recently approved entries |
| `knowledge://domain/{name}` | All approved entries in domain |
| `knowledge://cluster/{id}` | Entries in a cluster |
| `agents://generated` | All published agents for caller's team |
| `agents://domain/{name}` | Latest published agent for a domain |

### New MCP prompt: `enhance_with_context`

Layers: applicable rules (existing `prompt_enhance`) + top-3 similar entries + published agent for domain. Returns the caller's prompt wrapped with this preamble.

---

## New Environment Variables

| Variable | Default | Description |
|----------|---------|-------------|
| `SUPERADMIN_KEY` | — | Seeds first superadmin API key on startup |
| `OIDC_CLIENT_SECRET` | — | OIDC provider client secret (never stored in DB) |
| `MCP_HTTP_ADDR` | `""` | Address for MCP HTTP/SSE transport; empty = disabled |
| `MCP_HTTP_PATH` | `/mcp` | SSE endpoint path |

---

## New React Pages

| Page | Route | Min Role | Description |
|------|-------|----------|-------------|
| Analytics | `/analytics` | member | Heatmap, top entries, gaps, leaderboard |
| Pending Queue | `/pending` | curator | Approve/reject knowledge entries |
| Settings | `/settings` | admin | Domains, thresholds, agent model |
| Team Admin | `/admin/teams` | superadmin | Create/disable/delete teams |
| Auth Config | `/admin/auth` | superadmin | OIDC provider setup (Clerk / Zitadel / local) |

Analytics heatmap: CSS grid, no chart library dependency.
Auth Config page: provider selector (local / Clerk / Zitadel) with provider-specific fields and a connection test button.

---

## New Files

| File | Purpose |
|------|---------|
| `internal/auth/provider.go` | `Provider` interface, `LocalProvider`, `OIDCProvider` |
| `internal/auth/provider_test.go` | Provider tests |
| `internal/auth/middleware.go` | `TeamContext`, `RequireAuth`, role gates |
| `internal/auth/middleware_test.go` | Middleware tests |
| `internal/storage/teams.go` | Types + `TeamStore` interface |
| `internal/storage/teams_sqlite.go` | `TeamStore` on `*SQLiteStore` |
| `internal/storage/teams_test.go` | TeamStore tests |
| `internal/web/auth_handlers.go` | Login, OIDC callback, logout, setup |
| `internal/web/admin_handlers.go` | Team CRUD, user management, API key management |
| `internal/web/analytics.go` | Usage, gaps, contributions handlers |
| `internal/web/analytics_test.go` | Analytics handler tests |
| `internal/web/settings.go` | Settings + auth config handlers |
| `internal/web/settings_test.go` | Settings handler tests |
| `internal/mcp/prompt_suggest.go` | `HandlePromptSuggest`, registration |
| `internal/mcp/resources.go` | Resource handlers + registration |
| `internal/mcp/remote.go` | HTTP/SSE transport setup |
| `web/src/pages/Analytics.tsx` | Analytics page |
| `web/src/pages/PendingQueue.tsx` | Curator queue |
| `web/src/pages/Settings.tsx` | Team settings |
| `web/src/pages/AdminTeams.tsx` | Superadmin team management |
| `web/src/pages/AuthConfig.tsx` | OIDC provider config UI |

## Modified Files

| File | Change |
|------|--------|
| `internal/storage/sqlite.go` | 7 new tables; ALTER TABLE migrations; `ApproveEntry`, `RejectEntry`; team-scoped queries |
| `internal/storage/storage.go` | `TeamID`+`Status` on `ListFilter`; `Status` on `KnowledgeEntry`; `ApproveEntry`/`RejectEntry` on `Store` |
| `internal/storage/analysis.go` | `team_id` scoping on cluster queries |
| `internal/storage/agents_sqlite.go` | `team_id` scoping on agent queries |
| `internal/storage/rules_sqlite.go` | `team_id` scoping on rule queries |
| `internal/web/server.go` | stdlib mux → chi; `AllStore` embeds `TeamStore`; mount all route groups |
| `internal/web/handlers.go` | All handlers read `TeamContext`; add `handleKnowledgeStore`, `handleApprove`, `handleReject`, `handlePipelineTrigger` |
| `internal/mcp/server.go` | Register `knowledge_search`, `knowledge_rate`, `prompt_suggest`, resources, `enhance_with_context` |
| `cmd/server/main.go` | Superadmin key bootstrap; start MCP HTTP/SSE if `MCP_HTTP_ADDR` set |
| `internal/config/config.go` | `SuperadminKey`, `OIDCClientSecret`, `MCPHTTPAddr`, `MCPHTTPPath` |
| `web/src/App.tsx` | Add routes for new pages; role-gated rendering |
| `web/src/components/Layout.tsx` | Conditional nav by role |
| `web/src/lib/api.ts` | Analytics, settings, auth, admin, pending endpoints |
| `go.mod` | Add `github.com/go-chi/chi/v5`, `github.com/coreos/go-oidc/v3`, `golang.org/x/oauth2` |

---

## Exit Criteria

- Web UI fully functional with live REST data, team-scoped
- Superadmin can create teams, configure OIDC, disable teams
- Members store knowledge → lands in pending queue → curator approves → enters pipeline
- Analytics pages render with real data (heatmap, gaps, leaderboard)
- Settings page saves team config to DB
- Pipeline manually triggerable from UI by admin
- API keys (team + per-user) work for MCP and REST callers
- MCP HTTP/SSE transport accepts API key auth and scopes tools to team
- `prompt_suggest` and `enhance_with_context` work via both stdio and remote MCP
- All Go tests pass; React build succeeds; `CGO_ENABLED=1 go build ./cmd/server/` clean
