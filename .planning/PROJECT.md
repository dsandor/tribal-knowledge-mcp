# Tribal Knowledge MCP Server

## Vision

A general-purpose Model Context Protocol server — built as a single Go binary — that captures team tribal knowledge, continuously analyzes and reorganizes it, and generates evolving specialized AI agents from it. Teams gain a self-improving knowledge engine: the more knowledge stored, the better and more specialized the agents become. A built-in web UI provides full visibility into stored knowledge, processed datasets, and downloadable agents.

**Pilot use case:** Financial analyst teams using LLMs to produce stock/earnings reports.

**General applicability:** Any team where LLM-assisted work benefits from shared prompt patterns, domain knowledge, and specialized agent personas.

---

## Problem Statement

When teams use LLMs, output quality is highly dependent on individual prompting skill. That institutional knowledge lives in individuals' heads and never gets shared. Additionally, even when knowledge is captured, it sits inert — no system synthesizes it into reusable agent configurations that the whole team can deploy.

The result:
- Inconsistent output quality across team members
- Duplicated effort reinventing prompt patterns
- Knowledge walks out the door when people leave
- No mechanism for the team's collective intelligence to compound over time
- No way to operationalize learned patterns as deployable agents

---

## Goals

1. **Capture** — Store prompt patterns, workflows, domain facts, and anti-patterns
2. **Analyze** — Continuously cluster, deduplicate, score, and reorganize knowledge
3. **Generate Agents** — Synthesize knowledge clusters into specialized AI agent configurations
4. **Evolve** — Agents improve automatically as more knowledge is stored and rated
5. **Specialize** — Different agents for different categories of work, generated from the knowledge taxonomy
6. **Expose** — Web UI (embedded in the Go binary) for browsing knowledge, datasets, agent history, and downloads
7. **Serve via MCP** — All capabilities available as MCP tools/resources/prompts for LLM clients

---

## Non-Goals (v1)

- Cross-organization knowledge federation
- Real-time collaborative editing of knowledge
- Fine-tuning or training neural networks on team data
- Workflow automation / agent orchestration at runtime
- Authentication / SSO (basic team auth only in v1)

---

## Architecture Overview

```
┌─────────────────────────────────────────────────────────────┐
│                        MCP Clients                          │
│           (Claude, Cursor, Zed, Custom Apps)                │
└───────────────────────────┬─────────────────────────────────┘
                            │ MCP Protocol (stdio / HTTP-SSE)
┌───────────────────────────▼─────────────────────────────────┐
│                  Go Binary (Single Artifact)                 │
│                                                             │
│  ┌──────────────────────┐  ┌────────────────────────────┐  │
│  │    MCP Server        │  │    Web UI Server           │  │
│  │  tools/resources/    │  │  embedded React + REST API │  │
│  │  prompts             │  │  (go:embed)                │  │
│  └──────────┬───────────┘  └──────────────┬─────────────┘  │
│             └──────────────┬──────────────┘               │
│  ┌──────────────────────────▼──────────────────────────┐  │
│  │               Knowledge API Layer                    │  │
│  │        CRUD · Search · Enhance · Rate · Export      │  │
│  └──────────────────────────┬──────────────────────────┘  │
│                             │                              │
│  ┌──────────────────────────▼──────────────────────────┐  │
│  │          Knowledge Analysis Pipeline                 │  │
│  │   Cluster · Summarize · Deduplicate · Score · Tag  │  │
│  │          (background goroutines + LLM calls)        │  │
│  └──────────────────────────┬──────────────────────────┘  │
│                             │                              │
│  ┌──────────────────────────▼──────────────────────────┐  │
│  │            Agent Generation Engine                   │  │
│  │   Generate · Version · Specialize · Export · Serve  │  │
│  │     (one agent per knowledge cluster / category)    │  │
│  └──────────────────────────┬──────────────────────────┘  │
│                             │                              │
│  ┌──────────────────────────▼──────────────────────────┐  │
│  │                  Storage Layer                       │  │
│  │    SQLite + sqlite-vec (v1) · PostgreSQL + pgvector  │  │
│  │    Entries · Embeddings · Clusters · Agent Versions │  │
│  └──────────────────────────┬──────────────────────────┘  │
└─────────────────────────────┼────────────────────────────┘
                              │
               ┌──────────────▼──────────────┐
               │    LLM API (Anthropic /     │
               │    OpenAI) — Analysis +     │
               │    Agent Generation         │
               └─────────────────────────────┘
```

---

## Components

### 1. MCP Server

