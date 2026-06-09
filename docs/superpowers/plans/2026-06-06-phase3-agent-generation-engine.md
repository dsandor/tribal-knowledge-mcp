# Agent Generation Engine — Phase 3 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Implement an Agent Generation Engine that synthesizes specialized AI agent definitions from knowledge clusters, versions them with human-readable changelogs, exports them in three formats (md/txt/json), and exposes them via MCP tools and a prompt.

**Architecture:** A new `internal/agent` package provides three pure functions — `Generate` (LLM → structured agent), `Diff` (field-by-field changelog string), and `Export` (three output formats). A new `AgentStore` interface extends `AnalysisStore` with agent CRUD and version history backed by two new SQLite tables (`agents`, `agent_versions`). The pipeline gains an optional `WithAgentGeneration` hook that fires after each cluster is stored. Four new MCP tools (`agent_list`, `agent_get`, `agent_publish`, `agent_export`) plus one MCP prompt (`use_agent`) expose the generated agents to LLM clients.

**Tech Stack:** Go 1.24+, SQLite/sqlite-vec (existing), Anthropic Messages API via existing `llm.AnthropicClient`, mark3labs/mcp-go v0.54.1 (existing).

---

## File Map

### New files
| File | Purpose |
|------|---------|
| `internal/storage/agents.go` | `Agent`, `AgentVersion` types and `AgentStore` interface |
| `internal/storage/agents_sqlite.go` | `AgentStore` methods on `*SQLiteStore` |
| `internal/storage/agents_test.go` | Tests for all `AgentStore` methods |
| `internal/agent/generate.go` | `Generate(ctx, client, cluster, entries)` — LLM → draft Agent |
| `internal/agent/generate_test.go` | Tests with local `mockLLM` |
| `internal/agent/diff.go` | `Diff(old, new *storage.Agent) string` — field-by-field changelog |
| `internal/agent/diff_test.go` | Diff unit tests |
| `internal/agent/export.go` | `Export(agent *storage.Agent, format string) string` |
| `internal/agent/export_test.go` | Export format tests |
| `internal/mcp/agent_tools.go` | `HandleAgentList`, `HandleAgentGet`, `HandleAgentPublish`, `HandleAgentExport` + `RegisterAgentTools` |
| `internal/mcp/agent_tools_test.go` | Tests for MCP agent handlers (`package mcp_test`) |

### Modified files
| File | Change |
|------|--------|
| `internal/storage/sqlite.go` | Add `agents` and `agent_versions` tables to `migrate()` |
| `internal/pipeline/pipeline.go` | Add `agentStore`/`agentLLM` fields, `WithAgentGeneration()` method, capture cluster ID, call agent generation |
| `internal/pipeline/testhelpers_test.go` | Add `mockAgentStore` |
| `internal/pipeline/pipeline_test.go` | Add agent generation test |
| `internal/mcp/server.go` | Add `RegisterAgentTools`, add `server.WithPromptCapabilities(false)` |
| `internal/config/config.go` | Add `AgentModel string` field |
| `internal/config/config_test.go` | Test `AgentModel` default |
| `cmd/server/main.go` | Create `agentLLMClient`, call `WithAgentGeneration`, call `RegisterAgentTools` |

---

## Task 1: Agent types, AgentStore interface, and SQLite implementation

**Files:**
- Create: `internal/storage/agents.go`
- Create: `internal/storage/agents_sqlite.go`
- Create: `internal/storage/agents_test.go`
- Modify: `internal/storage/sqlite.go`

### Background

Two new tables:
- `agents`: one row per domain (UNIQUE constraint on `domain`), always the current state.
- `agent_versions`: append-only history; every pipeline pass appends one row per domain.

`AgentStore` embeds `AnalysisStore`, so a single `*SQLiteStore` satisfies both interfaces and the pipeline needs only one store argument.

- [ ] **Step 1: Write the failing tests**

Create `internal/storage/agents_test.go`:

```go
package storage

import (
	"context"
	"testing"
)

func newTestAgentStore(t *testing.T) *SQLiteStore {
	t.Helper()
	return newTestAnalysisStore(t)
}

func TestUpsertAndGetAgent_Create(t *testing.T) {
	s := newTestAgentStore(t)
	ctx := context.Background()

	agent := Agent{
		Domain:       "finance",
		Version:      1,
		Status:       AgentStatusDraft,
		SystemPrompt: "You are a finance assistant.",
		Instructions: "Use DCF for valuation.",
		AntiPatterns: "Do not guess earnings.",
		SourceRefs:   []string{"cluster-1"},
		ClusterID:    "cluster-1",
	}

	id, err := s.UpsertAgent(ctx, agent)
	if err != nil {
		t.Fatalf("UpsertAgent: %v", err)
	}
	if id == "" {
		t.Fatal("expected non-empty id")
	}

	got, err := s.GetAgent(ctx, id)
	if err != nil {
		t.Fatalf("GetAgent: %v", err)
	}
	if got.Domain != "finance" {
		t.Errorf("domain = %q, want finance", got.Domain)
	}
	if got.SystemPrompt != "You are a finance assistant." {
		t.Errorf("system_prompt = %q", got.SystemPrompt)
	}
	if got.Status != AgentStatusDraft {
		t.Errorf("status = %q, want draft", got.Status)
	}
	if len(got.SourceRefs) != 1 || got.SourceRefs[0] != "cluster-1" {
		t.Errorf("source_refs = %v, want [cluster-1]", got.SourceRefs)
	}
}

func TestUpsertAgent_UpdateExisting(t *testing.T) {
	s := newTestAgentStore(t)
	ctx := context.Background()

	id, _ := s.UpsertAgent(ctx, Agent{Domain: "finance", Version: 1, Status: AgentStatusDraft, SystemPrompt: "v1"})

	a2 := Agent{ID: id, Domain: "finance", Version: 2, Status: AgentStatusDraft, SystemPrompt: "v2"}
	id2, err := s.UpsertAgent(ctx, a2)
	if err != nil {
		t.Fatalf("UpsertAgent update: %v", err)
	}
	if id != id2 {
		t.Errorf("id changed on update: got %q, want %q", id2, id)
	}

	got, _ := s.GetAgent(ctx, id)
	if got.SystemPrompt != "v2" {
		t.Errorf("system_prompt after update = %q, want v2", got.SystemPrompt)
	}
	if got.Version != 2 {
		t.Errorf("version = %d, want 2", got.Version)
	}
}

func TestGetAgentByDomain(t *testing.T) {
	s := newTestAgentStore(t)
	ctx := context.Background()

	s.UpsertAgent(ctx, Agent{Domain: "legal", Version: 1, Status: AgentStatusDraft, SystemPrompt: "legal agent"})

	got, err := s.GetAgentByDomain(ctx, "legal")
	if err != nil {
		t.Fatalf("GetAgentByDomain: %v", err)
	}
	if got == nil {
		t.Fatal("expected agent, got nil")
	}
	if got.Domain != "legal" {
		t.Errorf("domain = %q, want legal", got.Domain)
	}
}

func TestGetAgentByDomain_NotFound(t *testing.T) {
	s := newTestAgentStore(t)
	ctx := context.Background()

	got, err := s.GetAgentByDomain(ctx, "nonexistent")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != nil {
		t.Errorf("expected nil for missing domain, got %+v", got)
	}
}

func TestListAgents(t *testing.T) {
	s := newTestAgentStore(t)
	ctx := context.Background()

	s.UpsertAgent(ctx, Agent{Domain: "finance", Version: 1, Status: AgentStatusPublished, SystemPrompt: "a"})
	s.UpsertAgent(ctx, Agent{Domain: "legal", Version: 1, Status: AgentStatusDraft, SystemPrompt: "b"})

	agents, err := s.ListAgents(ctx)
	if err != nil {
		t.Fatalf("ListAgents: %v", err)
	}
	if len(agents) != 2 {
		t.Errorf("want 2 agents, got %d", len(agents))
	}
}

func TestPublishAgent(t *testing.T) {
	s := newTestAgentStore(t)
	ctx := context.Background()

	id, _ := s.UpsertAgent(ctx, Agent{Domain: "ops", Version: 1, Status: AgentStatusDraft, SystemPrompt: "ops"})

	if err := s.PublishAgent(ctx, id); err != nil {
		t.Fatalf("PublishAgent: %v", err)
	}
	got, _ := s.GetAgent(ctx, id)
	if got.Status != AgentStatusPublished {
		t.Errorf("status = %q, want published", got.Status)
	}
}

func TestStoreAndListAgentVersions(t *testing.T) {
	s := newTestAgentStore(t)
	ctx := context.Background()

	id, _ := s.UpsertAgent(ctx, Agent{Domain: "finance", Version: 1, Status: AgentStatusDraft, SystemPrompt: "v1"})

	err := s.StoreAgentVersion(ctx, AgentVersion{
		AgentID:      id,
		Version:      1,
		SystemPrompt: "v1",
		Changelog:    "initial generation",
	})
	if err != nil {
		t.Fatalf("StoreAgentVersion: %v", err)
	}

	versions, err := s.ListAgentVersions(ctx, id)
	if err != nil {
		t.Fatalf("ListAgentVersions: %v", err)
	}
	if len(versions) != 1 {
		t.Fatalf("want 1 version, got %d", len(versions))
	}
	if versions[0].Changelog != "initial generation" {
		t.Errorf("changelog = %q", versions[0].Changelog)
	}
	if versions[0].AgentID != id {
		t.Errorf("agent_id = %q, want %q", versions[0].AgentID, id)
	}
}
```

