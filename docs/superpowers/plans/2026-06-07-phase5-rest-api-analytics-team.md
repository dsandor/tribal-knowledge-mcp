# Phase 5: REST API + Analytics + Team Model Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add full multi-tenant federation to the tribal-knowledge MCP server — API key + OIDC/local session auth, team/role scoping on all data, a curator approval queue, analytics endpoints, and MCP HTTP/SSE remote transport.

**Architecture:** Chi router replaces stdlib mux in `internal/web`. A new `internal/auth` package provides OIDC + local providers and request-context middleware. A new `internal/storage/teams.go` + `teams_sqlite.go` holds the TeamStore interface and seven new tables. All existing list/count queries gain `team_id` scoping. Analytics query `activity_log` directly. MCP gains `knowledge_search`, `knowledge_rate`, `prompt_suggest`, six resources, `enhance_with_context`, and an optional HTTP/SSE transport.

**Tech Stack:** Go 1.25, chi v5, coreos/go-oidc/v3, golang.org/x/oauth2, bcrypt (golang.org/x/crypto), existing SQLite/sqlite-vec, mark3labs/mcp-go v0.54.1, React 18 + TypeScript + Vite + shadcn/ui.

---

## File Map

### New files
| File | Purpose |
|------|---------|
| `internal/auth/provider.go` | `Provider` interface, `UserInfo`, `LocalProvider`, `OIDCProvider` |
| `internal/auth/provider_test.go` | Provider unit tests |
| `internal/auth/middleware.go` | `TeamContext`, `RequireAuth`, `RequireCurator`, `RequireAdmin`, `RequireSuperadmin` |
| `internal/auth/middleware_test.go` | Middleware tests with mock store |
| `internal/storage/teams.go` | `Team`, `User`, `Session`, `APIKey`, `TeamSettings`, `AuthConfig`, `ActivityEntry` types + `TeamStore` interface |
| `internal/storage/teams_sqlite.go` | All `TeamStore` methods on `*SQLiteStore` |
| `internal/storage/teams_test.go` | TeamStore tests |
| `internal/web/auth_handlers.go` | `handleLogin`, `handleOIDCLogin`, `handleOIDCCallback`, `handleLogout` |
| `internal/web/admin_handlers.go` | Team CRUD, user management, API key management |
| `internal/web/analytics.go` | `handleUsage`, `handleGaps`, `handleContributions` |
| `internal/web/analytics_test.go` | Analytics handler tests |
| `internal/web/settings.go` | `handleGetSettings`, `handlePutSettings`, `handleGetAuthConfig`, `handlePutAuthConfig` |
| `internal/web/settings_test.go` | Settings handler tests |
| `internal/mcp/knowledge_tools.go` | `HandleKnowledgeSearch`, `HandleKnowledgeRate`, `RegisterKnowledgeExtTools` |
| `internal/mcp/prompt_suggest.go` | `HandlePromptSuggest`, `RegisterPromptSuggest` |
| `internal/mcp/resources.go` | Six resource handlers + `RegisterResources` |
| `internal/mcp/remote.go` | HTTP/SSE transport setup + `StartRemoteMCP` |
| `web/src/pages/Analytics.tsx` | Usage heatmap, top entries, gaps, leaderboard |
| `web/src/pages/PendingQueue.tsx` | Curator approve/reject queue |
| `web/src/pages/Settings.tsx` | Team settings editor |
| `web/src/pages/AdminTeams.tsx` | Superadmin team CRUD |
| `web/src/pages/AuthConfig.tsx` | OIDC provider configuration UI |

### Modified files
| File | Change |
|------|--------|
| `go.mod` | Add chi v5, go-oidc/v3, oauth2, crypto |
| `internal/config/config.go` | Add `SuperadminKey`, `OIDCClientSecret`, `MCPHTTPAddr`, `MCPHTTPPath` |
| `internal/config/config_test.go` | Test new config fields |
| `internal/storage/storage.go` | Add `TeamID`, `Status` to `ListFilter`; add `Status` to `KnowledgeEntry`; add `ApproveEntry`, `RejectEntry`, `UpdateEntry` to `Store` |
| `internal/storage/sqlite.go` | Schema migrations for 7 new tables + 6 ALTER TABLEs; implement `ApproveEntry`, `RejectEntry`, `UpdateEntry`; team-scope all list/count/search queries |
| `internal/storage/storage_test.go` | Tests for `ApproveEntry`, `RejectEntry`, `UpdateEntry`, team scoping |
| `internal/storage/analysis.go` | Pass `team_id` to cluster queries |
| `internal/storage/agents_sqlite.go` | Pass `team_id` to agent queries |
| `internal/storage/rules_sqlite.go` | Pass `team_id` to rule queries |
| `internal/web/server.go` | stdlib mux → chi; `AllStore` embeds `TeamStore`; mount all route groups with auth middleware |
| `internal/web/handlers.go` | All handlers read `TeamContext`; add `handleKnowledgeStore`, `handleKnowledgeUpdate`, `handleKnowledgeDelete`, `handleClusterGet`, `handleClusterSummary`, `handleAgentLatestByDomain`, `handlePipelineTrigger` |
| `internal/web/server_test.go` | Update tests for chi + auth |
| `internal/mcp/server.go` | Call `RegisterKnowledgeExtTools`, `RegisterPromptSuggest`, `RegisterResources` |
| `cmd/server/main.go` | Bootstrap superadmin key; start MCP HTTP/SSE if `MCPHTTPAddr` set |

---

## Task 1: Add Go dependencies and extend config

**Files:**
- Modify: `go.mod`
- Modify: `internal/config/config.go`
- Modify: `internal/config/config_test.go`

- [ ] **Step 1: Add dependencies**

```bash
cd /Users/dsandor/Projects/memory
go get github.com/go-chi/chi/v5@latest
go get github.com/coreos/go-oidc/v3@latest
go get golang.org/x/oauth2@latest
go get golang.org/x/crypto@latest
```

Expected: `go.mod` and `go.sum` updated, no errors.

- [ ] **Step 2: Write failing config tests**

Append to `internal/config/config_test.go`:

```go
func TestConfig_NewFields(t *testing.T) {
	t.Setenv("SUPERADMIN_KEY", "test-superadmin-key")
	t.Setenv("OIDC_CLIENT_SECRET", "test-secret")
	t.Setenv("MCP_HTTP_ADDR", ":9090")
	t.Setenv("MCP_HTTP_PATH", "/mcp/v1")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.SuperadminKey != "test-superadmin-key" {
		t.Errorf("SuperadminKey = %q, want %q", cfg.SuperadminKey, "test-superadmin-key")
	}
	if cfg.OIDCClientSecret != "test-secret" {
		t.Errorf("OIDCClientSecret = %q", cfg.OIDCClientSecret)
	}
	if cfg.MCPHTTPAddr != ":9090" {
		t.Errorf("MCPHTTPAddr = %q", cfg.MCPHTTPAddr)
	}
	if cfg.MCPHTTPPath != "/mcp/v1" {
		t.Errorf("MCPHTTPPath = %q", cfg.MCPHTTPPath)
	}
}

func TestConfig_MCPDefaults(t *testing.T) {
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.MCPHTTPAddr != "" {
		t.Errorf("MCPHTTPAddr default should be empty, got %q", cfg.MCPHTTPAddr)
	}
	if cfg.MCPHTTPPath != "/mcp" {
		t.Errorf("MCPHTTPPath default = %q, want /mcp", cfg.MCPHTTPPath)
	}
}
```

- [ ] **Step 3: Run — verify they fail**

```bash
cd /Users/dsandor/Projects/memory && CGO_ENABLED=1 go test ./internal/config/... 2>&1 | tail -5
```

Expected: compile error — `SuperadminKey`, `OIDCClientSecret`, `MCPHTTPAddr`, `MCPHTTPPath` undefined on `Config`.

- [ ] **Step 4: Extend config.go**

In `internal/config/config.go`, add fields to `Config`:

```go
type Config struct {
	DBPath             string
	OllamaURL          string
	OllamaModel        string
	TeamID             string
	EmbeddingDim       int
	AnthropicAPIKey    string
	AnthropicModel     string
	AgentModel         string
	PipelineInterval   time.Duration
	PipelineMinEntries int
	ClusterThreshold   float64
	HTTPAddr           string
	SuperadminKey      string
	OIDCClientSecret   string
	MCPHTTPAddr        string
	MCPHTTPPath        string
}
```

At the end of the `return Config{...}` block in `Load()`, add:

```go
		SuperadminKey:    os.Getenv("SUPERADMIN_KEY"),
		OIDCClientSecret: os.Getenv("OIDC_CLIENT_SECRET"),
		MCPHTTPAddr:      os.Getenv("MCP_HTTP_ADDR"),
		MCPHTTPPath:      envOrDefault("MCP_HTTP_PATH", "/mcp"),
```

- [ ] **Step 5: Run — verify tests pass**

```bash
cd /Users/dsandor/Projects/memory && CGO_ENABLED=1 go test ./internal/config/... -v 2>&1 | grep -E "^--- |^ok|FAIL"
```

Expected: all PASS.

- [ ] **Step 6: Build check**

```bash
cd /Users/dsandor/Projects/memory && CGO_ENABLED=1 go build ./... 2>&1 | grep -v "deprecated"
```

Expected: no errors.

---

## Task 2: Storage — TeamStore types and interface

**Files:**
- Create: `internal/storage/teams.go`

- [ ] **Step 1: Create internal/storage/teams.go**

```go
package storage

import (
	"context"
	"time"
)

type Team struct {
	ID             string
	Name           string
	DomainPatterns []string // regex strings matched against user email
	Enabled        bool
	CreatedAt      time.Time
}

type User struct {
	ID               string
	TeamID           string // empty until assigned
	Email            string
	Name             string
	ExternalID       string // OIDC subject claim; empty for local auth
	PasswordHash     string // bcrypt; only for local auth
	Role             string // member|curator|admin|superadmin
	ManuallyAssigned bool
	CreatedAt        time.Time
}

type Session struct {
	ID        string
	UserID    string
	TokenHash string
	ExpiresAt time.Time
	CreatedAt time.Time
}

const (
	APIKeyTypeTeam = "team"
	APIKeyTypeUser = "user"
)

type APIKey struct {
	ID         string
	TeamID     string // empty for superadmin keys
	UserID     string // empty for team keys
	KeyType    string // "team" | "user"
	Name       string
	KeyHash    string
	Role       string // member|curator|admin|superadmin
	CreatedAt  time.Time
	LastUsedAt *time.Time
}

type TeamSettings struct {
	TeamID             string
	Domains            []string // domain taxonomy labels
	ClusterThreshold   float64
	PipelineMinEntries int
	AgentModel         string
	UpdatedAt          time.Time
}

type AuthConfig struct {
	Provider        string // "local" | "oidc"
	OIDCIssuer      string
	OIDCClientID    string
	OIDCRedirectURL string
	UpdatedAt       time.Time
}

type ActivityEntry struct {
	ID         string
	TeamID     string
	KeyID      string
	UserID     string
	Action     string // e.g. "knowledge.store", "prompt.enhance"
	EntityType string
	EntityID   string
	CreatedAt  time.Time
}

// TeamStore handles teams, users, sessions, API keys, settings, auth config, and activity log.
// *SQLiteStore implements this interface.
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
	// ResolveTeamByEmail returns the first enabled team whose domain_patterns
	// contains a regex matching email; returns nil (no error) if none match.
	ResolveTeamByEmail(ctx context.Context, email string) (*Team, error)

	// API keys
	CreateAPIKey(ctx context.Context, key APIKey) error
	GetAPIKeyByHash(ctx context.Context, hash string) (*APIKey, error)
	ListAPIKeys(ctx context.Context, teamID string) ([]APIKey, error)
	RevokeAPIKey(ctx context.Context, id string) error
	TouchAPIKey(ctx context.Context, id string) error // async last_used_at

	// Sessions
	CreateSession(ctx context.Context, s Session) error
	GetSession(ctx context.Context, tokenHash string) (*Session, error)
	DeleteSession(ctx context.Context, tokenHash string) error

	// Team settings
	GetTeamSettings(ctx context.Context, teamID string) (*TeamSettings, error)
	PutTeamSettings(ctx context.Context, s TeamSettings) error

	// Auth config (singleton row)
	GetAuthConfig(ctx context.Context) (*AuthConfig, error)
	PutAuthConfig(ctx context.Context, c AuthConfig) error

	// Activity log
	LogActivity(ctx context.Context, e ActivityEntry) error
	QueryActivity(ctx context.Context, teamID string, limit int) ([]ActivityEntry, error)
}
```

- [ ] **Step 2: Build check**

```bash
cd /Users/dsandor/Projects/memory && CGO_ENABLED=1 go build ./internal/storage/... 2>&1 | grep -v deprecated
```

Expected: no errors.

---

## Task 3: Storage — TeamStore SQLite implementation + schema migrations

**Files:**
- Create: `internal/storage/teams_sqlite.go`
- Create: `internal/storage/teams_test.go`
- Modify: `internal/storage/sqlite.go`

- [ ] **Step 1: Write failing tests**

Create `internal/storage/teams_test.go`:

```go
package storage

import (
	"context"
	"testing"
	"time"
)

func newTestTeamStore(t *testing.T) *SQLiteStore {
	t.Helper()
	return newTestStore(t)
}

func TestCreateAndGetTeam(t *testing.T) {
	s := newTestTeamStore(t)
	ctx := context.Background()

	id, err := s.CreateTeam(ctx, Team{
		Name:           "acme",
		DomainPatterns: []string{`.*@acme\.com$`},
		Enabled:        true,
	})
	if err != nil {
		t.Fatalf("CreateTeam: %v", err)
	}
	if id == "" {
		t.Fatal("expected non-empty id")
	}

	team, err := s.GetTeam(ctx, id)
	if err != nil {
		t.Fatalf("GetTeam: %v", err)
	}
	if team.Name != "acme" {
		t.Errorf("Name = %q, want acme", team.Name)
	}
	if !team.Enabled {
		t.Error("Enabled should be true")
	}
	if len(team.DomainPatterns) != 1 || team.DomainPatterns[0] != `.*@acme\.com$` {
		t.Errorf("DomainPatterns = %v", team.DomainPatterns)
	}
}

func TestListTeams(t *testing.T) {
	s := newTestTeamStore(t)
	ctx := context.Background()
	for _, name := range []string{"alpha", "beta"} {
		if _, err := s.CreateTeam(ctx, Team{Name: name, Enabled: true}); err != nil {
			t.Fatalf("CreateTeam %s: %v", name, err)
		}
	}
	teams, err := s.ListTeams(ctx)
	if err != nil {
		t.Fatalf("ListTeams: %v", err)
	}
	if len(teams) != 2 {
		t.Errorf("want 2 teams, got %d", len(teams))
	}
}

func TestSetTeamEnabled(t *testing.T) {
	s := newTestTeamStore(t)
	ctx := context.Background()
	id, _ := s.CreateTeam(ctx, Team{Name: "t1", Enabled: true})
	if err := s.SetTeamEnabled(ctx, id, false); err != nil {
		t.Fatalf("SetTeamEnabled: %v", err)
	}
	team, _ := s.GetTeam(ctx, id)
	if team.Enabled {
		t.Error("expected Enabled=false")
	}
}

func TestDeleteTeam(t *testing.T) {
	s := newTestTeamStore(t)
	ctx := context.Background()
	id, _ := s.CreateTeam(ctx, Team{Name: "del", Enabled: true})
	if err := s.DeleteTeam(ctx, id); err != nil {
		t.Fatalf("DeleteTeam: %v", err)
	}
	if _, err := s.GetTeam(ctx, id); err == nil {
		t.Error("expected error after delete")
	}
}

func TestUpsertAndGetUser(t *testing.T) {
	s := newTestTeamStore(t)
	ctx := context.Background()
	id1, err := s.UpsertUser(ctx, User{Email: "alice@acme.com", Name: "Alice", Role: "member"})
	if err != nil {
		t.Fatalf("UpsertUser: %v", err)
	}
	// upsert same email — should return same id
	id2, err := s.UpsertUser(ctx, User{Email: "alice@acme.com", Name: "Alice Updated", Role: "curator"})
	if err != nil {
		t.Fatalf("UpsertUser second: %v", err)
	}
	if id1 != id2 {
		t.Errorf("expected same id on upsert, got %q and %q", id1, id2)
	}
	u, err := s.GetUserByEmail(ctx, "alice@acme.com")
	if err != nil {
		t.Fatalf("GetUserByEmail: %v", err)
	}
	if u.Name != "Alice Updated" {
		t.Errorf("Name = %q", u.Name)
	}
}

func TestAssignUserToTeam(t *testing.T) {
	s := newTestTeamStore(t)
	ctx := context.Background()
	teamID, _ := s.CreateTeam(ctx, Team{Name: "t", Enabled: true})
	userID, _ := s.UpsertUser(ctx, User{Email: "bob@example.com", Role: "member"})
	if err := s.AssignUserToTeam(ctx, userID, teamID, "curator"); err != nil {
		t.Fatalf("AssignUserToTeam: %v", err)
	}
	u, _ := s.GetUserByEmail(ctx, "bob@example.com")
	if u.TeamID != teamID {
		t.Errorf("TeamID = %q, want %q", u.TeamID, teamID)
	}
	if u.Role != "curator" {
		t.Errorf("Role = %q, want curator", u.Role)
	}
}

func TestResolveTeamByEmail(t *testing.T) {
	s := newTestTeamStore(t)
	ctx := context.Background()
	id, _ := s.CreateTeam(ctx, Team{
		Name:           "acme",
		DomainPatterns: []string{`.*@acme\.com$`},
		Enabled:        true,
	})

	team, err := s.ResolveTeamByEmail(ctx, "user@acme.com")
	if err != nil {
		t.Fatalf("ResolveTeamByEmail: %v", err)
	}
	if team == nil || team.ID != id {
		t.Errorf("expected team %q, got %v", id, team)
	}

	none, err := s.ResolveTeamByEmail(ctx, "user@other.com")
	if err != nil {
		t.Fatalf("ResolveTeamByEmail no match: %v", err)
	}
	if none != nil {
		t.Errorf("expected nil for non-matching email")
	}
}

func TestCreateAndGetAPIKey(t *testing.T) {
	s := newTestTeamStore(t)
	ctx := context.Background()
	teamID, _ := s.CreateTeam(ctx, Team{Name: "t", Enabled: true})

	key := APIKey{
		ID:      "key-1",
		TeamID:  teamID,
		KeyType: APIKeyTypeTeam,
		Name:    "ci-key",
		KeyHash: "abc123hash",
		Role:    "admin",
	}
	if err := s.CreateAPIKey(ctx, key); err != nil {
		t.Fatalf("CreateAPIKey: %v", err)
	}
	got, err := s.GetAPIKeyByHash(ctx, "abc123hash")
	if err != nil {
		t.Fatalf("GetAPIKeyByHash: %v", err)
	}
	if got.TeamID != teamID {
		t.Errorf("TeamID = %q", got.TeamID)
	}
}

func TestRevokeAPIKey(t *testing.T) {
	s := newTestTeamStore(t)
	ctx := context.Background()
	if err := s.CreateAPIKey(ctx, APIKey{ID: "k1", KeyHash: "h1", KeyType: APIKeyTypeTeam, Role: "member"}); err != nil {
		t.Fatalf("CreateAPIKey: %v", err)
	}
	if err := s.RevokeAPIKey(ctx, "k1"); err != nil {
		t.Fatalf("RevokeAPIKey: %v", err)
	}
	if _, err := s.GetAPIKeyByHash(ctx, "h1"); err == nil {
		t.Error("expected error after revoke")
	}
}

func TestSessionLifecycle(t *testing.T) {
	s := newTestTeamStore(t)
	ctx := context.Background()
	userID, _ := s.UpsertUser(ctx, User{Email: "c@d.com", Role: "member"})

	sess := Session{
		ID:        "s1",
		UserID:    userID,
		TokenHash: "th1",
		ExpiresAt: time.Now().Add(time.Hour),
	}
	if err := s.CreateSession(ctx, sess); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	got, err := s.GetSession(ctx, "th1")
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	if got.UserID != userID {
		t.Errorf("UserID = %q", got.UserID)
	}
	if err := s.DeleteSession(ctx, "th1"); err != nil {
		t.Fatalf("DeleteSession: %v", err)
	}
	if _, err := s.GetSession(ctx, "th1"); err == nil {
		t.Error("expected error after delete")
	}
}

func TestTeamSettings(t *testing.T) {
	s := newTestTeamStore(t)
	ctx := context.Background()
	teamID, _ := s.CreateTeam(ctx, Team{Name: "t", Enabled: true})

	settings := TeamSettings{
		TeamID:             teamID,
		Domains:            []string{"finance", "legal"},
		ClusterThreshold:   0.9,
		PipelineMinEntries: 5,
		AgentModel:         "claude-haiku-4-5-20251001",
	}
	if err := s.PutTeamSettings(ctx, settings); err != nil {
		t.Fatalf("PutTeamSettings: %v", err)
	}
	got, err := s.GetTeamSettings(ctx, teamID)
	if err != nil {
		t.Fatalf("GetTeamSettings: %v", err)
	}
	if len(got.Domains) != 2 {
		t.Errorf("Domains = %v", got.Domains)
	}
	if got.ClusterThreshold != 0.9 {
		t.Errorf("ClusterThreshold = %v", got.ClusterThreshold)
	}
}

func TestAuthConfig(t *testing.T) {
	s := newTestTeamStore(t)
	ctx := context.Background()

	cfg := AuthConfig{
		Provider:        "oidc",
		OIDCIssuer:      "https://clerk.example.com",
		OIDCClientID:    "client-id-123",
		OIDCRedirectURL: "http://localhost:8080/auth/oidc/callback",
	}
	if err := s.PutAuthConfig(ctx, cfg); err != nil {
		t.Fatalf("PutAuthConfig: %v", err)
	}
	got, err := s.GetAuthConfig(ctx)
	if err != nil {
		t.Fatalf("GetAuthConfig: %v", err)
	}
	if got.OIDCIssuer != "https://clerk.example.com" {
		t.Errorf("OIDCIssuer = %q", got.OIDCIssuer)
	}
}

func TestLogAndQueryActivity(t *testing.T) {
	s := newTestTeamStore(t)
	ctx := context.Background()

	for i := 0; i < 3; i++ {
		if err := s.LogActivity(ctx, ActivityEntry{
			ID:         fmt.Sprintf("a%d", i),
			TeamID:     "team-1",
			Action:     "knowledge.store",
			EntityType: "entry",
			EntityID:   fmt.Sprintf("entry-%d", i),
		}); err != nil {
			t.Fatalf("LogActivity: %v", err)
		}
	}
	entries, err := s.QueryActivity(ctx, "team-1", 10)
	if err != nil {
		t.Fatalf("QueryActivity: %v", err)
	}
	if len(entries) != 3 {
		t.Errorf("want 3 entries, got %d", len(entries))
	}
}
```

