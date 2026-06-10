# Live Realtime Dashboard — Design Spec

**Date:** 2026-06-10
**Status:** Approved design, pending implementation plan
**Topic:** Real-time presence + live activity feed on the dashboard

## Goal

Give the dashboard a live, animated sense of "what is everyone doing right now":
1. **Online now** — how many users (and who) are currently active.
2. **Live activity feed** — prompts and actions spooling through the system in near-real-time, animated, including **verbatim fragments** of the actual data.

## Decisions (locked)

| Decision | Choice |
|---|---|
| Data fragments | **Verbatim**, no redaction (length-capped, HTML-escaped at render). Optional default-OFF secret scrubber. |
| Presence model | **Recent-activity window** — online = authenticated activity within the last 60s. No client heartbeat. |
| Scope | **Team-scoped**; superadmin sees all teams. |
| Event types | enrich_context (incoming prompts), knowledge stored/used/rated, pipeline_complete/agent_generated, approve/reject, sign-in. |
| Transport | **In-process SSE hub** (Approach A), behind an `EventBus` interface for a future Redis swap. |

## Existing foundation (reuse, don't rebuild)

- `feed_activity` table + `ListActivity` + `GET /api/activity` already persist rich events. Reused for the **snapshot/backfill**. `event_type` is free text and `metadata` is JSON, so **no schema change** is required — new event types and the `fragment`/`title` fields ride in existing columns.
- `web/src/pages/Dashboard.tsx` already polls `/api/activity` every 6s and renders the feed via `ActivityFeed`, `mergeEvents`, `relativeTime`, `eventIcon`, `eventLabel`, with an 800ms "new event" highlight and a `LiveDot`. These rendering helpers are **reused**; only the data source changes (poll → stream), and a `fragment` field is added.
- `api_keys.last_used_at` is touched on every authenticated request (`TouchAPIKey`) — confirms auth middleware is the right place to also update presence.
- The current "active users" count is faked from the contributions leaderboard length; it is **replaced** with real presence.
- Frontend deps: MUI 9 + emotion + lucide-react + clsx. **No new dependency** — animations use MUI `Grow`/`Fade` + CSS keyframes.

## Architecture

### 1. Event bus & event model — `internal/live/`

```go
// internal/live/event.go
type ActorRef struct {
    ID      string `json:"id"`
    Display string `json:"display"` // resolved email/name, falls back to id
}

type LiveEvent struct {
    ID        string            `json:"id"`
    Type      string            `json:"type"`   // enrich_context | knowledge_stored | knowledge_used |
                                                 // knowledge_rated | approved | rejected |
                                                 // pipeline_complete | agent_generated | signin | presence
    TeamID    string            `json:"-"`      // used for fan-out filtering; never serialized to clients
    Actor     ActorRef          `json:"actor"`
    Fragment  string            `json:"fragment,omitempty"` // verbatim text, length-capped
    EntryID   string            `json:"entry_id,omitempty"`
    Title     string            `json:"title,omitempty"`
    Meta      map[string]string `json:"meta,omitempty"`
    CreatedAt time.Time         `json:"created_at"`
}

// internal/live/hub.go
type EventBus interface {
    Publish(ev LiveEvent)
    Subscribe(teamID string, superadmin bool) (<-chan LiveEvent, func())
}
```

- **Hub** is the in-memory `EventBus`. Each subscriber holds a buffered channel (cap ~256). `Publish` fans out to every subscriber whose `(teamID == ev.TeamID) || superadmin`. Sends are **non-blocking with drop-oldest** so a slow/stalled dashboard cannot block producers or the server.
- A global subscriber cap (e.g. 256 concurrent streams) guards resources; excess connects get `503`.
- `TeamID` is filtered server-side and stripped from the JSON sent to clients (privacy).

### 2. Presence — `internal/live/presence.go`