- [ ] **Step 2: Run tests — verify they fail**

```bash
CGO_ENABLED=1 go test ./internal/storage/... 2>&1 | head -20
```

Expected: compilation errors — `Agent`, `AgentStatusDraft`, `AgentStatusPublished`, `AgentVersion` not defined.

- [ ] **Step 3: Create internal/storage/agents.go**

```go
package storage

import (
	"context"
	"time"
)

type AgentStatus string

const (
	AgentStatusDraft     AgentStatus = "draft"
	AgentStatusPublished AgentStatus = "published"
)

type Agent struct {
	ID           string
	Domain       string
	Version      int
	Status       AgentStatus
	SystemPrompt string
	Instructions string
	AntiPatterns string
	SourceRefs   []string
	ClusterID    string
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

type AgentVersion struct {
	ID           string
	AgentID      string
	Version      int
	SystemPrompt string
	Instructions string
	AntiPatterns string
	Changelog    string
	CreatedAt    time.Time
}

// AgentStore extends AnalysisStore with agent generation and versioning methods.
type AgentStore interface {
	AnalysisStore
	UpsertAgent(ctx context.Context, agent Agent) (string, error)
	GetAgent(ctx context.Context, id string) (*Agent, error)
	GetAgentByDomain(ctx context.Context, domain string) (*Agent, error)
	ListAgents(ctx context.Context) ([]Agent, error)
	PublishAgent(ctx context.Context, id string) error
	StoreAgentVersion(ctx context.Context, version AgentVersion) error
	ListAgentVersions(ctx context.Context, agentID string) ([]AgentVersion, error)
}
```

- [ ] **Step 4: Extend migrate() in sqlite.go with agent tables**

In `internal/storage/sqlite.go`, add the following two `CREATE TABLE` blocks inside the `migrate()` function, just before the final `return nil`:

```go
	_, err = s.db.Exec(`
		CREATE TABLE IF NOT EXISTS agents (
			id            TEXT PRIMARY KEY,
			domain        TEXT NOT NULL UNIQUE,
			version       INTEGER NOT NULL DEFAULT 1,
			status        TEXT NOT NULL DEFAULT 'draft',
			system_prompt TEXT NOT NULL DEFAULT '',
			instructions  TEXT NOT NULL DEFAULT '',
			anti_patterns TEXT NOT NULL DEFAULT '',
			source_refs   TEXT NOT NULL DEFAULT '[]',
			cluster_id    TEXT NOT NULL DEFAULT '',
			created_at    DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at    DATETIME DEFAULT CURRENT_TIMESTAMP
		);
	`)
	if err != nil {
		return fmt.Errorf("create agents table: %w", err)
	}

	_, err = s.db.Exec(`
		CREATE TABLE IF NOT EXISTS agent_versions (
			id            TEXT PRIMARY KEY,
			agent_id      TEXT NOT NULL REFERENCES agents(id),
			version       INTEGER NOT NULL,
			system_prompt TEXT NOT NULL DEFAULT '',
			instructions  TEXT NOT NULL DEFAULT '',
			anti_patterns TEXT NOT NULL DEFAULT '',
			changelog     TEXT NOT NULL DEFAULT '',
			created_at    DATETIME DEFAULT CURRENT_TIMESTAMP
		);
	`)
	if err != nil {
		return fmt.Errorf("create agent_versions table: %w", err)
	}
```

- [ ] **Step 5: Create internal/storage/agents_sqlite.go**

```go
package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"
)

func (s *SQLiteStore) UpsertAgent(ctx context.Context, a Agent) (string, error) {
	// If no ID given, look up by domain so callers don't need to track IDs.
	if a.ID == "" {
		existing, err := s.GetAgentByDomain(ctx, a.Domain)
		if err != nil {
			return "", fmt.Errorf("lookup agent by domain: %w", err)
		}
		if existing != nil {
			a.ID = existing.ID
		} else {
			a.ID = uuid.NewString()
		}
	}

	refsJSON, err := json.Marshal(a.SourceRefs)
	if err != nil {
		return "", fmt.Errorf("marshal source_refs: %w", err)
	}
	if a.SourceRefs == nil {
		refsJSON = []byte("[]")
	}

	_, err = s.db.ExecContext(ctx, `
		INSERT INTO agents (id, domain, version, status, system_prompt, instructions, anti_patterns, source_refs, cluster_id)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			version       = excluded.version,
			status        = excluded.status,
			system_prompt = excluded.system_prompt,
			instructions  = excluded.instructions,
			anti_patterns = excluded.anti_patterns,
			source_refs   = excluded.source_refs,
			cluster_id    = excluded.cluster_id,
			updated_at    = CURRENT_TIMESTAMP
	`, a.ID, a.Domain, a.Version, string(a.Status),
		a.SystemPrompt, a.Instructions, a.AntiPatterns,
		string(refsJSON), a.ClusterID)
	if err != nil {
		return "", fmt.Errorf("upsert agent: %w", err)
	}
	return a.ID, nil
}

func (s *SQLiteStore) GetAgent(ctx context.Context, id string) (*Agent, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, domain, version, status, system_prompt, instructions, anti_patterns,
		       source_refs, cluster_id, created_at, updated_at
		FROM agents WHERE id = ?
	`, id)
	a, err := scanAgent(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return a, nil
}

func (s *SQLiteStore) GetAgentByDomain(ctx context.Context, domain string) (*Agent, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, domain, version, status, system_prompt, instructions, anti_patterns,
		       source_refs, cluster_id, created_at, updated_at
		FROM agents WHERE domain = ?
	`, domain)
	a, err := scanAgent(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return a, nil
}

func (s *SQLiteStore) ListAgents(ctx context.Context) ([]Agent, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, domain, version, status, system_prompt, instructions, anti_patterns,
		       source_refs, cluster_id, created_at, updated_at
		FROM agents ORDER BY domain ASC
	`)
	if err != nil {
		return nil, fmt.Errorf("list agents: %w", err)
	}
	defer rows.Close()

	var agents []Agent
	for rows.Next() {
		a, err := scanAgent(rows)
		if err != nil {
			return nil, fmt.Errorf("scan agent: %w", err)
		}
		agents = append(agents, *a)
	}
	return agents, rows.Err()
}

func (s *SQLiteStore) PublishAgent(ctx context.Context, id string) error {
	res, err := s.db.ExecContext(ctx,
		"UPDATE agents SET status = 'published', updated_at = CURRENT_TIMESTAMP WHERE id = ?", id)
	if err != nil {
		return fmt.Errorf("publish agent: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("agent %q: %w", id, ErrNotFound)
	}
	return nil
}

func (s *SQLiteStore) StoreAgentVersion(ctx context.Context, v AgentVersion) error {
	v.ID = uuid.NewString()
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO agent_versions (id, agent_id, version, system_prompt, instructions, anti_patterns, changelog)
		VALUES (?, ?, ?, ?, ?, ?, ?)
	`, v.ID, v.AgentID, v.Version, v.SystemPrompt, v.Instructions, v.AntiPatterns, v.Changelog)
	if err != nil {
		return fmt.Errorf("store agent version: %w", err)
	}
	return nil
}

func (s *SQLiteStore) ListAgentVersions(ctx context.Context, agentID string) ([]AgentVersion, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, agent_id, version, system_prompt, instructions, anti_patterns, changelog, created_at
		FROM agent_versions WHERE agent_id = ? ORDER BY version ASC
	`, agentID)
	if err != nil {
		return nil, fmt.Errorf("list agent versions: %w", err)
	}
	defer rows.Close()

	var versions []AgentVersion
	for rows.Next() {
		var v AgentVersion
		var createdAt string
		if err := rows.Scan(&v.ID, &v.AgentID, &v.Version,
			&v.SystemPrompt, &v.Instructions, &v.AntiPatterns,
			&v.Changelog, &createdAt); err != nil {
			return nil, fmt.Errorf("scan agent version: %w", err)
		}
		v.CreatedAt = parseTimestamp(createdAt)
		versions = append(versions, v)
	}
	return versions, rows.Err()
}