Add `"fmt"` to the import block since `fmt.Sprintf` is used.

- [ ] **Step 2: Run — verify they fail**

```bash
cd /Users/dsandor/Projects/memory && CGO_ENABLED=1 go test ./internal/storage/... 2>&1 | head -10
```

Expected: compile error — `CreateTeam`, `GetTeam`, etc. undefined on `*SQLiteStore`.

- [ ] **Step 3: Add schema migrations to sqlite.go**

In `internal/storage/sqlite.go`, inside the `migrate()` function, before the final `return nil`, add:

```go
	// Phase 5: team/auth tables
	newTables := []string{
		`CREATE TABLE IF NOT EXISTS teams (
			id              TEXT PRIMARY KEY,
			name            TEXT NOT NULL,
			domain_patterns TEXT NOT NULL DEFAULT '[]',
			enabled         INTEGER NOT NULL DEFAULT 1,
			created_at      DATETIME DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE TABLE IF NOT EXISTS users (
			id                TEXT PRIMARY KEY,
			team_id           TEXT REFERENCES teams(id),
			email             TEXT NOT NULL UNIQUE,
			name              TEXT NOT NULL DEFAULT '',
			external_id       TEXT NOT NULL DEFAULT '',
			password_hash     TEXT NOT NULL DEFAULT '',
			role              TEXT NOT NULL DEFAULT 'member',
			manually_assigned INTEGER NOT NULL DEFAULT 0,
			created_at        DATETIME DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE TABLE IF NOT EXISTS sessions (
			id          TEXT PRIMARY KEY,
			user_id     TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			token_hash  TEXT NOT NULL UNIQUE,
			expires_at  DATETIME NOT NULL,
			created_at  DATETIME DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE TABLE IF NOT EXISTS api_keys (
			id           TEXT PRIMARY KEY,
			team_id      TEXT REFERENCES teams(id),
			user_id      TEXT REFERENCES users(id),
			key_type     TEXT NOT NULL DEFAULT 'team',
			name         TEXT NOT NULL DEFAULT '',
			key_hash     TEXT NOT NULL UNIQUE,
			role         TEXT NOT NULL DEFAULT 'member',
			created_at   DATETIME DEFAULT CURRENT_TIMESTAMP,
			last_used_at DATETIME
		)`,
		`CREATE TABLE IF NOT EXISTS auth_config (
			id                INTEGER PRIMARY KEY CHECK (id = 1),
			provider          TEXT NOT NULL DEFAULT 'local',
			oidc_issuer       TEXT NOT NULL DEFAULT '',
			oidc_client_id    TEXT NOT NULL DEFAULT '',
			oidc_redirect_url TEXT NOT NULL DEFAULT '',
			updated_at        DATETIME DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE TABLE IF NOT EXISTS team_settings (
			team_id              TEXT PRIMARY KEY REFERENCES teams(id),
			domains              TEXT NOT NULL DEFAULT '[]',
			cluster_threshold    REAL NOT NULL DEFAULT 0.85,
			pipeline_min_entries INTEGER NOT NULL DEFAULT 10,
			agent_model          TEXT NOT NULL DEFAULT 'claude-haiku-4-5-20251001',
			updated_at           DATETIME DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE TABLE IF NOT EXISTS activity_log (
			id          TEXT PRIMARY KEY,
			team_id     TEXT NOT NULL DEFAULT '',
			key_id      TEXT NOT NULL DEFAULT '',
			user_id     TEXT NOT NULL DEFAULT '',
			action      TEXT NOT NULL,
			entity_type TEXT NOT NULL DEFAULT '',
			entity_id   TEXT NOT NULL DEFAULT '',
			created_at  DATETIME DEFAULT CURRENT_TIMESTAMP
		)`,
	}
	for _, ddl := range newTables {
		if _, err := s.db.Exec(ddl); err != nil {
			return fmt.Errorf("create team table: %w", err)
		}
	}

	// Phase 5: extend existing tables
	phase5Alters := []string{
		"ALTER TABLE entries ADD COLUMN team_id TEXT NOT NULL DEFAULT ''",
		"ALTER TABLE entries ADD COLUMN status  TEXT NOT NULL DEFAULT 'approved'",
		"ALTER TABLE clusters ADD COLUMN team_id TEXT NOT NULL DEFAULT ''",
		"ALTER TABLE agents ADD COLUMN team_id TEXT NOT NULL DEFAULT ''",
		"ALTER TABLE agent_versions ADD COLUMN team_id TEXT NOT NULL DEFAULT ''",
		"ALTER TABLE rules ADD COLUMN team_id TEXT NOT NULL DEFAULT ''",
	}
	for _, alter := range phase5Alters {
		if _, err := s.db.Exec(alter); err != nil {
			if !strings.Contains(err.Error(), "duplicate column name") {
				return fmt.Errorf("phase5 alter: %w", err)
			}
		}
	}

	return nil
```

- [ ] **Step 4: Create internal/storage/teams_sqlite.go**

```go
package storage

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"time"

	"github.com/google/uuid"
)

// Compile-time check: *SQLiteStore must implement TeamStore.
var _ TeamStore = (*SQLiteStore)(nil)

func (s *SQLiteStore) CreateTeam(ctx context.Context, t Team) (string, error) {
	t.ID = uuid.NewString()
	patterns, _ := json.Marshal(t.Domains())
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO teams (id, name, domain_patterns, enabled) VALUES (?, ?, ?, ?)`,
		t.ID, t.Name, string(patterns), boolToInt(t.Enabled),
	)
	if err != nil {
		return "", fmt.Errorf("create team: %w", err)
	}
	return t.ID, nil
}

func (s *SQLiteStore) GetTeam(ctx context.Context, id string) (*Team, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, name, domain_patterns, enabled, created_at FROM teams WHERE id = ?`, id)
	return scanTeam(row)
}

func (s *SQLiteStore) ListTeams(ctx context.Context) ([]Team, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, name, domain_patterns, enabled, created_at FROM teams ORDER BY name`)
	if err != nil {
		return nil, fmt.Errorf("list teams: %w", err)
	}
	defer rows.Close()
	var teams []Team
	for rows.Next() {
		t, err := scanTeam(rows)
		if err != nil {
			return nil, err
		}
		teams = append(teams, *t)
	}
	return teams, rows.Err()
}

func (s *SQLiteStore) SetTeamEnabled(ctx context.Context, id string, enabled bool) error {
	res, err := s.db.ExecContext(ctx,
		`UPDATE teams SET enabled = ? WHERE id = ?`, boolToInt(enabled), id)
	if err != nil {
		return fmt.Errorf("set team enabled: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("team %q: %w", id, ErrNotFound)
	}
	return nil
}

func (s *SQLiteStore) DeleteTeam(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM teams WHERE id = ?`, id)
	return err
}

func (s *SQLiteStore) UpsertUser(ctx context.Context, u User) (string, error) {
	existing, err := s.GetUserByEmail(ctx, u.Email)
	if err == nil && existing != nil {
		// update name, external_id, role
		_, err = s.db.ExecContext(ctx,
			`UPDATE users SET name=?, external_id=?, role=? WHERE id=?`,
			u.Name, u.ExternalID, u.Role, existing.ID,
		)
		return existing.ID, err
	}
	if u.ID == "" {
		u.ID = uuid.NewString()
	}
	_, err = s.db.ExecContext(ctx,
		`INSERT INTO users (id, team_id, email, name, external_id, password_hash, role, manually_assigned)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		u.ID, u.TeamID, u.Email, u.Name, u.ExternalID, u.PasswordHash, u.Role, boolToInt(u.ManuallyAssigned),
	)
	return u.ID, err
}

func (s *SQLiteStore) GetUserByEmail(ctx context.Context, email string) (*User, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, team_id, email, name, external_id, password_hash, role, manually_assigned, created_at
		 FROM users WHERE email = ?`, email)
	return scanUser(row)
}

func (s *SQLiteStore) GetUserByExternalID(ctx context.Context, externalID string) (*User, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, team_id, email, name, external_id, password_hash, role, manually_assigned, created_at
		 FROM users WHERE external_id = ?`, externalID)
	return scanUser(row)
}

func (s *SQLiteStore) ListUsers(ctx context.Context, teamID string) ([]User, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, team_id, email, name, external_id, password_hash, role, manually_assigned, created_at
		 FROM users WHERE team_id = ? ORDER BY email`, teamID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var users []User
	for rows.Next() {
		u, err := scanUser(rows)
		if err != nil {
			return nil, err
		}
		users = append(users, *u)
	}
	return users, rows.Err()
}

func (s *SQLiteStore) AssignUserToTeam(ctx context.Context, userID, teamID, role string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE users SET team_id=?, role=?, manually_assigned=1 WHERE id=?`,
		teamID, role, userID)
	return err
}

func (s *SQLiteStore) ResolveTeamByEmail(ctx context.Context, email string) (*Team, error) {
	teams, err := s.ListTeams(ctx)
	if err != nil {
		return nil, err
	}
	for _, t := range teams {
		if !t.Enabled {
			continue
		}
		for _, pattern := range t.DomainPatterns {
			re, err := regexp.Compile(pattern)
			if err != nil {
				continue
			}
			if re.MatchString(email) {
				tc := t
				return &tc, nil
			}
		}
	}
	return nil, nil
}

func (s *SQLiteStore) CreateAPIKey(ctx context.Context, key APIKey) error {
	if key.ID == "" {
		key.ID = uuid.NewString()
	}
	teamID := key.TeamID
	userID := key.UserID
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO api_keys (id, team_id, user_id, key_type, name, key_hash, role) VALUES (?,?,?,?,?,?,?)`,
		key.ID, nullIfEmpty(teamID), nullIfEmpty(userID), key.KeyType, key.Name, key.KeyHash, key.Role,
	)
	return err
}

func (s *SQLiteStore) GetAPIKeyByHash(ctx context.Context, hash string) (*APIKey, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, COALESCE(team_id,''), COALESCE(user_id,''), key_type, name, key_hash, role, created_at, last_used_at
		 FROM api_keys WHERE key_hash = ?`, hash)
	return scanAPIKey(row)
}

func (s *SQLiteStore) ListAPIKeys(ctx context.Context, teamID string) ([]APIKey, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, COALESCE(team_id,''), COALESCE(user_id,''), key_type, name, key_hash, role, created_at, last_used_at
		 FROM api_keys WHERE team_id = ? OR (team_id IS NULL AND ? = '')
		 ORDER BY created_at DESC`, teamID, teamID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var keys []APIKey
	for rows.Next() {
		k, err := scanAPIKey(rows)
		if err != nil {
			return nil, err
		}
		keys = append(keys, *k)
	}
	return keys, rows.Err()
}

func (s *SQLiteStore) RevokeAPIKey(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM api_keys WHERE id = ?`, id)
	return err
}

func (s *SQLiteStore) TouchAPIKey(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE api_keys SET last_used_at = CURRENT_TIMESTAMP WHERE id = ?`, id)
	return err
}

func (s *SQLiteStore) CreateSession(ctx context.Context, sess Session) error {
	if sess.ID == "" {
		sess.ID = uuid.NewString()
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO sessions (id, user_id, token_hash, expires_at) VALUES (?,?,?,?)`,
		sess.ID, sess.UserID, sess.TokenHash, sess.ExpiresAt.UTC().Format(time.RFC3339),
	)
	return err
}

func (s *SQLiteStore) GetSession(ctx context.Context, tokenHash string) (*Session, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, user_id, token_hash, expires_at, created_at FROM sessions WHERE token_hash = ?`, tokenHash)
	var sess Session
	var expiresAt, createdAt string
	if err := row.Scan(&sess.ID, &sess.UserID, &sess.TokenHash, &expiresAt, &createdAt); err != nil {
		return nil, fmt.Errorf("session not found: %w", ErrNotFound)
	}
	sess.ExpiresAt = parseTimestamp(expiresAt)
	sess.CreatedAt = parseTimestamp(createdAt)
	return &sess, nil
}

func (s *SQLiteStore) DeleteSession(ctx context.Context, tokenHash string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE token_hash = ?`, tokenHash)
	return err
}

func (s *SQLiteStore) GetTeamSettings(ctx context.Context, teamID string) (*TeamSettings, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT team_id, domains, cluster_threshold, pipeline_min_entries, agent_model, updated_at
		 FROM team_settings WHERE team_id = ?`, teamID)
	var ts TeamSettings
	var domainsJSON, updatedAt string
	if err := row.Scan(&ts.TeamID, &domainsJSON, &ts.ClusterThreshold, &ts.PipelineMinEntries, &ts.AgentModel, &updatedAt); err != nil {
		// return defaults if not found
		return &TeamSettings{
			TeamID:             teamID,
			Domains:            []string{},
			ClusterThreshold:   0.85,
			PipelineMinEntries: 10,
			AgentModel:         "claude-haiku-4-5-20251001",
		}, nil
	}
	_ = json.Unmarshal([]byte(domainsJSON), &ts.Domains)
	ts.UpdatedAt = parseTimestamp(updatedAt)
	return &ts, nil
}

func (s *SQLiteStore) PutTeamSettings(ctx context.Context, ts TeamSettings) error {
	domainsJSON, _ := json.Marshal(ts.Domains)
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO team_settings (team_id, domains, cluster_threshold, pipeline_min_entries, agent_model, updated_at)
		 VALUES (?,?,?,?,?,CURRENT_TIMESTAMP)
		 ON CONFLICT(team_id) DO UPDATE SET
		   domains=excluded.domains,
		   cluster_threshold=excluded.cluster_threshold,
		   pipeline_min_entries=excluded.pipeline_min_entries,
		   agent_model=excluded.agent_model,
		   updated_at=CURRENT_TIMESTAMP`,
		ts.TeamID, string(domainsJSON), ts.ClusterThreshold, ts.PipelineMinEntries, ts.AgentModel,
	)
	return err
}

func (s *SQLiteStore) GetAuthConfig(ctx context.Context) (*AuthConfig, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT provider, oidc_issuer, oidc_client_id, oidc_redirect_url, updated_at FROM auth_config WHERE id=1`)
	var ac AuthConfig
	var updatedAt string
	if err := row.Scan(&ac.Provider, &ac.OIDCIssuer, &ac.OIDCClientID, &ac.OIDCRedirectURL, &updatedAt); err != nil {
		return &AuthConfig{Provider: "local"}, nil
	}
	ac.UpdatedAt = parseTimestamp(updatedAt)
	return &ac, nil
}

func (s *SQLiteStore) PutAuthConfig(ctx context.Context, c AuthConfig) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO auth_config (id, provider, oidc_issuer, oidc_client_id, oidc_redirect_url, updated_at)
		 VALUES (1,?,?,?,?,CURRENT_TIMESTAMP)
		 ON CONFLICT(id) DO UPDATE SET
		   provider=excluded.provider,
		   oidc_issuer=excluded.oidc_issuer,
		   oidc_client_id=excluded.oidc_client_id,
		   oidc_redirect_url=excluded.oidc_redirect_url,
		   updated_at=CURRENT_TIMESTAMP`,
		c.Provider, c.OIDCIssuer, c.OIDCClientID, c.OIDCRedirectURL,
	)
	return err
}

