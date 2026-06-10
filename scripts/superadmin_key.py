#!/usr/bin/env python3
"""
Recover or reset the superadmin API key directly in the datastore.

A superadmin key is a string of the form  tk_<64 hex chars>  and the server
stores only its SHA-256 hash in api_keys.key_hash (role = 'superadmin').
Because the bootstrap key (cmd/server/main.go: bootstrapSuperadmin) is stored
hash-only, a forgotten key generally CANNOT be recovered -- it must be reset.

This tool works against the same datastore the server uses:
  * SQLite  (default; DATABASE_PATH or knowledge.db)
  * Postgres (when --database-url / DATABASE_URL is set; needs psycopg2 or psycopg)

Usage:
    # List superadmin keys; print the plaintext if raw_key was retained.
    python scripts/superadmin_key.py show

    # Rotate the existing superadmin key to a fresh one (prints the new key once).
    python scripts/superadmin_key.py reset

    # If several superadmin rows exist, target one explicitly:
    python scripts/superadmin_key.py reset --id <key-id>

    # Always create a brand-new superadmin row instead of rotating:
    python scripts/superadmin_key.py reset --new

Options:
    --db PATH            SQLite file (default: $DATABASE_PATH or knowledge.db)
    --database-url URL   Postgres DSN (default: $DATABASE_URL); selects Postgres
    --name NAME          Name for the created/rotated key (default: 'superadmin-reset')
    --yes                Skip the confirmation prompt

Requires: Python 3.9+. Postgres mode also needs psycopg2-binary or psycopg.
"""

import argparse
import hashlib
import os
import secrets
import sys
import uuid


def generate_raw_key() -> str:
    """Match the server's generateRawKey(): 'tk_' + hex(32 random bytes)."""
    return "tk_" + secrets.token_hex(32)


def hash_sha256(s: str) -> str:
    """Match auth.HashSHA256: hex-encoded SHA-256."""
    return hashlib.sha256(s.encode("utf-8")).hexdigest()


# ── Datastore abstraction ──────────────────────────────────────────────────────

class Store:
    """Thin wrapper hiding SQLite vs Postgres parameter style and quirks."""

    def __init__(self, conn, kind: str):
        self.conn = conn
        self.kind = kind  # 'sqlite' | 'postgres'
        self.ph = "?" if kind == "sqlite" else "%s"

    # -- schema --

    def has_raw_key_column(self) -> bool:
        cur = self.conn.cursor()
        if self.kind == "sqlite":
            cur.execute("PRAGMA table_info(api_keys)")
            return any(row[1] == "raw_key" for row in cur.fetchall())
        cur.execute(
            "SELECT 1 FROM information_schema.columns "
            "WHERE table_name = 'api_keys' AND column_name = 'raw_key'"
        )
        return cur.fetchone() is not None

    def ensure_raw_key_column(self) -> None:
        if self.has_raw_key_column():
            return
        cur = self.conn.cursor()
        # Same DDL the server's migrations apply.
        cur.execute("ALTER TABLE api_keys ADD COLUMN raw_key TEXT NOT NULL DEFAULT ''")
        self.conn.commit()

    # -- queries --

    def list_superadmins(self):
        cur = self.conn.cursor()
        raw_col = "COALESCE(raw_key, '')" if self.has_raw_key_column() else "''"
        cur.execute(
            f"SELECT id, name, key_hash, {raw_col}, "
            f"COALESCE(created_at::text, ''), COALESCE(last_used_at::text, '') "
            f"FROM api_keys WHERE role = {self.ph} ORDER BY created_at"
            if self.kind == "postgres" else
            f"SELECT id, name, key_hash, {raw_col}, "
            f"COALESCE(created_at, ''), COALESCE(last_used_at, '') "
            f"FROM api_keys WHERE role = {self.ph} ORDER BY created_at",
            ("superadmin",),
        )
        cols = ("id", "name", "key_hash", "raw_key", "created_at", "last_used_at")
        return [dict(zip(cols, row)) for row in cur.fetchall()]

    def rotate(self, key_id: str, key_hash: str, raw_key: str, name: str) -> None:
        cur = self.conn.cursor()
        cur.execute(
            f"UPDATE api_keys SET key_hash = {self.ph}, raw_key = {self.ph}, "
            f"name = {self.ph}, last_used_at = NULL WHERE id = {self.ph}",
            (key_hash, raw_key, name, key_id),
        )
        self.conn.commit()

    def insert(self, key_hash: str, raw_key: str, name: str) -> str:
        key_id = str(uuid.uuid4())
        cur = self.conn.cursor()
        if self.kind == "sqlite":
            # team_id/user_id are nullable; superadmin keys carry neither.
            cur.execute(
                "INSERT INTO api_keys (id, team_id, user_id, key_type, name, key_hash, raw_key, role) "
                "VALUES (?, NULL, NULL, 'team', ?, ?, ?, 'superadmin')",
                (key_id, name, key_hash, raw_key),
            )
        else:
            # Postgres columns are NOT NULL DEFAULT '' for team_id/user_id.
            cur.execute(
                "INSERT INTO api_keys (id, team_id, user_id, key_type, name, key_hash, raw_key, role) "
                "VALUES (%s, '', '', 'team', %s, %s, %s, 'superadmin')",
                (key_id, name, key_hash, raw_key),
            )
        self.conn.commit()
        return key_id