// Ensure *SQLiteStore implements AgentStore at compile time.
var _ AgentStore = (*SQLiteStore)(nil)

func scanAgent(row rowScanner) (*Agent, error) {
	var a Agent
	var refsJSON, createdAt, updatedAt string
	err := row.Scan(&a.ID, &a.Domain, &a.Version, &a.Status,
		&a.SystemPrompt, &a.Instructions, &a.AntiPatterns,
		&refsJSON, &a.ClusterID, &createdAt, &updatedAt)
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal([]byte(refsJSON), &a.SourceRefs); err != nil {
		a.SourceRefs = []string{}
	}
	a.CreatedAt = parseTimestamp(createdAt)
	a.UpdatedAt = parseTimestamp(updatedAt)
	return &a, nil
}
```

- [ ] **Step 6: Run tests — verify they pass**

```bash
CGO_ENABLED=1 go test ./internal/storage/... -v 2>&1 | tail -25
```

Expected: all previous tests plus 7 new agent store tests PASS.

- [ ] **Step 7: Build check**

```bash
CGO_ENABLED=1 go build ./cmd/server/ && echo "ok"
```

Expected: `ok`

- [ ] **Step 8: Commit**

```bash
git add internal/storage/agents.go internal/storage/agents_sqlite.go internal/storage/agents_test.go internal/storage/sqlite.go
git commit -m "feat(storage): add AgentStore interface and agents/agent_versions schema"
```

---

## Task 2: Agent generation from cluster

**Files:**
- Create: `internal/agent/generate.go`
- Create: `internal/agent/generate_test.go`

### Background

`Generate` makes a single LLM call with the cluster title, summary, and its knowledge entries. It parses the JSON response into a `*storage.Agent` draft — not yet persisted. The caller (pipeline) is responsible for persistence. This keeps the function pure and testable without a database.

- [ ] **Step 1: Write the failing tests**

Create `internal/agent/generate_test.go`:

```go
package agent

import (
	"context"
	"fmt"
	"testing"

	"github.com/dsandor/memory/internal/storage"
)

type mockLLM struct {
	response string
	err      error
}

func (m *mockLLM) Complete(_ context.Context, _ string) (string, error) {
	return m.response, m.err
}

func TestGenerate_ParsesLLMResponse(t *testing.T) {
	mock := &mockLLM{response: `{
		"system_prompt": "You are a financial analysis assistant.",
		"instructions": "Always use DCF for valuation.\nCite sources.",
		"anti_patterns": "Do not guess earnings without data."
	}`}
	cluster := storage.Cluster{
		ID:      "c1",
		Domain:  "finance",
		Title:   "Finance Patterns",
		Summary: "Common finance analysis patterns.",
	}
	entries := []storage.KnowledgeEntry{
		{Title: "DCF Analysis", Content: "Use discounted cash flow..."},
		{Title: "Earnings Patterns", Content: "Look for EPS growth..."},
	}

	a, err := Generate(context.Background(), mock, cluster, entries)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if a.Domain != "finance" {
		t.Errorf("domain = %q, want finance", a.Domain)
	}
	if a.SystemPrompt != "You are a financial analysis assistant." {
		t.Errorf("system_prompt = %q", a.SystemPrompt)
	}
	if a.Instructions == "" {
		t.Error("instructions is empty")
	}
	if a.AntiPatterns == "" {
		t.Error("anti_patterns is empty")
	}
	if a.ClusterID != "c1" {
		t.Errorf("cluster_id = %q, want c1", a.ClusterID)
	}
	if len(a.SourceRefs) == 0 || a.SourceRefs[0] != "c1" {
		t.Errorf("source_refs = %v, want [c1]", a.SourceRefs)
	}
	if a.Status != storage.AgentStatusDraft {
		t.Errorf("status = %q, want draft", a.Status)
	}
	if a.Version != 1 {
		t.Errorf("version = %d, want 1", a.Version)
	}
}

func TestGenerate_MarkdownFence(t *testing.T) {
	mock := &mockLLM{response: "```json\n{\"system_prompt\":\"SP\",\"instructions\":\"I\",\"anti_patterns\":\"AP\"}\n```"}
	cluster := storage.Cluster{ID: "c2", Domain: "ops", Title: "Ops", Summary: "ops patterns"}
	entries := []storage.KnowledgeEntry{{Title: "E", Content: "C"}}

	a, err := Generate(context.Background(), mock, cluster, entries)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if a.SystemPrompt != "SP" {
		t.Errorf("system_prompt = %q, want SP", a.SystemPrompt)
	}
}

func TestGenerate_LLMError(t *testing.T) {
	mock := &mockLLM{err: fmt.Errorf("network error")}
	cluster := storage.Cluster{Domain: "finance", Title: "F", Summary: "S"}
	_, err := Generate(context.Background(), mock, cluster, nil)
	if err == nil {
		t.Fatal("expected error from LLM failure")
	}
}
```

- [ ] **Step 2: Run tests — verify they fail**

```bash
go test ./internal/agent/... 2>&1 | head -10
```

Expected: compilation error (package not found; `Generate` undefined).

- [ ] **Step 3: Create internal/agent/generate.go**

```go
package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/dsandor/memory/internal/llm"
	"github.com/dsandor/memory/internal/storage"
)

var jsonFenceRe = regexp.MustCompile("(?s)```(?:json)?\\s*(.*?)```")

func extractJSON(s string) string {
	if m := jsonFenceRe.FindStringSubmatch(s); len(m) > 1 {
		return strings.TrimSpace(m[1])
	}
	return strings.TrimSpace(s)
}

type generateResponse struct {
	SystemPrompt string `json:"system_prompt"`
	Instructions string `json:"instructions"`
	AntiPatterns string `json:"anti_patterns"`
}