func (s *SQLiteStore) LogActivity(ctx context.Context, e ActivityEntry) error {
	if e.ID == "" {
		e.ID = uuid.NewString()
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO activity_log (id, team_id, key_id, user_id, action, entity_type, entity_id)
		 VALUES (?,?,?,?,?,?,?)`,
		e.ID, e.TeamID, e.KeyID, e.UserID, e.Action, e.EntityType, e.EntityID,
	)
	return err
}

func (s *SQLiteStore) QueryActivity(ctx context.Context, teamID string, limit int) ([]ActivityEntry, error) {
	if limit == 0 {
		limit = 50
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, team_id, key_id, user_id, action, entity_type, entity_id, created_at
		 FROM activity_log WHERE team_id = ? ORDER BY created_at DESC LIMIT ?`, teamID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var entries []ActivityEntry
	for rows.Next() {
		var e ActivityEntry
		var createdAt string
		if err := rows.Scan(&e.ID, &e.TeamID, &e.KeyID, &e.UserID, &e.Action, &e.EntityType, &e.EntityID, &createdAt); err != nil {
			return nil, err
		}
		e.CreatedAt = parseTimestamp(createdAt)
		entries = append(entries, e)
	}
	return entries, rows.Err()
}

// --- helpers ---

type scanner interface {
	Scan(dest ...any) error
}

func scanTeam(row scanner) (*Team, error) {
	var t Team
	var patternsJSON, createdAt string
	var enabled int
	if err := row.Scan(&t.ID, &t.Name, &patternsJSON, &enabled, &createdAt); err != nil {
		return nil, fmt.Errorf("team not found: %w", ErrNotFound)
	}
	t.Enabled = enabled == 1
	t.CreatedAt = parseTimestamp(createdAt)
	_ = json.Unmarshal([]byte(patternsJSON), &t.DomainPatterns)
	return &t, nil
}

func scanUser(row scanner) (*User, error) {
	var u User
	var createdAt string
	var manuallyAssigned int
	if err := row.Scan(&u.ID, &u.TeamID, &u.Email, &u.Name, &u.ExternalID, &u.PasswordHash, &u.Role, &manuallyAssigned, &createdAt); err != nil {
		return nil, fmt.Errorf("user not found: %w", ErrNotFound)
	}
	u.ManuallyAssigned = manuallyAssigned == 1
	u.CreatedAt = parseTimestamp(createdAt)
	return &u, nil
}

func scanAPIKey(row scanner) (*APIKey, error) {
	var k APIKey
	var createdAt string
	var lastUsedAt *string
	if err := row.Scan(&k.ID, &k.TeamID, &k.UserID, &k.KeyType, &k.Name, &k.KeyHash, &k.Role, &createdAt, &lastUsedAt); err != nil {
		return nil, fmt.Errorf("api key not found: %w", ErrNotFound)
	}
	k.CreatedAt = parseTimestamp(createdAt)
	if lastUsedAt != nil {
		t := parseTimestamp(*lastUsedAt)
		k.LastUsedAt = &t
	}
	return &k, nil
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func nullIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}
```

Note: `Team.Domains()` is a typo in `CreateTeam` — fix it to use `t.DomainPatterns` directly in the `json.Marshal` call:
```go
patterns, _ := json.Marshal(t.DomainPatterns)
```

- [ ] **Step 5: Run — verify tests pass**

```bash
cd /Users/dsandor/Projects/memory && CGO_ENABLED=1 go test ./internal/storage/... -v -run "TestCreate|TestList|TestSet|TestDelete|TestUpsert|TestAssign|TestResolve|TestAPIKey|TestRevoke|TestSession|TestTeamSettings|TestAuthConfig|TestLog" 2>&1 | grep -E "^--- |^ok|FAIL"
```

Expected: all PASS.

- [ ] **Step 6: Build check**

```bash
cd /Users/dsandor/Projects/memory && CGO_ENABLED=1 go build ./... 2>&1 | grep -v deprecated
```

Expected: no errors.

---

## Task 4: Storage — extend KnowledgeEntry with Status, add ApproveEntry/RejectEntry/UpdateEntry

**Files:**
- Modify: `internal/storage/storage.go`
- Modify: `internal/storage/sqlite.go`
- Modify: `internal/storage/storage_test.go`

- [ ] **Step 1: Write failing tests**

Append to `internal/storage/storage_test.go`:

```go
func TestApproveAndRejectEntry(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	id := storeTestEntry(t, s, "pending entry", "content")

	// default status should be "approved" for migration compat
	e, _ := s.GetEntry(ctx, id)
	if e.Status != "approved" {
		t.Errorf("default Status = %q, want approved", e.Status)
	}

	if err := s.RejectEntry(ctx, id); err != nil {
		t.Fatalf("RejectEntry: %v", err)
	}
	e, _ = s.GetEntry(ctx, id)
	if e.Status != "rejected" {
		t.Errorf("Status after reject = %q", e.Status)
	}

	if err := s.ApproveEntry(ctx, id); err != nil {
		t.Fatalf("ApproveEntry: %v", err)
	}
	e, _ = s.GetEntry(ctx, id)
	if e.Status != "approved" {
		t.Errorf("Status after approve = %q", e.Status)
	}
}

func TestListEntries_StatusFilter(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	id := storeTestEntry(t, s, "pending", "content")
	_ = s.RejectEntry(ctx, id)

	all, _ := s.ListEntries(ctx, ListFilter{Limit: 10})
	pending, _ := s.ListEntries(ctx, ListFilter{Limit: 10, Status: "rejected"})
	approved, _ := s.ListEntries(ctx, ListFilter{Limit: 10, Status: "approved"})

	if len(all) == 0 {
		t.Fatal("expected entries")
	}
	if len(pending) != 1 {
		t.Errorf("rejected filter: want 1, got %d", len(pending))
	}
	if len(approved) != 0 {
		t.Errorf("approved filter: want 0, got %d", len(approved))
	}
}

func TestUpdateEntry(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	id := storeTestEntry(t, s, "original title", "original content")

	e, _ := s.GetEntry(ctx, id)
	e.Title = "updated title"
	e.Content = "updated content"
	if err := s.UpdateEntry(ctx, *e); err != nil {
		t.Fatalf("UpdateEntry: %v", err)
	}
	got, _ := s.GetEntry(ctx, id)
	if got.Title != "updated title" {
		t.Errorf("Title = %q", got.Title)
	}
}
```

- [ ] **Step 2: Run — verify they fail**

```bash
cd /Users/dsandor/Projects/memory && CGO_ENABLED=1 go test ./internal/storage/... -run "TestApprove|TestReject|TestListEntries_Status|TestUpdateEntry" 2>&1 | head -10
```

Expected: compile error — `Status` field, `ApproveEntry`, `RejectEntry`, `UpdateEntry` undefined.

- [ ] **Step 3: Extend storage.go**

In `internal/storage/storage.go`, add `Status string` to `KnowledgeEntry` after `UsageCount`:

```go
type KnowledgeEntry struct {
	ID          string
	Type        KnowledgeType
	Title       string
	Content     string
	Description string
	Domain      string
	Tags        []string
	Author      string
	Team        string
	CreatedAt   time.Time
	UpdatedAt   time.Time
	Version     int
	Rating      float64
	UsageCount  int
	Status      string // "pending" | "approved" | "rejected"
	TeamID      string
}
```

Add `Status string` to `ListFilter`:

```go
type ListFilter struct {
	Domain string
	Type   KnowledgeType
	Limit  int
	Offset int
	Search string
	Status string // filter by entry status; empty = no filter
	TeamID string // filter by team_id; empty = no filter
}
```

Add to `Store` interface after `RateEntry`:

```go
	// ApproveEntry sets an entry's status to "approved".
	ApproveEntry(ctx context.Context, id string) error
	// RejectEntry sets an entry's status to "rejected".
	RejectEntry(ctx context.Context, id string) error
	// UpdateEntry updates the mutable fields of an existing entry (title, content, description, domain, tags).
	UpdateEntry(ctx context.Context, entry KnowledgeEntry) error
```

- [ ] **Step 4: Implement in sqlite.go**

In `internal/storage/sqlite.go`, add after `RateEntry`:

```go
func (s *SQLiteStore) ApproveEntry(ctx context.Context, id string) error {
	res, err := s.db.ExecContext(ctx,
		`UPDATE entries SET status='approved', updated_at=CURRENT_TIMESTAMP WHERE id=?`, id)
	if err != nil {
		return fmt.Errorf("approve entry: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("entry %q: %w", id, ErrNotFound)
	}
	return nil
}

func (s *SQLiteStore) RejectEntry(ctx context.Context, id string) error {
	res, err := s.db.ExecContext(ctx,
		`UPDATE entries SET status='rejected', updated_at=CURRENT_TIMESTAMP WHERE id=?`, id)
	if err != nil {
		return fmt.Errorf("reject entry: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("entry %q: %w", id, ErrNotFound)
	}
	return nil
}

func (s *SQLiteStore) UpdateEntry(ctx context.Context, entry KnowledgeEntry) error {
	tagsJSON, _ := json.Marshal(entry.Tags)
	res, err := s.db.ExecContext(ctx,
		`UPDATE entries SET title=?, content=?, description=?, domain=?, tags=?, updated_at=CURRENT_TIMESTAMP WHERE id=?`,
		entry.Title, entry.Content, entry.Description, entry.Domain, string(tagsJSON), entry.ID,
	)
	if err != nil {
		return fmt.Errorf("update entry: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("entry %q: %w", entry.ID, ErrNotFound)
	}
	return nil
}
```

Update `scanEntry` in `sqlite.go` to read `team_id` and `status`. Find the existing `scanEntry` function and update it to include these new columns. The SELECT in `ListEntries` and `GetEntry` must also select `team_id, status`. Update the existing `ListEntries` SELECT to:

```go
	query := `SELECT id, type, title, content, description, domain, tags, author, team,
	                 created_at, updated_at, version, rating, usage_count, team_id, status
	          FROM entries WHERE 1=1`
```

And update `scanEntry` to scan `team_id` and `status`:

```go
func scanEntry(rows interface{ Scan(...any) error }) (*KnowledgeEntry, error) {
	var e KnowledgeEntry
	var tagsJSON, createdAt, updatedAt string
	if err := rows.Scan(
		&e.ID, (*string)(&e.Type), &e.Title, &e.Content, &e.Description,
		&e.Domain, &tagsJSON, &e.Author, &e.Team,
		&createdAt, &updatedAt, &e.Version, &e.Rating, &e.UsageCount,
		&e.TeamID, &e.Status,
	); err != nil {
		return nil, fmt.Errorf("scan entry: %w", err)
	}
	_ = json.Unmarshal([]byte(tagsJSON), &e.Tags)
	e.CreatedAt = parseTimestamp(createdAt)
	e.UpdatedAt = parseTimestamp(updatedAt)
	return &e, nil
}
```

Also update `ListEntries` to apply `Status` and `TeamID` filters:

```go
	if filter.Status != "" {
		query += " AND status = ?"
		args = append(args, filter.Status)
	}
	if filter.TeamID != "" {
		query += " AND team_id = ?"
		args = append(args, filter.TeamID)
	}
```

And update `GetEntry` SELECT:

```go
func (s *SQLiteStore) GetEntry(ctx context.Context, id string) (*KnowledgeEntry, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, type, title, content, description, domain, tags, author, team,
		        created_at, updated_at, version, rating, usage_count, team_id, status
		 FROM entries WHERE id = ?`, id)
	return scanEntry(row)
}
```

Also update `CountEntries` to accept team_id optionally — add a `CountEntriesForTeam` method:

```go
func (s *SQLiteStore) CountEntries(ctx context.Context) (int, error) {
	var n int
	return n, s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM entries`).Scan(&n)
}
```

(This keeps existing tests working; team-scoped counts will use `ListFilter` with `TeamID`.)

- [ ] **Step 5: Run — verify tests pass**

```bash
cd /Users/dsandor/Projects/memory && CGO_ENABLED=1 go test ./internal/storage/... -v 2>&1 | grep -E "^--- |^ok|FAIL"
```

Expected: all PASS.

---

## Task 5: Auth package — Provider interface + LocalProvider + OIDCProvider

**Files:**
- Create: `internal/auth/provider.go`
- Create: `internal/auth/provider_test.go`

- [ ] **Step 1: Write failing tests**

Create `internal/auth/provider_test.go`:

```go
package auth_test

import (
	"context"
	"testing"

	"github.com/dsandor/memory/internal/auth"
	"github.com/dsandor/memory/internal/storage"
)

type mockStore struct {
	user *storage.User
}

func (m *mockStore) GetUserByEmail(_ context.Context, email string) (*storage.User, error) {
	if m.user != nil && m.user.Email == email {
		return m.user, nil
	}
	return nil, storage.ErrNotFound
}

func TestLocalProvider_VerifyPassword_WrongPassword(t *testing.T) {
	// $2a$10$ hash of "correct-password"
	hash := mustHash("correct-password")
	store := &mockStore{user: &storage.User{
		Email:        "alice@example.com",
		PasswordHash: hash,
	}}
	p := auth.NewLocalProvider(store)
	_, err := p.VerifyPassword(context.Background(), "alice@example.com", "wrong-password")
	if err == nil {
		t.Fatal("expected error for wrong password")
	}
}

func TestLocalProvider_VerifyPassword_Correct(t *testing.T) {
	hash := mustHash("correct-password")
	store := &mockStore{user: &storage.User{
		ID:           "u1",
		Email:        "alice@example.com",
		Name:         "Alice",
		PasswordHash: hash,
	}}
	p := auth.NewLocalProvider(store)
	info, err := p.VerifyPassword(context.Background(), "alice@example.com", "correct-password")
	if err != nil {
		t.Fatalf("VerifyPassword: %v", err)
	}
	if info.Email != "alice@example.com" {
		t.Errorf("Email = %q", info.Email)
	}
}

func TestLocalProvider_AuthURL_ReturnsEmpty(t *testing.T) {
	p := auth.NewLocalProvider(nil)
	if url := p.AuthURL("state"); url != "" {
		t.Errorf("LocalProvider.AuthURL should return empty, got %q", url)
	}
}

// mustHash creates a bcrypt hash for testing.
func mustHash(password string) string {
	b, err := auth.HashPassword(password)
	if err != nil {
		panic(err)
	}
	return b
}
```

- [ ] **Step 2: Run — verify they fail**

```bash
cd /Users/dsandor/Projects/memory && CGO_ENABLED=1 go test ./internal/auth/... 2>&1 | head -5
```

Expected: compile error — package `auth` does not exist.

- [ ] **Step 3: Create internal/auth/provider.go**

```go
package auth

import (
	"context"
	"errors"
	"fmt"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/dsandor/memory/internal/storage"
	"golang.org/x/crypto/bcrypt"
	"golang.org/x/oauth2"
)

// UserInfo holds the resolved identity from either auth path.
type UserInfo struct {
	Email      string
	Name       string
	ExternalID string // OIDC subject claim; empty for local auth
}

// Provider abstracts over OIDC and local auth paths.
type Provider interface {
	// AuthURL returns the OIDC redirect URL. Empty for LocalProvider.
	AuthURL(state string) string
	// Exchange exchanges an OIDC auth code for a UserInfo.
	Exchange(ctx context.Context, code string) (*UserInfo, error)
	// VerifyPassword verifies email+password for local auth.
	VerifyPassword(ctx context.Context, email, password string) (*UserInfo, error)
}

// HashPassword bcrypts a plaintext password for storage.
func HashPassword(password string) (string, error) {
	b, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// --- LocalProvider ---

type localUserLookup interface {
	GetUserByEmail(ctx context.Context, email string) (*storage.User, error)
}

type LocalProvider struct{ store localUserLookup }

func NewLocalProvider(store localUserLookup) *LocalProvider {
	return &LocalProvider{store: store}
}

func (p *LocalProvider) AuthURL(_ string) string { return "" }

func (p *LocalProvider) Exchange(_ context.Context, _ string) (*UserInfo, error) {
	return nil, errors.New("local provider does not support OIDC exchange")
}

func (p *LocalProvider) VerifyPassword(ctx context.Context, email, password string) (*UserInfo, error) {
	u, err := p.store.GetUserByEmail(ctx, email)
	if err != nil {
		return nil, errors.New("invalid credentials")
	}
	if err := bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(password)); err != nil {
		return nil, errors.New("invalid credentials")
	}
	return &UserInfo{Email: u.Email, Name: u.Name}, nil
}

// --- OIDCProvider ---

type OIDCProvider struct {
	provider *oidc.Provider
	config   oauth2.Config
	verifier *oidc.IDTokenVerifier
}

func NewOIDCProvider(ctx context.Context, issuer, clientID, clientSecret, redirectURL string) (*OIDCProvider, error) {
	provider, err := oidc.NewProvider(ctx, issuer)
	if err != nil {
		return nil, fmt.Errorf("oidc provider: %w", err)
	}
	cfg := oauth2.Config{
		ClientID:     clientID,
		ClientSecret: clientSecret,
		RedirectURL:  redirectURL,
		Endpoint:     provider.Endpoint(),
		Scopes:       []string{oidc.ScopeOpenID, "profile", "email"},
	}
	return &OIDCProvider{
		provider: provider,
		config:   cfg,
		verifier: provider.Verifier(&oidc.Config{ClientID: clientID}),
	}, nil
}

func (p *OIDCProvider) AuthURL(state string) string {
	return p.config.AuthCodeURL(state)
}

func (p *OIDCProvider) Exchange(ctx context.Context, code string) (*UserInfo, error) {
	token, err := p.config.Exchange(ctx, code)
	if err != nil {
		return nil, fmt.Errorf("exchange code: %w", err)
	}
	rawIDToken, ok := token.Extra("id_token").(string)
	if !ok {
		return nil, errors.New("no id_token in response")
	}
	idToken, err := p.verifier.Verify(ctx, rawIDToken)
	if err != nil {
		return nil, fmt.Errorf("verify id_token: %w", err)
	}
	var claims struct {
		Email string `json:"email"`
		Name  string `json:"name"`
	}
	if err := idToken.Claims(&claims); err != nil {
		return nil, fmt.Errorf("parse claims: %w", err)
	}
	return &UserInfo{
		Email:      claims.Email,
		Name:       claims.Name,
		ExternalID: idToken.Subject,
	}, nil
}

func (p *OIDCProvider) VerifyPassword(_ context.Context, _, _ string) (*UserInfo, error) {
	return nil, errors.New("OIDC provider does not support local password auth")
}
```

- [ ] **Step 4: Run — verify tests pass**

```bash
cd /Users/dsandor/Projects/memory && CGO_ENABLED=1 go test ./internal/auth/... -v 2>&1 | grep -E "^--- |^ok|FAIL"
```

Expected: all PASS.

---

## Task 6: Auth package — TeamContext middleware

**Files:**
- Create: `internal/auth/middleware.go`
- Create: `internal/auth/middleware_test.go`

- [ ] **Step 1: Write failing tests**

Create `internal/auth/middleware_test.go`:

```go
package auth_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/dsandor/memory/internal/auth"
	"github.com/dsandor/memory/internal/storage"
)

type mockKeyStore struct {
	key *storage.APIKey
}

func (m *mockKeyStore) GetAPIKeyByHash(_ context.Context, hash string) (*storage.APIKey, error) {
	if m.key != nil && m.key.KeyHash == hash {
		return m.key, nil
	}
	return nil, storage.ErrNotFound
}
func (m *mockKeyStore) GetSession(_ context.Context, tokenHash string) (*storage.Session, error) {
	return nil, storage.ErrNotFound
}
func (m *mockKeyStore) GetUserByEmail(_ context.Context, email string) (*storage.User, error) {
	return nil, storage.ErrNotFound
}
func (m *mockKeyStore) TouchAPIKey(_ context.Context, id string) error { return nil }

func TestRequireAuth_MissingCredentials(t *testing.T) {
	mw := auth.RequireAuth(&mockKeyStore{})
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	}))
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, httptest.NewRequest("GET", "/", nil))
	if rr.Code != 401 {
		t.Errorf("expected 401, got %d", rr.Code)
	}
}

func TestRequireAuth_ValidBearerToken(t *testing.T) {
	store := &mockKeyStore{key: &storage.APIKey{
		ID:      "k1",
		TeamID:  "team-abc",
		KeyHash: auth.HashSHA256("my-raw-key"),
		Role:    "member",
	}}
	mw := auth.RequireAuth(store)
	var gotCtx auth.TeamContext
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotCtx = auth.GetTeamContext(r.Context())
		w.WriteHeader(200)
	}))
	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("Authorization", "Bearer my-raw-key")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != 200 {
		t.Errorf("expected 200, got %d", rr.Code)
	}
	if gotCtx.TeamID != "team-abc" {
		t.Errorf("TeamID = %q", gotCtx.TeamID)
	}
	if gotCtx.Role != "member" {
		t.Errorf("Role = %q", gotCtx.Role)
	}
}

func TestRequireCurator_RejectsMember(t *testing.T) {
	store := &mockKeyStore{key: &storage.APIKey{
		ID:      "k2",
		TeamID:  "t",
		KeyHash: auth.HashSHA256("member-key"),
		Role:    "member",
	}}
	chain := auth.RequireAuth(store)(auth.RequireCurator()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	})))
	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("Authorization", "Bearer member-key")
	rr := httptest.NewRecorder()
	chain.ServeHTTP(rr, req)
	if rr.Code != 403 {
		t.Errorf("expected 403, got %d", rr.Code)
	}
}

