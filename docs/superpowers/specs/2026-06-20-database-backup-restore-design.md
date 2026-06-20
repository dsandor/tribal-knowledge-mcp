# Database Backup & Restore — Design

**Date:** 2026-06-20
**Status:** Approved (design)

## Goal

Provide a logical, engine-neutral backup of the **entire** server so an operator can
archive the database and restore/migrate it to another server — including across storage
engines (SQLite ↔ PostgreSQL). The primary use case is migrating a local SQLite instance
to a production PostgreSQL instance while carrying **all** state: knowledge, embeddings,
clusters, agents, rules, teams, users, API keys, auth config, settings, and usage/activity
history.

This is a *logical* (row-level) backup, not a physical one (`pg_dump` / `.backup` file copy),
because it must round-trip between two different database engines.

## Decisions (locked)

| Topic | Decision |
|-------|----------|
| Interface | **CLI subcommands + Web UI** (CLI for migration, Settings page for convenience) |
| Data scope | **Everything except ephemeral** — all tables except `sessions` |
| Embeddings | **Export as JSON float arrays**; re-insert in target's native format (no re-embedding) |
| Restore mode | **Refuse if target non-empty unless `--force`**; `--force` truncates then restores |
| Archive security | **Plaintext + loud runtime warning**; `chmod 0600` output; superadmin-only web access. Encryption deferred. |
| Tenant scope | **Whole database, all teams** (per-team export deferred) |
| Serialization | **Generic table dump** (approach A): `SELECT *` → `[]map[string]any` per engine |

## Tables covered

In dependency order (parents before children). `sessions` is intentionally excluded.

1. `teams`
2. `users`
3. `api_keys`
4. `auth_config`
5. `team_settings`
6. `entries`
7. `entry_embeddings` (special: blob ⇄ pgvector, serialized as float arrays)
8. `clusters`
9. `pipeline_runs`
10. `dataset_snapshots`
11. `analysis_cache`
12. `rules`
13. `agents`
14. `agent_versions`
15. `activity_log`
16. `usage_events`
17. `outcome_ratings`
18. `feed_activity`

> The SQLite-only `vec_entries` virtual table is **not** dumped directly; it is rebuilt from
> `entry_embeddings` on restore. Likewise the Postgres pgvector column is rebuilt from the
> float arrays. `entries.rowid` (SQLite autoincrement) is not portable and is **not** carried;
> the stable `entries.id` (UUID) is the join key, and `entry_embeddings` is keyed to entries by
> that UUID in the archive (see Embeddings handling).

## Architecture

### `internal/backup` package (engine-independent orchestration)

- `Archive` / manifest types.
- `Export(ctx, store BackupStore, w io.Writer) (Manifest, error)` — streams a `.tar.gz`.
- `Import(ctx, store BackupStore, r io.Reader, opts ImportOptions) (Report, error)`.
- Owns the canonical ordered table list and the dependency order used for truncate (reverse)
  and insert (forward).
- Knows nothing about SQL — delegates all row I/O to the `BackupStore` interface.

### `BackupStore` interface (implemented by `SQLiteStore` and `PostgresStore`)

```go
type BackupStore interface {
    // DumpTable streams every row of the named table as ordered column→value maps.
    DumpTable(ctx context.Context, table string, fn func(row map[string]any) error) error
    // LoadTable inserts the given rows (parameterized) into the named table.
    LoadTable(ctx context.Context, table string, rows []map[string]any) error
    // DumpEmbeddings / LoadEmbeddings handle the engine-specific vector storage,
    // exchanging embeddings as ([]float32 keyed by entry UUID).
    DumpEmbeddings(ctx context.Context, fn func(entryID string, vec []float32) error) error
    LoadEmbeddings(ctx context.Context, items []EmbeddingItem) error
    // IsEmpty reports whether the target holds any non-bootstrap data.
    IsEmpty(ctx context.Context) (bool, error)
    // TruncateAll deletes all backup-covered tables in FK-safe order (used by --force).
    TruncateAll(ctx context.Context) error
    // RunInTx executes fn inside a single transaction.
    RunInTx(ctx context.Context, fn func(BackupStore) error) error
}
```