def open_store(args) -> Store:
    database_url = args.database_url or os.getenv("DATABASE_URL", "")
    if database_url:
        try:
            try:
                import psycopg2 as pg  # type: ignore
            except ImportError:
                import psycopg as pg  # type: ignore
        except ImportError:
            sys.exit("Postgres mode needs psycopg2-binary or psycopg: pip install psycopg2-binary")
        conn = pg.connect(database_url)
        return Store(conn, "postgres")

    import sqlite3
    db_path = args.db or os.getenv("DATABASE_PATH", "knowledge.db")
    if not os.path.exists(db_path):
        sys.exit(f"SQLite database not found: {db_path}\n"
                 f"Pass --db <path> or set DATABASE_PATH (or use --database-url for Postgres).")
    conn = sqlite3.connect(db_path)
    return Store(conn, "sqlite")


# ── Commands ────────────────────────────────────────────────────────────────────

def cmd_show(store: Store) -> int:
    rows = store.list_superadmins()
    if not rows:
        print("No superadmin keys found in api_keys.")
        print("Run 'reset --new' to create one, or set SUPERADMIN_KEY and restart the server.")
        return 1
    print(f"Found {len(rows)} superadmin key(s):\n")
    recoverable = 0
    for r in rows:
        print(f"  id:         {r['id']}")
        print(f"  name:       {r['name']}")
        print(f"  created_at: {r['created_at'] or '-'}")
        print(f"  last_used:  {r['last_used_at'] or '-'}")
        if r["raw_key"]:
            recoverable += 1
            print(f"  KEY:        {r['raw_key']}   <-- recovered plaintext")
        else:
            print(f"  KEY:        (hash-only; plaintext NOT recoverable -- use 'reset')")
        print()
    if recoverable == 0:
        print("None of these keys retained a plaintext value. Use 'reset' to issue a new one.")
    return 0


def cmd_reset(store: Store, args) -> int:
    store.ensure_raw_key_column()
    rows = store.list_superadmins()
    name = args.name or "superadmin-reset"

    raw_key = generate_raw_key()
    key_hash = hash_sha256(raw_key)

    # Decide target: insert-new vs rotate-existing.
    target_id = None
    if not args.new:
        if args.id:
            ids = {r["id"] for r in rows}
            if args.id not in ids:
                sys.exit(f"No superadmin key with id {args.id!r}. Run 'show' to list them.")
            target_id = args.id
        elif len(rows) == 1:
            target_id = rows[0]["id"]
        elif len(rows) > 1:
            print("Multiple superadmin keys exist; pick one with --id, or use --new:\n")
            for r in rows:
                print(f"  {r['id']}  ({r['name']})")
            return 1
        # len(rows) == 0 falls through to insert.

    action = "ROTATE existing key" if target_id else "CREATE a new superadmin key"
    if not args.yes:
        print(f"About to {action} in the {store.kind} datastore.")
        if target_id:
            print(f"  The old key for id {target_id} will stop working immediately.")
        resp = input("Type 'yes' to continue: ").strip().lower()
        if resp != "yes":
            print("Aborted.")
            return 1

    if target_id:
        store.rotate(target_id, key_hash, raw_key, name)
        print(f"\nRotated superadmin key (id {target_id}).")
    else:
        new_id = store.insert(key_hash, raw_key, name)
        print(f"\nCreated superadmin key (id {new_id}).")

    print("\n  NEW SUPERADMIN KEY:\n")
    print(f"    {raw_key}\n")
    print("  Use it as the Authorization bearer token / X-API-Key header.")
    print("  It is also stored in api_keys.raw_key so it can be re-copied from the UI.")
    return 0


def main() -> int:
    p = argparse.ArgumentParser(description="Recover or reset the superadmin API key.")
    p.add_argument("--db", help="SQLite file (default: $DATABASE_PATH or knowledge.db)")
    p.add_argument("--database-url", help="Postgres DSN (default: $DATABASE_URL); selects Postgres")
    sub = p.add_subparsers(dest="command", required=True)

    sub.add_parser("show", help="List superadmin keys; print plaintext if retained.")

    rp = sub.add_parser("reset", help="Generate a new superadmin key.")
    rp.add_argument("--id", help="Rotate the superadmin row with this id.")
    rp.add_argument("--new", action="store_true", help="Always insert a new row instead of rotating.")
    rp.add_argument("--name", help="Name for the key (default: 'superadmin-reset').")
    rp.add_argument("--yes", action="store_true", help="Skip the confirmation prompt.")

    args = p.parse_args()
    store = open_store(args)
    try:
        if args.command == "show":
            return cmd_show(store)
        return cmd_reset(store, args)
    finally:
        store.conn.close()


if __name__ == "__main__":
    sys.exit(main())