// Generate creates a draft Agent from a cluster and its knowledge entries using an LLM.
// The returned Agent is not persisted; the caller must call AgentStore.UpsertAgent.
func Generate(ctx context.Context, client llm.Client, cluster storage.Cluster, entries []storage.KnowledgeEntry) (*storage.Agent, error) {
	var sb strings.Builder
	for i, e := range entries {
		fmt.Fprintf(&sb, "%d. [%s] %s\n   %s\n\n", i+1, e.Type, e.Title, truncate(e.Content, 300))
	}

	prompt := fmt.Sprintf(
		"You are building a specialized AI agent definition from a knowledge cluster.\n"+
			"Cluster: %s\nSummary: %s\n\nKnowledge entries:\n%s\n"+
			"Generate a specialized AI agent that embodies this domain expertise.\n"+
			"Return ONLY valid JSON with these three fields:\n"+
			"- system_prompt: 2-4 sentences defining this agent's role and expertise\n"+
			"- instructions: step-by-step guidelines the agent should follow (newline-separated)\n"+
			"- anti_patterns: behaviors this agent should avoid (newline-separated)\n\n"+
			"JSON only, no other text.",
		cluster.Title, cluster.Summary, sb.String(),
	)

	resp, err := client.Complete(ctx, prompt)
	if err != nil {
		return nil, fmt.Errorf("llm: %w", err)
	}

	var result generateResponse
	if err := json.Unmarshal([]byte(extractJSON(resp)), &result); err != nil {
		return nil, fmt.Errorf("parse agent response: %w", err)
	}

	return &storage.Agent{
		Domain:       cluster.Domain,
		Version:      1,
		Status:       storage.AgentStatusDraft,
		SystemPrompt: result.SystemPrompt,
		Instructions: result.Instructions,
		AntiPatterns: result.AntiPatterns,
		SourceRefs:   []string{cluster.ID},
		ClusterID:    cluster.ID,
	}, nil
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
```

- [ ] **Step 4: Run tests**

```bash
go test ./internal/agent/... -v 2>&1 | tail -15
```

Expected: all 3 tests PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/agent/generate.go internal/agent/generate_test.go
git commit -m "feat(agent): add LLM-backed agent generation from cluster"
```

---

## Task 3: Diff engine and export formats

**Files:**
- Create: `internal/agent/diff.go`
- Create: `internal/agent/diff_test.go`
- Create: `internal/agent/export.go`
- Create: `internal/agent/export_test.go`

- [ ] **Step 1: Write failing diff tests**

Create `internal/agent/diff_test.go`:

```go
package agent

import (
	"strings"
	"testing"

	"github.com/dsandor/memory/internal/storage"
)

func TestDiff_NoChange(t *testing.T) {
	a := &storage.Agent{SystemPrompt: "SP", Instructions: "I", AntiPatterns: "AP"}
	result := Diff(a, a)
	if result != "no changes" {
		t.Errorf("Diff(identical) = %q, want %q", result, "no changes")
	}
}

func TestDiff_SystemPromptChanged(t *testing.T) {
	old := &storage.Agent{SystemPrompt: "old prompt", Instructions: "I", AntiPatterns: "AP"}
	newA := &storage.Agent{SystemPrompt: "new prompt", Instructions: "I", AntiPatterns: "AP"}
	result := Diff(old, newA)
	if !strings.Contains(result, "system_prompt") {
		t.Errorf("Diff should mention system_prompt, got: %q", result)
	}
}

func TestDiff_MultipleFieldsChanged(t *testing.T) {
	old := &storage.Agent{SystemPrompt: "SP1", Instructions: "I1", AntiPatterns: "AP1"}
	newA := &storage.Agent{SystemPrompt: "SP2", Instructions: "I2", AntiPatterns: "AP1"}
	result := Diff(old, newA)
	if !strings.Contains(result, "system_prompt") {
		t.Errorf("Diff should mention system_prompt")
	}
	if !strings.Contains(result, "instructions") {
		t.Errorf("Diff should mention instructions")
	}
}

func TestDiff_NilOld(t *testing.T) {
	newA := &storage.Agent{SystemPrompt: "SP", Instructions: "I", AntiPatterns: "AP"}
	result := Diff(nil, newA)
	if !strings.Contains(result, "initial generation") {
		t.Errorf("Diff(nil, new) should say initial generation, got: %q", result)
	}
}
```

- [ ] **Step 2: Write failing export tests**

Create `internal/agent/export_test.go`:

```go
package agent

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/dsandor/memory/internal/storage"
)

func sampleAgent() *storage.Agent {
	return &storage.Agent{
		ID:           "agent-1",
		Domain:       "finance",
		Version:      2,
		Status:       storage.AgentStatusPublished,
		SystemPrompt: "You are a financial analysis expert.",
		Instructions: "Use DCF valuation.\nCite sources.",
		AntiPatterns: "Do not guess earnings.",
		SourceRefs:   []string{"cluster-1"},
		ClusterID:    "cluster-1",
	}
}

func TestExport_MD(t *testing.T) {
	a := sampleAgent()
	out := Export(a, "md")
	if !strings.Contains(out, "---") {
		t.Error("md export should have YAML frontmatter delimiters")
	}
	if !strings.Contains(out, "domain: finance") {
		t.Errorf("md export missing domain in frontmatter, got:\n%s", out)
	}
	if !strings.Contains(out, "You are a financial analysis expert.") {
		t.Error("md export should contain system prompt in body")
	}
}

func TestExport_TXT(t *testing.T) {
	a := sampleAgent()
	out := Export(a, "txt")
	if !strings.Contains(out, "You are a financial analysis expert.") {
		t.Error("txt export should contain system prompt")
	}
	if !strings.Contains(out, "Use DCF valuation.") {
		t.Error("txt export should contain instructions")
	}
}

func TestExport_JSON(t *testing.T) {
	a := sampleAgent()
	out := Export(a, "json")
	var parsed map[string]any
	if err := json.Unmarshal([]byte(out), &parsed); err != nil {
		t.Fatalf("json export is not valid JSON: %v\n%s", err, out)
	}
	if parsed["domain"] != "finance" {
		t.Errorf("json export domain = %v, want finance", parsed["domain"])
	}
	if parsed["version"].(float64) != 2 {
		t.Errorf("json export version = %v, want 2", parsed["version"])
	}
}

func TestExport_UnknownFormat(t *testing.T) {
	a := sampleAgent()
	out := Export(a, "xml")
	if !strings.Contains(out, "unsupported format") {
		t.Errorf("unknown format should return error message, got: %q", out)
	}
}
```

- [ ] **Step 3: Run tests — verify they fail**

```bash
go test ./internal/agent/... 2>&1 | head -15
```

Expected: compilation errors — `Diff` and `Export` not defined.

- [ ] **Step 4: Create internal/agent/diff.go**

```go
package agent

import (
	"fmt"
	"strings"

	"github.com/dsandor/memory/internal/storage"
)

// Diff produces a human-readable changelog string comparing two agent versions.
// If old is nil (first generation), returns "initial generation".
func Diff(old, new *storage.Agent) string {
	if old == nil {
		return "initial generation"
	}
	var changes []string
	if old.SystemPrompt != new.SystemPrompt {
		changes = append(changes, "system_prompt updated")
	}
	if old.Instructions != new.Instructions {
		changes = append(changes, "instructions updated")
	}
	if old.AntiPatterns != new.AntiPatterns {
		changes = append(changes, "anti_patterns updated")
	}
	if len(changes) == 0 {
		return "no changes"
	}
	return fmt.Sprintf("changed: %s", strings.Join(changes, ", "))
}
```

- [ ] **Step 5: Create internal/agent/export.go**

```go
package agent

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/dsandor/memory/internal/storage"
)

// Export serializes an agent to the requested format.
// Supported formats: "md" (YAML frontmatter + markdown body), "txt" (plain text), "json" (full config).
// Returns an error message string for unknown formats.
func Export(a *storage.Agent, format string) string {
	switch format {
	case "md":
		return exportMD(a)
	case "txt":
		return exportTXT(a)
	case "json":
		return exportJSON(a)
	default:
		return fmt.Sprintf("unsupported format %q: use md, txt, or json", format)
	}
}

func exportMD(a *storage.Agent) string {
	var sb strings.Builder
	sb.WriteString("---\n")
	fmt.Fprintf(&sb, "name: %s-agent\n", a.Domain)
	fmt.Fprintf(&sb, "domain: %s\n", a.Domain)
	fmt.Fprintf(&sb, "version: %d\n", a.Version)
	fmt.Fprintf(&sb, "status: %s\n", string(a.Status))
	sb.WriteString("---\n\n")
	sb.WriteString(a.SystemPrompt)
	if a.Instructions != "" {
		sb.WriteString("\n\n## Instructions\n\n")
		sb.WriteString(a.Instructions)
	}
	if a.AntiPatterns != "" {
		sb.WriteString("\n\n## Anti-Patterns\n\n")
		sb.WriteString(a.AntiPatterns)
	}
	return sb.String()
}

func exportTXT(a *storage.Agent) string {
	var sb strings.Builder
	sb.WriteString(a.SystemPrompt)
	if a.Instructions != "" {
		sb.WriteString("\n\nInstructions:\n")
		sb.WriteString(a.Instructions)
	}
	if a.AntiPatterns != "" {
		sb.WriteString("\n\nAvoid:\n")
		sb.WriteString(a.AntiPatterns)
	}
	return sb.String()
}

func exportJSON(a *storage.Agent) string {
	type exportShape struct {
		ID           string   `json:"id"`
		Domain       string   `json:"domain"`
		Version      int      `json:"version"`
		Status       string   `json:"status"`
		SystemPrompt string   `json:"system_prompt"`
		Instructions string   `json:"instructions"`
		AntiPatterns string   `json:"anti_patterns"`
		SourceRefs   []string `json:"source_refs"`
		ClusterID    string   `json:"cluster_id"`
	}
	refs := a.SourceRefs
	if refs == nil {
		refs = []string{}
	}
	data, _ := json.MarshalIndent(exportShape{
		ID:           a.ID,
		Domain:       a.Domain,
		Version:      a.Version,
		Status:       string(a.Status),
		SystemPrompt: a.SystemPrompt,
		Instructions: a.Instructions,
		AntiPatterns: a.AntiPatterns,
		SourceRefs:   refs,
		ClusterID:    a.ClusterID,
	}, "", "  ")
	return string(data)
}
```

- [ ] **Step 6: Run all agent tests**

```bash
go test ./internal/agent/... -v 2>&1 | tail -20
```

Expected: all 11 tests PASS (3 generate + 4 diff + 4 export).

- [ ] **Step 7: Build check**

```bash
CGO_ENABLED=1 go build ./cmd/server/ && echo "ok"
```

Expected: `ok`

- [ ] **Step 8: Commit**

```bash
git add internal/agent/diff.go internal/agent/diff_test.go internal/agent/export.go internal/agent/export_test.go
git commit -m "feat(agent): add diff engine and md/txt/json export formats"
```

---

## Task 4: Pipeline integration — generate agents after clustering

**Files:**
- Modify: `internal/pipeline/pipeline.go`
- Modify: `internal/pipeline/testhelpers_test.go`
- Modify: `internal/pipeline/pipeline_test.go`
- Modify: `internal/config/config.go`
- Modify: `internal/config/config_test.go`

### Background

The pipeline currently discards the cluster ID returned by `StoreCluster`. We fix that to capture it, then optionally call agent generation. The `WithAgentGeneration` method adds the agent store and LLM client without changing the existing `New` signature — the two existing pipeline tests continue to pass unchanged.

- [ ] **Step 1: Add AgentModel to config.go**

In `internal/config/config.go`, add `AgentModel string` to the `Config` struct:

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
}
```

In the `Load()` function's return statement, add the `AgentModel` field:

```go
		AgentModel:         envOrDefault("AGENT_MODEL", "claude-sonnet-4-6"),
