# First user becomes superadmin — design

**Date:** 2026-06-23
**Status:** Approved

## Problem

A fresh deployment has no superadmin user. Superadmin is granted only via the
`SUPERADMIN_KEY` bootstrap API key (`cmd/server/main.go:319`) or
`DEV_BYPASS_AUTH`. There is no path to promote an OIDC (or local) *user* to
superadmin — `validAdminRole` deliberately excludes `superadmin`. So after OIDC
is configured, the first user to sign in lands as `member` and gets a 403 on
superadmin-only routes such as `POST /api/admin/teams`, blocking onboarding
without manual env/key configuration.

## Goal

On an empty deployment (no superadmin *user* exists), the first user to
authenticate is automatically promoted to `superadmin`, so initial onboarding
needs no manual key configuration.

## Decisions

- **Trigger:** promote when *no superadmin user exists* in the `users` table.
  The `SUPERADMIN_KEY` bootstrap creates an API *key* (no `users` row), so it
  never affects this check — which is why auto-promotion happens regardless of
  whether the key is set.
- **Scope:** any auth method. In practice only OIDC creates/identifies a first
  user today (no local-user registration path exists), so the local hook is a
  no-op until/unless a local-user creation flow is added. We add the hook anyway
  so the behavior is consistent for free.
- **`manually_assigned`** is set to `1` on promotion so the auto team-assignment
  logic does not bounce the owner between teams.

## Design

### Storage

New `TeamStore` method, implemented for SQLite and Postgres:

```go
// ClaimFirstSuperadmin promotes userID to superadmin iff no superadmin user
// currently exists. Returns true if it promoted. Idempotent; safe to call on
// every login.
ClaimFirstSuperadmin(ctx context.Context, userID string) (bool, error)
```

Implemented as a single conditional UPDATE (atomic in both engines):

```sql
UPDATE users SET role='superadmin', manually_assigned=1
WHERE id=? AND NOT EXISTS (SELECT 1 FROM users WHERE role='superadmin')
```

`promoted` is derived from rows-affected (1 = promoted, 0 = a superadmin already
existed or the id was unknown).

Rationale for placement: keeping the atomic claim in storage (where transactions
live) while the *decision to call it* stays in the auth/handler layer. Rejected
alternatives: embedding count+promote in `UpsertUser` (wrong layer, runs on
every upsert), and count-then-update in the handler (non-atomic across two
calls).

### Call sites

- `handleOIDCCallback` (`internal/web/auth_handlers.go`): after `UpsertUser`,
  call `ClaimFirstSuperadmin(uid)`. On `promoted == true`, emit an
  `slog.Info` audit line ("bootstrapped first superadmin", user id + email).
- `handleLogin` (local): after successful password verification, same call.

### Effect

The session middleware reads `user.Role` live from the DB on every request
(`internal/auth/middleware.go:139-141`), so a promoted user is superadmin on
their next request — no session/token format changes required.

## Residual race (accepted)

Two *different* brand-new users hitting the callback within milliseconds on a
never-before-onboarded deployment could both promote (Postgres READ COMMITTED,
different rows → no lock conflict). This is vanishingly unlikely during
onboarding, and "the org's first two logins both become owner" is not a security
failure. A partial unique index would prevent it but would also forbid the
legitimate later state of multiple superadmins, so we accept the race.

## Testing

- Storage tests (SQLite + Postgres if harness available):
  - promotes when no superadmin exists (returns true, role updated)
  - no-op when a superadmin already exists (returns false, role unchanged)
  - second call targeting a different user after one promotion returns false
  - unknown user id returns false, no error
- Handler test for OIDC callback first-user promotion if a provider seam exists.

## Docs

- `docs/oauth-providers.md`: note `SUPERADMIN_KEY` is optional for onboarding —
  the first user to sign in owns the deployment.
- `.env.example`: same note near `SUPERADMIN_KEY`.