Implements MCP protocol over stdio (primary) and HTTP-SSE (for remote clients).

**Tools:**
- `knowledge_store` — Save a knowledge entry with type, domain, tags
- `knowledge_search` — Semantic search with filters (type, domain, rating, cluster)
- `knowledge_get` — Retrieve entry by ID
- `knowledge_list` — Browse with filters and sort
- `knowledge_rate` — Rate entry 1–5 with optional feedback
- `knowledge_delete` — Remove entry (author / admin)
- `prompt_enhance` — Improve draft prompt using retrieved team knowledge
- `prompt_suggest` — Get prompt suggestions for a described task
- `agent_get` — Retrieve a generated agent by ID or domain
- `agent_list` — List all generated agents with version history
- `cluster_list` — List current knowledge clusters
- `analysis_status` — Get status of the analysis pipeline

**Resources:**
- `knowledge://team/top` — Top-rated entries
- `knowledge://team/recent` — Recently added entries
- `knowledge://domain/{name}` — Entries by domain
- `knowledge://cluster/{id}` — Entries in a specific knowledge cluster
- `agents://generated` — All current agent definitions
- `agents://domain/{name}` — Agent for a specific domain

**Prompts:**
- `enhance_with_context` — Meta-prompt wrapping user input with retrieved knowledge
- `use_agent/{domain}` — Apply a generated domain-specialized agent

---

### 2. Knowledge Entry Schema

```go
type KnowledgeEntry struct {
    ID          string          // UUID
    Type        KnowledgeType   // prompt | pattern | workflow | domain_fact | anti_pattern
    Title       string
    Content     string          // Embedded for semantic search
    Description string          // When/why to use this
    Domain      string          // User-defined domain tag
    Tags        []string
    ClusterID   *string         // Assigned by analysis pipeline
    Author      UserRef
    Team        string
    Rating      RatingSummary   // avg, count, distribution
    UsageCount  int
    Quality     float64         // 0–1, scored by analysis pipeline
    CreatedAt   time.Time
    UpdatedAt   time.Time
    Version     int             // incremented when content is improved by pipeline
}

type KnowledgeType string
const (
    KTPrompt      KnowledgeType = "prompt"
    KTPattern     KnowledgeType = "pattern"
    KTWorkflow    KnowledgeType = "workflow"
    KTDomainFact  KnowledgeType = "domain_fact"
    KTAntiPattern KnowledgeType = "anti_pattern"
)
```

---

### 3. Knowledge Analysis Pipeline

Runs as background goroutines on a configurable schedule (default: after every N new entries, or on a time interval).

**Stages:**

```
[Raw Entries] → Embed → Cluster → Deduplicate → Score → Summarize → Tag → [Processed Dataset]
```

1. **Embedding** — Generate vector embeddings for all un-embedded entries
2. **Clustering** — Group semantically similar entries (k-means or HDBSCAN over embeddings)
3. **Deduplication** — Flag near-duplicate entries; merge with LLM assistance
4. **Quality Scoring** — Score each entry: (rating × usage_count) + coherence + specificity
5. **Summarization** — LLM produces a canonical summary per cluster (the distilled knowledge)
6. **Auto-tagging** — LLM suggests improved domain/tag assignments
7. **Gap Detection** — Identify domains with thin coverage; surface as suggestions to team

Each pipeline run is versioned. The UI shows before/after diffs.

---

### 4. Agent Generation Engine

Triggered after each analysis pipeline run. Produces one or more specialized agent definitions per knowledge cluster/domain.

**Agent Definition:**
```go
type Agent struct {
    ID          string
    Version     int
    Domain      string          // Which cluster/domain this agent covers
    Name        string          // Human-readable: "Earnings Analysis Agent v3"
    SystemPrompt string         // Generated system prompt
    Context     []KnowledgeRef  // Knowledge entries baked into this agent
    Instructions []string       // Distilled do's and don'ts
    AntiPatterns []string       // What to avoid (from anti_pattern entries)
    Tags        []string
    GeneratedAt time.Time
    BaseClusterID string        // Source cluster
    ChangeLog   []string        // What changed from previous version
    ExportFormats []ExportFormat // claude-subagent | system-prompt | json
}
```

**Generation flow:**
1. For each knowledge cluster with sufficient entries (configurable threshold):
   - Retrieve cluster summary + top-rated entries + anti-patterns
   - Call LLM to synthesize a specialized agent system prompt
   - Diff against previous version; record changelog
   - Persist new agent version
2. Cross-domain agents for clusters that span multiple domains
3. Mark agents as `draft` until curator approval (optional config)

