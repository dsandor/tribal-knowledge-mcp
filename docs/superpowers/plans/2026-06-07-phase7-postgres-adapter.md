# Phase 7 — PostgreSQL + pgvector Storage Adapter

**Date:** 2026-06-07
**Status:** pending
**Module:** `github.com/dsandor/memory`
**Go version:** 1.25

---

## Overview

Add a PostgreSQL storage backend that implements the same `Store`, `AnalysisStore`, `AgentStore`, `TeamStore`, and `RuleStore` interfaces as `SQLiteStore`. When `DATABASE_URL` is set, `main.go` opens a `PostgresStore` instead of `SQLiteStore`. When unset, behaviour is unchanged.

Vector similarity search uses the `pgvector` extension via `github.com/pgvector/pgvector-go` and `github.com/jackc/pgx/v5/stdlib` (database/sql compatible).

No breaking changes to existing SQLite path.

---

## Task 1 — Dependencies + Config + Factory

### 1.1 Add Go dependencies

```
go get github.com/jackc/pgx/v5
go get github.com/pgvector/pgvector-go
```

The `pgx/v5/stdlib` package wraps pgx as a `database/sql` driver, keeping the codebase on `database/sql` throughout (no pgx-specific API needed).

### 1.2 Add `DatabaseURL` to Config

**File:** `internal/config/config.go`

```go
DatabaseURL string // DATABASE_URL — if non-empty, uses PostgreSQL instead of SQLite
```

In `Load()`:
```go
DatabaseURL: os.Getenv("DATABASE_URL"),
```

### 1.3 Update `.env.example`

Add under the `── Server ──` section:
```dotenv
DATABASE_URL=         # postgres://user:pass@host:5432/dbname?sslmode=disable — leave empty to use SQLite
```

### 1.4 Update `main.go` with a storage factory

Replace the `storage.NewSQLiteStore(...)` call with a helper:

```go
var store *storage.SQLiteStore  // existing type
```

becomes:

```go
// openStore returns either a PostgresStore (if DATABASE_URL is set) or SQLiteStore.
// Both satisfy the AgentStore + TeamStore + RuleStore interfaces.
```

In practice, since both stores are concrete types, the cleanest approach is to
accept an interface in all call sites. The `web.AllStore` and MCP registrations
already accept interfaces. The only place that receives a concrete type is
`bootstrapSuperadmin` — change its parameter to `storage.TeamStore`.

Use a local `type combinedStore interface { storage.AgentStore; storage.TeamStore; storage.RuleStore }` 
in `main.go` to hold either implementation.

---

## Task 2 — PostgresStore: Core (Store interface)

**File:** `internal/storage/postgres.go`

### Struct

```go
type PostgresStore struct {
    db           *sql.DB
    embeddingDim int
}
```

### Constructor

```go
func NewPostgresStore(dsn string, embeddingDim int) (*PostgresStore, error)
```

Register the pgvector type with pgx stdlib, open the connection, run `migrate()`.

### Schema (migrate function)

```sql
CREATE EXTENSION IF NOT EXISTS vector;

CREATE TABLE IF NOT EXISTS entries (
    id          TEXT PRIMARY KEY,
    type        TEXT NOT NULL,
    title       TEXT NOT NULL,
    content     TEXT NOT NULL,
    description TEXT    NOT NULL DEFAULT '',
    domain      TEXT    NOT NULL DEFAULT '',
    tags        JSONB   NOT NULL DEFAULT '[]',
    author      TEXT    NOT NULL DEFAULT '',
    team        TEXT    NOT NULL DEFAULT '',
    team_id     TEXT    NOT NULL DEFAULT '',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    version     INT     NOT NULL DEFAULT 1,
    rating      FLOAT   NOT NULL DEFAULT 0,
    usage_count INT     NOT NULL DEFAULT 0,
    status      TEXT    NOT NULL DEFAULT 'pending'
);

CREATE TABLE IF NOT EXISTS embeddings (
    entry_id TEXT PRIMARY KEY REFERENCES entries(id) ON DELETE CASCADE,
    embedding vector(%d)  -- dimension injected at migrate time
);

CREATE INDEX IF NOT EXISTS embeddings_ivfflat_idx
    ON embeddings USING ivfflat (embedding vector_cosine_ops)
    WITH (lists = 100);
```

> Note: `ivfflat` requires at least 1 row to be useful; it's safe to create even on an empty table. For exact search on small datasets (< 1000 rows), the index is optional but harmless.

### Store interface implementation notes

- `StoreEntry`: INSERT into `entries`, then INSERT into `embeddings` using `pgvector.NewVector(embedding)`, all in a transaction.
- `GetEntry`: SELECT + JSON unmarshal for `tags`.
- `ListEntries`: Dynamic WHERE clauses same as SQLite, using `$1`, `$2` parameters.
- `SearchSimilar`: `SELECT entry_id, embedding <=> $1 AS distance FROM embeddings ORDER BY distance LIMIT $2`, then JOIN with `entries`.
- `RateEntry`, `ApproveEntry`, `RejectEntry`, `UpdateEntry`, `DeleteEntry`: straightforward UPDATE/DELETE.
- `Ping`: `db.PingContext(ctx)`.
- `Close`: `db.Close()`.

---

## Task 3 — AnalysisStore + AgentStore

**Files:** `internal/storage/postgres_analysis.go`, `internal/storage/postgres_agents.go`

### AnalysisStore

Same SQL logic as the SQLite implementation, translated to PostgreSQL syntax:
- JSON arrays (`entry_ids`, `errors`) use `JSONB`
- `pipeline_runs.completed_at` is `TIMESTAMPTZ NULL`
- `GetAllEmbeddings`: `SELECT entry_id, embedding FROM embeddings` — convert `pgvector.Vector` → `[]float32`