- In-memory registry: `map[teamID]map[actorID]presenceEntry{ display, lastSeen }`, guarded by a mutex.
- `Touch(teamID, actor)` is called from the auth middleware on **every authenticated request** (web API and remote-MCP HTTP — covers analysts' MCP clients). `online = now - lastSeen <= 60s`.
- A **10s ticker** recomputes each team's online set, diffs against the previous tick, and `Publish`es `presence` events (`meta: {online_count, joined, left}`) plus updates the per-team online roster used in the snapshot. Entries past the window are evicted.
- `Snapshot(teamID, superadmin) []ActorRef` returns the current online roster for connect-time hydration.

### 3. SSE endpoint — `internal/web/stream_handler.go`

`GET /api/activity/stream` (registered under the authenticated route group):
- Headers: `Content-Type: text/event-stream`, `Cache-Control: no-cache`, `Connection: keep-alive`, `X-Accel-Buffering: no` (defeats proxy buffering).
- Requires `http.Flusher`; resolves team context from `auth.GetTeamContext`.
- On connect: write `event: snapshot` with `{ online: []ActorRef, recent: []LiveEvent }` (recent = last 30 from `feed_activity` via `ListActivity`, newest-last — matching the dashboard's current feed length), then `Subscribe(teamID, superadmin)` and loop:
  - on event → `event: activity` (or `event: presence`) + `data: <json>\n\n` + flush.
  - every ~20s with no traffic → `: ping\n\n` keepalive.
  - on `r.Context().Done()` → unsubscribe and return.
- Superadmin subscribes across all teams; everyone else is restricted to their own `TeamID`.

### 4. Producers (publish to hub; keep persisting where they already do)

Inject a `live.EventBus` into the relevant call sites:
- **`internal/mcp/enrich_context.go`** — NEW: on each call, build a `LiveEvent{Type: "enrich_context", Fragment: <raw user message, capped>}` and `Publish`. Also persist a capped fragment to `feed_activity` (so the snapshot includes recent prompts). This is the headline "prompts flowing" signal.
- **`internal/mcp/usage_tools.go`** — `knowledge_used` / `knowledge_rated` on `RecordUsage` / rating.
- **knowledge store** — `internal/web/handlers.go: handleKnowledgeStore` and the MCP store tool → `knowledge_stored`.
- **approvals** — `internal/web/batch_handlers.go: handleBatchApprove/handleBatchReject` → `approved` / `rejected`.
- **pipeline** — `internal/pipeline/pipeline.go` (already calls `RecordActivity`) → also `Publish` `pipeline_complete` / `agent_generated`.
- **sign-in** — `internal/web/auth_handlers.go: handleLogin` → `signin`.

Actor display names are resolved best-effort (user email/name, else key name, else id). Fragments are capped at **280 chars** stored / surfaced; the cap constant lives in `internal/live`.

### 5. Privacy & security

- Team-scoped fan-out enforced in the hub; cross-team only for superadmin. `TeamID` never serialized to clients.
- Fragments rendered as React text nodes (auto HTML-escaped → no XSS) and hard-capped server-side.
- **`SCRUB_SECRETS`** setting (env + team setting), **default OFF**. When ON, a regex pass redacts `sk-[A-Za-z0-9-]+`, `AKIA[0-9A-Z]{16}`, `Bearer <token>`, and email addresses in `Fragment` before publish/persist. Lives in `internal/live/scrub.go` so producers call one helper.
- Stream endpoint behind `RequireAuth`; subscriber cap prevents resource exhaustion.

### 6. Frontend — `web/src/`

- **`hooks/useActivityStream.ts`** — opens `/api/activity/stream` via `fetch` + `ReadableStream` reader (so it can send `Authorization: Bearer` from `localStorage`, unlike native `EventSource`). Parses SSE frames (`event:`/`data:`), dispatches snapshot vs activity vs presence. Exposes `{ online: ActorRef[], events: ActivityEvent[], connected: boolean }`. Auto-reconnects with capped exponential backoff; caps `events` at ~100; coalesces bursts via `requestAnimationFrame`.
- **`components/OnlineNow.tsx`** — live count + animated user chips (display name, role color), gentle pulse/`Grow` on join, fade on leave.
- **`components/LiveActivityFeed.tsx`** — spooling list reusing the existing `eventIcon`/`eventLabel`/`relativeTime` helpers (extracted from `Dashboard.tsx` into a shared module). New rows `Grow`/`Fade` in at top, color-coded by type, showing `actor · type · fragment (clamped ~2 lines, expand on click) · relative time`. A **LIVE** indicator bound to `connected`; reconnect banner on drop; **pause-on-hover** halts auto-advance so a fragment can be read.
- **`pages/Dashboard.tsx`** — replace the 6s activity poll with `useActivityStream`; feed the hook's `events` into the existing feed renderer; replace the faked `activeUsers` with `online.length` and render `OnlineNow`. Stats/trending/analytics polling unchanged.
- **`lib/api.ts`** — add `fragment?`, `title?` to `ActivityEvent`; add `ActorRef` type.

### 7. Module/file plan

Backend: `internal/live/{event,hub,presence,scrub}.go`, `internal/web/stream_handler.go`; wiring in `internal/web/server.go` (own hub+presence, expose to handlers), `internal/auth/middleware.go` (presence touch), and the producer files in §4.

Frontend: `web/src/hooks/useActivityStream.ts`, `web/src/components/{OnlineNow,LiveActivityFeed}.tsx`, shared `web/src/lib/activity.ts` (extracted icon/label/time helpers), edits to `web/src/pages/Dashboard.tsx` and `web/src/lib/api.ts`.

### 8. Testing

- **Go:** hub fan-out + team filtering + superadmin-all + drop-on-full; presence touch/expiry/diff; SSE handler via `httptest` (assert snapshot frame then a live frame after a `Publish`, and context-cancel cleanup); a producer test asserting `enrich_context` publishes with a capped fragment; scrub helper unit tests.
- **Frontend:** `useActivityStream` SSE-frame parser unit test (mock `ReadableStream`), event-cap behavior, presence add/remove. `npm run build` (tsc + vite) must pass.

### 9. Build sequence (expert team, after the implementation plan)

1. Backend (Go developer): `internal/live` + SSE handler + producer wiring + Go tests.
2. UX (ux-expert): feed/online widget visual + animation spec — runs **in parallel** with (1).
3. Frontend (developer): hook + components + Dashboard integration, consuming (1)'s frames and (2)'s design.
4. Review/QA (code-reviewer + qa-engineer): correctness + `go test` + `npm run build` verification before completion.

## Out of scope (YAGNI)

- Multi-instance fan-out (Redis) — interface left in place for later.
- Client heartbeat / idle-vs-offline distinction.
- Historical playback / time-scrubbing of the feed.
- Per-user mute/filter controls (could be a fast follow).

## Risks

- **Verbatim fragments can leak secrets** (e.g. an API key pasted into a prompt). Mitigated only by team-scoping + auth + the optional scrubber; accepted per decision. Revisit if the dashboard audience widens.
- In-memory presence/hub reset on restart — acceptable; snapshot rehydrates the feed from `feed_activity`, presence repopulates within one activity window.