**Export formats:**
- **Claude subagent** — Markdown file with YAML frontmatter compatible with `.claude/agents/`
- **System prompt** — Plain text system prompt for any LLM
- **JSON** — Full structured agent config with all knowledge refs

---

### 5. Web UI (Embedded in Go Binary)

Built as a React SPA, compiled to static assets, embedded via `//go:embed`.

**Pages / Views:**

| View | Description |
|------|-------------|
| Dashboard | Overview: entry count, cluster count, agent count, pipeline status |
| Knowledge Browser | Browse/search/filter all entries; view detail; rate; edit |
| Clusters | Visual cluster map; entries per cluster; cluster summary |
| Datasets | Versioned processed datasets; before/after pipeline diffs |
| Agents | List all generated agents; version history; changelog; download |
| Agent Detail | Full agent definition; source knowledge; diff from prior version |
| Analytics | Usage heatmaps; top entries; domain coverage gaps; contribution leaderboard |
| Settings | Team config; domain taxonomy; pipeline schedule; embedding provider |

**Download capabilities:**
- Single agent export (Claude subagent .md, system prompt .txt, .json)
- Bulk agent export (zip)
- Full knowledge dataset export (JSON/CSV)
- Cluster summary export

---

### 6. REST API (served by Go binary)

Backs the web UI. Internal to the binary — not part of the MCP surface.

```
GET  /api/knowledge              list entries (paginated, filtered)
POST /api/knowledge              create entry
GET  /api/knowledge/:id          get entry
PUT  /api/knowledge/:id          update entry
DELETE /api/knowledge/:id        delete entry
POST /api/knowledge/:id/rate     rate entry

GET  /api/clusters               list clusters
GET  /api/clusters/:id           get cluster with entries
GET  /api/clusters/:id/summary   get LLM-generated cluster summary

GET  /api/agents                 list agents (all versions)
GET  /api/agents/:id             get agent
GET  /api/agents/:id/download    download agent in specified format
GET  /api/agents/:domain/latest  get latest agent for a domain

GET  /api/pipeline/status        pipeline run history and current status
POST /api/pipeline/trigger       manually trigger analysis run

GET  /api/analytics/usage        usage stats
GET  /api/analytics/gaps         coverage gap analysis
GET  /api/analytics/contributions user contribution stats
```

---

## Tech Stack

| Layer | Technology | Rationale |
|-------|-----------|-----------|
| Runtime | Go | Single binary deployment; excellent concurrency for pipeline; `go:embed` for UI |
| MCP Protocol | Go MCP SDK (mark3labs/mcp-go) | Most mature Go MCP implementation |
| Storage (v1) | SQLite + go-sqlite + sqlite-vec | Zero external deps; embeddable |
| Storage (v2) | PostgreSQL + pgvector | Production scale |
| Embeddings | Pluggable (Ollama / OpenAI) | Provider-agnostic; env-configured |
| LLM (analysis) | Anthropic API (claude-haiku-4-5 for analysis, sonnet for agent gen) | Cost-efficient at scale |
| Frontend | React + TypeScript + Vite | Fast builds; strong TS ecosystem |
| UI Component | shadcn/ui + Tailwind | Rapid, consistent UI |
| Build | Vite → `//go:embed dist/` | Static assets embedded in binary |
| Testing | Go `testing` + Vitest | Native test tools per layer |
| Config | env vars + JSON schema | 12-factor; validated on startup |

---

## Data Flows

### Store Knowledge
```
Client → knowledge_store(title, content, type, domain, tags)
  → Validate schema
  → Generate embedding
  → Persist to SQLite
  → Schedule pipeline run (if threshold met)
  → Return entry ID
```

### Analysis Pipeline Run
```
Trigger (scheduled or manual)
  → Embed all un-embedded entries
  → Cluster entries (cosine similarity grouping)
  → Deduplicate near-duplicates (LLM merge)
  → Score entries (rating + usage + coherence)
  → Summarize each cluster (LLM)
  → Auto-tag entries (LLM)
  → Detect coverage gaps
  → Persist versioned processed dataset
  → Trigger agent generation
```

### Agent Generation
```
For each cluster with >= threshold entries:
  → Load cluster summary + top entries + anti-patterns
  → LLM call: synthesize specialized system prompt
  → Diff against previous agent version
  → Record changelog
  → Persist new agent version (draft or published)
  → Notify via MCP resource update
```

### Prompt Enhancement
```
Client → prompt_enhance(draft_prompt, domain?)
  → Semantic search knowledge base
  → Retrieve top-K relevant entries
  → Also fetch relevant domain agent if exists
  → Assemble context block
  → Return: original + knowledge + agent context + enhanced prompt
```

