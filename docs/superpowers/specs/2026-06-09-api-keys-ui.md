# API Keys Management UI — Design Spec

**Date:** 2026-06-09  
**Status:** Approved

---

## Problem

There is no UI for managing API keys. Users must use raw `curl` commands against the admin REST API to create keys, which is a poor experience and a barrier to onboarding.

---

## Goal

A standalone `/api-keys` page that lets admins create and revoke team-level and user-level API keys, with the raw key revealed inline immediately after creation (one-time only).

---

## Backend (already implemented)

All required endpoints exist under the `admin` role group:

| Method | Path | Purpose |
|--------|------|---------|
| `GET` | `/api/api-keys` | List all keys for the team (keys are masked — no raw value) |
| `POST` | `/api/api-keys` | Create a key — response includes `raw_key` (only time it's returned) |
| `DELETE` | `/api/api-keys/:id` | Revoke a key |

`POST /api/api-keys` body: `{ name, role, key_type ("team"|"user"), user_id? }`  
Valid roles: `member`, `curator`, `admin` (cannot grant equal/higher than caller's role).

The frontend already has `createAPIKey(name, role, keyType)` in `web/src/lib/api.ts:374`.

Missing frontend helpers needed: `listAPIKeys()`, `revokeAPIKey(id)`, `listUsers()` (for user picker dropdown).

---

## Page Design

### Route & Navigation

- Route: `/api-keys`
- Nav entry: "API Keys" with a `KeyRound` icon, added to `Layout.tsx` nav array after "Settings"
- Role gate: only shown/accessible to `admin` and `superadmin`

### Layout

Single page, two independent sections stacked vertically:

**Team Keys section**
- Header: "Team Keys" + subtitle "Shared credentials not tied to a specific user" + "New Team Key" button (right-aligned)
- Clicking "New Team Key" expands an inline create form directly below the header (not a modal)
- Table columns: Name | Key | Role | Created | (Revoke button)

**User Keys section**  
- Header: "User Keys" + subtitle "Personal keys tied to a team member" + "New User Key" button
- Same inline form pattern
- Table columns: Name | Key | User | Role | Created | (Revoke button)
- User column shows email/ID from the team user list

### Inline Create Form

Appears below the section header when "New …" button is clicked. Dismissed by Cancel or after successful creation.

- **Team Key form fields:** Name (text input, required) · Role (select: member/curator/admin)
- **User Key form fields:** Name · Role · User (select dropdown populated from `GET /api/users`)
- "Create" button calls `POST /api/api-keys` with appropriate `key_type`

### Key Reveal (inline row)

After successful creation the API returns `{ raw_key, ... }`. The new key is inserted at the top of the relevant table in a **highlighted reveal row**:

- Green-tinted background row
- Key column shows the full `raw_key` in monospace + "Copy" button (uses `navigator.clipboard`)
- Amber warning: "Copy now — won't be shown again"
- "Got it ✓" button in the action column dismisses the reveal (collapses to masked `tk_••••••••`)

Once dismissed (or on page reload), the key is masked permanently.

### Masked Keys

All keys except the just-created reveal row show `tk_••••••••••••` in the Key column.

### Revoke

"Revoke" button (red-tinted) calls `DELETE /api/api-keys/:id`. On success, row is removed from the table. No confirmation dialog — revoke is immediate (fast recovery if accidental: just create a new key).

### Empty States

Each section shows a subtle "No team keys yet" / "No user keys yet" placeholder row when empty.

### Loading & Error States

- Skeleton rows during initial fetch
- Toast/inline error on create or revoke failure
- Form "Create" button shows spinner while in-flight, disabled to prevent double-submit

---

## Frontend Files

| File | Change |
|------|--------|
| `web/src/pages/APIKeys.tsx` | New page component |
| `web/src/lib/api.ts` | Add `listAPIKeys()`, `revokeAPIKey(id)`, `listUsers()` |
| `web/src/App.tsx` | Add `/api-keys` route |
| `web/src/components/Layout.tsx` | Add "API Keys" nav entry |

---

## Constraints

- Dark theme throughout (matches existing shadcn/ui dark theme)
- No modal dialogs — all interactions inline on the page
- `DEV_BYPASS_AUTH=true` injects superadmin context so the page works in local dev without a real key
- Do not add role-hiding logic to Layout yet — role-aware nav is a future task
