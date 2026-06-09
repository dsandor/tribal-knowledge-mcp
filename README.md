# Tribal Knowledge MCP Server

A general-purpose tribal knowledge server for teams that use LLMs. It captures what your team knows — prompt templates, checklists, best practices, debugging techniques — clusters it by domain, and continuously improves it through analysis. The result is a shared knowledge base accessible to any MCP-compatible AI client.

**Motivation:** Teams using LLMs often see wildly different output quality between members. High-performers have discovered prompt patterns that low-performers haven't found yet. This server lets teams share, rate, and refine those patterns so everyone benefits from the best approaches — applicable to any team using LLMs, not just financial analysts.

---

## Table of Contents

- [Architecture Overview](#architecture-overview)
- [For Developers](#for-developers)
  - [Prerequisites](#prerequisites)
  - [Option A: SQLite (simplest)](#option-a-sqlite-simplest)
  - [Option B: PostgreSQL via run.sh (MCP dev)](#option-b-postgresql-via-runsh-mcp-dev)
  - [Option C: Full stack with Docker Compose](#option-c-full-stack-with-docker-compose)
  - [Seeding Example Data](#seeding-example-data)
  - [Running Tests](#running-tests)
- [Deploying to a Server](#deploying-to-a-server)
  - [Docker Compose (recommended)](#docker-compose-recommended)
  - [Behind a Reverse Proxy](#behind-a-reverse-proxy)
  - [Environment Variable Reference](#environment-variable-reference)
- [Connecting MCP Clients](#connecting-mcp-clients)
  - [Claude Desktop (stdio)](#claude-desktop-stdio)
  - [Cursor](#cursor)
  - [Remote Clients (HTTP/SSE)](#remote-clients-httpsse)
- [First-Time Setup](#first-time-setup)
- [MCP Tools Reference](#mcp-tools-reference)
- [Building and Publishing Images](#building-and-publishing-images)
- [License](#license)

---

## Architecture Overview

```
┌──────────────────────────────────────────────────────┐
│                   MCP Clients                         │
│   Claude Desktop  /  Cursor / Zed / Remote SSE       │
└───────────────┬──────────────────────────────────────┘
                │ stdio or HTTP/SSE
┌───────────────▼──────────────────────────────────────┐
│                 server binary                         │
│  ┌────────────┐  ┌──────────────┐  ┌──────────────┐  │
│  │  MCP Layer │  │ REST HTTP API│  │ Embedded SPA │  │
│  └────────────┘  └──────────────┘  └──────────────┘  │
│  ┌────────────────────────────────────────────────┐   │
│  │            Analysis Pipeline (goroutine)        │   │
│  │   cluster → score → summarize → generate agent │   │
│  └────────────────────────────────────────────────┘   │
│  ┌────────────────────────────────────────────────┐   │
│  │     Storage — SQLite+sqlite-vec or PostgreSQL   │   │
│  └────────────────────────────────────────────────┘   │
└──────────────────────────────────────────────────────┘
         │ embeddings          │ LLM calls
┌────────▼──────┐    ┌─────────▼──────────┐
│  Ollama       │    │  Anthropic API      │
│  (local/sidecar)   │  claude-haiku/sonnet│
└───────────────┘    └────────────────────┘
```

The server is a single Go binary that embeds the React SPA. Storage is selectable at startup: SQLite (default, zero config) or PostgreSQL (set `DATABASE_URL`). The analysis pipeline runs as a background goroutine.

---

## For Developers

### Prerequisites

| Tool | Version | Notes |
|------|---------|-------|
| Go | 1.22+ | CGO required (sqlite-vec uses C) |
| Node.js | 18+ | for building the web UI |
| gcc / Xcode CLT | any | required for CGO |
| Docker | any | required only for PostgreSQL path |
| Ollama | any | for local embeddings; can be skipped if `ANTHROPIC_API_KEY` is set and you don't embed locally |

Install Xcode command-line tools on macOS: `xcode-select --install`

---

### Option A: SQLite (simplest)

The fastest way to get running. No Docker, no PostgreSQL — just the binary and a local `.db` file.

```bash
git clone https://github.com/dsandor/memory
cd memory

# 1. Build the web UI
make web

# 2. Build the Go binary (CGO required)
make build

# 3. Run with auth disabled and SQLite storage
DEV_BYPASS_AUTH=true ANTHROPIC_API_KEY=sk-ant-... ./tribal-knowledge
```

Open [http://localhost:8080](http://localhost:8080) — the onboarding wizard will guide you through initial setup.

**SQLite with a specific database path:**
```bash
DEV_BYPASS_AUTH=true DATABASE_PATH=./dev-knowledge.db ./tribal-knowledge
```

**Skip embeddings entirely** (no Ollama needed; semantic search disabled):
```bash
DEV_BYPASS_AUTH=true OLLAMA_URL="" ./tribal-knowledge
```

> `DEV_BYPASS_AUTH=true` injects a superadmin session for every request. Never use it outside local development.

---

### Option B: PostgreSQL via run.sh (MCP dev)

`run.sh` starts a Docker-managed PostgreSQL container, waits for it to be healthy, then launches the server via `go run`. This is the recommended setup when developing MCP integrations locally, because it matches the production storage backend.

```bash
chmod +x run.sh

# Minimum: just set your Anthropic key
ANTHROPIC_API_KEY=sk-ant-... SUPERADMIN_KEY=dev-secret ./run.sh
```

The script:
1. Creates (or restarts) a `memory-postgres` Docker container persisting data to `./db`
2. Waits for PostgreSQL to accept connections
3. Exports `DATABASE_URL` and all server env vars
4. Replaces itself with `go run ./cmd/server` (stdout/stderr become the MCP server's I/O)

All variables can be overridden:
```bash
DB_DIR=/tmp/mydb POSTGRES_PORT=5433 DEV_BYPASS_AUTH=true ./run.sh
```

**Register with Claude Desktop** (`~/.config/claude/claude_desktop_config.json`):
```json
{
  "mcpServers": {
    "tribal-knowledge": {
      "command": "/absolute/path/to/memory/run.sh",
      "env": {
        "ANTHROPIC_API_KEY": "sk-ant-...",
        "SUPERADMIN_KEY": "change-me"
      }
    }
  }
}
```

After saving, restart Claude Desktop. The MCP server appears in the tool panel.

---

### Option C: Full stack with Docker Compose

Runs PostgreSQL + the server (+ optional Ollama) in containers. Good for testing the Docker image before deploying.

```bash
cp .env.example .env
# Edit .env: at minimum set ANTHROPIC_API_KEY, SUPERADMIN_KEY, POSTGRES_PASSWORD

docker compose up -d
open http://localhost:8080
```

Add Ollama for local embeddings:
```bash
docker compose --profile ollama up -d
```

Tail logs:
```bash
docker compose logs -f server
```

Rebuild after code changes:
```bash
make web && docker compose up -d --build server
```

---

### Seeding Example Data

Seeds 9 realistic entries across three domains (financial-analysis, software-engineering, data-science):

```bash
# Server must be running with auth bypassed
DEV_BYPASS_AUTH=true ./tribal-knowledge &

pip install requests
python scripts/seed.py
```

---

### Running Tests

```bash
make test
# or directly:
CGO_ENABLED=1 go test ./...
```

Tests require CGO because the SQLite storage layer links against sqlite-vec. If you see linker errors, confirm gcc is installed and `CGO_ENABLED=1` is set.

---

## Deploying to a Server

### Docker Compose (recommended)

**1. Copy and edit the environment file:**
```bash
cp .env.example .env
```

Minimum production settings to change in `.env`:

```env
ANTHROPIC_API_KEY=sk-ant-...          # required for pipeline and agents
SUPERADMIN_KEY=<long-random-string>   # first admin account credential
POSTGRES_PASSWORD=<strong-password>   # database password
DEV_BYPASS_AUTH=false                 # must be false in production
```

Generate a strong random key:
```bash
openssl rand -hex 32
```

**2. Start the stack:**
```bash
docker compose up -d
```

This brings up:
- `postgres` — pgvector/pgvector:pg17, data persisted to `DB_DIR` (default `./db`)
- `server` — the tribal-knowledge binary, healthy when `/health` returns 200

**3. Verify:**
```bash
curl http://localhost:8080/health
```

Expected response:
```json
{
  "status": "ok",
  "storage": "ok",
  "embedding": "ok",
  "pipeline": "idle"
}
```

**4. Log in to the web UI:**

Open `http://your-server:8080` and use the `SUPERADMIN_KEY` value as your API key in the onboarding wizard.

---

### Behind a Reverse Proxy

When the server sits behind nginx, Caddy, or a cloud load balancer, you must configure the `TRUST_XFF` flag so rate limiting uses the real client IP rather than the proxy's IP.

**Only enable this if you control the proxy and it sets `X-Forwarded-For` correctly:**

```env
TRUST_XFF=true
```

When `TRUST_XFF=true`, the server uses the rightmost entry in `X-Forwarded-For` (the last hop before your proxy) as the client IP. Default is `false` — the rate limiter uses the direct connection IP.

**Example nginx config snippet:**
```nginx
location / {
    proxy_pass         http://localhost:8080;
    proxy_set_header   X-Forwarded-For $proxy_add_x_forwarded_for;
    proxy_set_header   Host $host;
}
```

**Expose the MCP HTTP/SSE endpoint for remote clients:**
```env
MCP_HTTP_ADDR=:9090
MCP_HTTP_PATH=/mcp
```

Then proxy `/mcp` separately or expose port 9090 through your firewall.

---

### Environment Variable Reference

| Variable | Default | Description |
|---|---|---|
| `DATABASE_PATH` | `knowledge.db` | SQLite file path. Ignored when `DATABASE_URL` is set. |
| `DATABASE_URL` | — | PostgreSQL connection string. If set, Postgres is used instead of SQLite. |
| `OLLAMA_URL` | `http://localhost:11434` | Ollama server for embeddings. Leave empty to disable. |
| `OLLAMA_MODEL` | `nomic-embed-text` | Embedding model name. |
| `EMBEDDING_DIM` | `768` | Vector dimension. Must match the model. |
| `HTTP_ADDR` | `:8080` | Address the HTTP server binds to. |
| `LOG_LEVEL` | `info` | Log verbosity: `debug`, `info`, `warn`, `error`. |
| `TEAM_ID` | `default` | Default team identifier. |
| `ANTHROPIC_API_KEY` | — | **Required** for pipeline analysis, agent generation, and `prompt_suggest`. |
| `ANTHROPIC_MODEL` | `claude-haiku-4-5-20251001` | Model for pipeline analysis. |
| `AGENT_MODEL` | `claude-sonnet-4-6` | Model for agent generation. |
| `PIPELINE_INTERVAL` | `1h` | How often the analysis pipeline runs. Go duration format: `30m`, `2h`. |
| `PIPELINE_MIN_ENTRIES` | `10` | Minimum entries before pipeline triggers. |
| `CLUSTER_THRESHOLD` | `0.85` | Cosine similarity threshold for clustering (0–1]. |
| `SUPERADMIN_KEY` | — | Raw API key bootstrapped as superadmin on first run. |
| `DEV_BYPASS_AUTH` | `false` | Skip auth; inject superadmin context. **Never set true in production.** |
| `MCP_HTTP_ADDR` | — | Enables HTTP/SSE MCP transport. Example: `:9090`. Leave empty for stdio-only. |
| `MCP_HTTP_PATH` | `/mcp` | URL path for the SSE MCP endpoint. |
| `RATE_LIMIT_RPS` | `60` | Per-IP requests per second (token bucket). Set `0` to disable. |
| `TRUST_XFF` | `false` | Use `X-Forwarded-For` for client IP. Enable only behind a trusted proxy. |
| `DB_DIR` | `./db` | Host path for PostgreSQL data volume (Docker Compose and run.sh). |
| `POSTGRES_PASSWORD` | `memory` | PostgreSQL password. Change in production. |

---

## Connecting MCP Clients

### Claude Desktop (stdio)

**Using the built binary:**
```json
{
  "mcpServers": {
    "tribal-knowledge": {
      "command": "/path/to/tribal-knowledge",
      "env": {
        "DATABASE_PATH": "/path/to/knowledge.db",
        "ANTHROPIC_API_KEY": "sk-ant-...",
        "DEV_BYPASS_AUTH": "true"
      }
    }
  }
}
```

**Using run.sh (PostgreSQL, recommended):**
```json
{
  "mcpServers": {
    "tribal-knowledge": {
      "command": "/absolute/path/to/memory/run.sh",
      "env": {
        "ANTHROPIC_API_KEY": "sk-ant-...",
        "SUPERADMIN_KEY": "your-key-here"
      }
    }
  }
}
```

Config file location:
- macOS: `~/Library/Application Support/Claude/claude_desktop_config.json`
- Linux: `~/.config/claude/claude_desktop_config.json`

### Cursor

`~/.cursor/mcp.json`:
```json
{
  "mcpServers": {
    "tribal-knowledge": {
      "command": "/path/to/tribal-knowledge",
      "env": {
        "DATABASE_PATH": "/path/to/knowledge.db",
        "SUPERADMIN_KEY": "your-key-here"
      }
    }
  }
}
```

### Remote Clients (HTTP/SSE)

Start the server with `MCP_HTTP_ADDR` set:
```bash
MCP_HTTP_ADDR=:9090 ./tribal-knowledge
# or in .env: MCP_HTTP_ADDR=:9090
```

Connect any SSE-capable MCP client to:
```
http://your-server:9090/mcp
```

---

## First-Time Setup

On first launch (regardless of storage backend), the server has no teams or users. There are two ways to bootstrap:

**Option 1 — Onboarding wizard (recommended):**

When the knowledge list is empty, the web UI automatically redirects to `/onboarding`. The 4-step wizard walks through:
1. Welcome
2. Create a team
3. Generate an API key (copy it — shown once)
4. Seed example data

**Option 2 — SUPERADMIN_KEY env var:**

Set `SUPERADMIN_KEY=<value>` before first launch. On startup the server creates a superadmin user with that key. Use it as a `Bearer` token or `X-API-Key` header:

```bash
# Create your first team
curl -X POST http://localhost:8080/api/teams \
  -H "X-API-Key: your-superadmin-key" \
  -H "Content-Type: application/json" \
  -d '{"name":"my-team","description":"..."}'
```

After creating a team, generate per-user API keys through the web UI Settings page or REST API.

---

## MCP Tools Reference

| Tool | Description |
|---|---|
| `knowledge_store` | Store a new knowledge entry with title, content, type, domain, and tags |
| `knowledge_get` | Retrieve a knowledge entry by ID |
| `knowledge_list` | List knowledge entries with optional domain filter |
| `knowledge_delete` | Delete a knowledge entry by ID |
| `knowledge_search` | Semantic + keyword hybrid search with optional domain filter |
| `knowledge_rate` | Rate an entry (1–5 stars); affects pipeline quality scoring |
| `knowledge_use` | Record that a prompt suggestion was accepted (feeds active learning) |
| `cluster_list` | List knowledge clusters produced by the analysis pipeline |
| `analysis_status` | Get pipeline run status and last-run statistics |
| `agent_get` | Get a generated AI agent definition by ID |
| `agent_list` | List generated agents with optional domain filter |
| `agent_publish` | Publish a draft agent (curator role required) |
| `agent_export` | Export an agent as Claude subagent MD, plain TXT, or JSON |
| `rule_list` | List domain rules surfaced by the pipeline |
| `prompt_suggest` | Suggest prompt improvements based on team knowledge |
| `enhance_with_context` | Enhance a prompt using domain rules, similar entries, and the best agent for the domain |

---

## Building and Publishing Images

```bash
# Build Docker image locally
make image

# Build and push to a registry
make deploy REGISTRY=ghcr.io/yourorg

# Override image name and version
make deploy REGISTRY=docker.io/yourname IMAGE=tribal-knowledge VERSION=1.0.0
```

Requires `docker login <registry>` first.

---

## License

MIT