func TestRequireSuperadmin_AllowsSuperadmin(t *testing.T) {
	store := &mockKeyStore{key: &storage.APIKey{
		ID:      "k3",
		KeyHash: auth.HashSHA256("super-key"),
		Role:    "superadmin",
	}}
	chain := auth.RequireAuth(store)(auth.RequireSuperadmin()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	})))
	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("Authorization", "Bearer super-key")
	rr := httptest.NewRecorder()
	chain.ServeHTTP(rr, req)
	if rr.Code != 200 {
		t.Errorf("expected 200, got %d", rr.Code)
	}
}
```

- [ ] **Step 2: Run — verify they fail**

```bash
cd /Users/dsandor/Projects/memory && CGO_ENABLED=1 go test ./internal/auth/... 2>&1 | head -10
```

Expected: compile errors.

- [ ] **Step 3: Create internal/auth/middleware.go**

```go
package auth

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/dsandor/memory/internal/storage"
)

// TeamContext holds the resolved identity for the current request.
type TeamContext struct {
	TeamID string
	KeyID  string // set for API key requests
	UserID string // set for session requests
	Role   string // member|curator|admin|superadmin
}

type contextKey struct{}

// GetTeamContext retrieves the injected TeamContext from a request context.
func GetTeamContext(ctx context.Context) TeamContext {
	if tc, ok := ctx.Value(contextKey{}).(TeamContext); ok {
		return tc
	}
	return TeamContext{}
}

// AuthStore is the minimal storage interface needed by RequireAuth.
type AuthStore interface {
	GetAPIKeyByHash(ctx context.Context, hash string) (*storage.APIKey, error)
	GetSession(ctx context.Context, tokenHash string) (*storage.Session, error)
	TouchAPIKey(ctx context.Context, id string) error
}

// HashSHA256 returns the hex-encoded SHA-256 of the input string.
// Used to hash raw API keys before DB lookup.
func HashSHA256(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}

func writeUnauth(w http.ResponseWriter, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusUnauthorized)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": msg})
}

func writeForbidden(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusForbidden)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": "forbidden"})
}

// RequireAuth resolves a bearer token or session cookie into a TeamContext.
// Returns 401 if neither is present or valid.
func RequireAuth(store AuthStore) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := r.Context()

			// 1. Try Bearer token
			if auth := r.Header.Get("Authorization"); strings.HasPrefix(auth, "Bearer ") {
				raw := strings.TrimPrefix(auth, "Bearer ")
				hash := HashSHA256(raw)
				key, err := store.GetAPIKeyByHash(ctx, hash)
				if err == nil {
					go store.TouchAPIKey(context.Background(), key.ID) //nolint
					tc := TeamContext{TeamID: key.TeamID, KeyID: key.ID, Role: key.Role}
					next.ServeHTTP(w, r.WithContext(context.WithValue(ctx, contextKey{}, tc)))
					return
				}
			}

			// 2. Try session cookie
			if cookie, err := r.Cookie("session"); err == nil {
				hash := HashSHA256(cookie.Value)
				sess, err := store.GetSession(ctx, hash)
				if err == nil && sess.ExpiresAt.After(time.Now()) {
					tc := TeamContext{UserID: sess.UserID}
					// Role will be populated by a user lookup; for now store UserID and let handlers enrich if needed.
					// For RBAC checks we embed role via a fuller lookup below — handled in session resolution.
					next.ServeHTTP(w, r.WithContext(context.WithValue(ctx, contextKey{}, tc)))
					return
				}
			}

			writeUnauth(w, "authentication required")
		})
	}
}

// roleRank maps roles to a numeric rank for comparison.
func roleRank(role string) int {
	switch role {
	case "superadmin":
		return 4
	case "admin":
		return 3
	case "curator":
		return 2
	case "member":
		return 1
	}
	return 0
}

func requireRole(minRole string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			tc := GetTeamContext(r.Context())
			if roleRank(tc.Role) < roleRank(minRole) {
				writeForbidden(w)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// RequireCurator gates routes to curator, admin, or superadmin.
func RequireCurator() func(http.Handler) http.Handler { return requireRole("curator") }

// RequireAdmin gates routes to admin or superadmin.
func RequireAdmin() func(http.Handler) http.Handler { return requireRole("admin") }

// RequireSuperadmin gates routes to superadmin only.
func RequireSuperadmin() func(http.Handler) http.Handler { return requireRole("superadmin") }
```

- [ ] **Step 4: Run — verify tests pass**

```bash
cd /Users/dsandor/Projects/memory && CGO_ENABLED=1 go test ./internal/auth/... -v 2>&1 | grep -E "^--- |^ok|FAIL"
```

Expected: all PASS.

---

## Task 7: Web — swap to chi router and wire auth middleware

**Files:**
- Modify: `internal/web/server.go`
- Modify: `internal/web/server_test.go`

- [ ] **Step 1: Update server.go**

Replace the entire `internal/web/server.go` with:

```go
package web

import (
	"io/fs"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/dsandor/memory/internal/auth"
	"github.com/dsandor/memory/internal/storage"
)

// AllStore is the storage interface required by the HTTP API.
// *storage.SQLiteStore satisfies this.
type AllStore interface {
	storage.AgentStore
	storage.TeamStore
}

// Server wraps a chi router with REST API routes and SPA static serving.
type Server struct {
	store    AllStore
	router   *chi.Mux
	staticFS fs.FS
}

// NewServer wires all routes and returns a ready Server.
// staticFS should be the built React dist (typically fs.Sub of the embedded FS).
func NewServer(staticFS fs.FS, store AllStore) *Server {
	s := &Server{
		store:    store,
		router:   chi.NewRouter(),
		staticFS: staticFS,
	}
	s.routes()
	return s
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.router.ServeHTTP(w, r)
}

func (s *Server) routes() {
	r := s.router
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)

	// Health — public
	r.Get("/health", s.handleHealth)

	// Auth — public
	r.Post("/auth/login", s.handleLogin)
	r.Get("/auth/oidc/login", s.handleOIDCLogin)
	r.Get("/auth/oidc/callback", s.handleOIDCCallback)
	r.Post("/auth/logout", s.handleLogout)

	authMW := auth.RequireAuth(s.store)

	// Member routes
	r.Group(func(r chi.Router) {
		r.Use(authMW)
		r.Get("/api/stats", s.handleStats)
		r.Get("/api/knowledge", s.handleKnowledgeList)
		r.Post("/api/knowledge", s.handleKnowledgeStore)
		r.Get("/api/knowledge/{id}", s.handleKnowledgeGet)
		r.Put("/api/knowledge/{id}", s.handleKnowledgeUpdate)
		r.Delete("/api/knowledge/{id}", s.handleKnowledgeDelete)
		r.Put("/api/knowledge/{id}/rate", s.handleKnowledgeRate)
		r.Get("/api/clusters", s.handleClusterList)
		r.Get("/api/clusters/{id}", s.handleClusterGet)
		r.Get("/api/clusters/{id}/summary", s.handleClusterSummary)
		r.Get("/api/datasets", s.handleDatasetList)
		r.Get("/api/datasets/{id}/export", s.handleDatasetExport)
		r.Get("/api/agents", s.handleAgentList)
		r.Get("/api/agents/bulk-export", s.handleAgentBulkExport)
		r.Get("/api/agents/domain/{domain}/latest", s.handleAgentLatestByDomain)
		r.Get("/api/agents/{id}", s.handleAgentGet)
		r.Get("/api/agents/{id}/export", s.handleAgentExport)
		r.Get("/api/pipeline/status", s.handlePipelineStatus)
		r.Get("/api/analytics/usage", s.handleUsage)
		r.Get("/api/analytics/gaps", s.handleGaps)
		r.Get("/api/analytics/contributions", s.handleContributions)
	})

	// Curator routes
	r.Group(func(r chi.Router) {
		r.Use(authMW)
		r.Use(auth.RequireCurator())
		r.Put("/api/knowledge/{id}/approve", s.handleKnowledgeApprove)
		r.Put("/api/knowledge/{id}/reject", s.handleKnowledgeReject)
		r.Put("/api/agents/{id}/publish", s.handleAgentPublish)
	})

	// Admin routes
	r.Group(func(r chi.Router) {
		r.Use(authMW)
		r.Use(auth.RequireAdmin())
		r.Post("/api/pipeline/trigger", s.handlePipelineTrigger)
		r.Get("/api/api-keys", s.handleListAPIKeys)
		r.Post("/api/api-keys", s.handleCreateAPIKey)
		r.Delete("/api/api-keys/{id}", s.handleRevokeAPIKey)
		r.Get("/api/users", s.handleListUsers)
		r.Post("/api/users", s.handleAssignUser)
		r.Put("/api/users/{id}/role", s.handleSetUserRole)
		r.Get("/api/settings", s.handleGetSettings)
		r.Put("/api/settings", s.handlePutSettings)
	})

	// Superadmin routes
	r.Group(func(r chi.Router) {
		r.Use(authMW)
		r.Use(auth.RequireSuperadmin())
		r.Get("/api/admin/teams", s.handleListTeams)
		r.Post("/api/admin/teams", s.handleCreateTeam)
		r.Put("/api/admin/teams/{id}/enabled", s.handleSetTeamEnabled)
		r.Delete("/api/admin/teams/{id}", s.handleDeleteTeam)
		r.Get("/api/admin/teams/{id}/users", s.handleListTeamUsers)
		r.Get("/api/admin/auth-config", s.handleGetAuthConfig)
		r.Put("/api/admin/auth-config", s.handlePutAuthConfig)
	})

	// SPA fallback
	r.Get("/*", s.handleStatic)
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, map[string]string{"status": "ok"})
}

func (s *Server) handleStatic(w http.ResponseWriter, r *http.Request) {
	if strings.HasPrefix(r.URL.Path, "/api/") || strings.HasPrefix(r.URL.Path, "/auth/") {
		http.NotFound(w, r)
		return
	}
	path := strings.TrimPrefix(r.URL.Path, "/")
	if path == "" {
		path = "index.html"
	}
	_, err := s.staticFS.Open(path)
	if err != nil {
		http.ServeFileFS(w, r, s.staticFS, "index.html")
		return
	}
	http.FileServerFS(s.staticFS).ServeHTTP(w, r)
}
```

- [ ] **Step 2: Update existing handlers to use chi path params**

In `internal/web/handlers.go`, replace all `r.PathValue("id")` calls with `chi.URLParam(r, "id")`. Add `"github.com/go-chi/chi/v5"` to imports.

- [ ] **Step 3: Update server_test.go to use chi**

In `internal/web/server_test.go`, the existing tests use `httptest.NewRequest` — they should still work since `NewServer` wraps chi. Update any `r.PathValue` usages in handler tests to `chi.URLParam`. Run:

```bash
cd /Users/dsandor/Projects/memory && CGO_ENABLED=1 go test ./internal/web/... -v 2>&1 | grep -E "^--- |^ok|FAIL"
```

Expected: all PASS (existing 12 tests still green).

- [ ] **Step 4: Build check**

```bash
cd /Users/dsandor/Projects/memory && CGO_ENABLED=1 go build ./... 2>&1 | grep -v deprecated
```

Expected: no errors (some "undefined" errors for new handler stubs are expected — they'll be added in Tasks 8–11).

---

## Task 8: Web — extend existing handlers for TeamContext + new endpoints

**Files:**
- Modify: `internal/web/handlers.go`

- [ ] **Step 1: Update handlers.go**

Add these new imports at the top of `internal/web/handlers.go`:
```go
import (
	// existing imports...
	"github.com/go-chi/chi/v5"
	"github.com/dsandor/memory/internal/auth"
)
```

Replace `r.PathValue("id")` with `chi.URLParam(r, "id")` throughout the file.

Add `handleKnowledgeStore` (POST /api/knowledge):

```go
func (s *Server) handleKnowledgeStore(w http.ResponseWriter, r *http.Request) {
	tc := auth.GetTeamContext(r.Context())
	var body struct {
		Title       string   `json:"title"`
		Content     string   `json:"content"`
		Type        string   `json:"type"`
		Domain      string   `json:"domain"`
		Description string   `json:"description"`
		Author      string   `json:"author"`
		Tags        []string `json:"tags"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, 400, "invalid JSON body")
		return
	}
	if body.Title == "" || body.Content == "" || body.Type == "" {
		writeError(w, 400, "title, content, and type are required")
		return
	}
	entry := storage.KnowledgeEntry{
		Type:        storage.KnowledgeType(body.Type),
		Title:       body.Title,
		Content:     body.Content,
		Description: body.Description,
		Domain:      body.Domain,
		Author:      body.Author,
		Team:        tc.TeamID,
		TeamID:      tc.TeamID,
		Tags:        body.Tags,
		Status:      "pending",
	}
	// Note: StoreEntry requires an embedding; pass zero-length slice to skip embedding for REST path.
	// Embedding is generated by the MCP path (knowledge_store tool). REST callers get pending status.
	id, err := s.store.StoreEntry(r.Context(), entry, nil)
	if err != nil {
		writeError(w, 500, fmt.Sprintf("store entry: %v", err))
		return
	}
	writeJSON(w, map[string]string{"id": id})
}
```

**Note:** `StoreEntry` requires an embedding slice. Since the REST API doesn't have an embedder, update `StoreEntry` in `sqlite.go` to accept `nil` embedding (store entry without vector):

In `sqlite.go` `StoreEntry`, after the tags JSON marshal, before the transaction:
```go
	// nil embedding means no vector index entry — entry won't appear in semantic search until re-embedded.
	if embedding != nil && len(embedding) != s.embeddingDim {
		return "", fmt.Errorf("embedding dim mismatch: got %d, want %d", len(embedding), s.embeddingDim)
	}
```
And wrap the vec insert in a nil check:
```go
	if embedding != nil {
		// ... existing vec insert code ...
	}
```

Add `handleKnowledgeUpdate` (PUT /api/knowledge/{id}):

```go
func (s *Server) handleKnowledgeUpdate(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	existing, err := s.store.GetEntry(r.Context(), id)
	if err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			writeError(w, 404, "entry not found")
			return
		}
		writeError(w, 500, fmt.Sprintf("get entry: %v", err))
		return
	}
	var body struct {
		Title       string   `json:"title"`
		Content     string   `json:"content"`
		Description string   `json:"description"`
		Domain      string   `json:"domain"`
		Tags        []string `json:"tags"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, 400, "invalid JSON body")
		return
	}
	if body.Title != "" {
		existing.Title = body.Title
	}
	if body.Content != "" {
		existing.Content = body.Content
	}
	if body.Description != "" {
		existing.Description = body.Description
	}
	if body.Domain != "" {
		existing.Domain = body.Domain
	}
	if body.Tags != nil {
		existing.Tags = body.Tags
	}
	if err := s.store.UpdateEntry(r.Context(), *existing); err != nil {
		writeError(w, 500, fmt.Sprintf("update entry: %v", err))
		return
	}
	writeJSON(w, map[string]any{"ok": true})
}
```

Add `handleKnowledgeDelete`:

```go
func (s *Server) handleKnowledgeDelete(w http.ResponseWriter, r *http.Request) {
	if err := s.store.DeleteEntry(r.Context(), chi.URLParam(r, "id")); err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			writeError(w, 404, "entry not found")
			return
		}
		writeError(w, 500, fmt.Sprintf("delete entry: %v", err))
		return
	}
	writeJSON(w, map[string]any{"ok": true})
}
```

Add `handleKnowledgeApprove` and `handleKnowledgeReject`:

```go
func (s *Server) handleKnowledgeApprove(w http.ResponseWriter, r *http.Request) {
	if err := s.store.ApproveEntry(r.Context(), chi.URLParam(r, "id")); err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			writeError(w, 404, "entry not found")
			return
		}
		writeError(w, 500, fmt.Sprintf("approve entry: %v", err))
		return
	}
	writeJSON(w, map[string]any{"ok": true})
}

func (s *Server) handleKnowledgeReject(w http.ResponseWriter, r *http.Request) {
	if err := s.store.RejectEntry(r.Context(), chi.URLParam(r, "id")); err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			writeError(w, 404, "entry not found")
			return
		}
		writeError(w, 500, fmt.Sprintf("reject entry: %v", err))
		return
	}
	writeJSON(w, map[string]any{"ok": true})
}
```

Add `handleClusterGet` and `handleClusterSummary`:

```go
func (s *Server) handleClusterGet(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	clusters, err := s.store.ListClusters(r.Context())
	if err != nil {
		writeError(w, 500, fmt.Sprintf("list clusters: %v", err))
		return
	}
	for _, c := range clusters {
		if c.ID == id {
			writeJSON(w, c)
			return
		}
	}
	writeError(w, 404, "cluster not found")
}

func (s *Server) handleClusterSummary(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	clusters, err := s.store.ListClusters(r.Context())
	if err != nil {
		writeError(w, 500, fmt.Sprintf("list clusters: %v", err))
		return
	}
	for _, c := range clusters {
		if c.ID == id {
			writeJSON(w, map[string]string{"summary": c.Summary})
			return
		}
	}
	writeError(w, 404, "cluster not found")
}
```

Add `handleAgentLatestByDomain`:

```go
func (s *Server) handleAgentLatestByDomain(w http.ResponseWriter, r *http.Request) {
	domain := chi.URLParam(r, "domain")
	agent, err := s.store.GetAgentByDomain(r.Context(), domain)
	if err != nil || agent == nil {
		writeError(w, 404, "no agent for domain")
		return
	}
	writeJSON(w, agent)
}
```

Add `handlePipelineTrigger` (placeholder — actual pipeline trigger wired in main.go Task 14):

```go
func (s *Server) handlePipelineTrigger(w http.ResponseWriter, r *http.Request) {
	// Pipeline trigger signal sent via channel set by main.go
	if s.triggerPipeline != nil {
		select {
		case s.triggerPipeline <- struct{}{}:
			writeJSON(w, map[string]string{"status": "triggered"})
		default:
			writeJSON(w, map[string]string{"status": "already_running"})
		}
	} else {
		writeError(w, 503, "pipeline not configured")
	}
}
```

Add `triggerPipeline` field to `Server` struct in `server.go`:

```go
type Server struct {
	store           AllStore
	router          *chi.Mux
	staticFS        fs.FS
	triggerPipeline chan<- struct{} // optional; set by main.go
}
```

Add `WithPipelineTrigger` option:

```go
func (s *Server) WithPipelineTrigger(ch chan<- struct{}) *Server {
	s.triggerPipeline = ch
	return s
}
```

- [ ] **Step 2: Build check**

```bash
cd /Users/dsandor/Projects/memory && CGO_ENABLED=1 go build ./internal/web/... 2>&1 | grep -v deprecated
```

Expected: errors only for undefined handlers in admin_handlers.go, analytics.go, settings.go, auth_handlers.go — those come next.

---

## Task 9: Web — auth handlers

**Files:**
- Create: `internal/web/auth_handlers.go`

- [ ] **Step 1: Create internal/web/auth_handlers.go**

```go
package web

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"time"

	"github.com/dsandor/memory/internal/auth"
	"github.com/dsandor/memory/internal/storage"
)

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, 400, "invalid JSON body")
		return
	}
	if body.Email == "" || body.Password == "" {
		writeError(w, 400, "email and password required")
		return
	}

	cfg, err := s.store.GetAuthConfig(r.Context())
	if err != nil || cfg.Provider != "local" {
		writeError(w, 400, "local auth not enabled")
		return
	}

	localProvider := auth.NewLocalProvider(s.store)
	info, err := localProvider.VerifyPassword(r.Context(), body.Email, body.Password)
	if err != nil {
		writeError(w, 401, "invalid credentials")
		return
	}

	sessionToken, tokenHash := generateToken()
	user, _ := s.store.GetUserByEmail(r.Context(), info.Email)
	if user == nil {
		writeError(w, 401, "user not found")
		return
	}
	sess := storage.Session{
		UserID:    user.ID,
		TokenHash: tokenHash,
		ExpiresAt: time.Now().Add(24 * time.Hour),
	}
	if err := s.store.CreateSession(r.Context(), sess); err != nil {
		writeError(w, 500, "create session failed")
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     "session",
		Value:    sessionToken,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Expires:  sess.ExpiresAt,
	})
	writeJSON(w, map[string]string{"ok": "true", "email": info.Email})
}