Each store owns its engine quirks (placeholder style `?` vs `$1`, blob vs pgvector,
`DELETE` vs `TRUNCATE ... CASCADE`). The generic `DumpTable`/`LoadTable` use the DB-reported
column names so they tolerate added columns without code changes.

### CLI dispatch

`cmd/server/main.go` inspects `os.Args[1]` **before** starting the server:

```
tribal-knowledge export --out <file>        # default: backup-<timestamp>.tar.gz; --stdout to pipe
tribal-knowledge import --in <file> [--force]
```

Both subcommands reuse the existing store-construction logic (DATABASE_URL → Postgres,
else SQLite + DB_PATH), run the operation, log a summary, and exit. The default (no
recognized subcommand) starts the server exactly as today.

### Web UI (superadmin-only)

- Settings page gains a **Backup & Restore** section.
- `GET /api/admin/backup` → streams the `.tar.gz` as a download (`Content-Disposition`).
- `POST /api/admin/restore` → multipart upload; requires `?force=true` to overwrite a
  non-empty DB. This route is **exempt** from the 1 MB `maxBodySize` middleware (archives
  exceed it) and from CSV/JSON body guards.
- Both routes require the superadmin role.

## Archive format

A gzip-compressed tar containing:

- `manifest.json`:
  ```json
  {
    "format_version": 1,
    "tool_version": "<build version>",
    "created_at": "2026-06-20T12:00:00Z",
    "source_engine": "sqlite" | "postgres",
    "embedding_dim": 768,
    "tables": { "entries": 142, "agents": 8, "...": 0 }
  }
  ```
- `tables/<name>.jsonl` — one JSON object per line (column→value). JSONL keeps memory flat
  for large tables (embeddings).
- `tables/entry_embeddings.jsonl` — `{ "entry_id": "<uuid>", "embedding": [0.0123, ...] }`
  per line.

## Restore flow

1. Open the target store (existing init creates the schema if absent).
2. Read & validate `manifest.json`:
   - `format_version` is supported.
   - `embedding_dim` **equals** the target's configured dimension → otherwise **refuse**
     (cannot restore mismatched vectors).
3. `IsEmpty` check: if non-empty and `--force` not set → **refuse** with a clear message.
4. Inside one transaction (`RunInTx`):
   - If `--force`: `TruncateAll` (reverse dependency order).
   - Insert each table in forward dependency order.
   - Rebuild embeddings in the target's native format from the float arrays.
5. Print a `Report`: per-table inserted counts, embeddings restored, duration.

Sessions are neither exported nor imported; users re-authenticate after a restore.

## Security

- The archive contains secrets: API key hashes, `auth_config`, user password hashes.
- Export writes the file with mode `0600` and prints a prominent warning that the archive
  must be treated as a secret (secure transfer, restricted storage).
- Web backup/restore endpoints are superadmin-only.
- README documents secure handling. Passphrase encryption (`--encrypt`) is explicitly out
  of scope for v1 and noted as a future enhancement.

## Testing

- **Round-trip (SQLite→SQLite):** seed a DB, export, import into a fresh DB, assert
  table-by-table row equality including embedding values within float tolerance.
- **Cross-engine (SQLite→Postgres):** export from SQLite, import into Postgres, gated behind
  the same `DATABASE_URL` test env used by existing Postgres tests; skipped when unset.
- **`--force` truncate path:** restoring over a populated DB replaces contents.
- **Refusal paths:** non-empty target without `--force`; `embedding_dim` mismatch;
  unsupported `format_version`.
- **Manifest integrity:** counts in manifest match rows written.

## Out of scope (v1)

- Passphrase/at-rest encryption of the archive.
- Per-team / selective export.
- Incremental or differential backups.
- Scheduled/automated backups.
- Physical (engine-native) dumps.