---

## Requirements

### Validated
(None yet — ship to validate)

### Active

**MCP Layer**
- [ ] MCP server over stdio transport
- [ ] HTTP-SSE transport for remote clients
- [ ] 12 tools: knowledge CRUD, search, enhance, suggest, agent_get/list, cluster_list, analysis_status
- [ ] 6 resources: top, recent, domain, cluster, agents/generated, agents/domain
- [ ] 2 prompt templates: enhance_with_context, use_agent/{domain}

**Storage**
- [ ] SQLite + sqlite-vec; schema: entries, embeddings, clusters, agents, agent_versions, ratings, users, teams
- [ ] Embedding generation on store
- [ ] Hybrid search: vector similarity + keyword BM25

**Analysis Pipeline**
- [ ] Background goroutine pipeline with configurable trigger (count threshold + time interval)
- [ ] Clustering via cosine similarity grouping
- [ ] LLM-assisted deduplication and merging
- [ ] Quality scoring: rating × usage × coherence
- [ ] LLM cluster summarization
- [ ] Auto-tagging
- [ ] Coverage gap detection
- [ ] Versioned dataset snapshots

**Agent Generation**
- [ ] LLM-generated system prompt per knowledge cluster
- [ ] Agent versioning with changelog
- [ ] Cross-domain agent generation
- [ ] Export: Claude subagent .md, system prompt .txt, JSON
- [ ] Draft / published agent states

**Web UI**
- [ ] React SPA embedded in Go binary via `//go:embed`
- [ ] Dashboard, Knowledge Browser, Clusters, Datasets, Agents, Analytics, Settings views
- [ ] Agent detail with version diff viewer
- [ ] Single and bulk agent download
- [ ] Knowledge dataset export (JSON/CSV)
- [ ] Pipeline status and manual trigger

**REST API**
- [ ] Full CRUD for knowledge entries
- [ ] Cluster and agent endpoints
- [ ] Pipeline status and trigger endpoint
- [ ] Analytics endpoints (usage, gaps, contributions)
- [ ] Download endpoint with format selection

### Out of Scope (v1)

- Cross-team / cross-org knowledge federation — trust model TBD
- SSO / OAuth — basic team auth for v1
- Fine-tuning or neural training on team data
- Real-time push notifications (polling or manual refresh in UI)
- Workflow automation / agent runtime orchestration

---

## Key Decisions

| Decision | Rationale | Outcome |
|----------|-----------|---------|
| Go binary (not TypeScript) | Single deployable artifact; `go:embed` for UI; excellent concurrency for pipeline | Adopted |
| Embedded React UI | No separate deploy; ships with binary; browsable immediately on launch | Adopted |
| LLM for analysis + agent gen | Knowledge synthesis and agent generation require generative capability | Adopted |
| SQLite + sqlite-vec for v1 | Zero-dependency deploy; trivially upgradeable to Postgres | Adopted |
| No LLM calls on MCP path | Enhancement is retrieval-based; LLM is only used in background pipeline | Adopted |
| Versioned agents | Teams need to understand how agents evolved and what changed | Adopted |
| Draft/published agent states | Curators approve before agents are served via MCP | Adopted |
| General-purpose | Financial analysts are v1 pilot — not the product identity | Adopted |

---

## Phases

### Phase 1 — Core MCP + Storage
Go binary scaffold, MCP server, knowledge CRUD tools, SQLite + sqlite-vec, embedding service, basic semantic search.

### Phase 2 — Knowledge Analysis Pipeline
Background clustering, LLM deduplication, quality scoring, LLM summarization, auto-tagging, gap detection, versioned datasets.

### Phase 3 — Agent Generation Engine
LLM agent synthesis per cluster, versioning, changelog, export formats (Claude subagent, system prompt, JSON), draft/published states.

### Phase 4 — Web UI (Embedded)
React SPA: Dashboard, Knowledge Browser, Clusters, Datasets, Agents (with version diffs), download flows. Vite build → `go:embed`.

### Phase 5 — REST API + Analytics
Full REST API backing the UI. Usage analytics, contribution stats, coverage gap view. Pipeline status and manual trigger. Settings page.

### Phase 6 — Polish & Developer Experience
Additional MCP transport (HTTP-SSE), health checks, structured logging, Docker, seed data, onboarding wizard, full documentation.

---

*Last updated: 2026-06-05 — expanded to include knowledge analysis pipeline, agent generation engine, and embedded Go web UI*