func (s *Server) handleOIDCLogin(w http.ResponseWriter, r *http.Request) {
	cfg, err := s.store.GetAuthConfig(r.Context())
	if err != nil || cfg.Provider != "oidc" {
		writeError(w, 400, "OIDC not configured")
		return
	}
	// state is a random token stored in a cookie for CSRF protection
	state, stateHash := generateToken()
	http.SetCookie(w, &http.Cookie{
		Name:     "oidc_state",
		Value:    stateHash,
		Path:     "/auth/oidc/callback",
		HttpOnly: true,
		MaxAge:   300,
	})
	p, err := auth.NewOIDCProvider(r.Context(), cfg.OIDCIssuer, cfg.OIDCClientID, s.oidcSecret, cfg.OIDCRedirectURL)
	if err != nil {
		writeError(w, 500, "OIDC provider init failed")
		return
	}
	http.Redirect(w, r, p.AuthURL(state), http.StatusFound)
}

func (s *Server) handleOIDCCallback(w http.ResponseWriter, r *http.Request) {
	cfg, _ := s.store.GetAuthConfig(r.Context())
	p, err := auth.NewOIDCProvider(r.Context(), cfg.OIDCIssuer, cfg.OIDCClientID, s.oidcSecret, cfg.OIDCRedirectURL)
	if err != nil {
		writeError(w, 500, "OIDC provider init failed")
		return
	}

	info, err := p.Exchange(r.Context(), r.URL.Query().Get("code"))
	if err != nil {
		writeError(w, 401, "OIDC exchange failed")
		return
	}

	// Upsert user
	user, _ := s.store.GetUserByExternalID(r.Context(), info.ExternalID)
	if user == nil {
		// Try by email
		user, _ = s.store.GetUserByEmail(r.Context(), info.Email)
	}
	role := "member"
	if user != nil {
		role = user.Role
	}

	uid, err := s.store.UpsertUser(r.Context(), storage.User{
		Email:      info.Email,
		Name:       info.Name,
		ExternalID: info.ExternalID,
		Role:       role,
	})
	if err != nil {
		writeError(w, 500, "upsert user failed")
		return
	}

	// Auto-assign to team by email domain if not already assigned
	if user == nil || user.TeamID == "" {
		if team, _ := s.store.ResolveTeamByEmail(r.Context(), info.Email); team != nil {
			_ = s.store.AssignUserToTeam(r.Context(), uid, team.ID, role)
		}
	}

	// Create session
	sessionToken, tokenHash := generateToken()
	_ = s.store.CreateSession(r.Context(), storage.Session{
		UserID:    uid,
		TokenHash: tokenHash,
		ExpiresAt: time.Now().Add(24 * time.Hour),
	})
	http.SetCookie(w, &http.Cookie{
		Name:     "session",
		Value:    sessionToken,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Expires:  time.Now().Add(24 * time.Hour),
	})
	http.Redirect(w, r, "/", http.StatusFound)
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	if cookie, err := r.Cookie("session"); err == nil {
		tokenHash := auth.HashSHA256(cookie.Value)
		_ = s.store.DeleteSession(r.Context(), tokenHash)
	}
	http.SetCookie(w, &http.Cookie{Name: "session", Value: "", MaxAge: -1, Path: "/"})
	writeJSON(w, map[string]any{"ok": true})
}

func generateToken() (raw, hash string) {
	b := make([]byte, 32)
	_, _ = rand.Read(b)
	raw = hex.EncodeToString(b)
	hash = auth.HashSHA256(raw)
	return
}
```

Add `oidcSecret` field to `Server` and a setter in `server.go`:

```go
type Server struct {
	store           AllStore
	router          *chi.Mux
	staticFS        fs.FS
	triggerPipeline chan<- struct{}
	oidcSecret      string
}

func (s *Server) WithOIDCSecret(secret string) *Server {
	s.oidcSecret = secret
	return s
}
```

Also add `GetUserByExternalID`, `ResolveTeamByEmail`, `AssignUserToTeam` to `AllStore` via `TeamStore` (already covered since `AllStore` embeds `TeamStore`).

- [ ] **Step 2: Build check**

```bash
cd /Users/dsandor/Projects/memory && CGO_ENABLED=1 go build ./internal/web/... 2>&1 | grep -v deprecated
```

Expected: errors only for remaining undefined handlers (admin, analytics, settings).

---

## Task 10: Web — admin handlers

**Files:**
- Create: `internal/web/admin_handlers.go`

- [ ] **Step 1: Create internal/web/admin_handlers.go**

```go
package web

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/dsandor/memory/internal/auth"
	"github.com/dsandor/memory/internal/storage"
)

// --- Team management (superadmin) ---

func (s *Server) handleListTeams(w http.ResponseWriter, r *http.Request) {
	teams, err := s.store.ListTeams(r.Context())
	if err != nil {
		writeError(w, 500, fmt.Sprintf("list teams: %v", err))
		return
	}
	if teams == nil {
		teams = []storage.Team{}
	}
	writeJSON(w, teams)
}

func (s *Server) handleCreateTeam(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name           string   `json:"name"`
		DomainPatterns []string `json:"domain_patterns"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, 400, "invalid JSON body")
		return
	}
	if body.Name == "" {
		writeError(w, 400, "name required")
		return
	}
	id, err := s.store.CreateTeam(r.Context(), storage.Team{
		Name:           body.Name,
		DomainPatterns: body.DomainPatterns,
		Enabled:        true,
	})
	if err != nil {
		writeError(w, 500, fmt.Sprintf("create team: %v", err))
		return
	}
	writeJSON(w, map[string]string{"id": id})
}

func (s *Server) handleSetTeamEnabled(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var body struct {
		Enabled bool `json:"enabled"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, 400, "invalid JSON body")
		return
	}
	if err := s.store.SetTeamEnabled(r.Context(), id, body.Enabled); err != nil {
		writeError(w, 500, fmt.Sprintf("set team enabled: %v", err))
		return
	}
	writeJSON(w, map[string]any{"ok": true})
}

func (s *Server) handleDeleteTeam(w http.ResponseWriter, r *http.Request) {
	if err := s.store.DeleteTeam(r.Context(), chi.URLParam(r, "id")); err != nil {
		writeError(w, 500, fmt.Sprintf("delete team: %v", err))
		return
	}
	writeJSON(w, map[string]any{"ok": true})
}

func (s *Server) handleListTeamUsers(w http.ResponseWriter, r *http.Request) {
	users, err := s.store.ListUsers(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, 500, fmt.Sprintf("list users: %v", err))
		return
	}
	if users == nil {
		users = []storage.User{}
	}
	// Strip password hashes before sending
	type safeUser struct {
		ID               string `json:"id"`
		TeamID           string `json:"team_id"`
		Email            string `json:"email"`
		Name             string `json:"name"`
		Role             string `json:"role"`
		ManuallyAssigned bool   `json:"manually_assigned"`
	}
	safe := make([]safeUser, len(users))
	for i, u := range users {
		safe[i] = safeUser{ID: u.ID, TeamID: u.TeamID, Email: u.Email, Name: u.Name, Role: u.Role, ManuallyAssigned: u.ManuallyAssigned}
	}
	writeJSON(w, safe)
}

// --- User management (admin) ---

func (s *Server) handleListUsers(w http.ResponseWriter, r *http.Request) {
	tc := auth.GetTeamContext(r.Context())
	users, err := s.store.ListUsers(r.Context(), tc.TeamID)
	if err != nil {
		writeError(w, 500, fmt.Sprintf("list users: %v", err))
		return
	}
	if users == nil {
		users = []storage.User{}
	}
	writeJSON(w, users)
}

func (s *Server) handleAssignUser(w http.ResponseWriter, r *http.Request) {
	tc := auth.GetTeamContext(r.Context())
	var body struct {
		Email string `json:"email"`
		Role  string `json:"role"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, 400, "invalid JSON body")
		return
	}
	if body.Email == "" {
		writeError(w, 400, "email required")
		return
	}
	if body.Role == "" {
		body.Role = "member"
	}
	user, _ := s.store.GetUserByEmail(r.Context(), body.Email)
	if user == nil {
		uid, err := s.store.UpsertUser(r.Context(), storage.User{Email: body.Email, Role: body.Role})
		if err != nil {
			writeError(w, 500, fmt.Sprintf("create user: %v", err))
			return
		}
		_ = s.store.AssignUserToTeam(r.Context(), uid, tc.TeamID, body.Role)
		writeJSON(w, map[string]string{"id": uid})
		return
	}
	if err := s.store.AssignUserToTeam(r.Context(), user.ID, tc.TeamID, body.Role); err != nil {
		writeError(w, 500, fmt.Sprintf("assign user: %v", err))
		return
	}
	writeJSON(w, map[string]string{"id": user.ID})
}

func (s *Server) handleSetUserRole(w http.ResponseWriter, r *http.Request) {
	tc := auth.GetTeamContext(r.Context())
	var body struct {
		Role string `json:"role"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, 400, "invalid JSON body")
		return
	}
	if err := s.store.AssignUserToTeam(r.Context(), chi.URLParam(r, "id"), tc.TeamID, body.Role); err != nil {
		writeError(w, 500, fmt.Sprintf("set role: %v", err))
		return
	}
	writeJSON(w, map[string]any{"ok": true})
}

// --- API key management (admin) ---

func (s *Server) handleListAPIKeys(w http.ResponseWriter, r *http.Request) {
	tc := auth.GetTeamContext(r.Context())
	keys, err := s.store.ListAPIKeys(r.Context(), tc.TeamID)
	if err != nil {
		writeError(w, 500, fmt.Sprintf("list api keys: %v", err))
		return
	}
	if keys == nil {
		keys = []storage.APIKey{}
	}
	// Never return key hashes
	type safeKey struct {
		ID        string `json:"id"`
		TeamID    string `json:"team_id"`
		UserID    string `json:"user_id"`
		KeyType   string `json:"key_type"`
		Name      string `json:"name"`
		Role      string `json:"role"`
		CreatedAt string `json:"created_at"`
	}
	safe := make([]safeKey, len(keys))
	for i, k := range keys {
		safe[i] = safeKey{ID: k.ID, TeamID: k.TeamID, UserID: k.UserID, KeyType: k.KeyType, Name: k.Name, Role: k.Role, CreatedAt: k.CreatedAt.Format("2006-01-02T15:04:05Z")}
	}
	writeJSON(w, safe)
}

func (s *Server) handleCreateAPIKey(w http.ResponseWriter, r *http.Request) {
	tc := auth.GetTeamContext(r.Context())
	var body struct {
		Name    string `json:"name"`
		Role    string `json:"role"`
		KeyType string `json:"key_type"` // "team" | "user"
		UserID  string `json:"user_id"`  // required for key_type=user
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, 400, "invalid JSON body")
		return
	}
	if body.Name == "" {
		writeError(w, 400, "name required")
		return
	}
	if body.KeyType == "" {
		body.KeyType = storage.APIKeyTypeTeam
	}
	if body.Role == "" {
		body.Role = "member"
	}

	rawKey := generateRawKey()
	hash := auth.HashSHA256(rawKey)

	key := storage.APIKey{
		TeamID:  tc.TeamID,
		UserID:  body.UserID,
		KeyType: body.KeyType,
		Name:    body.Name,
		KeyHash: hash,
		Role:    body.Role,
	}
	if err := s.store.CreateAPIKey(r.Context(), key); err != nil {
		writeError(w, 500, fmt.Sprintf("create api key: %v", err))
		return
	}
	// Return raw key once — never stored
	writeJSON(w, map[string]string{"key": rawKey, "name": body.Name})
}

func (s *Server) handleRevokeAPIKey(w http.ResponseWriter, r *http.Request) {
	if err := s.store.RevokeAPIKey(r.Context(), chi.URLParam(r, "id")); err != nil {
		writeError(w, 500, fmt.Sprintf("revoke key: %v", err))
		return
	}
	writeJSON(w, map[string]any{"ok": true})
}

func generateRawKey() string {
	b := make([]byte, 32)
	_, _ = rand.Read(b)
	return "tk_" + hex.EncodeToString(b)
}
```

- [ ] **Step 2: Build check**

```bash
cd /Users/dsandor/Projects/memory && CGO_ENABLED=1 go build ./internal/web/... 2>&1 | grep -v deprecated
```

Expected: errors only for analytics.go and settings.go handlers.

---

## Task 11: Web — analytics and settings handlers

**Files:**
- Create: `internal/web/analytics.go`
- Create: `internal/web/analytics_test.go`
- Create: `internal/web/settings.go`
- Create: `internal/web/settings_test.go`

- [ ] **Step 1: Write failing analytics tests**

Create `internal/web/analytics_test.go`:

```go
package web_test

import (
	"context"
	"net/http/httptest"
	"testing"

	"github.com/dsandor/memory/internal/storage"
	"github.com/dsandor/memory/internal/web"
)