```

- [ ] **Step 2: Add config test**

Append to `internal/config/config_test.go` (inside `package config`):

```go
func TestLoad_AgentModelDefault(t *testing.T) {
	t.Setenv("AGENT_MODEL", "")
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.AgentModel != "claude-sonnet-4-6" {
		t.Errorf("AgentModel = %q, want claude-sonnet-4-6", cfg.AgentModel)
	}
}
```

- [ ] **Step 3: Run config tests**

```bash
go test ./internal/config/... -v 2>&1 | tail -10
```

Expected: all config tests PASS including the new one.

- [ ] **Step 4: Add mockAgentStore to testhelpers_test.go**

Append to `internal/pipeline/testhelpers_test.go` (stays in `package pipeline`):

```go
type mockAgentStore struct {
	mockAnalysisStore
	agents        []storage.Agent
	agentVersions []storage.AgentVersion
}

func (m *mockAgentStore) UpsertAgent(_ context.Context, a storage.Agent) (string, error) {
	if a.ID == "" {
		a.ID = "agent-" + a.Domain
	}
	for i, existing := range m.agents {
		if existing.Domain == a.Domain {
			m.agents[i] = a
			return a.ID, nil
		}
	}
	m.agents = append(m.agents, a)
	return a.ID, nil
}

func (m *mockAgentStore) GetAgent(_ context.Context, id string) (*storage.Agent, error) {
	for _, a := range m.agents {
		if a.ID == id {
			return &a, nil
		}
	}
	return nil, nil
}

func (m *mockAgentStore) GetAgentByDomain(_ context.Context, domain string) (*storage.Agent, error) {
	for _, a := range m.agents {
		if a.Domain == domain {
			return &a, nil
		}
	}
	return nil, nil
}

func (m *mockAgentStore) ListAgents(_ context.Context) ([]storage.Agent, error) {
	return m.agents, nil
}

func (m *mockAgentStore) PublishAgent(_ context.Context, _ string) error { return nil }

func (m *mockAgentStore) StoreAgentVersion(_ context.Context, v storage.AgentVersion) error {
	m.agentVersions = append(m.agentVersions, v)
	return nil
}

func (m *mockAgentStore) ListAgentVersions(_ context.Context, agentID string) ([]storage.AgentVersion, error) {
	var out []storage.AgentVersion
	for _, v := range m.agentVersions {
		if v.AgentID == agentID {
			out = append(out, v)
		}
	}
	return out, nil
}
```

- [ ] **Step 5: Replace pipeline.go**

Replace the entire contents of `internal/pipeline/pipeline.go`:

```go
package pipeline

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"github.com/dsandor/memory/internal/agent"
	"github.com/dsandor/memory/internal/llm"
	"github.com/dsandor/memory/internal/storage"
)

// Config controls when and how the pipeline runs.
type Config struct {
	MinEntries       int
	Interval         time.Duration
	ClusterThreshold float64
}

// Pipeline orchestrates the knowledge analysis pipeline.
type Pipeline struct {
	store      storage.AnalysisStore
	agentStore storage.AgentStore
	llm        llm.Client
	agentLLM   llm.Client
	cfg        Config
}

// New creates a new Pipeline. Call WithAgentGeneration to enable agent synthesis.
func New(store storage.AnalysisStore, llmClient llm.Client, cfg Config) *Pipeline {
	return &Pipeline{store: store, llm: llmClient, cfg: cfg}
}

// WithAgentGeneration configures the pipeline to generate agents from clusters.
// agentStore must also implement storage.AnalysisStore (i.e. *storage.SQLiteStore satisfies both).
func (p *Pipeline) WithAgentGeneration(agentStore storage.AgentStore, agentLLM llm.Client) *Pipeline {
	p.agentStore = agentStore
	p.agentLLM = agentLLM
	return p
}

// Start launches the pipeline as a background goroutine until ctx is cancelled.
func (p *Pipeline) Start(ctx context.Context) {
	go func() {
		ticker := time.NewTicker(p.cfg.Interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				count, err := p.store.CountEntries(ctx)
				if err != nil {
					log.Printf("pipeline: count entries: %v", err)
					continue
				}
				if count < p.cfg.MinEntries {
					continue
				}
				if err := p.Run(ctx, "interval"); err != nil {
					log.Printf("pipeline run error: %v", err)
				}
			}
		}
	}()
}

// Run executes a single pipeline pass: cluster → score → summarize → detect gaps → snapshot → generate agents.
func (p *Pipeline) Run(ctx context.Context, trigger string) error {
	prevRun, _ := p.store.GetLatestPipelineRun(ctx)
	var prevRunID string
	if prevRun != nil {
		prevRunID = prevRun.ID
	}

	runID, err := p.store.StartPipelineRun(ctx, trigger)
	if err != nil {
		return fmt.Errorf("start run: %w", err)
	}

	finishCtx := context.Background()

	var runErrs []string
	clustersFound := 0

	entries, err := p.store.ListEntries(ctx, storage.ListFilter{Limit: -1})
	if err != nil {
		runErrs = append(runErrs, fmt.Sprintf("list entries: %v", err))
		return p.store.FinishPipelineRun(finishCtx, runID, "failed", 0, 0, runErrs)
	}

	embeddings, err := p.store.GetAllEmbeddings(ctx)
	if err != nil {
		runErrs = append(runErrs, fmt.Sprintf("get embeddings: %v", err))
		return p.store.FinishPipelineRun(finishCtx, runID, "failed", len(entries), 0, runErrs)
	}

	entryByID := make(map[string]storage.KnowledgeEntry, len(entries))
	domainByID := make(map[string]string, len(entries))
	domainCounts := make(map[string]int)
	for _, e := range entries {
		entryByID[e.ID] = e
		domainByID[e.ID] = e.Domain
		domainCounts[e.Domain]++
	}

	candidates := Cluster(embeddings, domainByID, p.cfg.ClusterThreshold)

	for _, cand := range candidates {
		clusterEntries := make([]storage.KnowledgeEntry, 0, len(cand.EntryIDs))
		for _, id := range cand.EntryIDs {
			if e, ok := entryByID[id]; ok {
				clusterEntries = append(clusterEntries, e)
			}
		}

		summary, err := SummarizeCluster(ctx, p.llm, clusterEntries)
		if err != nil {
			runErrs = append(runErrs, fmt.Sprintf("summarize cluster: %v", err))
			continue
		}

		var totalScore float64
		for _, e := range clusterEntries {
			if score, err := ScoreEntry(ctx, p.llm, e); err == nil {
				totalScore += score.Total
			}
		}
		avgScore := 0.0
		if len(clusterEntries) > 0 {
			avgScore = totalScore / float64(len(clusterEntries))
		}

		cluster := storage.Cluster{
			Domain:        cand.Domain,
			Title:         summary.Title,
			Summary:       summary.Summary,
			EntryIDs:      cand.EntryIDs,
			QualityScore:  avgScore,
			PipelineRunID: runID,
		}
		clusterID, err := p.store.StoreCluster(ctx, cluster)
		if err != nil {
			runErrs = append(runErrs, fmt.Sprintf("store cluster: %v", err))
			continue
		}
		cluster.ID = clusterID
		clustersFound++

		if p.agentStore != nil && p.agentLLM != nil {
			if err := p.generateAgent(ctx, cluster, clusterEntries); err != nil {
				runErrs = append(runErrs, fmt.Sprintf("generate agent for %s: %v", cand.Domain, err))
			}
		}
	}

	if prevRunID != "" {
		if err := p.store.DeleteClustersByRunID(finishCtx, prevRunID); err != nil {
			runErrs = append(runErrs, fmt.Sprintf("delete old clusters: %v", err))
		}
	}

	gaps, err := DetectGaps(ctx, p.llm, domainCounts)
	if err != nil {
		runErrs = append(runErrs, fmt.Sprintf("detect gaps: %v", err))
		gaps = nil
	}

	latest, err := p.store.GetLatestSnapshot(ctx)
	if err != nil {
		runErrs = append(runErrs, fmt.Sprintf("get latest snapshot: %v", err))
		return p.store.FinishPipelineRun(finishCtx, runID, "failed", len(entries), clustersFound, runErrs)
	}
	version := 1
	if latest != nil {
		version = latest.Version + 1
	}

	type snapshotData struct {
		Gaps []DomainGap `json:"gaps"`
	}
	snapDataJSON, _ := json.Marshal(snapshotData{Gaps: gaps})

	snap := storage.DatasetSnapshot{
		Version:       version,
		ClusterCount:  clustersFound,
		EntryCount:    len(entries),
		Data:          string(snapDataJSON),
		PipelineRunID: runID,
	}
	if _, err := p.store.StoreSnapshot(finishCtx, snap); err != nil {
		runErrs = append(runErrs, fmt.Sprintf("store snapshot: %v", err))
	}

	status := "complete"
	if len(runErrs) > 0 {
		status = "complete_with_errors"
	}
	return p.store.FinishPipelineRun(finishCtx, runID, status, len(entries), clustersFound, runErrs)
}

