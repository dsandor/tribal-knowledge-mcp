# TODO Subsystem — Design Spec

**Date:** 2026-07-20
**Status:** Approved (design), pending implementation plan
**Proposed roadmap slot:** Phase 11

## Purpose

Make todos a first-class citizen in the tribal-knowledge server, alongside knowledge entries. Users and LLMs can create, view, edit, complete, and delete todos. Todos are grouped into named lists, carry a rich status/priority/due workflow, optionally reference knowledge entries, and can be linked to external issue trackers (Jira, ServiceNow, GitHub Issues, GitLab Issues). External-tracker **integrations are not built** — only the schema and data points to support them later.

The subsystem exposes:
- **Rich MCP tools** whose descriptions teach an LLM the correct workflow.
- A **beautiful web console** (Kanban board + list view) using the existing shadcn/Tailwind dark theme.
- A **REST API** backing the web UI.

## Design decisions (resolved during brainstorming)

1. **Ownership:** Todos are owned + assignable and team-visible. Each item has a `created_by` and an optional `assignee`. Supports "assigned to me" / "created by me" views.
2. **Structure:** 2-tier. `TodoList` is a first-class container; `TodoItem` belongs to a list.
3. **Workflow:** Rich status `open → in_progress → blocked → done → cancelled`, with `priority` (low/medium/high/urgent), `due_date`, `completed_at`.
4. **Knowledge link:** A todo may reference 0..N knowledge entries (bidirectional; shown on both the todo and the Knowledge Detail page).
5. **UI primary view:** Kanban board (columns by status) with a list-view toggle. Todos get their own top-level nav entry and a Dashboard widget.
6. **No vector embeddings for todos** — they are relational/actionable. Search is filter- and keyword-based.

## Data model

All entities are team-scoped. Identity resolves through `auth.GetTeamContext(ctx).EffectiveActorID()` (user → key → local), matching existing per-user features, because web requests often lack a UserID.

### `TodoList`
| Field | Type | Notes |
|---|---|---|
| ID | string (uuid) | assigned on create |
| TeamID | string | scoping |
| Name | string | required |
| Description | string | optional, markdown |
| Domain | string | optional — associates the list with a knowledge domain |
| Color | string | optional — UI accent |
| Archived | bool | soft-hide without delete |
| CreatedBy | string | actor id |
| CreatedAt / UpdatedAt | time.Time | |
| (derived) OpenCount / TotalCount | int | computed on read |

### `TodoItem`
| Field | Type | Notes |
|---|---|---|
| ID | string (uuid) | |
| ListID | string | FK → TodoList |
| TeamID | string | denormalized for scoping |
| Title | string | required |
| Notes | string | markdown body |
| Status | string enum | `open` \| `in_progress` \| `blocked` \| `done` \| `cancelled` |
| Priority | string enum | `low` \| `medium` \| `high` \| `urgent` |
| CreatedBy | string | actor id |
| Assignee | string | actor id, optional |
| DueDate | *time.Time | optional |
| CompletedAt | *time.Time | set when status → done |
| Position | int | manual ordering within a list/column |
| Tags | []string | |
| ExternalLinks | []ExternalLink | child rows |
| KnowledgeRefs | []string | entry IDs (join table) |
| CreatedAt / UpdatedAt | time.Time | |

### `ExternalLink` (child table `todo_external_links`)
| Field | Type | Notes |
|---|---|---|
| ID | string | |
| TodoID | string | FK |
| Provider | string enum | `jira` \| `servicenow` \| `github` \| `gitlab` \| `other` |
| ExternalID | string | e.g. `PROJ-123`, `#456` |
| URL | string | deep link |
| ExternalStatus | string | free-form mirror of remote status (no sync yet) |
| SyncedAt | *time.Time | reserved for future integration |

Stored as a child table (not JSON) so a future integration can query by `(provider, external_id)`.

### Knowledge refs (join table `todo_knowledge_refs`)
`(todo_id, entry_id)` — enables the bidirectional view: knowledge refs on a todo, and a "Related todos" panel on the Knowledge Detail page.

## Storage layer

New files mirroring the `rules` feature shape:
- `internal/storage/todos.go` — models + `TodoStore` interface.
- `internal/storage/todos_sqlite.go` — SQLite implementation.
- `internal/storage/postgres_todos.go` — Postgres implementation.

Tables: `todo_lists`, `todo_items`, `todo_external_links`, `todo_knowledge_refs`. Created idempotently in the existing schema-init path for both backends. All queries filtered by `team_id`.