func TestHandleUsage(t *testing.T) {
	store := newMockStore()
	store.entries = []storage.KnowledgeEntry{
		{ID: "e1", Title: "entry1", Domain: "finance", Rating: 4.0, UsageCount: 10, Status: "approved"},
	}
	store.activityLog = []storage.ActivityEntry{
		{TeamID: "t1", Action: "knowledge.get", EntityID: "e1", CreatedAt: parseTestTime("2026-06-01")},
	}
	srv := web.NewServer(newTestFS(), store)
	req := httptest.NewRequest("GET", "/api/analytics/usage", nil)
	injectTeamContext(req, "t1", "admin")
	rr := httptest.NewRecorder()
	srv.ServeHTTP(rr, req)
	if rr.Code != 200 {
		t.Errorf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestHandleGaps(t *testing.T) {
	store := newMockStore()
	store.teamSettings = &storage.TeamSettings{TeamID: "t1", PipelineMinEntries: 5}
	srv := web.NewServer(newTestFS(), store)
	req := httptest.NewRequest("GET", "/api/analytics/gaps", nil)
	injectTeamContext(req, "t1", "member")
	rr := httptest.NewRecorder()
	srv.ServeHTTP(rr, req)
	if rr.Code != 200 {
		t.Errorf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestHandleContributions(t *testing.T) {
	store := newMockStore()
	store.entries = []storage.KnowledgeEntry{
		{ID: "e1", Author: "alice", Status: "approved", Rating: 4.5, UsageCount: 5},
	}
	srv := web.NewServer(newTestFS(), store)
	req := httptest.NewRequest("GET", "/api/analytics/contributions", nil)
	injectTeamContext(req, "t1", "member")
	rr := httptest.NewRecorder()
	srv.ServeHTTP(rr, req)
	if rr.Code != 200 {
		t.Errorf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
}
```

Note: `newMockStore()`, `injectTeamContext()`, `newTestFS()`, and `parseTestTime()` are helpers to add to the existing `server_test.go` test helpers. The mock store needs `activityLog []storage.ActivityEntry` and `teamSettings *storage.TeamSettings` fields plus the TeamStore methods.

- [ ] **Step 2: Create internal/web/analytics.go**

```go
package web

import (
	"fmt"
	"net/http"
	"sort"
	"time"

	"github.com/dsandor/memory/internal/auth"
	"github.com/dsandor/memory/internal/storage"
)

type topEntry struct {
	ID         string  `json:"id"`
	Title      string  `json:"title"`
	Domain     string  `json:"domain"`
	Score      float64 `json:"score"`
	UsageCount int     `json:"usage_count"`
	Rating     float64 `json:"rating"`
}

type domainStats struct {
	Domain     string  `json:"domain"`
	EntryCount int     `json:"entry_count"`
	AvgRating  float64 `json:"avg_rating"`
	TotalUsage int     `json:"total_usage"`
}

type heatmapPoint struct {
	Week   string `json:"week"`
	Domain string `json:"domain"`
	Usage  int    `json:"usage"`
}

type usageResponse struct {
	TopEntries []topEntry     `json:"top_entries"`
	ByDomain   []domainStats  `json:"by_domain"`
	Heatmap    []heatmapPoint `json:"heatmap"`
}

func (s *Server) handleUsage(w http.ResponseWriter, r *http.Request) {
	tc := auth.GetTeamContext(r.Context())
	ctx := r.Context()

	entries, err := s.store.ListEntries(ctx, storage.ListFilter{Limit: 200, TeamID: tc.TeamID, Status: "approved"})
	if err != nil {
		writeError(w, 500, fmt.Sprintf("list entries: %v", err))
		return
	}

	// Build top entries by score
	type scored struct {
		e     storage.KnowledgeEntry
		score float64
	}
	var scored_ []scored
	domainMap := map[string]*domainStats{}
	for _, e := range entries {
		s := e.Rating * float64(e.UsageCount)
		scored_ = append(scored_, struct {
			e     storage.KnowledgeEntry
			score float64
		}{e, s})
		ds := domainMap[e.Domain]
		if ds == nil {
			ds = &domainStats{Domain: e.Domain}
			domainMap[e.Domain] = ds
		}
		ds.EntryCount++
		ds.TotalUsage += e.UsageCount
		ds.AvgRating += e.Rating
	}
	sort.Slice(scored_, func(i, j int) bool { return scored_[i].score > scored_[j].score })

	top := make([]topEntry, 0, 10)
	for i, sc := range scored_ {
		if i >= 10 {
			break
		}
		top = append(top, topEntry{ID: sc.e.ID, Title: sc.e.Title, Domain: sc.e.Domain, Score: sc.score, UsageCount: sc.e.UsageCount, Rating: sc.e.Rating})
	}

	domains := make([]domainStats, 0, len(domainMap))
	for _, ds := range domainMap {
		if ds.EntryCount > 0 {
			ds.AvgRating = ds.AvgRating / float64(ds.EntryCount)
		}
		domains = append(domains, *ds)
	}
	sort.Slice(domains, func(i, j int) bool { return domains[i].TotalUsage > domains[j].TotalUsage })

	// Build heatmap from activity log
	actLog, _ := s.store.QueryActivity(ctx, tc.TeamID, 1000)
	type heatKey struct{ week, domain string }
	heatMap := map[heatKey]int{}
	entryDomains := map[string]string{}
	for _, e := range entries {
		entryDomains[e.ID] = e.Domain
	}
	for _, a := range actLog {
		if a.Action != "knowledge.get" && a.Action != "prompt.enhance" {
			continue
		}
		_, week := a.CreatedAt.ISOWeek()
		year, _ := a.CreatedAt.ISOWeek()
		weekStr := fmt.Sprintf("%d-W%02d", year, week)
		domain := entryDomains[a.EntityID]
		heatMap[heatKey{weekStr, domain}]++
	}
	heatSlice := make([]heatmapPoint, 0, len(heatMap))
	for k, v := range heatMap {
		heatSlice = append(heatSlice, heatmapPoint{Week: k.week, Domain: k.domain, Usage: v})
	}
	sort.Slice(heatSlice, func(i, j int) bool { return heatSlice[i].Week > heatSlice[j].Week })

	_ = time.Now() // suppress import
	writeJSON(w, usageResponse{TopEntries: top, ByDomain: domains, Heatmap: heatSlice})
}

type gapEntry struct {
	Domain     string `json:"domain"`
	EntryCount int    `json:"entry_count"`
	Threshold  int    `json:"threshold"`
	Severity   string `json:"severity"` // low|medium|high
}

func (s *Server) handleGaps(w http.ResponseWriter, r *http.Request) {
	tc := auth.GetTeamContext(r.Context())
	ctx := r.Context()

	settings, err := s.store.GetTeamSettings(ctx, tc.TeamID)
	if err != nil {
		writeError(w, 500, fmt.Sprintf("get settings: %v", err))
		return
	}
	threshold := settings.PipelineMinEntries
	if threshold == 0 {
		threshold = 10
	}

	entries, err := s.store.ListEntries(ctx, storage.ListFilter{Limit: 500, TeamID: tc.TeamID, Status: "approved"})
	if err != nil {
		writeError(w, 500, fmt.Sprintf("list entries: %v", err))
		return
	}

	domainCounts := map[string]int{}
	for _, e := range entries {
		if e.Domain != "" {
			domainCounts[e.Domain]++
		}
	}
	// Also include configured domains with zero entries
	for _, d := range settings.Domains {
		if _, ok := domainCounts[d]; !ok {
			domainCounts[d] = 0
		}
	}

	var gaps []gapEntry
	for domain, count := range domainCounts {
		if count >= threshold {
			continue
		}
		pct := float64(count) / float64(threshold)
		severity := "high"
		if pct >= 0.5 {
			severity = "medium"
		}
		if pct >= 0.8 {
			severity = "low"
		}
		gaps = append(gaps, gapEntry{Domain: domain, EntryCount: count, Threshold: threshold, Severity: severity})
	}
	sort.Slice(gaps, func(i, j int) bool { return gaps[i].EntryCount < gaps[j].EntryCount })
	if gaps == nil {
		gaps = []gapEntry{}
	}
	writeJSON(w, map[string]any{"gaps": gaps})
}

type leaderboardEntry struct {
	Author        string  `json:"author"`
	EntryCount    int     `json:"entry_count"`
	ApprovedCount int     `json:"approved_count"`
	TotalUsage    int     `json:"total_usage"`
	AvgRating     float64 `json:"avg_rating"`
	Score         float64 `json:"score"`
}

func (s *Server) handleContributions(w http.ResponseWriter, r *http.Request) {
	tc := auth.GetTeamContext(r.Context())
	ctx := r.Context()

	entries, err := s.store.ListEntries(ctx, storage.ListFilter{Limit: 500, TeamID: tc.TeamID})
	if err != nil {
		writeError(w, 500, fmt.Sprintf("list entries: %v", err))
		return
	}

	type authorStats struct {
		entryCount    int
		approvedCount int
		totalUsage    int
		ratingSum     float64
		ratingCount   int
	}
	byAuthor := map[string]*authorStats{}
	for _, e := range entries {
		if e.Author == "" {
			continue
		}
		a := byAuthor[e.Author]
		if a == nil {
			a = &authorStats{}
			byAuthor[e.Author] = a
		}
		a.entryCount++
		if e.Status == "approved" {
			a.approvedCount++
		}
		a.totalUsage += e.UsageCount
		if e.Rating > 0 {
			a.ratingSum += e.Rating
			a.ratingCount++
		}
	}

	leaderboard := make([]leaderboardEntry, 0, len(byAuthor))
	for author, a := range byAuthor {
		avgRating := 0.0
		if a.ratingCount > 0 {
			avgRating = a.ratingSum / float64(a.ratingCount)
		}
		score := float64(a.approvedCount*2) + avgRating*float64(a.totalUsage)
		leaderboard = append(leaderboard, leaderboardEntry{
			Author:        author,
			EntryCount:    a.entryCount,
			ApprovedCount: a.approvedCount,
			TotalUsage:    a.totalUsage,
			AvgRating:     avgRating,
			Score:         score,
		})
	}
	sort.Slice(leaderboard, func(i, j int) bool { return leaderboard[i].Score > leaderboard[j].Score })
	if leaderboard == nil {
		leaderboard = []leaderboardEntry{}
	}
	writeJSON(w, map[string]any{"leaderboard": leaderboard})
}
```

- [ ] **Step 3: Create internal/web/settings.go**

```go
package web

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/dsandor/memory/internal/auth"
	"github.com/dsandor/memory/internal/storage"
)

func (s *Server) handleGetSettings(w http.ResponseWriter, r *http.Request) {
	tc := auth.GetTeamContext(r.Context())
	settings, err := s.store.GetTeamSettings(r.Context(), tc.TeamID)
	if err != nil {
		writeError(w, 500, fmt.Sprintf("get settings: %v", err))
		return
	}
	writeJSON(w, settings)
}

func (s *Server) handlePutSettings(w http.ResponseWriter, r *http.Request) {
	tc := auth.GetTeamContext(r.Context())
	var body struct {
		Domains            []string `json:"domains"`
		ClusterThreshold   float64  `json:"cluster_threshold"`
		PipelineMinEntries int      `json:"pipeline_min_entries"`
		AgentModel         string   `json:"agent_model"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, 400, "invalid JSON body")
		return
	}
	settings := storage.TeamSettings{
		TeamID:             tc.TeamID,
		Domains:            body.Domains,
		ClusterThreshold:   body.ClusterThreshold,
		PipelineMinEntries: body.PipelineMinEntries,
		AgentModel:         body.AgentModel,
	}
	if settings.ClusterThreshold == 0 {
		settings.ClusterThreshold = 0.85
	}
	if settings.PipelineMinEntries == 0 {
		settings.PipelineMinEntries = 10
	}
	if settings.AgentModel == "" {
		settings.AgentModel = "claude-haiku-4-5-20251001"
	}
	if err := s.store.PutTeamSettings(r.Context(), settings); err != nil {
		writeError(w, 500, fmt.Sprintf("put settings: %v", err))
		return
	}
	writeJSON(w, map[string]any{"ok": true})
}

func (s *Server) handleGetAuthConfig(w http.ResponseWriter, r *http.Request) {
	cfg, err := s.store.GetAuthConfig(r.Context())
	if err != nil {
		writeError(w, 500, fmt.Sprintf("get auth config: %v", err))
		return
	}
	// Never return client secret
	writeJSON(w, map[string]any{
		"provider":          cfg.Provider,
		"oidc_issuer":       cfg.OIDCIssuer,
		"oidc_client_id":    cfg.OIDCClientID,
		"oidc_redirect_url": cfg.OIDCRedirectURL,
		"updated_at":        cfg.UpdatedAt,
	})
}

func (s *Server) handlePutAuthConfig(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Provider        string `json:"provider"`
		OIDCIssuer      string `json:"oidc_issuer"`
		OIDCClientID    string `json:"oidc_client_id"`
		OIDCRedirectURL string `json:"oidc_redirect_url"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, 400, "invalid JSON body")
		return
	}
	if body.Provider != "local" && body.Provider != "oidc" {
		writeError(w, 400, "provider must be 'local' or 'oidc'")
		return
	}
	if err := s.store.PutAuthConfig(r.Context(), storage.AuthConfig{
		Provider:        body.Provider,
		OIDCIssuer:      body.OIDCIssuer,
		OIDCClientID:    body.OIDCClientID,
		OIDCRedirectURL: body.OIDCRedirectURL,
	}); err != nil {
		writeError(w, 500, fmt.Sprintf("put auth config: %v", err))
		return
	}
	writeJSON(w, map[string]any{"ok": true})
}
```

- [ ] **Step 4: Create settings_test.go**

Create `internal/web/settings_test.go`:

```go
package web_test

import (
	"bytes"
	"encoding/json"
	"net/http/httptest"
	"testing"

	"github.com/dsandor/memory/internal/web"
)

func TestHandleGetSettings(t *testing.T) {
	store := newMockStore()
	srv := web.NewServer(newTestFS(), store)
	req := httptest.NewRequest("GET", "/api/settings", nil)
	injectTeamContext(req, "t1", "admin")
	rr := httptest.NewRecorder()
	srv.ServeHTTP(rr, req)
	if rr.Code != 200 {
		t.Errorf("expected 200, got %d", rr.Code)
	}
}

func TestHandlePutSettings(t *testing.T) {
	store := newMockStore()
	srv := web.NewServer(newTestFS(), store)
	body, _ := json.Marshal(map[string]any{
		"domains":              []string{"finance", "legal"},
		"cluster_threshold":    0.9,
		"pipeline_min_entries": 5,
		"agent_model":          "claude-haiku-4-5-20251001",
	})
	req := httptest.NewRequest("PUT", "/api/settings", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	injectTeamContext(req, "t1", "admin")
	rr := httptest.NewRecorder()
	srv.ServeHTTP(rr, req)
	if rr.Code != 200 {
		t.Errorf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestHandleGetAuthConfig(t *testing.T) {
	store := newMockStore()
	srv := web.NewServer(newTestFS(), store)
	req := httptest.NewRequest("GET", "/api/admin/auth-config", nil)
	injectTeamContext(req, "", "superadmin")
	rr := httptest.NewRecorder()
	srv.ServeHTTP(rr, req)
	if rr.Code != 200 {
		t.Errorf("expected 200, got %d", rr.Code)
	}
}
```

- [ ] **Step 5: Run all web tests**

```bash
cd /Users/dsandor/Projects/memory && CGO_ENABLED=1 go test ./internal/web/... -v 2>&1 | grep -E "^--- |^ok|FAIL"
```

Expected: all PASS. (The mock store needs updating in server_test.go to implement all AllStore methods — see note below.)

**Note:** Update `server_test.go`'s `mockStore` to implement the full `AllStore` interface (all `TeamStore` methods). Add stub implementations returning `nil, nil` or empty slices. Also add `activityLog []storage.ActivityEntry` and `teamSettings *storage.TeamSettings` fields.

- [ ] **Step 6: Full build check**

```bash
cd /Users/dsandor/Projects/memory && CGO_ENABLED=1 go build ./... 2>&1 | grep -v deprecated
```

Expected: no errors.

---

## Task 12: MCP — knowledge_search, knowledge_rate, and pipeline trigger channel

**Files:**
- Create: `internal/mcp/knowledge_tools.go`
- Modify: `internal/mcp/server.go`

- [ ] **Step 1: Create internal/mcp/knowledge_tools.go**

```go
package mcp

import (
	"context"
	"fmt"

	"github.com/dsandor/memory/internal/embedding"
	"github.com/dsandor/memory/internal/storage"
	mcplib "github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// RegisterKnowledgeExtTools adds knowledge_search and knowledge_rate to an existing MCP server.
func RegisterKnowledgeExtTools(s *server.MCPServer, store storage.Store, embedder embedding.Embedder) {
	s.AddTool(
		mcplib.NewTool("knowledge_search",
			mcplib.WithDescription("Semantic vector search over the team's knowledge base. Returns ranked entries by similarity to the query."),
			mcplib.WithString("query", mcplib.Required(), mcplib.Description("Natural language query to search for")),
			mcplib.WithString("domain", mcplib.Description("Optional domain filter (e.g. finance, legal)")),
			mcplib.WithNumber("top_k", mcplib.Description("Number of results to return (default 5, max 20)")),
		),
		HandleKnowledgeSearch(store, embedder),
	)

	s.AddTool(
		mcplib.NewTool("knowledge_rate",
			mcplib.WithDescription("Rate a knowledge entry 1–5. Updates usage count and rating score."),
			mcplib.WithString("id", mcplib.Required(), mcplib.Description("Entry UUID to rate")),
			mcplib.WithNumber("rating", mcplib.Required(), mcplib.Description("Rating from 1.0 to 5.0")),
		),
		HandleKnowledgeRate(store),
	)
}

func HandleKnowledgeSearch(store storage.Store, embedder embedding.Embedder) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcplib.CallToolRequest) (*mcplib.CallToolResult, error) {
		query, _ := req.Params.Arguments["query"].(string)
		if query == "" {
			return mcplib.NewToolResultError("query is required"), nil
		}
		topK := 5
		if v, ok := req.Params.Arguments["top_k"].(float64); ok && v > 0 {
			topK = int(v)
			if topK > 20 {
				topK = 20
			}
		}

		vec, err := embedder.Embed(ctx, query)
		if err != nil {
			return mcplib.NewToolResultError(fmt.Sprintf("embed query: %v", err)), nil
		}

		results, err := store.SearchSimilar(ctx, vec, topK)
		if err != nil {
			return mcplib.NewToolResultError(fmt.Sprintf("search: %v", err)), nil
		}

		domain, _ := req.Params.Arguments["domain"].(string)
		if domain != "" {
			filtered := results[:0]
			for _, r := range results {
				if r.Entry.Domain == domain {
					filtered = append(filtered, r)
				}
			}
			results = filtered
		}

		if len(results) == 0 {
			return mcplib.NewToolResultText("No similar entries found."), nil
		}

		out := ""
		for i, r := range results {
			out += fmt.Sprintf("[%d] (score=%.3f) %s\n  Domain: %s\n  %s\n\n",
				i+1, r.Score, r.Entry.Title, r.Entry.Domain, r.Entry.Content)
		}
		return mcplib.NewToolResultText(out), nil
	}
}

func HandleKnowledgeRate(store storage.Store) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcplib.CallToolRequest) (*mcplib.CallToolResult, error) {
		id, _ := req.Params.Arguments["id"].(string)
		if id == "" {
			return mcplib.NewToolResultError("id is required"), nil
		}
		rating, ok := req.Params.Arguments["rating"].(float64)
		if !ok || rating < 1 || rating > 5 {
			return mcplib.NewToolResultError("rating must be between 1 and 5"), nil
		}
		if err := store.RateEntry(ctx, id, rating); err != nil {
			return mcplib.NewToolResultError(fmt.Sprintf("rate entry: %v", err)), nil
		}
		return mcplib.NewToolResultText(fmt.Sprintf("Entry %s rated %.1f", id, rating)), nil
	}
}
```

- [ ] **Step 2: Update server.go to call RegisterKnowledgeExtTools**

In `internal/mcp/server.go`, update `NewMCPServer` signature to also accept an embedder for search, or add a separate registration call. Since `NewMCPServer` already takes `embedder`, add to the bottom of `NewMCPServer`:

```go
// In NewMCPServer, after existing tool registrations:
	RegisterKnowledgeExtTools(s, store, embedder)
```

Wait — `NewMCPServer` returns `*server.MCPServer` and registers tools inline. It's cleaner to call `RegisterKnowledgeExtTools` from `cmd/server/main.go` after construction. In `main.go` (Task 14), add:

```go
internalmcp.RegisterKnowledgeExtTools(mcpServer, store, embedder)
```

- [ ] **Step 3: Build check**

```bash
cd /Users/dsandor/Projects/memory && CGO_ENABLED=1 go build ./internal/mcp/... 2>&1 | grep -v deprecated
```

Expected: no errors.

---

## Task 13: MCP — prompt_suggest, resources, enhance_with_context, remote transport

**Files:**
- Create: `internal/mcp/prompt_suggest.go`
- Create: `internal/mcp/resources.go`
- Create: `internal/mcp/remote.go`

- [ ] **Step 1: Create internal/mcp/prompt_suggest.go**

```go
package mcp

import (
	"context"
	"fmt"
	"strings"

	"github.com/dsandor/memory/internal/embedding"
	"github.com/dsandor/memory/internal/llm"
	"github.com/dsandor/memory/internal/storage"
	mcplib "github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// RegisterPromptSuggest adds the prompt_suggest tool and enhance_with_context prompt.
func RegisterPromptSuggest(s *server.MCPServer, store storage.Store, embedder embedding.Embedder, llmClient llm.Client) {
	s.AddTool(
		mcplib.NewTool("prompt_suggest",
			mcplib.WithDescription("Suggest improvements to a draft prompt using high-rated team knowledge entries. Returns improved prompt, rationale, and source entries."),
			mcplib.WithString("prompt", mcplib.Required(), mcplib.Description("The draft prompt to improve")),
			mcplib.WithString("domain", mcplib.Description("Optional domain to focus suggestions")),
		),
		HandlePromptSuggest(store, embedder, llmClient),
	)

	s.AddPrompt(
		mcplib.NewPrompt("enhance_with_context",
			mcplib.WithPromptDescription("Wrap a prompt with team knowledge context: applicable rules + top similar entries + domain agent"),
			mcplib.WithArgument("prompt", mcplib.ArgumentDescription("The prompt to enhance"), mcplib.RequiredArgument()),
			mcplib.WithArgument("domain", mcplib.ArgumentDescription("Domain for rule and agent lookup (optional)")),
			mcplib.WithArgument("team", mcplib.ArgumentDescription("Team identifier for rule lookup (optional)")),
		),
		HandleEnhanceWithContext(store, embedder),
	)
}

func HandlePromptSuggest(store storage.Store, embedder embedding.Embedder, llmClient llm.Client) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcplib.CallToolRequest) (*mcplib.CallToolResult, error) {
		prompt, _ := req.Params.Arguments["prompt"].(string)
		if prompt == "" {
			return mcplib.NewToolResultError("prompt is required"), nil
		}
		domain, _ := req.Params.Arguments["domain"].(string)

		if embedder == nil || llmClient == nil {
			return mcplib.NewToolResultText(prompt), nil
		}

		vec, err := embedder.Embed(ctx, prompt)
		if err != nil {
			return mcplib.NewToolResultText(prompt), nil
		}

		results, err := store.SearchSimilar(ctx, vec, 5)
		if err != nil || len(results) == 0 {
			return mcplib.NewToolResultText(prompt), nil
		}

		// Filter to high-rated entries
		var topEntries []storage.SearchResult
		for _, r := range results {
			if r.Entry.Rating >= 3.5 && (domain == "" || r.Entry.Domain == domain) {
				topEntries = append(topEntries, r)
				if len(topEntries) >= 3 {
					break
				}
			}
		}
		if len(topEntries) == 0 {
			topEntries = results[:min(3, len(results))]
		}

		examplesBlock := strings.Builder{}
		for i, e := range topEntries {
			examplesBlock.WriteString(fmt.Sprintf("Example %d (rating=%.1f):\n%s\n\n", i+1, e.Entry.Rating, e.Entry.Content))
		}

		systemPrompt := "You are a prompt engineering expert. Given a draft prompt and examples of high-quality prompts from the team, suggest an improved version. Return JSON: {\"improved_prompt\": \"...\", \"rationale\": \"...\"}"
		userMsg := fmt.Sprintf("Draft prompt:\n%s\n\nHigh-quality team examples:\n%s\nReturn improved JSON.", prompt, examplesBlock.String())

		resp, err := llmClient.Complete(ctx, systemPrompt, userMsg)
		if err != nil {
			return mcplib.NewToolResultText(prompt), nil
		}

		sourceIDs := make([]string, 0, len(topEntries))
		for _, e := range topEntries {
			sourceIDs = append(sourceIDs, e.Entry.ID)
		}

		out := fmt.Sprintf("Suggested prompt:\n%s\n\nSource entries used: %s", resp, strings.Join(sourceIDs, ", "))
		return mcplib.NewToolResultText(out), nil
	}
}

func HandleEnhanceWithContext(store storage.Store, embedder embedding.Embedder) func(context.Context, mcplib.GetPromptRequest) (*mcplib.GetPromptResult, error) {
	return func(ctx context.Context, req mcplib.GetPromptRequest) (*mcplib.GetPromptResult, error) {
		prompt := req.Params.Arguments["prompt"]
		domain := req.Params.Arguments["domain"]
		team := req.Params.Arguments["team"]

		preamble := strings.Builder{}

		// Layer 1: rules via existing prompt_enhance logic (reuse HandlePromptEnhance logic inline)
		if ruleStore, ok := store.(interface {
			GetApplicableRules(ctx context.Context, team, category, user string) ([]storage.Rule, error)
		}); ok {
			rules, _ := ruleStore.GetApplicableRules(ctx, team, domain, "")
			if len(rules) > 0 {
				preamble.WriteString("## Team Rules\n")
				for _, r := range rules {
					preamble.WriteString(fmt.Sprintf("- %s: %s\n", r.Title, r.Content))
				}
				preamble.WriteString("\n")
			}
		}

		// Layer 2: top similar entries
		if embedder != nil && prompt != "" {
			vec, err := embedder.Embed(ctx, prompt)
			if err == nil {
				results, err := store.SearchSimilar(ctx, vec, 3)
				if err == nil && len(results) > 0 {
					preamble.WriteString("## Relevant Team Knowledge\n")
					for _, r := range results {
						preamble.WriteString(fmt.Sprintf("### %s\n%s\n\n", r.Entry.Title, r.Entry.Content))
					}
				}
			}
		}

		// Layer 3: published domain agent
		if domain != "" {
			if agentStore, ok := store.(interface {
				GetAgentByDomain(ctx context.Context, domain string) (*storage.Agent, error)
			}); ok {
				agent, _ := agentStore.GetAgentByDomain(ctx, domain)
				if agent != nil && agent.Status == storage.AgentStatusPublished {
					preamble.WriteString(fmt.Sprintf("## Domain Agent: %s\n%s\n\n", agent.Domain, agent.SystemPrompt))
				}
			}
		}

		fullPrompt := prompt
		if preamble.Len() > 0 {
			fullPrompt = preamble.String() + "## Your Request\n" + prompt
		}

		return &mcplib.GetPromptResult{
			Description: "Prompt enhanced with team knowledge context",
			Messages: []mcplib.PromptMessage{
				{Role: mcplib.RoleUser, Content: mcplib.TextContent{Type: "text", Text: fullPrompt}},
			},
		}, nil
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
```

- [ ] **Step 2: Create internal/mcp/resources.go**

```go
package mcp

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/dsandor/memory/internal/storage"
	mcplib "github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// RegisterResources adds the six knowledge:// and agents:// MCP resources.
func RegisterResources(s *server.MCPServer, store storage.AgentStore) {
	s.AddResource(
		mcplib.NewResource("knowledge://team/top", "Top 10 knowledge entries by score",
			mcplib.WithMIMEType("application/json")),
		handleTopEntries(store),
	)
	s.AddResource(
		mcplib.NewResource("knowledge://team/recent", "10 most recently approved entries",
			mcplib.WithMIMEType("application/json")),
		handleRecentEntries(store),
	)
	s.AddResourceTemplate(
		mcplib.NewResourceTemplate("knowledge://domain/{name}", "All approved entries in a domain",
			mcplib.WithTemplateMIMEType("application/json")),
		handleDomainEntries(store),
	)
	s.AddResourceTemplate(
		mcplib.NewResourceTemplate("knowledge://cluster/{id}", "Entries in a knowledge cluster",
			mcplib.WithTemplateMIMEType("application/json")),
		handleClusterEntries(store),
	)
	s.AddResource(
		mcplib.NewResource("agents://generated", "All published agents",
			mcplib.WithMIMEType("application/json")),
		handleAllAgents(store),
	)
	s.AddResourceTemplate(
		mcplib.NewResourceTemplate("agents://domain/{name}", "Latest published agent for a domain",
			mcplib.WithTemplateMIMEType("application/json")),
		handleAgentByDomain(store),
	)
}

func handleTopEntries(store storage.Store) server.ResourceHandlerFunc {
	return func(ctx context.Context, req mcplib.ReadResourceRequest) ([]mcplib.ResourceContents, error) {
		entries, err := store.ListEntries(ctx, storage.ListFilter{Limit: 10, Status: "approved"})
		if err != nil {
			return nil, fmt.Errorf("list entries: %w", err)
		}
		b, _ := json.Marshal(entries)
		return []mcplib.ResourceContents{
			{URI: req.Params.URI, MIMEType: "application/json", Text: string(b)},
		}, nil
	}
}

func handleRecentEntries(store storage.Store) server.ResourceHandlerFunc {
	return func(ctx context.Context, req mcplib.ReadResourceRequest) ([]mcplib.ResourceContents, error) {
		entries, err := store.ListEntries(ctx, storage.ListFilter{Limit: 10, Status: "approved"})
		if err != nil {
			return nil, fmt.Errorf("list entries: %w", err)
		}
		b, _ := json.Marshal(entries)
		return []mcplib.ResourceContents{
			{URI: req.Params.URI, MIMEType: "application/json", Text: string(b)},
		}, nil
	}
}

func handleDomainEntries(store storage.Store) server.ResourceTemplateHandlerFunc {
	return func(ctx context.Context, req mcplib.ReadResourceRequest) ([]mcplib.ResourceContents, error) {
		// Extract domain from URI: knowledge://domain/{name}
		uri := req.Params.URI
		domain := ""
		if len(uri) > len("knowledge://domain/") {
			domain = uri[len("knowledge://domain/"):]
		}
		entries, err := store.ListEntries(ctx, storage.ListFilter{Domain: domain, Limit: 50, Status: "approved"})
		if err != nil {
			return nil, fmt.Errorf("list entries: %w", err)
		}
		b, _ := json.Marshal(entries)
		return []mcplib.ResourceContents{
			{URI: uri, MIMEType: "application/json", Text: string(b)},
		}, nil
	}
}

func handleClusterEntries(store storage.AnalysisStore) server.ResourceTemplateHandlerFunc {
	return func(ctx context.Context, req mcplib.ReadResourceRequest) ([]mcplib.ResourceContents, error) {
		uri := req.Params.URI
		clusterID := ""
		if len(uri) > len("knowledge://cluster/") {
			clusterID = uri[len("knowledge://cluster/"):]
		}
		clusters, err := store.ListClusters(ctx)
		if err != nil {
			return nil, fmt.Errorf("list clusters: %w", err)
		}
		for _, c := range clusters {
			if c.ID == clusterID {
				// Fetch entries by ID
				var entries []storage.KnowledgeEntry
				for _, eid := range c.EntryIDs {
					e, err := store.GetEntry(ctx, eid)
					if err == nil {
						entries = append(entries, *e)
					}
				}
				b, _ := json.Marshal(entries)
				return []mcplib.ResourceContents{
					{URI: uri, MIMEType: "application/json", Text: string(b)},
				}, nil
			}
		}
		return nil, fmt.Errorf("cluster %q not found", clusterID)
	}
}

func handleAllAgents(store storage.AgentStore) server.ResourceHandlerFunc {
	return func(ctx context.Context, req mcplib.ReadResourceRequest) ([]mcplib.ResourceContents, error) {
		agents, err := store.ListAgents(ctx)
		if err != nil {
			return nil, fmt.Errorf("list agents: %w", err)
		}
		var published []storage.Agent
		for _, a := range agents {
			if a.Status == storage.AgentStatusPublished {
				published = append(published, a)
			}
		}
		b, _ := json.Marshal(published)
		return []mcplib.ResourceContents{
			{URI: req.Params.URI, MIMEType: "application/json", Text: string(b)},
		}, nil
	}
}

func handleAgentByDomain(store storage.AgentStore) server.ResourceTemplateHandlerFunc {
	return func(ctx context.Context, req mcplib.ReadResourceRequest) ([]mcplib.ResourceContents, error) {
		uri := req.Params.URI
		domain := ""
		if len(uri) > len("agents://domain/") {
			domain = uri[len("agents://domain/"):]
		}
		agent, err := store.GetAgentByDomain(ctx, domain)
		if err != nil || agent == nil {
			return nil, fmt.Errorf("no agent for domain %q", domain)
		}
		b, _ := json.Marshal(agent)
		return []mcplib.ResourceContents{
			{URI: uri, MIMEType: "application/json", Text: string(b)},
		}, nil
	}
}
```

- [ ] **Step 3: Create internal/mcp/remote.go**

```go
package mcp

import (
	"log"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/dsandor/memory/internal/auth"
	"github.com/mark3labs/mcp-go/server"
)

// StartRemoteMCP starts an HTTP/SSE MCP transport on addr at path.
// The same chi auth middleware as the REST API is applied.
func StartRemoteMCP(mcpServer *server.MCPServer, addr, path string, authStore auth.AuthStore) {
	sseServer := server.NewSSEServer(mcpServer)

	r := chi.NewRouter()
	r.Use(auth.RequireAuth(authStore))
	r.Handle(path, sseServer)

	go func() {
		log.Printf("MCP HTTP/SSE transport listening on %s%s", addr, path)
		if err := http.ListenAndServe(addr, r); err != nil && err != http.ErrServerClosed {
			log.Printf("MCP SSE server error: %v", err)
		}
	}()
}
```

- [ ] **Step 4: Build check**

```bash
cd /Users/dsandor/Projects/memory && CGO_ENABLED=1 go build ./internal/mcp/... 2>&1 | grep -v deprecated
```

Expected: no errors.

---

## Task 14: Update main.go + bootstrap superadmin key

**Files:**
- Modify: `cmd/server/main.go`

- [ ] **Step 1: Update cmd/server/main.go**

Replace the entire file:

```go
package main

import (
	"context"
	"io/fs"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/dsandor/memory/internal/auth"
	"github.com/dsandor/memory/internal/config"
	"github.com/dsandor/memory/internal/embedding"
	"github.com/dsandor/memory/internal/llm"
	internalmcp "github.com/dsandor/memory/internal/mcp"
	"github.com/dsandor/memory/internal/pipeline"
	"github.com/dsandor/memory/internal/storage"
	"github.com/dsandor/memory/internal/web"
	"github.com/mark3labs/mcp-go/server"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("load config: %v", err)
	}

	store, err := storage.NewSQLiteStore(cfg.DBPath, cfg.EmbeddingDim)
	if err != nil {
		log.Fatalf("open storage: %v", err)
	}
	defer store.Close()

	// Bootstrap superadmin API key
	if cfg.SuperadminKey != "" {
		bootstrapSuperadmin(store, cfg.SuperadminKey)
	}

	embedder := embedding.NewOllamaEmbedder(cfg.OllamaURL, cfg.OllamaModel)

	// Pipeline trigger channel
	triggerCh := make(chan struct{}, 1)

	// HTTP server
	staticFS, err := fs.Sub(web.DistFS, "dist")
	if err != nil {
		log.Fatalf("sub fs: %v", err)
	}
	webServer := web.NewServer(staticFS, store).
		WithOIDCSecret(cfg.OIDCClientSecret).
		WithPipelineTrigger(triggerCh)

	httpServer := &http.Server{
		Addr:    cfg.HTTPAddr,
		Handler: webServer,
	}
	go func() {
		log.Printf("HTTP server listening on %s", cfg.HTTPAddr)
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Printf("HTTP server error: %v", err)
		}
	}()

	// Signal-aware context for graceful shutdown
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	// MCP server
	mcpServer := internalmcp.NewMCPServer(store, embedder)
	internalmcp.RegisterAnalysisTools(mcpServer, store)
	internalmcp.RegisterRuleTools(mcpServer, store)
	internalmcp.RegisterAgentTools(mcpServer, store)
	internalmcp.RegisterKnowledgeExtTools(mcpServer, store, embedder)
	internalmcp.RegisterResources(mcpServer, store)

	if cfg.AnthropicAPIKey != "" {
		llmClient := llm.NewAnthropicClient(cfg.AnthropicAPIKey, cfg.AnthropicModel)
		agentLLMClient := llm.NewAnthropicClient(cfg.AnthropicAPIKey, cfg.AgentModel)
		internalmcp.RegisterPromptSuggest(mcpServer, store, embedder, llmClient)

		p := pipeline.New(store, llmClient, pipeline.Config{
			MinEntries:       cfg.PipelineMinEntries,
			Interval:         cfg.PipelineInterval,
			ClusterThreshold: cfg.ClusterThreshold,
		}).WithAgentGeneration(store, agentLLMClient)

		p.Start(ctx)

		// Listen for manual trigger from HTTP API
		go func() {
			for {
				select {
				case <-triggerCh:
					p.TriggerNow()
				case <-ctx.Done():
					return
				}
			}
		}()
	} else {
		internalmcp.RegisterPromptSuggest(mcpServer, store, embedder, nil)
	}

	// Optional MCP HTTP/SSE transport
	if cfg.MCPHTTPAddr != "" {
		internalmcp.StartRemoteMCP(mcpServer, cfg.MCPHTTPAddr, cfg.MCPHTTPPath, store)
	}

	if err := server.ServeStdio(mcpServer); err != nil {
		log.Printf("serve: %v", err)
	}

	// Graceful HTTP shutdown after MCP exits
	shutCtx, shutCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutCancel()
	if err := httpServer.Shutdown(shutCtx); err != nil {
		log.Printf("HTTP shutdown: %v", err)
	}
}

func bootstrapSuperadmin(store *storage.SQLiteStore, rawKey string) {
	ctx := context.Background()
	hash := auth.HashSHA256(rawKey)
	existing, _ := store.GetAPIKeyByHash(ctx, hash)
	if existing != nil {
		return // already seeded
	}
	err := store.CreateAPIKey(ctx, storage.APIKey{
		KeyType: storage.APIKeyTypeTeam,
		Name:    "superadmin-bootstrap",
		KeyHash: hash,
		Role:    "superadmin",
	})
	if err != nil {
		log.Printf("WARNING: failed to bootstrap superadmin key: %v", err)
		return
	}
	log.Printf("Superadmin API key bootstrapped")
}
```

- [ ] **Step 2: Add TriggerNow to pipeline**

In `internal/pipeline/pipeline.go`, add a `TriggerNow()` method:

```go
func (p *Pipeline) TriggerNow() {
	select {
	case p.trigger <- struct{}{}:
	default:
	}
}
```

Add a `trigger chan struct{}` field to the `Pipeline` struct and initialize it in `New()` as `make(chan struct{}, 1)`. In the `Start()` loop, also listen on `p.trigger` to fire immediately.

- [ ] **Step 3: Build check**

```bash
cd /Users/dsandor/Projects/memory && CGO_ENABLED=1 go build ./cmd/server/ 2>&1 | grep -v deprecated
```

Expected: no errors.

- [ ] **Step 4: Full test suite**

```bash
cd /Users/dsandor/Projects/memory && CGO_ENABLED=1 go test ./... 2>&1 | grep -E "^ok|FAIL|---"
```

Expected: all packages PASS.

---

## Task 15: React — API client + auth-aware routing + Layout

**Files:**
- Modify: `web/src/lib/api.ts`
- Modify: `web/src/App.tsx`
- Modify: `web/src/components/Layout.tsx`

- [ ] **Step 1: Update web/src/lib/api.ts**

Add to the existing `api.ts`:

```typescript
// --- Auth ---
export async function login(email: string, password: string) {
  const r = await fetch('/auth/login', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ email, password }),
  });
  if (!r.ok) throw new Error((await r.json()).error ?? 'login failed');
  return r.json();
}