func (p *Pipeline) generateAgent(ctx context.Context, cluster storage.Cluster, entries []storage.KnowledgeEntry) error {
	newAgent, err := agent.Generate(ctx, p.agentLLM, cluster, entries)
	if err != nil {
		return fmt.Errorf("generate: %w", err)
	}

	existing, err := p.agentStore.GetAgentByDomain(ctx, cluster.Domain)
	if err != nil {
		return fmt.Errorf("get existing agent: %w", err)
	}

	changelog := agent.Diff(existing, newAgent)

	if existing != nil {
		newAgent.ID = existing.ID
		newAgent.Version = existing.Version + 1
	}

	id, err := p.agentStore.UpsertAgent(ctx, *newAgent)
	if err != nil {
		return fmt.Errorf("upsert agent: %w", err)
	}

	return p.agentStore.StoreAgentVersion(ctx, storage.AgentVersion{
		AgentID:      id,
		Version:      newAgent.Version,
		SystemPrompt: newAgent.SystemPrompt,
		Instructions: newAgent.Instructions,
		AntiPatterns: newAgent.AntiPatterns,
		Changelog:    changelog,
	})
}
```

- [ ] **Step 6: Add agent generation test to pipeline_test.go**

Append to `internal/pipeline/pipeline_test.go` (stays in `package pipeline`):

```go
func TestPipeline_Run_GeneratesAgentWhenStoreProvided(t *testing.T) {
	baseStore := &mockAnalysisStore{
		entries: []storage.KnowledgeEntry{
			{ID: "a", Title: "Entry A", Content: "Finance pattern", Domain: "finance"},
			{ID: "b", Title: "Entry B", Content: "Finance workflow", Domain: "finance"},
		},
		embeddings: map[string][]float32{
			"a": {1, 0, 0, 0},
			"b": {0.99, 0.14, 0, 0},
		},
	}
	agentStore := &mockAgentStore{mockAnalysisStore: *baseStore}

	// llmMock handles: SummarizeCluster (title+summary), ScoreEntry (coherence+specificity), DetectGaps (gaps).
	llmMock := &mockLLM{response: `{"title":"Finance","summary":"Finance entries.","coherence":0.8,"specificity":0.7,"gaps":[]}`}
	// agentLLMMock handles: agent.Generate (system_prompt+instructions+anti_patterns).
	agentLLMMock := &mockLLM{response: `{"system_prompt":"You are a finance agent.","instructions":"Use DCF.","anti_patterns":"No guessing."}`}

	p := New(baseStore, llmMock, Config{
		MinEntries:       2,
		Interval:         time.Hour,
		ClusterThreshold: 0.9,
	}).WithAgentGeneration(agentStore, agentLLMMock)

	if err := p.Run(context.Background(), "test"); err != nil {
		t.Fatalf("pipeline run: %v", err)
	}

	if len(agentStore.agents) != 1 {
		t.Errorf("want 1 agent generated, got %d", len(agentStore.agents))
	}
	if agentStore.agents[0].Domain != "finance" {
		t.Errorf("agent domain = %q, want finance", agentStore.agents[0].Domain)
	}
	if agentStore.agents[0].SystemPrompt == "" {
		t.Error("agent system_prompt should not be empty")
	}
	if len(agentStore.agentVersions) != 1 {
		t.Errorf("want 1 agent version stored, got %d", len(agentStore.agentVersions))
	}
	if agentStore.agentVersions[0].Changelog != "initial generation" {
		t.Errorf("changelog = %q, want initial generation", agentStore.agentVersions[0].Changelog)
	}
}
```

- [ ] **Step 7: Run all pipeline tests**

```bash
go test ./internal/pipeline/... -v 2>&1 | tail -20
```

Expected: all 4 pipeline tests PASS (3 from Phase 2 + 1 new agent test).

- [ ] **Step 8: Build check**

```bash
CGO_ENABLED=1 go build ./cmd/server/ && echo "ok"
```

Expected: `ok`

- [ ] **Step 9: Commit**

```bash
git add internal/pipeline/pipeline.go internal/pipeline/testhelpers_test.go internal/pipeline/pipeline_test.go internal/config/config.go internal/config/config_test.go
git commit -m "feat(pipeline): generate agents from clusters; add AgentModel config"
```

---

## Task 5: MCP tools, server wiring, and main.go

**Files:**
- Create: `internal/mcp/agent_tools.go`
- Create: `internal/mcp/agent_tools_test.go`
- Modify: `internal/mcp/server.go`
- Modify: `cmd/server/main.go`

### Background

Four tools expose agent management to LLM clients. Tests are in `package mcp_test` (consistent with `tools_test.go` and `analysis_tools_test.go`), which means they can embed the already-defined `mockAnalysisStore` from `analysis_tools_test.go`. The `use_agent` MCP prompt lets clients pull a domain agent's system prompt directly. The server needs `server.WithPromptCapabilities(false)` added to enable prompt support.

- [ ] **Step 1: Write failing tests**

Create `internal/mcp/agent_tools_test.go`:

```go
package mcp_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	internalmcp "github.com/dsandor/memory/internal/mcp"
	"github.com/dsandor/memory/internal/storage"
	mcplib "github.com/mark3labs/mcp-go/mcp"
)

// mockAgentStore embeds mockAnalysisStore (defined in analysis_tools_test.go, same package mcp_test)
// and adds AgentStore methods.
type mockAgentStore struct {
	mockAnalysisStore
	agents        []storage.Agent
	agentVersions []storage.AgentVersion
}

func (m *mockAgentStore) UpsertAgent(_ context.Context, a storage.Agent) (string, error) {
	if a.ID == "" {
		a.ID = "agent-" + a.Domain
	}
	for i, existing := range m.agents {
		if existing.Domain == a.Domain {
			m.agents[i] = a
			return a.ID, nil
		}
	}
	m.agents = append(m.agents, a)
	return a.ID, nil
}

func (m *mockAgentStore) GetAgent(_ context.Context, id string) (*storage.Agent, error) {
	for _, a := range m.agents {
		if a.ID == id {
			return &a, nil
		}
	}
	return nil, nil
}

func (m *mockAgentStore) GetAgentByDomain(_ context.Context, domain string) (*storage.Agent, error) {
	for _, a := range m.agents {
		if a.Domain == domain {
			return &a, nil
		}
	}
	return nil, nil
}

func (m *mockAgentStore) ListAgents(_ context.Context) ([]storage.Agent, error) {
	return m.agents, nil
}

func (m *mockAgentStore) PublishAgent(_ context.Context, id string) error {
	for i, a := range m.agents {
		if a.ID == id {
			m.agents[i].Status = storage.AgentStatusPublished
			return nil
		}
	}
	return nil
}

func (m *mockAgentStore) StoreAgentVersion(_ context.Context, v storage.AgentVersion) error {
	m.agentVersions = append(m.agentVersions, v)
	return nil
}

func (m *mockAgentStore) ListAgentVersions(_ context.Context, agentID string) ([]storage.AgentVersion, error) {
	var out []storage.AgentVersion
	for _, v := range m.agentVersions {
		if v.AgentID == agentID {
			out = append(out, v)
		}
	}
	return out, nil
}

func testAgentStoreWithData() *mockAgentStore {
	return &mockAgentStore{
		agents: []storage.Agent{
			{
				ID: "agent-finance", Domain: "finance", Version: 1,
				Status:       storage.AgentStatusPublished,
				SystemPrompt: "You are a finance agent.",
				Instructions: "Use DCF.",
				AntiPatterns: "No guessing.",
				SourceRefs:   []string{"cluster-1"},
				CreatedAt:    time.Now(),
				UpdatedAt:    time.Now(),
			},
			{
				ID: "agent-legal", Domain: "legal", Version: 1,
				Status:       storage.AgentStatusDraft,
				SystemPrompt: "You are a legal agent.",
				CreatedAt:    time.Now(),
				UpdatedAt:    time.Now(),
			},
		},
	}
}