### AgentStore

Same SQL logic as `agents_sqlite.go`:
- `source_refs` stored as `JSONB`
- `UpsertAgent`: `INSERT ... ON CONFLICT (id) DO UPDATE SET ...`
- `StoreAgentVersion`: `INSERT ... ON CONFLICT (agent_id, version) DO NOTHING`

---

## Task 4 — TeamStore + RuleStore

**Files:** `internal/storage/postgres_teams.go`, `internal/storage/postgres_rules.go`

### TeamStore schema additions

```sql
CREATE TABLE IF NOT EXISTS teams (
    id              TEXT PRIMARY KEY,
    name            TEXT NOT NULL,
    domain_patterns JSONB NOT NULL DEFAULT '[]',
    enabled         BOOLEAN NOT NULL DEFAULT TRUE,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS users (
    id                TEXT PRIMARY KEY,
    team_id           TEXT NOT NULL DEFAULT '',
    email             TEXT NOT NULL DEFAULT '',
    name              TEXT NOT NULL DEFAULT '',
    external_id       TEXT NOT NULL DEFAULT '',
    password_hash     TEXT NOT NULL DEFAULT '',
    role              TEXT NOT NULL DEFAULT 'member',
    manually_assigned BOOLEAN NOT NULL DEFAULT FALSE,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE UNIQUE INDEX IF NOT EXISTS users_email_idx ON users(email) WHERE email <> '';
CREATE UNIQUE INDEX IF NOT EXISTS users_ext_id_idx ON users(external_id) WHERE external_id <> '';

CREATE TABLE IF NOT EXISTS sessions (
    id          TEXT PRIMARY KEY,
    user_id     TEXT NOT NULL,
    token_hash  TEXT UNIQUE NOT NULL,
    expires_at  TIMESTAMPTZ NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS api_keys (
    id           TEXT PRIMARY KEY,
    team_id      TEXT NOT NULL DEFAULT '',
    user_id      TEXT NOT NULL DEFAULT '',
    key_type     TEXT NOT NULL DEFAULT 'team',
    name         TEXT NOT NULL,
    key_hash     TEXT UNIQUE NOT NULL,
    role         TEXT NOT NULL DEFAULT 'member',
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    last_used_at TIMESTAMPTZ NULL
);

CREATE TABLE IF NOT EXISTS team_settings (
    team_id              TEXT PRIMARY KEY,
    domains              JSONB NOT NULL DEFAULT '[]',
    cluster_threshold    FLOAT NOT NULL DEFAULT 0.85,
    pipeline_min_entries INT NOT NULL DEFAULT 10,
    agent_model          TEXT NOT NULL DEFAULT '',
    updated_at           TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS auth_config (
    id               INT PRIMARY KEY DEFAULT 1 CHECK (id = 1),
    provider         TEXT NOT NULL DEFAULT 'local',
    oidc_issuer      TEXT NOT NULL DEFAULT '',
    oidc_client_id   TEXT NOT NULL DEFAULT '',
    oidc_redirect_url TEXT NOT NULL DEFAULT '',
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
INSERT INTO auth_config DEFAULT VALUES ON CONFLICT DO NOTHING;

CREATE TABLE IF NOT EXISTS activity_log (
    id          TEXT PRIMARY KEY,
    team_id     TEXT NOT NULL DEFAULT '',
    key_id      TEXT NOT NULL DEFAULT '',
    user_id     TEXT NOT NULL DEFAULT '',
    action      TEXT NOT NULL,
    entity_type TEXT NOT NULL DEFAULT '',
    entity_id   TEXT NOT NULL DEFAULT '',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS activity_team_idx ON activity_log(team_id, created_at DESC);
```

### RuleStore schema

```sql
CREATE TABLE IF NOT EXISTS rules (
    id          TEXT PRIMARY KEY,
    title       TEXT NOT NULL,
    content     TEXT NOT NULL,
    scope       TEXT NOT NULL DEFAULT 'team',
    scope_value TEXT NOT NULL DEFAULT '',
    priority    INT NOT NULL DEFAULT 0,
    enabled     BOOLEAN NOT NULL DEFAULT TRUE,
    author      TEXT NOT NULL DEFAULT '',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
```

---

## Task 5 — Compile Assertions + Build Verification

Add at the top of `postgres.go`:

```go
var _ AgentStore  = (*PostgresStore)(nil)
var _ TeamStore   = (*PostgresStore)(nil)
var _ RuleStore   = (*PostgresStore)(nil)
```

Run `CGO_ENABLED=1 go build ./...` — must compile clean (no runtime test needed since Postgres isn't guaranteed to be running in CI).

Update `cmd/server/main.go`:
- Change `bootstrapSuperadmin` to accept `storage.TeamStore` instead of `*storage.SQLiteStore`
- Add factory logic: if `cfg.DatabaseURL != ""`, open PostgresStore; else open SQLiteStore
- Wire the store into the same downstream consumers

---

## File Index

| Task | Files |
|---|---|
| 1 | `go.mod`, `go.sum`, `internal/config/config.go`, `.env.example`, `cmd/server/main.go` |
| 2 | `internal/storage/postgres.go` (new) |
| 3 | `internal/storage/postgres_analysis.go` (new), `internal/storage/postgres_agents.go` (new) |
| 4 | `internal/storage/postgres_teams.go` (new), `internal/storage/postgres_rules.go` (new) |
| 5 | `cmd/server/main.go` (factory wiring), compile assertions in postgres.go |