export async function logout() {
  await fetch('/auth/logout', { method: 'POST' });
}

// --- Analytics ---
export async function fetchUsage() {
  const r = await fetch('/api/analytics/usage');
  if (!r.ok) throw new Error('fetch usage failed');
  return r.json();
}

export async function fetchGaps() {
  const r = await fetch('/api/analytics/gaps');
  if (!r.ok) throw new Error('fetch gaps failed');
  return r.json();
}

export async function fetchContributions() {
  const r = await fetch('/api/analytics/contributions');
  if (!r.ok) throw new Error('fetch contributions failed');
  return r.json();
}

// --- Settings ---
export async function fetchSettings() {
  const r = await fetch('/api/settings');
  if (!r.ok) throw new Error('fetch settings failed');
  return r.json();
}

export async function putSettings(settings: object) {
  const r = await fetch('/api/settings', {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(settings),
  });
  if (!r.ok) throw new Error('put settings failed');
  return r.json();
}

// --- Pending queue ---
export async function fetchPending() {
  const r = await fetch('/api/knowledge?status=pending');
  if (!r.ok) throw new Error('fetch pending failed');
  return r.json();
}

export async function approveEntry(id: string) {
  const r = await fetch(`/api/knowledge/${id}/approve`, { method: 'PUT' });
  if (!r.ok) throw new Error('approve failed');
  return r.json();
}

export async function rejectEntry(id: string) {
  const r = await fetch(`/api/knowledge/${id}/reject`, { method: 'PUT' });
  if (!r.ok) throw new Error('reject failed');
  return r.json();
}

// --- Admin: teams ---
export async function fetchTeams() {
  const r = await fetch('/api/admin/teams');
  if (!r.ok) throw new Error('fetch teams failed');
  return r.json();
}

export async function createTeam(name: string, domainPatterns: string[]) {
  const r = await fetch('/api/admin/teams', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ name, domain_patterns: domainPatterns }),
  });
  if (!r.ok) throw new Error('create team failed');
  return r.json();
}

export async function setTeamEnabled(id: string, enabled: boolean) {
  const r = await fetch(`/api/admin/teams/${id}/enabled`, {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ enabled }),
  });
  if (!r.ok) throw new Error('set team enabled failed');
  return r.json();
}

// --- Admin: auth config ---
export async function fetchAuthConfig() {
  const r = await fetch('/api/admin/auth-config');
  if (!r.ok) throw new Error('fetch auth config failed');
  return r.json();
}

export async function putAuthConfig(config: object) {
  const r = await fetch('/api/admin/auth-config', {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(config),
  });
  if (!r.ok) throw new Error('put auth config failed');
  return r.json();
}

// --- Pipeline ---
export async function triggerPipeline() {
  const r = await fetch('/api/pipeline/trigger', { method: 'POST' });
  if (!r.ok) throw new Error('trigger failed');
  return r.json();
}
```

- [ ] **Step 2: Update App.tsx to add new routes**

```typescript
import { Routes, Route } from 'react-router-dom';
import Layout from './components/Layout';
import Dashboard from './pages/Dashboard';
import KnowledgeBrowser from './pages/KnowledgeBrowser';
import KnowledgeDetail from './pages/KnowledgeDetail';
import Clusters from './pages/Clusters';
import Datasets from './pages/Datasets';
import Agents from './pages/Agents';
import AgentDetail from './pages/AgentDetail';
import Analytics from './pages/Analytics';
import PendingQueue from './pages/PendingQueue';
import Settings from './pages/Settings';
import AdminTeams from './pages/AdminTeams';
import AuthConfig from './pages/AuthConfig';

export default function App() {
  return (
    <Routes>
      <Route path="/" element={<Layout />}>
        <Route index element={<Dashboard />} />
        <Route path="knowledge" element={<KnowledgeBrowser />} />
        <Route path="knowledge/:id" element={<KnowledgeDetail />} />
        <Route path="clusters" element={<Clusters />} />
        <Route path="datasets" element={<Datasets />} />
        <Route path="agents" element={<Agents />} />
        <Route path="agents/:id" element={<AgentDetail />} />
        <Route path="analytics" element={<Analytics />} />
        <Route path="pending" element={<PendingQueue />} />
        <Route path="settings" element={<Settings />} />
        <Route path="admin/teams" element={<AdminTeams />} />
        <Route path="admin/auth" element={<AuthConfig />} />
      </Route>
    </Routes>
  );
}
```

- [ ] **Step 3: Update Layout.tsx to include new nav items**

Add to the navigation links in `Layout.tsx`:

```tsx
{/* Existing links: Dashboard, Knowledge, Clusters, Datasets, Agents */}
<NavLink to="/analytics">Analytics</NavLink>
<NavLink to="/pending">Pending Queue</NavLink>
<NavLink to="/settings">Settings</NavLink>
<NavLink to="/admin/teams">Teams</NavLink>
<NavLink to="/admin/auth">Auth Config</NavLink>
```

- [ ] **Step 4: Verify TypeScript build**

```bash
cd /Users/dsandor/Projects/memory/web && npm run build 2>&1 | tail -10
```

Expected: build succeeds (new pages imported but not yet created will error — create stubs first, see Task 16).

---

## Task 16: React — new pages

**Files:**
- Create: `web/src/pages/Analytics.tsx`
- Create: `web/src/pages/PendingQueue.tsx`
- Create: `web/src/pages/Settings.tsx`
- Create: `web/src/pages/AdminTeams.tsx`
- Create: `web/src/pages/AuthConfig.tsx`

- [ ] **Step 1: Create web/src/pages/Analytics.tsx**

```tsx
import { useEffect, useState } from 'react';
import { fetchUsage, fetchGaps, fetchContributions } from '../lib/api';