### `TodoStore` interface (draft)
```go
type TodoStore interface {
    // Lists
    CreateTodoList(ctx, list TodoList) (string, error)
    GetTodoList(ctx, id string) (*TodoList, error)
    ListTodoLists(ctx, teamID string, includeArchived bool) ([]TodoList, error)
    UpdateTodoList(ctx, list TodoList) error
    DeleteTodoList(ctx, id string) error // cascades items

    // Items
    CreateTodo(ctx, item TodoItem) (string, error)
    GetTodo(ctx, id string) (*TodoItem, error)
    QueryTodos(ctx, filter TodoFilter) ([]TodoItem, error)
    UpdateTodo(ctx, item TodoItem) error
    CompleteTodo(ctx, id string) error
    DeleteTodo(ctx, id string) error

    // Links
    AddExternalLink(ctx, link ExternalLink) (string, error)
    RemoveExternalLink(ctx, linkID string) error
    SetKnowledgeRefs(ctx, todoID string, entryIDs []string) error
    ListTodosForEntry(ctx, entryID string) ([]TodoItem, error)
}

type TodoFilter struct {
    TeamID   string
    ListID   string   // optional
    Status   []string // optional
    Assignee string   // optional; "" = any
    Priority []string // optional
    Tag      string   // optional
    DueBefore *time.Time // for overdue/soon
    Query    string   // optional keyword match on title/notes
    Limit    int      // 0 = default (50), negative = unlimited
}
```

## MCP tools (`internal/mcp/todo_tools.go`)

Registered via `RegisterTodoTools(s *server.MCPServer, store storage.TodoStore)` following the `RegisterRuleTools` pattern. Tool descriptions are written to **teach the workflow** (when to make a list vs add to one, what each status means, when to link an issue).

| Tool | Purpose |
|---|---|
| `todo_lists` | Enumerate lists with open/total counts |
| `todo_list_create` | Create a list |
| `todo_list_update` | Rename / describe / archive a list |
| `todo_list_delete` | Delete a list (and its items) |
| `todo_add` | Create an item by `list_id` or `list_name` (auto-creates the list by name) |
| `todo_get` | Full item incl. links + knowledge refs |
| `todo_query` | Filtered search: status, assignee (`me`), priority, due/overdue, list, tag, keyword |
| `todo_update` | Patch any field incl. status transition and assignment |
| `todo_complete` | Convenience: mark done + set `completed_at` |
| `todo_delete` | Remove an item |
| `todo_link_issue` | Attach a Jira/ServiceNow/GitHub/GitLab link |
| `todo_link_knowledge` | Attach/detach knowledge entry references |

**MCP resources:** `todos://mine`, `todos://overdue`, `todos://list/{id}`.
**MCP prompt:** `manage_todos` — explains conventions and the intended tool workflow to the LLM.

## REST API (`internal/web/todo_handlers.go`, chi)

| Method + path | Action |
|---|---|
| `GET /api/todo-lists` | list (query `?archived=`) |
| `POST /api/todo-lists` | create |
| `GET/PUT/DELETE /api/todo-lists/:id` | read / update / delete |
| `GET /api/todo-lists/:id/items` | items in a list |
| `GET /api/todos` | filtered query (status/assignee/priority/due/tag/list/q) |
| `POST /api/todos` | create |
| `GET/PUT/DELETE /api/todos/:id` | read / update / delete |
| `POST /api/todos/:id/complete` | mark done |
| `POST/DELETE /api/todos/:id/links` | add/remove external link |
| `POST/DELETE /api/todos/:id/knowledge-refs` | attach/detach entry refs |

Role + team scoping enforced like existing handlers. Todo create/complete recorded via existing `RecordActivity` so they appear in the activity feed.

## Web UI (React + existing shadcn/Tailwind dark theme)

- New **Todos** top-level nav entry (`web/src/pages/Todos.tsx`).
- Primary view = **Kanban board**: columns Open / In Progress / Blocked / Done, drag to reorder and move across columns; **list-view toggle**.
- Left sidebar selects a `TodoList` (or "All"); filter bar for Assignee (Me/All), Priority, Due.
- Cards: priority chip, due-date urgency color, assignee avatar, provider badges, knowledge-ref count.
- **Todo detail drawer**: edit all fields; manage external links (with open-in-tracker buttons) and knowledge refs (linking to Knowledge Detail).
- **"Related todos"** panel added to the existing Knowledge Detail page.
- **Dashboard widget**: "My open todos" + overdue count.
- API client additions in `web/src/lib`.

## Testing & verification

TDD per superpowers:
- Go unit tests for `TodoStore` (SQLite + Postgres), covering team scoping, cascade delete, filter/query, external links, knowledge-ref round-trip and reverse lookup.
- MCP handler tests (valid/invalid input, actor resolution).
- Web handler tests (team scoping, role enforcement, actor resolution when UserID is absent).
- Verification before "done": `go test ./...`, `go build ./...`, and a clean Vite production build (`make web` / vite build).

## Build approach

Subagent-driven development (the "team of experts"), decomposed and sequenced:
1. Storage layer (`TodoStore` + SQLite + Postgres + tests)
2. MCP tools + resources + prompt (+ registration in `server.go`)
3. REST handlers (+ route registration)
4. Web UI (Todos page, board/list, detail drawer, Knowledge Detail panel, Dashboard widget)
5. Wiring, end-to-end verification, docs (README + CHANGELOG), `.planning` Phase 11 update

## Out of scope (v1)

- Actual synchronization with Jira/ServiceNow/GitHub/GitLab (schema-only support now).
- Vector/semantic search over todos.
- Recurring todos, reminders/notifications, subtasks/checklists within an item.
- Todo participation in `enrich_context` (natural future extension).