func TestHandleAgentList(t *testing.T) {
	store := testAgentStoreWithData()
	handler := internalmcp.HandleAgentList(store)
	result, err := handler(context.Background(), mcplib.CallToolRequest{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Errorf("unexpected tool error: %s", textContent(result))
	}
	var agents []map[string]any
	if err := json.Unmarshal([]byte(textContent(result)), &agents); err != nil {
		t.Fatalf("parse agents JSON: %v", err)
	}
	if len(agents) != 2 {
		t.Errorf("want 2 agents, got %d", len(agents))
	}
}

func TestHandleAgentGet_ByID(t *testing.T) {
	store := testAgentStoreWithData()
	handler := internalmcp.HandleAgentGet(store)
	result, err := handler(context.Background(), callReq("id", "agent-finance"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Errorf("unexpected tool error: %s", textContent(result))
	}
	if !strings.Contains(textContent(result), "finance") {
		t.Error("result should contain domain 'finance'")
	}
}

func TestHandleAgentGet_ByDomain(t *testing.T) {
	store := testAgentStoreWithData()
	handler := internalmcp.HandleAgentGet(store)
	result, err := handler(context.Background(), callReq("domain", "legal"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Errorf("unexpected tool error: %s", textContent(result))
	}
	if !strings.Contains(textContent(result), "legal") {
		t.Error("result should contain domain 'legal'")
	}
}

func TestHandleAgentGet_MissingBoth(t *testing.T) {
	store := testAgentStoreWithData()
	handler := internalmcp.HandleAgentGet(store)
	result, err := handler(context.Background(), callReq())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Error("expected IsError=true when neither id nor domain provided")
	}
}

func TestHandleAgentPublish(t *testing.T) {
	store := testAgentStoreWithData()
	handler := internalmcp.HandleAgentPublish(store)
	result, err := handler(context.Background(), callReq("id", "agent-legal"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Errorf("unexpected tool error: %s", textContent(result))
	}
	got, _ := store.GetAgent(context.Background(), "agent-legal")
	if got.Status != storage.AgentStatusPublished {
		t.Errorf("status = %q after publish, want published", got.Status)
	}
}

func TestHandleAgentExport_MD(t *testing.T) {
	store := testAgentStoreWithData()
	handler := internalmcp.HandleAgentExport(store)
	result, err := handler(context.Background(), callReq("id", "agent-finance", "format", "md"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Errorf("unexpected tool error: %s", textContent(result))
	}
	if !strings.Contains(textContent(result), "---") {
		t.Error("md export should contain frontmatter")
	}
}

func TestHandleAgentExport_JSON(t *testing.T) {
	store := testAgentStoreWithData()
	handler := internalmcp.HandleAgentExport(store)
	result, err := handler(context.Background(), callReq("id", "agent-finance", "format", "json"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Errorf("unexpected tool error: %s", textContent(result))
	}
	var parsed map[string]any
	if err := json.Unmarshal([]byte(textContent(result)), &parsed); err != nil {
		t.Fatalf("json export not valid JSON: %v", err)
	}
	if parsed["domain"] != "finance" {
		t.Errorf("domain = %v, want finance", parsed["domain"])
	}
}
```

- [ ] **Step 2: Run tests — verify they fail**

```bash
CGO_ENABLED=1 go test ./internal/mcp/... 2>&1 | head -15
```

Expected: compilation errors — `HandleAgentList`, `HandleAgentGet`, `HandleAgentPublish`, `HandleAgentExport` not defined.

- [ ] **Step 3: Create internal/mcp/agent_tools.go**

```go
package mcp

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/dsandor/memory/internal/agent"
	"github.com/dsandor/memory/internal/storage"
	mcplib "github.com/mark3labs/mcp-go/mcp"
)

// HandleAgentList returns a handler that lists all generated agents.
func HandleAgentList(store storage.AgentStore) func(context.Context, mcplib.CallToolRequest) (*mcplib.CallToolResult, error) {
	return func(ctx context.Context, req mcplib.CallToolRequest) (*mcplib.CallToolResult, error) {
		agents, err := store.ListAgents(ctx)
		if err != nil {
			return mcplib.NewToolResultError(fmt.Sprintf("list agents: %v", err)), nil
		}
		if agents == nil {
			agents = []storage.Agent{}
		}
		data, _ := json.Marshal(agents)
		return mcplib.NewToolResultText(string(data)), nil
	}
}

// HandleAgentGet returns a handler that retrieves an agent by id or domain.
func HandleAgentGet(store storage.AgentStore) func(context.Context, mcplib.CallToolRequest) (*mcplib.CallToolResult, error) {
	return func(ctx context.Context, req mcplib.CallToolRequest) (*mcplib.CallToolResult, error) {
		id := req.GetString("id", "")
		domain := req.GetString("domain", "")
		if id == "" && domain == "" {
			return mcplib.NewToolResultError("provide either id or domain"), nil
		}

		var a *storage.Agent
		var err error
		if id != "" {
			a, err = store.GetAgent(ctx, id)
		} else {
			a, err = store.GetAgentByDomain(ctx, domain)
		}
		if err != nil {
			return mcplib.NewToolResultError(fmt.Sprintf("get agent: %v", err)), nil
		}
		if a == nil {
			return mcplib.NewToolResultError("agent not found"), nil
		}

		data, _ := json.Marshal(a)
		return mcplib.NewToolResultText(string(data)), nil
	}
}

// HandleAgentPublish returns a handler that approves a draft agent (sets status = published).
func HandleAgentPublish(store storage.AgentStore) func(context.Context, mcplib.CallToolRequest) (*mcplib.CallToolResult, error) {
	return func(ctx context.Context, req mcplib.CallToolRequest) (*mcplib.CallToolResult, error) {
		id := req.GetString("id", "")
		if id == "" {
			return mcplib.NewToolResultError("id is required"), nil
		}
		if err := store.PublishAgent(ctx, id); err != nil {
			return mcplib.NewToolResultError(fmt.Sprintf("publish agent: %v", err)), nil
		}
		return mcplib.NewToolResultText(fmt.Sprintf("agent %s published", id)), nil
	}
}

// HandleAgentExport returns a handler that exports an agent in md, txt, or json format.
func HandleAgentExport(store storage.AgentStore) func(context.Context, mcplib.CallToolRequest) (*mcplib.CallToolResult, error) {
	return func(ctx context.Context, req mcplib.CallToolRequest) (*mcplib.CallToolResult, error) {
		id := req.GetString("id", "")
		domain := req.GetString("domain", "")
		format := req.GetString("format", "md")

		if id == "" && domain == "" {
			return mcplib.NewToolResultError("provide either id or domain"), nil
		}

		var a *storage.Agent
		var err error
		if id != "" {
			a, err = store.GetAgent(ctx, id)
		} else {
			a, err = store.GetAgentByDomain(ctx, domain)
		}
		if err != nil || a == nil {
			return mcplib.NewToolResultError("agent not found"), nil
		}

		return mcplib.NewToolResultText(agent.Export(a, format)), nil
	}
}
```

- [ ] **Step 4: Add RegisterAgentTools to server.go**

Add `"context"` and `"fmt"` to the import block in `internal/mcp/server.go` if not already present.

Then add this function after `RegisterRuleTools` in `internal/mcp/server.go`:

```go
// RegisterAgentTools adds agent management tools and a use_agent prompt to an existing MCP server.
func RegisterAgentTools(s *server.MCPServer, store storage.AgentStore) {
	s.AddTool(
		mcplib.NewTool("agent_list",
			mcplib.WithDescription("List all AI agents generated from knowledge clusters, with their domain, version, and status"),
		),
		HandleAgentList(store),
	)
	s.AddTool(
		mcplib.NewTool("agent_get",
			mcplib.WithDescription("Get a specific agent by id or domain name"),
			mcplib.WithString("id", mcplib.Description("Agent UUID (optional if domain provided)")),
			mcplib.WithString("domain", mcplib.Description("Domain name, e.g. finance (optional if id provided)")),
		),
		HandleAgentGet(store),
	)
	s.AddTool(
		mcplib.NewTool("agent_publish",
			mcplib.WithDescription("Approve a draft agent — sets its status to published so it can be served to clients"),
			mcplib.WithString("id", mcplib.Required(), mcplib.Description("Agent UUID to publish")),
		),
		HandleAgentPublish(store),
	)
	s.AddTool(
		mcplib.NewTool("agent_export",
			mcplib.WithDescription("Export an agent as a Claude subagent .md file, plain text .txt, or structured .json"),
			mcplib.WithString("id", mcplib.Description("Agent UUID (optional if domain provided)")),
			mcplib.WithString("domain", mcplib.Description("Domain name (optional if id provided)")),
			mcplib.WithString("format", mcplib.Description("Export format: md, txt, or json (default: md)")),
		),
		HandleAgentExport(store),
	)

	s.AddPrompt(
		mcplib.NewPrompt("use_agent",
			mcplib.WithPromptDescription("Get the system prompt for a domain's published agent to use as context"),
			mcplib.WithArgument("domain",
				mcplib.ArgumentDescription("Domain name, e.g. finance, legal, engineering"),
				mcplib.RequiredArgument(),
			),
		),
		func(ctx context.Context, req mcplib.GetPromptRequest) (*mcplib.GetPromptResult, error) {
			domain := req.Params.Arguments["domain"]
			a, err := store.GetAgentByDomain(ctx, domain)
			if err != nil || a == nil {
				msg := fmt.Sprintf("No agent found for domain: %s", domain)
				return &mcplib.GetPromptResult{
					Description: msg,
					Messages: []mcplib.PromptMessage{
						{Role: mcplib.RoleUser, Content: mcplib.TextContent{Type: "text", Text: msg}},
					},
				}, nil
			}
			return &mcplib.GetPromptResult{
				Description: fmt.Sprintf("%s domain agent v%d (%s)", a.Domain, a.Version, string(a.Status)),
				Messages: []mcplib.PromptMessage{
					{Role: mcplib.RoleUser, Content: mcplib.TextContent{Type: "text", Text: a.SystemPrompt}},
				},
			}, nil
		},
	)
}
```

Also update `NewMCPServer` in `server.go` to add `server.WithPromptCapabilities(false)` alongside the existing `server.WithToolCapabilities(false)`:

```go
func NewMCPServer(store storage.Store, embedder embedding.Embedder) *server.MCPServer {
	s := server.NewMCPServer(
		"tribal-knowledge",
		"0.1.0",
		server.WithToolCapabilities(false),
		server.WithPromptCapabilities(false),
	)
	// ... rest unchanged
```

- [ ] **Step 5: Run MCP tests**

```bash
CGO_ENABLED=1 go test ./internal/mcp/... -v 2>&1 | tail -25
```

Expected: all tests PASS — existing tests plus 7 new agent tool tests.

If the `s.AddPrompt` call or `mcplib.GetPromptRequest` / `mcplib.PromptMessage` types fail to compile, remove the `s.AddPrompt` block from `RegisterAgentTools` (the four tools are the critical deliverables). Note which mcp-go types are missing for Phase 4.

- [ ] **Step 6: Update cmd/server/main.go**

Replace `cmd/server/main.go` with:

```go
package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/dsandor/memory/internal/config"
	"github.com/dsandor/memory/internal/embedding"
	"github.com/dsandor/memory/internal/llm"
	internalmcp "github.com/dsandor/memory/internal/mcp"
	"github.com/dsandor/memory/internal/pipeline"
	"github.com/dsandor/memory/internal/storage"
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

	embedder := embedding.NewOllamaEmbedder(cfg.OllamaURL, cfg.OllamaModel)

	mcpServer := internalmcp.NewMCPServer(store, embedder)
	internalmcp.RegisterAnalysisTools(mcpServer, store)
	internalmcp.RegisterRuleTools(mcpServer, store)
	internalmcp.RegisterAgentTools(mcpServer, store)

	if cfg.AnthropicAPIKey != "" {
		llmClient := llm.NewAnthropicClient(cfg.AnthropicAPIKey, cfg.AnthropicModel)
		agentLLMClient := llm.NewAnthropicClient(cfg.AnthropicAPIKey, cfg.AgentModel)

		p := pipeline.New(store, llmClient, pipeline.Config{
			MinEntries:       cfg.PipelineMinEntries,
			Interval:         cfg.PipelineInterval,
			ClusterThreshold: cfg.ClusterThreshold,
		}).WithAgentGeneration(store, agentLLMClient)

		ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer cancel()
		p.Start(ctx)
	}

	if err := server.ServeStdio(mcpServer); err != nil {
		log.Printf("serve: %v", err)
	}
}
```

- [ ] **Step 7: Build the complete binary**

```bash
CGO_ENABLED=1 go build -o tribal-knowledge ./cmd/server/ && echo "build ok"
```

Expected: `build ok`

- [ ] **Step 8: Run all tests**

```bash
CGO_ENABLED=1 go test ./... 2>&1 | tail -20
```

Expected: all packages PASS. Approximate count: storage (~20), config (~7), llm (3), pipeline (4), agent (11), mcp (~17) = ~62 tests.

- [ ] **Step 9: Cleanup build artifact**

```bash
rm tribal-knowledge
```

- [ ] **Step 10: Commit**

```bash
git add internal/mcp/agent_tools.go internal/mcp/agent_tools_test.go internal/mcp/server.go cmd/server/main.go
git commit -m "feat: add agent MCP tools, use_agent prompt, and Phase 3 server wiring"
```

---

## Self-Review

### Spec coverage

| Requirement | Task |
|-------------|------|
| Agent generation: LLM → system prompt synthesis (claude-sonnet-4-6) | Task 2 (generate.go) + Task 4 (AgentModel config, agentLLMClient) |
| Agent schema: ID, Version, Domain, SystemPrompt, Instructions, AntiPatterns, SourceRefs, Status, ChangeLog | Task 1 (agents.go Agent + AgentVersion types) |
| Agent versioning: monotonic versions, full history preserved | Task 1 (agent_versions table) + Task 4 (generateAgent increments version, calls StoreAgentVersion) |
| Diff engine: compare versions, human-readable changelog | Task 3 (diff.go Diff function) |
| Draft / published states: curators approve | Task 1 (AgentStatus) + Task 5 (agent_publish tool) |
| Export: Claude subagent .md (YAML frontmatter + system prompt) | Task 3 (export.go exportMD) |
| Export: System prompt .txt (plain text) | Task 3 (export.go exportTXT) |
| Export: .json (full structured config with source refs) | Task 3 (export.go exportJSON) |
| Schema additions: agents, agent_versions | Task 1 (migrate in sqlite.go) |
| MCP tools: agent_get, agent_list | Task 5 (agent_tools.go + RegisterAgentTools) |
| MCP tools: agent_publish, agent_export | Task 5 (agent_tools.go + RegisterAgentTools) |
| MCP resources: agents://generated, agents://domain/{name} | **Deferred to Phase 4** — mcp-go resource template API requires verification against v0.54.1; tools + prompt cover the same data. |
| MCP prompt: use_agent/{domain} | Task 5 (RegisterAgentTools s.AddPrompt) |

### Placeholder scan

No TBD, TODO, or incomplete steps. Every step contains the actual code an engineer needs.

### Type consistency

- `storage.Agent` and `storage.AgentVersion` defined in Task 1 `agents.go`; used in Tasks 2, 3, 4, 5
- `storage.AgentStore` interface defined in Task 1; implemented on `*SQLiteStore` in Task 1 (verified by `var _ AgentStore = (*SQLiteStore)(nil)`); consumed in Tasks 4 and 5
- `agent.Generate(ctx context.Context, client llm.Client, cluster storage.Cluster, entries []storage.KnowledgeEntry) (*storage.Agent, error)` — defined in Task 2; called in Task 4's `generateAgent`
- `agent.Diff(old, new *storage.Agent) string` — defined in Task 3; called in Task 4's `generateAgent`
- `agent.Export(a *storage.Agent, format string) string` — defined in Task 3; called in Task 5's `HandleAgentExport`
- `pipeline.Pipeline.WithAgentGeneration(agentStore storage.AgentStore, agentLLM llm.Client) *Pipeline` — defined in Task 4; called in Task 5's `main.go`
- `config.Config.AgentModel` — added in Task 4; read in Task 5's `main.go` as `cfg.AgentModel`
- `storage.AgentStatusDraft` / `storage.AgentStatusPublished` — defined in Task 1; used in generate (Task 2), test data (Tasks 4, 5)
- `mockAgentStore` in `pipeline/testhelpers_test.go` embeds `mockAnalysisStore` (same file); `mockAgentStore` in `mcp/agent_tools_test.go` embeds `mockAnalysisStore` from `analysis_tools_test.go` (same `package mcp_test`)

---

## Exit Criteria Verification

After completing all 5 tasks:

1. `CGO_ENABLED=1 go build -o tribal-knowledge ./cmd/server/ && echo "build ok"` → `build ok`
2. `CGO_ENABLED=1 go test ./... 2>&1 | tail -20` → all packages PASS
3. Set `ANTHROPIC_API_KEY`, `PIPELINE_MIN_ENTRIES=3`, `PIPELINE_INTERVAL=1m`; store 3+ entries via `knowledge_store`
4. After 1 minute, pipeline fires and generates agents
5. Call `agent_list` → returns JSON array with `Domain`, `Version`, `Status` fields
6. Call `agent_get` with `domain=<domain>` → returns agent JSON
7. Call `agent_export` with `id=<id>` and `format=md` → returns YAML-frontmatter markdown
8. Call `agent_publish` with `id=<id>` → response says agent published; subsequent `agent_get` shows `Status: published`
9. Use `use_agent` MCP prompt with `domain=<domain>` → returns the agent's system prompt as a message