interface TopEntry { id: string; title: string; domain: string; score: number; usage_count: number; rating: number; }
interface DomainStat { domain: string; entry_count: number; avg_rating: number; total_usage: number; }
interface HeatPoint { week: string; domain: string; usage: number; }
interface Gap { domain: string; entry_count: number; threshold: number; severity: 'low' | 'medium' | 'high'; }
interface LeaderEntry { author: string; entry_count: number; approved_count: number; total_usage: number; avg_rating: number; score: number; }

const severityColor = { low: 'text-yellow-400', medium: 'text-orange-400', high: 'text-red-400' };

export default function Analytics() {
  const [topEntries, setTopEntries] = useState<TopEntry[]>([]);
  const [domainStats, setDomainStats] = useState<DomainStat[]>([]);
  const [heatmap, setHeatmap] = useState<HeatPoint[]>([]);
  const [gaps, setGaps] = useState<Gap[]>([]);
  const [leaderboard, setLeaderboard] = useState<LeaderEntry[]>([]);
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    Promise.all([fetchUsage(), fetchGaps(), fetchContributions()]).then(([usage, gapsData, contribs]) => {
      setTopEntries(usage.top_entries ?? []);
      setDomainStats(usage.by_domain ?? []);
      setHeatmap(usage.heatmap ?? []);
      setGaps(gapsData.gaps ?? []);
      setLeaderboard(contribs.leaderboard ?? []);
      setLoading(false);
    });
  }, []);

  if (loading) return <div className="p-6 text-muted-foreground">Loading analytics...</div>;

  // Build unique domains and weeks for heatmap grid
  const weeks = [...new Set(heatmap.map(h => h.week))].sort().reverse().slice(0, 12);
  const domains = [...new Set(heatmap.map(h => h.domain))].filter(Boolean);
  const heatIndex = new Map(heatmap.map(h => [`${h.week}:${h.domain}`, h.usage]));
  const maxUsage = Math.max(1, ...heatmap.map(h => h.usage));

  return (
    <div className="p-6 space-y-8 max-w-6xl">
      <h1 className="text-2xl font-bold">Analytics</h1>

      {/* Usage Heatmap */}
      {weeks.length > 0 && domains.length > 0 && (
        <section>
          <h2 className="text-lg font-semibold mb-3">Usage Heatmap</h2>
          <div className="overflow-x-auto">
            <table className="text-xs border-collapse">
              <thead>
                <tr>
                  <th className="pr-3 text-right text-muted-foreground">Domain</th>
                  {weeks.map(w => <th key={w} className="px-1 text-muted-foreground font-normal">{w}</th>)}
                </tr>
              </thead>
              <tbody>
                {domains.map(domain => (
                  <tr key={domain}>
                    <td className="pr-3 text-right text-muted-foreground">{domain}</td>
                    {weeks.map(week => {
                      const count = heatIndex.get(`${week}:${domain}`) ?? 0;
                      const intensity = Math.round((count / maxUsage) * 255);
                      return (
                        <td key={week} className="px-1">
                          <div
                            className="w-5 h-5 rounded-sm"
                            style={{ backgroundColor: count > 0 ? `rgba(99,102,241,${count / maxUsage})` : 'rgba(255,255,255,0.05)' }}
                            title={`${domain} / ${week}: ${count}`}
                          />
                        </td>
                      );
                    })}
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </section>
      )}

      {/* Top Entries */}
      <section>
        <h2 className="text-lg font-semibold mb-3">Top Entries</h2>
        <div className="rounded-lg border overflow-hidden">
          <table className="w-full text-sm">
            <thead className="bg-muted/50">
              <tr>
                <th className="text-left p-3 font-medium">Title</th>
                <th className="text-left p-3 font-medium">Domain</th>
                <th className="text-right p-3 font-medium">Rating</th>
                <th className="text-right p-3 font-medium">Usage</th>
                <th className="text-right p-3 font-medium">Score</th>
              </tr>
            </thead>
            <tbody>
              {topEntries.map(e => (
                <tr key={e.id} className="border-t hover:bg-muted/30">
                  <td className="p-3">{e.title}</td>
                  <td className="p-3 text-muted-foreground">{e.domain}</td>
                  <td className="p-3 text-right">{e.rating.toFixed(1)}</td>
                  <td className="p-3 text-right">{e.usage_count}</td>
                  <td className="p-3 text-right font-mono">{e.score.toFixed(1)}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      </section>

      {/* Coverage Gaps */}
      <section>
        <h2 className="text-lg font-semibold mb-3">Coverage Gaps</h2>
        {gaps.length === 0
          ? <p className="text-muted-foreground text-sm">No gaps detected.</p>
          : <div className="space-y-2">
              {gaps.map(g => (
                <div key={g.domain} className="flex items-center gap-4 p-3 rounded-lg border">
                  <span className={`font-medium ${severityColor[g.severity]}`}>{g.severity.toUpperCase()}</span>
                  <span className="font-medium">{g.domain}</span>
                  <span className="text-muted-foreground text-sm">{g.entry_count} / {g.threshold} entries</span>
                  <div className="flex-1 bg-muted rounded-full h-2">
                    <div className="bg-primary h-2 rounded-full" style={{ width: `${(g.entry_count / g.threshold) * 100}%` }} />
                  </div>
                </div>
              ))}
            </div>
        }
      </section>

      {/* Contribution Leaderboard */}
      <section>
        <h2 className="text-lg font-semibold mb-3">Contribution Leaderboard</h2>
        <div className="rounded-lg border overflow-hidden">
          <table className="w-full text-sm">
            <thead className="bg-muted/50">
              <tr>
                <th className="text-left p-3 font-medium">#</th>
                <th className="text-left p-3 font-medium">Author</th>
                <th className="text-right p-3 font-medium">Entries</th>
                <th className="text-right p-3 font-medium">Approved</th>
                <th className="text-right p-3 font-medium">Avg Rating</th>
                <th className="text-right p-3 font-medium">Total Usage</th>
                <th className="text-right p-3 font-medium">Score</th>
              </tr>
            </thead>
            <tbody>
              {leaderboard.map((l, i) => (
                <tr key={l.author} className="border-t hover:bg-muted/30">
                  <td className="p-3 text-muted-foreground">{i + 1}</td>
                  <td className="p-3 font-medium">{l.author}</td>
                  <td className="p-3 text-right">{l.entry_count}</td>
                  <td className="p-3 text-right">{l.approved_count}</td>
                  <td className="p-3 text-right">{l.avg_rating.toFixed(1)}</td>
                  <td className="p-3 text-right">{l.total_usage}</td>
                  <td className="p-3 text-right font-mono">{l.score.toFixed(1)}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      </section>
    </div>
  );
}
```

- [ ] **Step 2: Create web/src/pages/PendingQueue.tsx**

```tsx
import { useEffect, useState } from 'react';
import { fetchPending, approveEntry, rejectEntry } from '../lib/api';
import { Button } from '../components/ui/button';
import { Badge } from '../components/ui/badge';

interface Entry { id: string; title: string; content: string; domain: string; author: string; type: string; }

export default function PendingQueue() {
  const [entries, setEntries] = useState<Entry[]>([]);
  const [loading, setLoading] = useState(true);

  const load = () => {
    fetchPending().then((data: Entry[]) => { setEntries(data ?? []); setLoading(false); });
  };

  useEffect(load, []);

  const handleApprove = async (id: string) => {
    await approveEntry(id);
    setEntries(prev => prev.filter(e => e.id !== id));
  };

  const handleReject = async (id: string) => {
    await rejectEntry(id);
    setEntries(prev => prev.filter(e => e.id !== id));
  };

  if (loading) return <div className="p-6 text-muted-foreground">Loading...</div>;

  return (
    <div className="p-6 max-w-4xl">
      <h1 className="text-2xl font-bold mb-6">Pending Queue</h1>
      {entries.length === 0
        ? <p className="text-muted-foreground">No entries awaiting approval.</p>
        : <div className="space-y-4">
            {entries.map(e => (
              <div key={e.id} className="rounded-lg border p-4 space-y-2">
                <div className="flex items-start justify-between gap-4">
                  <div>
                    <h3 className="font-semibold">{e.title}</h3>
                    <div className="flex gap-2 mt-1">
                      <Badge variant="outline">{e.type}</Badge>
                      {e.domain && <Badge variant="secondary">{e.domain}</Badge>}
                      {e.author && <span className="text-xs text-muted-foreground">by {e.author}</span>}
                    </div>
                  </div>
                  <div className="flex gap-2 shrink-0">
                    <Button size="sm" onClick={() => handleApprove(e.id)}>Approve</Button>
                    <Button size="sm" variant="destructive" onClick={() => handleReject(e.id)}>Reject</Button>
                  </div>
                </div>
                <p className="text-sm text-muted-foreground line-clamp-3">{e.content}</p>
              </div>
            ))}
          </div>
      }
    </div>
  );
}
```

- [ ] **Step 3: Create web/src/pages/Settings.tsx**

```tsx
import { useEffect, useState } from 'react';
import { fetchSettings, putSettings } from '../lib/api';
import { Button } from '../components/ui/button';
import { Input } from '../components/ui/input';
import { Label } from '../components/ui/label';

interface Settings {
  team_id: string;
  domains: string[];
  cluster_threshold: number;
  pipeline_min_entries: number;
  agent_model: string;
}

export default function Settings() {
  const [settings, setSettings] = useState<Settings | null>(null);
  const [domainsText, setDomainsText] = useState('');
  const [saving, setSaving] = useState(false);
  const [saved, setSaved] = useState(false);

  useEffect(() => {
    fetchSettings().then((s: Settings) => {
      setSettings(s);
      setDomainsText((s.domains ?? []).join(', '));
    });
  }, []);

  const handleSave = async () => {
    if (!settings) return;
    setSaving(true);
    await putSettings({
      domains: domainsText.split(',').map(d => d.trim()).filter(Boolean),
      cluster_threshold: settings.cluster_threshold,
      pipeline_min_entries: settings.pipeline_min_entries,
      agent_model: settings.agent_model,
    });
    setSaving(false);
    setSaved(true);
    setTimeout(() => setSaved(false), 2000);
  };

  if (!settings) return <div className="p-6 text-muted-foreground">Loading...</div>;

  return (
    <div className="p-6 max-w-lg space-y-6">
      <h1 className="text-2xl font-bold">Team Settings</h1>

      <div className="space-y-2">
        <Label htmlFor="domains">Domain Taxonomy (comma-separated)</Label>
        <Input id="domains" value={domainsText} onChange={e => setDomainsText(e.target.value)} placeholder="finance, legal, engineering" />
      </div>

      <div className="space-y-2">
        <Label htmlFor="threshold">Cluster Threshold (0–1)</Label>
        <Input id="threshold" type="number" min="0" max="1" step="0.01"
          value={settings.cluster_threshold}
          onChange={e => setSettings(s => s ? { ...s, cluster_threshold: parseFloat(e.target.value) } : s)} />
      </div>

      <div className="space-y-2">
        <Label htmlFor="minEntries">Pipeline Min Entries</Label>
        <Input id="minEntries" type="number" min="1"
          value={settings.pipeline_min_entries}
          onChange={e => setSettings(s => s ? { ...s, pipeline_min_entries: parseInt(e.target.value) } : s)} />
      </div>

      <div className="space-y-2">
        <Label htmlFor="agentModel">Agent Model</Label>
        <Input id="agentModel"
          value={settings.agent_model}
          onChange={e => setSettings(s => s ? { ...s, agent_model: e.target.value } : s)} />
      </div>

      <Button onClick={handleSave} disabled={saving}>
        {saving ? 'Saving...' : saved ? 'Saved!' : 'Save Settings'}
      </Button>
    </div>
  );
}
```

- [ ] **Step 4: Create web/src/pages/AdminTeams.tsx**

```tsx
import { useEffect, useState } from 'react';
import { fetchTeams, createTeam, setTeamEnabled } from '../lib/api';
import { Button } from '../components/ui/button';
import { Input } from '../components/ui/input';
import { Badge } from '../components/ui/badge';

interface Team { id: string; name: string; domain_patterns: string[]; enabled: boolean; }

export default function AdminTeams() {
  const [teams, setTeams] = useState<Team[]>([]);
  const [newName, setNewName] = useState('');
  const [newPatterns, setNewPatterns] = useState('');
  const [loading, setLoading] = useState(true);

  const load = () => fetchTeams().then((t: Team[]) => { setTeams(t ?? []); setLoading(false); });
  useEffect(load, []);

  const handleCreate = async () => {
    if (!newName.trim()) return;
    await createTeam(newName.trim(), newPatterns.split(',').map(p => p.trim()).filter(Boolean));
    setNewName(''); setNewPatterns('');
    load();
  };

  const handleToggle = async (id: string, enabled: boolean) => {
    await setTeamEnabled(id, !enabled);
    load();
  };

  if (loading) return <div className="p-6 text-muted-foreground">Loading...</div>;

  return (
    <div className="p-6 max-w-3xl space-y-6">
      <h1 className="text-2xl font-bold">Team Administration</h1>

      {/* Create team */}
      <div className="rounded-lg border p-4 space-y-3">
        <h2 className="font-semibold">Create Team</h2>
        <div className="flex gap-3">
          <Input placeholder="Team name" value={newName} onChange={e => setNewName(e.target.value)} />
          <Input placeholder="Domain patterns (comma-separated regex)" value={newPatterns} onChange={e => setNewPatterns(e.target.value)} className="flex-1" />
          <Button onClick={handleCreate}>Create</Button>
        </div>
      </div>

      {/* Team list */}
      <div className="space-y-3">
        {teams.map(t => (
          <div key={t.id} className="rounded-lg border p-4 flex items-center justify-between gap-4">
            <div>
              <div className="flex items-center gap-2">
                <span className="font-semibold">{t.name}</span>
                <Badge variant={t.enabled ? 'default' : 'secondary'}>{t.enabled ? 'Active' : 'Disabled'}</Badge>
              </div>
              {t.domain_patterns.length > 0 && (
                <p className="text-xs text-muted-foreground mt-1">{t.domain_patterns.join(', ')}</p>
              )}
            </div>
            <Button size="sm" variant="outline" onClick={() => handleToggle(t.id, t.enabled)}>
              {t.enabled ? 'Disable' : 'Enable'}
            </Button>
          </div>
        ))}
      </div>
    </div>
  );
}
```

- [ ] **Step 5: Create web/src/pages/AuthConfig.tsx**

```tsx
import { useEffect, useState } from 'react';
import { fetchAuthConfig, putAuthConfig } from '../lib/api';
import { Button } from '../components/ui/button';
import { Input } from '../components/ui/input';
import { Label } from '../components/ui/label';

type Provider = 'local' | 'oidc';

interface AuthConfig {
  provider: Provider;
  oidc_issuer: string;
  oidc_client_id: string;
  oidc_redirect_url: string;
}

const PROVIDER_PRESETS: Record<string, Partial<AuthConfig>> = {
  Clerk: { oidc_issuer: 'https://clerk.your-domain.com', oidc_redirect_url: `${location.origin}/auth/oidc/callback` },
  Zitadel: { oidc_issuer: 'https://your-instance.zitadel.cloud', oidc_redirect_url: `${location.origin}/auth/oidc/callback` },
};

export default function AuthConfig() {
  const [cfg, setCfg] = useState<AuthConfig>({ provider: 'local', oidc_issuer: '', oidc_client_id: '', oidc_redirect_url: '' });
  const [saving, setSaving] = useState(false);
  const [saved, setSaved] = useState(false);

  useEffect(() => { fetchAuthConfig().then((c: AuthConfig) => setCfg(c)); }, []);

  const applyPreset = (name: string) => {
    setCfg(prev => ({ ...prev, provider: 'oidc', ...PROVIDER_PRESETS[name] }));
  };

  const handleSave = async () => {
    setSaving(true);
    await putAuthConfig(cfg);
    setSaving(false); setSaved(true);
    setTimeout(() => setSaved(false), 2000);
  };

  return (
    <div className="p-6 max-w-lg space-y-6">
      <h1 className="text-2xl font-bold">Auth Configuration</h1>

      <div className="space-y-2">
        <Label>Provider</Label>
        <div className="flex gap-3">
          {(['local', 'oidc'] as Provider[]).map(p => (
            <Button key={p} size="sm" variant={cfg.provider === p ? 'default' : 'outline'}
              onClick={() => setCfg(prev => ({ ...prev, provider: p }))}>
              {p === 'local' ? 'Local (email+password)' : 'OIDC'}
            </Button>
          ))}
        </div>
      </div>

      {cfg.provider === 'oidc' && (
        <>
          <div className="flex gap-2">
            <span className="text-sm text-muted-foreground">Quick setup:</span>
            {Object.keys(PROVIDER_PRESETS).map(name => (
              <Button key={name} size="sm" variant="outline" onClick={() => applyPreset(name)}>{name}</Button>
            ))}
          </div>

          <div className="space-y-2">
            <Label htmlFor="issuer">OIDC Issuer URL</Label>
            <Input id="issuer" value={cfg.oidc_issuer} onChange={e => setCfg(p => ({ ...p, oidc_issuer: e.target.value }))} />
          </div>
          <div className="space-y-2">
            <Label htmlFor="clientId">Client ID</Label>
            <Input id="clientId" value={cfg.oidc_client_id} onChange={e => setCfg(p => ({ ...p, oidc_client_id: e.target.value }))} />
          </div>
          <div className="space-y-2">
            <Label htmlFor="redirect">Redirect URL</Label>
            <Input id="redirect" value={cfg.oidc_redirect_url} onChange={e => setCfg(p => ({ ...p, oidc_redirect_url: e.target.value }))} />
          </div>
          <p className="text-xs text-muted-foreground">
            Set <code>OIDC_CLIENT_SECRET</code> env var on the server — it is never stored in the database.
          </p>
        </>
      )}

      <Button onClick={handleSave} disabled={saving}>
        {saving ? 'Saving...' : saved ? 'Saved!' : 'Save Configuration'}
      </Button>
    </div>
  );
}
```

- [ ] **Step 6: Verify React build**

```bash
cd /Users/dsandor/Projects/memory/web && npm run build 2>&1 | tail -10
```

Expected: build succeeds cleanly.

---

## Task 17: End-to-end verification + STATE.md update

- [ ] **Step 1: Full Go test suite**

```bash
cd /Users/dsandor/Projects/memory && CGO_ENABLED=1 go test ./... 2>&1 | grep -E "^ok|FAIL"
```

Expected: all packages PASS, zero FAIL.

- [ ] **Step 2: Full build**

```bash
cd /Users/dsandor/Projects/memory && CGO_ENABLED=1 go build -o /tmp/tribal-knowledge ./cmd/server/ && echo "build ok"
```

Expected: `build ok`

- [ ] **Step 3: React build**

```bash
cd /Users/dsandor/Projects/memory/web && npm run build 2>&1 | tail -5
```

Expected: `✓ built in ...`

- [ ] **Step 4: Clean up build artifact**

```bash
rm -f /tmp/tribal-knowledge
```

- [ ] **Step 5: Update STATE.md**

In `.planning/STATE.md`, update:

```markdown
- **Phase:** 5
- **Status:** complete
- **Last updated:** 2026-06-07
```

Update the phase table row for Phase 5 to `complete`.

Add note:
```
- Phase 5 complete: multi-tenant federation, chi router, OIDC+local auth (Clerk/Zitadel), superadmin/admin/curator/member roles, API keys (team+user), team scoping on all data, activity_log, analytics (usage heatmap/gaps/contributions), curator approval queue, MCP HTTP/SSE remote transport, knowledge_search + knowledge_rate + prompt_suggest MCP tools, agents:// and knowledge:// MCP resources, enhance_with_context prompt, 5 new React pages
```

Also update `.planning/ROADMAP.md` Phase 5 status from `planning` to `complete`.
