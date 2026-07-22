# TODO Subsystem Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Todos as first-class citizens — 2-tier TodoLists/TodoItems with rich workflow, external-tracker link schema, knowledge refs, 12 MCP tools, REST API, and a Kanban web UI.

**Architecture:** Mirrors the `rules` feature shape: `TodoStore` interface in `internal/storage/todos.go` with SQLite (`todos_sqlite.go`) and Postgres (`postgres_todos.go`) implementations; MCP tools in `internal/mcp/todo_tools.go` registered via `RegisterTodoTools`; chi REST handlers in `internal/web/todo_handlers.go`; MUI React pages in `web/src/`.

**Tech Stack:** Go 1.x, mattn/go-sqlite3, lib/pq, mark3labs/mcp-go, chi router; React + TypeScript + Vite + **MUI** (`@mui/material`) + lucide-react icons. NO new dependencies — Kanban drag-and-drop uses native HTML5 drag events.

**Spec:** `docs/superpowers/specs/2026-07-20-todo-subsystem-design.md`. NOTE: the spec says "shadcn/Tailwind"; the actual codebase uses MUI — follow MUI (`web/src/components/Layout.tsx` is the style reference).

## Global Constraints

- **DO NOT COMMIT OR PUSH TO GIT. EVER.** The developer commits manually. Plan steps therefore have NO commit steps — each task ends with a verification step instead.
- All todo data is team-scoped via `team_id`; identity resolves via `auth.GetTeamContext(ctx).EffectiveActorID()` (web requests often lack a UserID — never assume `UserID != ""`).
- Status enum: `open | in_progress | blocked | done | cancelled`. Priority enum: `low | medium | high | urgent`.
- External link providers: `jira | servicenow | github | gitlab | other`. NO tracker integrations — schema only.
- Empty JSON results must be `[]`, never `null` (existing convention).
- SQLite timestamps: store `time.Time` values as UTC strings `"2006-01-02 15:04:05"` (matches `CURRENT_TIMESTAMP`); parse with the existing `parseTimestamp` helper (`internal/storage/sqlite.go`).
- Verify with: `go build ./...`, `go test ./...`, and `cd web && npm run build` for frontend tasks.
- `ls` is aliased on this machine — use `/bin/ls` in shell commands.

---

### Task 1: Storage models, `TodoStore` interface, SQLite schema + TodoList CRUD

**Files:**
- Create: `internal/storage/todos.go`
- Create: `internal/storage/todos_sqlite.go`
- Create: `internal/storage/todos_test.go`
- Modify: `internal/storage/sqlite.go` (add todo tables at the end of `migrate()`, before its final `return nil`)

**Interfaces:**
- Consumes: existing `parseTimestamp(string) time.Time`, `ErrNotFound`, `*SQLiteStore` (all in `internal/storage/sqlite.go`), `newTestAnalysisStore(t)` test helper (`analysis_test.go`).
- Produces: `TodoList`, `TodoItem`, `ExternalLink`, `TodoFilter` structs; `TodoStore` interface; SQLite impls of `CreateTodoList`, `GetTodoList`, `ListTodoLists`, `UpdateTodoList`, `DeleteTodoList`. All later tasks use these exact names/types.

- [ ] **Step 1: Write `internal/storage/todos.go`** — models + interface (complete file):

```go
package storage

import (
	"context"
	"time"
)

type TodoStatus string

const (
	TodoStatusOpen       TodoStatus = "open"
	TodoStatusInProgress TodoStatus = "in_progress"
	TodoStatusBlocked    TodoStatus = "blocked"
	TodoStatusDone       TodoStatus = "done"
	TodoStatusCancelled  TodoStatus = "cancelled"
)

// ValidTodoStatus reports whether s is one of the allowed status values.
func ValidTodoStatus(s string) bool {
	switch TodoStatus(s) {
	case TodoStatusOpen, TodoStatusInProgress, TodoStatusBlocked, TodoStatusDone, TodoStatusCancelled:
		return true
	}
	return false
}

type TodoPriority string

const (
	TodoPriorityLow    TodoPriority = "low"
	TodoPriorityMedium TodoPriority = "medium"
	TodoPriorityHigh   TodoPriority = "high"
	TodoPriorityUrgent TodoPriority = "urgent"
)

// ValidTodoPriority reports whether p is one of the allowed priority values.
func ValidTodoPriority(p string) bool {
	switch TodoPriority(p) {
	case TodoPriorityLow, TodoPriorityMedium, TodoPriorityHigh, TodoPriorityUrgent:
		return true
	}
	return false
}

// ValidLinkProvider reports whether p is a supported external tracker provider.
func ValidLinkProvider(p string) bool {
	switch p {
	case "jira", "servicenow", "github", "gitlab", "other":
		return true
	}
	return false
}

// TodoList is a named container of todo items, scoped to a team.
type TodoList struct {
	ID          string
	TeamID      string
	Name        string
	Description string
	Domain      string // optional knowledge-domain association
	Color       string // optional UI accent
	Archived    bool
	CreatedBy   string // actor id
	CreatedAt   time.Time
	UpdatedAt   time.Time
	OpenCount   int // derived on read: items not done/cancelled
	TotalCount  int // derived on read
}

// TodoItem is a single actionable item belonging to a TodoList.
type TodoItem struct {
	ID            string
	ListID        string
	TeamID        string
	Title         string
	Notes         string // markdown
	Status        TodoStatus
	Priority      TodoPriority
	CreatedBy     string // actor id
	Assignee      string // actor id, optional
	DueDate       *time.Time
	CompletedAt   *time.Time
	Position      int
	Tags          []string
	ExternalLinks []ExternalLink // populated by GetTodo
	KnowledgeRefs []string       // entry IDs; populated by GetTodo
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

// ExternalLink associates a todo with an issue in an external tracker.
// No integration is built — schema only; SyncedAt is reserved for future sync.
type ExternalLink struct {
	ID             string
	TodoID         string
	Provider       string // jira | servicenow | github | gitlab | other
	ExternalID     string // e.g. "PROJ-123", "#456"
	URL            string
	ExternalStatus string // free-form mirror of remote status
	SyncedAt       *time.Time
	CreatedAt      time.Time
}

// TodoFilter selects todo items. Zero values mean "no filter".
type TodoFilter struct {
	TeamID    string
	ListID    string
	Status    []string
	Assignee  string
	Priority  []string
	Tag       string
	DueBefore *time.Time // items with due_date < DueBefore (overdue/soon)
	Query     string     // keyword match on title/notes
	Limit     int        // 0 = default (50), negative = unlimited
}

type TodoStore interface {
	// Lists
	CreateTodoList(ctx context.Context, list TodoList) (string, error)
	GetTodoList(ctx context.Context, id string) (*TodoList, error)
	ListTodoLists(ctx context.Context, teamID string, includeArchived bool) ([]TodoList, error)
	UpdateTodoList(ctx context.Context, list TodoList) error
	DeleteTodoList(ctx context.Context, id string) error // cascades items

	// Items
	CreateTodo(ctx context.Context, item TodoItem) (string, error)
	GetTodo(ctx context.Context, id string) (*TodoItem, error) // includes links + refs
	QueryTodos(ctx context.Context, filter TodoFilter) ([]TodoItem, error)
	UpdateTodo(ctx context.Context, item TodoItem) error
	CompleteTodo(ctx context.Context, id string) error
	DeleteTodo(ctx context.Context, id string) error

	// Links
	AddExternalLink(ctx context.Context, link ExternalLink) (string, error)
	RemoveExternalLink(ctx context.Context, linkID string) error
	SetKnowledgeRefs(ctx context.Context, todoID string, entryIDs []string) error
	ListTodosForEntry(ctx context.Context, entryID string) ([]TodoItem, error)
}
```

- [ ] **Step 2: Add SQLite schema to `migrate()`** in `internal/storage/sqlite.go`, immediately before the final `return nil` of `migrate()`:

```go
	for _, stmt := range []string{
		`CREATE TABLE IF NOT EXISTS todo_lists (
			id          TEXT PRIMARY KEY,
			team_id     TEXT NOT NULL DEFAULT '',
			name        TEXT NOT NULL,
			description TEXT NOT NULL DEFAULT '',
			domain      TEXT NOT NULL DEFAULT '',
			color       TEXT NOT NULL DEFAULT '',
			archived    INTEGER NOT NULL DEFAULT 0,
			created_by  TEXT NOT NULL DEFAULT '',
			created_at  DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at  DATETIME DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE TABLE IF NOT EXISTS todo_items (
			id           TEXT PRIMARY KEY,
			list_id      TEXT NOT NULL REFERENCES todo_lists(id) ON DELETE CASCADE,
			team_id      TEXT NOT NULL DEFAULT '',
			title        TEXT NOT NULL,
			notes        TEXT NOT NULL DEFAULT '',
			status       TEXT NOT NULL DEFAULT 'open',
			priority     TEXT NOT NULL DEFAULT 'medium',
			created_by   TEXT NOT NULL DEFAULT '',
			assignee     TEXT NOT NULL DEFAULT '',
			due_date     DATETIME,
			completed_at DATETIME,
			position     INTEGER NOT NULL DEFAULT 0,
			tags         TEXT NOT NULL DEFAULT '[]',
			created_at   DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at   DATETIME DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE INDEX IF NOT EXISTS idx_todo_items_list ON todo_items(list_id)`,
		`CREATE INDEX IF NOT EXISTS idx_todo_items_team_status ON todo_items(team_id, status)`,
		`CREATE TABLE IF NOT EXISTS todo_external_links (
			id              TEXT PRIMARY KEY,
			todo_id         TEXT NOT NULL REFERENCES todo_items(id) ON DELETE CASCADE,
			provider        TEXT NOT NULL,
			external_id     TEXT NOT NULL DEFAULT '',
			url             TEXT NOT NULL DEFAULT '',
			external_status TEXT NOT NULL DEFAULT '',
			synced_at       DATETIME,
			created_at      DATETIME DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE INDEX IF NOT EXISTS idx_todo_links_todo ON todo_external_links(todo_id)`,
		`CREATE TABLE IF NOT EXISTS todo_knowledge_refs (
			todo_id  TEXT NOT NULL REFERENCES todo_items(id) ON DELETE CASCADE,
			entry_id TEXT NOT NULL,
			PRIMARY KEY (todo_id, entry_id)
		)`,
	} {
		if _, err := s.db.Exec(stmt); err != nil {
			return fmt.Errorf("create todo tables: %w", err)
		}
	}
```

- [ ] **Step 3: Write failing tests** in `internal/storage/todos_test.go` (package `storage`, same style as `rules_test.go`; `newTestAnalysisStore(t)` returns a migrated `*SQLiteStore`):

```go
package storage

import (
	"context"
	"errors"
	"testing"
)

func TestTodoListCRUD(t *testing.T) {
	s := newTestAnalysisStore(t)
	ctx := context.Background()

	id, err := s.CreateTodoList(ctx, TodoList{
		TeamID: "team-a", Name: "Q3 Earnings Review",
		Description: "All Q3 prep work", Domain: "earnings", Color: "#38bdf8",
		CreatedBy: "alice",
	})
	if err != nil {
		t.Fatalf("CreateTodoList: %v", err)
	}
	if id == "" {
		t.Fatal("expected non-empty id")
	}

	l, err := s.GetTodoList(ctx, id)
	if err != nil {
		t.Fatalf("GetTodoList: %v", err)
	}
	if l.Name != "Q3 Earnings Review" || l.TeamID != "team-a" || l.CreatedBy != "alice" {
		t.Errorf("round-trip mismatch: %+v", l)
	}

	l.Name = "Q3 Review (final)"
	l.Archived = true
	if err := s.UpdateTodoList(ctx, *l); err != nil {
		t.Fatalf("UpdateTodoList: %v", err)
	}
	l2, _ := s.GetTodoList(ctx, id)
	if l2.Name != "Q3 Review (final)" || !l2.Archived {
		t.Errorf("update not applied: %+v", l2)
	}

	if err := s.DeleteTodoList(ctx, id); err != nil {
		t.Fatalf("DeleteTodoList: %v", err)
	}
	if _, err := s.GetTodoList(ctx, id); !errors.Is(err, ErrNotFound) {
		t.Fatalf("want ErrNotFound after delete, got %v", err)
	}
}

func TestListTodoLists_TeamScopedAndArchived(t *testing.T) {
	s := newTestAnalysisStore(t)
	ctx := context.Background()

	idA, _ := s.CreateTodoList(ctx, TodoList{TeamID: "team-a", Name: "A"})
	s.CreateTodoList(ctx, TodoList{TeamID: "team-b", Name: "B"})
	arch, _ := s.CreateTodoList(ctx, TodoList{TeamID: "team-a", Name: "Old", Archived: true})

	lists, err := s.ListTodoLists(ctx, "team-a", false)
	if err != nil {
		t.Fatalf("ListTodoLists: %v", err)
	}
	if len(lists) != 1 || lists[0].ID != idA {
		t.Fatalf("want only active team-a list, got %+v", lists)
	}

	all, _ := s.ListTodoLists(ctx, "team-a", true)
	if len(all) != 2 {
		t.Fatalf("want 2 with archived, got %d", len(all))
	}
	_ = arch
}

func TestListTodoLists_Counts(t *testing.T) {
	s := newTestAnalysisStore(t)
	ctx := context.Background()
	lid, _ := s.CreateTodoList(ctx, TodoList{TeamID: "t", Name: "L"})
	s.CreateTodo(ctx, TodoItem{ListID: lid, TeamID: "t", Title: "a"})
	s.CreateTodo(ctx, TodoItem{ListID: lid, TeamID: "t", Title: "b"})
	done, _ := s.CreateTodo(ctx, TodoItem{ListID: lid, TeamID: "t", Title: "c"})
	s.CompleteTodo(ctx, done)

	lists, _ := s.ListTodoLists(ctx, "t", false)
	if len(lists) != 1 || lists[0].TotalCount != 3 || lists[0].OpenCount != 2 {
		t.Fatalf("counts wrong: %+v", lists)
	}
}
```

(`TestListTodoLists_Counts` also needs `CreateTodo`/`CompleteTodo` — those arrive in Task 2; for THIS task write only the first two test funcs, and add the counts test in Task 2.)

- [ ] **Step 4: Run tests to verify they fail** — `go test ./internal/storage/ -run TestTodoList -v` → expected: compile error (`CreateTodoList` undefined).

- [ ] **Step 5: Implement list CRUD** in `internal/storage/todos_sqlite.go`:

```go
package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

// fmtSQLiteTime renders t in the same format CURRENT_TIMESTAMP produces so
// lexicographic SQL comparisons work; nil-safe for nullable columns.
func fmtSQLiteTime(t *time.Time) any {
	if t == nil {
		return nil
	}
	return t.UTC().Format("2006-01-02 15:04:05")
}

func (s *SQLiteStore) CreateTodoList(ctx context.Context, list TodoList) (string, error) {
	list.ID = uuid.NewString()
	archived := 0
	if list.Archived {
		archived = 1
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO todo_lists (id, team_id, name, description, domain, color, archived, created_by)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`, list.ID, list.TeamID, list.Name, list.Description, list.Domain, list.Color, archived, list.CreatedBy)
	if err != nil {
		return "", fmt.Errorf("insert todo list: %w", err)
	}
	return list.ID, nil
}

const todoListCols = `l.id, l.team_id, l.name, l.description, l.domain, l.color, l.archived, l.created_by, l.created_at, l.updated_at,
	COALESCE(c.total, 0), COALESCE(c.open, 0)`

const todoListCountJoin = `LEFT JOIN (
		SELECT list_id,
		       COUNT(*) AS total,
		       SUM(CASE WHEN status NOT IN ('done','cancelled') THEN 1 ELSE 0 END) AS open
		FROM todo_items GROUP BY list_id
	) c ON c.list_id = l.id`

func scanTodoListRow(scan func(...any) error) (*TodoList, error) {
	var l TodoList
	var archived int
	var createdAt, updatedAt string
	err := scan(&l.ID, &l.TeamID, &l.Name, &l.Description, &l.Domain, &l.Color,
		&archived, &l.CreatedBy, &createdAt, &updatedAt, &l.TotalCount, &l.OpenCount)
	if err != nil {
		return nil, err
	}
	l.Archived = archived != 0
	l.CreatedAt = parseTimestamp(createdAt)
	l.UpdatedAt = parseTimestamp(updatedAt)
	return &l, nil
}

func (s *SQLiteStore) GetTodoList(ctx context.Context, id string) (*TodoList, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT `+todoListCols+` FROM todo_lists l `+todoListCountJoin+` WHERE l.id = ?`, id)
	l, err := scanTodoListRow(row.Scan)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("get todo list: %w", err)
	}
	return l, nil
}

func (s *SQLiteStore) ListTodoLists(ctx context.Context, teamID string, includeArchived bool) ([]TodoList, error) {
	query := `SELECT ` + todoListCols + ` FROM todo_lists l ` + todoListCountJoin + ` WHERE l.team_id = ?`
	if !includeArchived {
		query += ` AND l.archived = 0`
	}
	query += ` ORDER BY l.created_at ASC`
	rows, err := s.db.QueryContext(ctx, query, teamID)
	if err != nil {
		return nil, fmt.Errorf("list todo lists: %w", err)
	}
	defer rows.Close()
	lists := []TodoList{}
	for rows.Next() {
		l, err := scanTodoListRow(rows.Scan)
		if err != nil {
			return nil, fmt.Errorf("scan todo list: %w", err)
		}
		lists = append(lists, *l)
	}
	return lists, rows.Err()
}

func (s *SQLiteStore) UpdateTodoList(ctx context.Context, list TodoList) error {
	archived := 0
	if list.Archived {
		archived = 1
	}
	res, err := s.db.ExecContext(ctx, `
		UPDATE todo_lists
		SET name = ?, description = ?, domain = ?, color = ?, archived = ?,
		    updated_at = CURRENT_TIMESTAMP
		WHERE id = ?
	`, list.Name, list.Description, list.Domain, list.Color, archived, list.ID)
	if err != nil {
		return fmt.Errorf("update todo list: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("rows affected: %w", err)
	}
	if n == 0 {
		return fmt.Errorf("todo list %q: %w", list.ID, ErrNotFound)
	}
	return nil
}

func (s *SQLiteStore) DeleteTodoList(ctx context.Context, id string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM todo_lists WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete todo list: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("rows affected: %w", err)
	}
	if n == 0 {
		return fmt.Errorf("todo list %q: %w", id, ErrNotFound)
	}
	return nil
}
```

(`encoding/json` and `strings` imports are used by Task 2's item code in this same file — if the compiler flags them as unused at this stage, add them in Task 2 instead.)

- [ ] **Step 6: Run tests** — `go test ./internal/storage/ -run TestTodoList -v` → expected: `TestTodoListCRUD` and `TestListTodoLists_TeamScopedAndArchived` PASS.
- [ ] **Step 7: Full verify** — `go build ./... && go test ./internal/storage/` → all pass, nothing else broken.

---

### Task 2: SQLite TodoItem CRUD + QueryTodos + CompleteTodo

**Files:**
- Modify: `internal/storage/todos_sqlite.go` (append item methods)
- Modify: `internal/storage/todos_test.go` (add item tests + the deferred `TestListTodoLists_Counts` from Task 1 Step 3)

**Interfaces:**
- Consumes: `TodoItem`, `TodoFilter`, tables from Task 1; `fmtSQLiteTime` helper.
- Produces: SQLite `CreateTodo(ctx, TodoItem) (string, error)`, `GetTodo(ctx, id) (*TodoItem, error)` (links/refs populated — empty slices until Task 3 adds those tables' methods; the queries against `todo_external_links`/`todo_knowledge_refs` are written HERE since tables already exist), `QueryTodos(ctx, TodoFilter) ([]TodoItem, error)`, `UpdateTodo(ctx, TodoItem) error`, `CompleteTodo(ctx, id) error`, `DeleteTodo(ctx, id) error`.

- [ ] **Step 1: Write failing tests** — append to `internal/storage/todos_test.go` (plus `TestListTodoLists_Counts` from Task 1 Step 3):

```go
func TestTodoItemCRUD(t *testing.T) {
	s := newTestAnalysisStore(t)
	ctx := context.Background()
	lid, _ := s.CreateTodoList(ctx, TodoList{TeamID: "t", Name: "L"})

	due := time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC)
	id, err := s.CreateTodo(ctx, TodoItem{
		ListID: lid, TeamID: "t", Title: "Pull AAPL 10-Q",
		Notes: "Focus on **services** margin", Priority: TodoPriorityHigh,
		CreatedBy: "alice", Assignee: "bob", DueDate: &due,
		Tags: []string{"earnings", "aapl"},
	})
	if err != nil {
		t.Fatalf("CreateTodo: %v", err)
	}

	item, err := s.GetTodo(ctx, id)
	if err != nil {
		t.Fatalf("GetTodo: %v", err)
	}
	if item.Title != "Pull AAPL 10-Q" || item.Status != TodoStatusOpen ||
		item.Priority != TodoPriorityHigh || item.Assignee != "bob" {
		t.Errorf("round-trip mismatch: %+v", item)
	}
	if item.DueDate == nil || !item.DueDate.Equal(due) {
		t.Errorf("due date mismatch: %v", item.DueDate)
	}
	if len(item.Tags) != 2 || item.Tags[0] != "earnings" {
		t.Errorf("tags mismatch: %v", item.Tags)
	}
	if item.Position != 1 {
		t.Errorf("position = %d, want 1", item.Position)
	}

	item.Status = TodoStatusInProgress
	item.Notes = "updated"
	item.Assignee = "carol"
	if err := s.UpdateTodo(ctx, *item); err != nil {
		t.Fatalf("UpdateTodo: %v", err)
	}
	item2, _ := s.GetTodo(ctx, id)
	if item2.Status != TodoStatusInProgress || item2.Assignee != "carol" {
		t.Errorf("update not applied: %+v", item2)
	}
	if item2.CompletedAt != nil {
		t.Error("CompletedAt should be nil while not done")
	}

	if err := s.CompleteTodo(ctx, id); err != nil {
		t.Fatalf("CompleteTodo: %v", err)
	}
	item3, _ := s.GetTodo(ctx, id)
	if item3.Status != TodoStatusDone || item3.CompletedAt == nil {
		t.Errorf("complete not applied: %+v", item3)
	}

	// Reopening clears CompletedAt.
	item3.Status = TodoStatusOpen
	s.UpdateTodo(ctx, *item3)
	item4, _ := s.GetTodo(ctx, id)
	if item4.Status != TodoStatusOpen || item4.CompletedAt != nil {
		t.Errorf("reopen should clear CompletedAt: %+v", item4)
	}

	if err := s.DeleteTodo(ctx, id); err != nil {
		t.Fatalf("DeleteTodo: %v", err)
	}
	if _, err := s.GetTodo(ctx, id); !errors.Is(err, ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
}

func TestQueryTodos_Filters(t *testing.T) {
	s := newTestAnalysisStore(t)
	ctx := context.Background()
	lid, _ := s.CreateTodoList(ctx, TodoList{TeamID: "t", Name: "L"})
	lid2, _ := s.CreateTodoList(ctx, TodoList{TeamID: "t", Name: "L2"})

	past := time.Now().UTC().Add(-24 * time.Hour)
	s.CreateTodo(ctx, TodoItem{ListID: lid, TeamID: "t", Title: "alpha report", Assignee: "alice", Priority: TodoPriorityUrgent, DueDate: &past})
	bID, _ := s.CreateTodo(ctx, TodoItem{ListID: lid, TeamID: "t", Title: "beta", Assignee: "bob", Tags: []string{"ml"}})
	s.CreateTodo(ctx, TodoItem{ListID: lid2, TeamID: "t", Title: "gamma", Assignee: "alice"})
	s.CreateTodo(ctx, TodoItem{ListID: lid, TeamID: "other", Title: "not-mine"})
	s.CompleteTodo(ctx, bID)

	got, err := s.QueryTodos(ctx, TodoFilter{TeamID: "t"})
	if err != nil {
		t.Fatalf("QueryTodos: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("team filter: want 3, got %d", len(got))
	}

	got, _ = s.QueryTodos(ctx, TodoFilter{TeamID: "t", ListID: lid2})
	if len(got) != 1 || got[0].Title != "gamma" {
		t.Fatalf("list filter: %+v", got)
	}

	got, _ = s.QueryTodos(ctx, TodoFilter{TeamID: "t", Assignee: "alice"})
	if len(got) != 2 {
		t.Fatalf("assignee filter: want 2, got %d", len(got))
	}

	got, _ = s.QueryTodos(ctx, TodoFilter{TeamID: "t", Status: []string{"done"}})
	if len(got) != 1 || got[0].ID != bID {
		t.Fatalf("status filter: %+v", got)
	}

	got, _ = s.QueryTodos(ctx, TodoFilter{TeamID: "t", Priority: []string{"urgent", "high"}})
	if len(got) != 1 || got[0].Title != "alpha report" {
		t.Fatalf("priority filter: %+v", got)
	}

	now := time.Now().UTC()
	got, _ = s.QueryTodos(ctx, TodoFilter{TeamID: "t", DueBefore: &now, Status: []string{"open", "in_progress", "blocked"}})
	if len(got) != 1 || got[0].Title != "alpha report" {
		t.Fatalf("overdue filter: %+v", got)
	}

	got, _ = s.QueryTodos(ctx, TodoFilter{TeamID: "t", Query: "alph"})
	if len(got) != 1 || got[0].Title != "alpha report" {
		t.Fatalf("keyword filter: %+v", got)
	}

	got, _ = s.QueryTodos(ctx, TodoFilter{TeamID: "t", Tag: "ml"})
	if len(got) != 1 || got[0].ID != bID {
		t.Fatalf("tag filter: %+v", got)
	}
}
```

Add `"time"` to the test file's imports.

- [ ] **Step 2: Run to verify failure** — `go test ./internal/storage/ -run 'TestTodoItem|TestQueryTodos' -v` → compile error (`CreateTodo` undefined).

- [ ] **Step 3: Implement** — append to `internal/storage/todos_sqlite.go`:

```go
const todoItemCols = `id, list_id, team_id, title, notes, status, priority, created_by, assignee,
	due_date, completed_at, position, tags, created_at, updated_at`

func scanTodoItemRow(scan func(...any) error) (*TodoItem, error) {
	var it TodoItem
	var status, priority, tagsJSON string
	var due, completed sql.NullString
	var createdAt, updatedAt string
	err := scan(&it.ID, &it.ListID, &it.TeamID, &it.Title, &it.Notes, &status, &priority,
		&it.CreatedBy, &it.Assignee, &due, &completed, &it.Position, &tagsJSON, &createdAt, &updatedAt)
	if err != nil {
		return nil, err
	}
	it.Status = TodoStatus(status)
	it.Priority = TodoPriority(priority)
	if due.Valid && due.String != "" {
		t := parseTimestamp(due.String)
		it.DueDate = &t
	}
	if completed.Valid && completed.String != "" {
		t := parseTimestamp(completed.String)
		it.CompletedAt = &t
	}
	if err := json.Unmarshal([]byte(tagsJSON), &it.Tags); err != nil || it.Tags == nil {
		it.Tags = []string{}
	}
	it.CreatedAt = parseTimestamp(createdAt)
	it.UpdatedAt = parseTimestamp(updatedAt)
	it.ExternalLinks = []ExternalLink{}
	it.KnowledgeRefs = []string{}
	return &it, nil
}

func (s *SQLiteStore) CreateTodo(ctx context.Context, item TodoItem) (string, error) {
	item.ID = uuid.NewString()
	if item.Status == "" {
		item.Status = TodoStatusOpen
	}
	if item.Priority == "" {
		item.Priority = TodoPriorityMedium
	}
	if item.Tags == nil {
		item.Tags = []string{}
	}
	tagsJSON, err := json.Marshal(item.Tags)
	if err != nil {
		return "", fmt.Errorf("marshal tags: %w", err)
	}
	_, err = s.db.ExecContext(ctx, `
		INSERT INTO todo_items (id, list_id, team_id, title, notes, status, priority,
			created_by, assignee, due_date, position, tags)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?,
			(SELECT COALESCE(MAX(position), 0) + 1 FROM todo_items WHERE list_id = ?), ?)
	`, item.ID, item.ListID, item.TeamID, item.Title, item.Notes, string(item.Status),
		string(item.Priority), item.CreatedBy, item.Assignee, fmtSQLiteTime(item.DueDate),
		item.ListID, string(tagsJSON))
	if err != nil {
		return "", fmt.Errorf("insert todo: %w", err)
	}
	return item.ID, nil
}

func (s *SQLiteStore) GetTodo(ctx context.Context, id string) (*TodoItem, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT `+todoItemCols+` FROM todo_items WHERE id = ?`, id)
	it, err := scanTodoItemRow(row.Scan)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("get todo: %w", err)
	}

	// External links
	lrows, err := s.db.QueryContext(ctx, `
		SELECT id, todo_id, provider, external_id, url, external_status, synced_at, created_at
		FROM todo_external_links WHERE todo_id = ? ORDER BY created_at ASC`, id)
	if err != nil {
		return nil, fmt.Errorf("get todo links: %w", err)
	}
	defer lrows.Close()
	for lrows.Next() {
		var l ExternalLink
		var synced sql.NullString
		var createdAt string
		if err := lrows.Scan(&l.ID, &l.TodoID, &l.Provider, &l.ExternalID, &l.URL,
			&l.ExternalStatus, &synced, &createdAt); err != nil {
			return nil, fmt.Errorf("scan link: %w", err)
		}
		if synced.Valid && synced.String != "" {
			t := parseTimestamp(synced.String)
			l.SyncedAt = &t
		}
		l.CreatedAt = parseTimestamp(createdAt)
		it.ExternalLinks = append(it.ExternalLinks, l)
	}
	if err := lrows.Err(); err != nil {
		return nil, err
	}

	// Knowledge refs
	krows, err := s.db.QueryContext(ctx,
		`SELECT entry_id FROM todo_knowledge_refs WHERE todo_id = ? ORDER BY entry_id`, id)
	if err != nil {
		return nil, fmt.Errorf("get todo refs: %w", err)
	}
	defer krows.Close()
	for krows.Next() {
		var eid string
		if err := krows.Scan(&eid); err != nil {
			return nil, fmt.Errorf("scan ref: %w", err)
		}
		it.KnowledgeRefs = append(it.KnowledgeRefs, eid)
	}
	return it, krows.Err()
}

func (s *SQLiteStore) QueryTodos(ctx context.Context, filter TodoFilter) ([]TodoItem, error) {
	limit := filter.Limit
	if limit == 0 {
		limit = 50
	}
	query := `SELECT ` + todoItemCols + ` FROM todo_items WHERE 1=1`
	args := []any{}
	if filter.TeamID != "" {
		query += ` AND team_id = ?`
		args = append(args, filter.TeamID)
	}
	if filter.ListID != "" {
		query += ` AND list_id = ?`
		args = append(args, filter.ListID)
	}
	if len(filter.Status) > 0 {
		query += ` AND status IN (?` + strings.Repeat(",?", len(filter.Status)-1) + `)`
		for _, st := range filter.Status {
			args = append(args, st)
		}
	}
	if filter.Assignee != "" {
		query += ` AND assignee = ?`
		args = append(args, filter.Assignee)
	}
	if len(filter.Priority) > 0 {
		query += ` AND priority IN (?` + strings.Repeat(",?", len(filter.Priority)-1) + `)`
		for _, p := range filter.Priority {
			args = append(args, p)
		}
	}
	if filter.Tag != "" {
		// tags stored as JSON array text: match the quoted element.
		query += ` AND tags LIKE ?`
		args = append(args, `%"`+filter.Tag+`"%`)
	}
	if filter.DueBefore != nil {
		query += ` AND due_date IS NOT NULL AND due_date < ?`
		args = append(args, fmtSQLiteTime(filter.DueBefore))
	}
	if filter.Query != "" {
		query += ` AND (title LIKE ? OR notes LIKE ?)`
		q := "%" + filter.Query + "%"
		args = append(args, q, q)
	}
	query += ` ORDER BY position ASC, created_at ASC`
	if limit > 0 {
		query += ` LIMIT ?`
		args = append(args, limit)
	}

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query todos: %w", err)
	}
	defer rows.Close()
	items := []TodoItem{}
	for rows.Next() {
		it, err := scanTodoItemRow(rows.Scan)
		if err != nil {
			return nil, fmt.Errorf("scan todo: %w", err)
		}
		items = append(items, *it)
	}
	return items, rows.Err()
}

func (s *SQLiteStore) UpdateTodo(ctx context.Context, item TodoItem) error {
	if item.Tags == nil {
		item.Tags = []string{}
	}
	tagsJSON, err := json.Marshal(item.Tags)
	if err != nil {
		return fmt.Errorf("marshal tags: %w", err)
	}
	res, err := s.db.ExecContext(ctx, `
		UPDATE todo_items
		SET list_id = ?, title = ?, notes = ?, status = ?, priority = ?, assignee = ?,
		    due_date = ?, position = ?, tags = ?,
		    completed_at = CASE
		        WHEN ? = 'done' THEN COALESCE(completed_at, CURRENT_TIMESTAMP)
		        ELSE NULL END,
		    updated_at = CURRENT_TIMESTAMP
		WHERE id = ?
	`, item.ListID, item.Title, item.Notes, string(item.Status), string(item.Priority),
		item.Assignee, fmtSQLiteTime(item.DueDate), item.Position, string(tagsJSON),
		string(item.Status), item.ID)
	if err != nil {
		return fmt.Errorf("update todo: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("rows affected: %w", err)
	}
	if n == 0 {
		return fmt.Errorf("todo %q: %w", item.ID, ErrNotFound)
	}
	return nil
}

func (s *SQLiteStore) CompleteTodo(ctx context.Context, id string) error {
	res, err := s.db.ExecContext(ctx, `
		UPDATE todo_items
		SET status = 'done', completed_at = CURRENT_TIMESTAMP, updated_at = CURRENT_TIMESTAMP
		WHERE id = ?
	`, id)
	if err != nil {
		return fmt.Errorf("complete todo: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("rows affected: %w", err)
	}
	if n == 0 {
		return fmt.Errorf("todo %q: %w", id, ErrNotFound)
	}
	return nil
}

func (s *SQLiteStore) DeleteTodo(ctx context.Context, id string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM todo_items WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete todo: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("rows affected: %w", err)
	}
	if n == 0 {
		return fmt.Errorf("todo %q: %w", id, ErrNotFound)
	}
	return nil
}
```

- [ ] **Step 4: Run tests** — `go test ./internal/storage/ -run 'TestTodo|TestListTodoLists|TestQueryTodos' -v` → all PASS.
- [ ] **Step 5: Full verify** — `go build ./... && go test ./internal/storage/`.

---

### Task 3: SQLite external links + knowledge refs

**Files:**
- Modify: `internal/storage/todos_sqlite.go` (append link methods + interface assertion)
- Modify: `internal/storage/todos_test.go` (append tests)

**Interfaces:**
- Consumes: `ExternalLink`, tables from Task 1, `GetTodo` from Task 2.
- Produces: SQLite `AddExternalLink(ctx, ExternalLink) (string, error)`, `RemoveExternalLink(ctx, linkID) error`, `SetKnowledgeRefs(ctx, todoID, entryIDs []string) error`, `ListTodosForEntry(ctx, entryID) ([]TodoItem, error)`; `var _ TodoStore = (*SQLiteStore)(nil)`.

- [ ] **Step 1: Write failing tests** — append to `internal/storage/todos_test.go`:

```go
func TestTodoExternalLinks(t *testing.T) {
	s := newTestAnalysisStore(t)
	ctx := context.Background()
	lid, _ := s.CreateTodoList(ctx, TodoList{TeamID: "t", Name: "L"})
	tid, _ := s.CreateTodo(ctx, TodoItem{ListID: lid, TeamID: "t", Title: "x"})

	linkID, err := s.AddExternalLink(ctx, ExternalLink{
		TodoID: tid, Provider: "jira", ExternalID: "PROJ-123",
		URL: "https://jira.example.com/browse/PROJ-123", ExternalStatus: "In Review",
	})
	if err != nil {
		t.Fatalf("AddExternalLink: %v", err)
	}
	s.AddExternalLink(ctx, ExternalLink{TodoID: tid, Provider: "github", ExternalID: "#456", URL: "https://github.com/o/r/issues/456"})

	item, _ := s.GetTodo(ctx, tid)
	if len(item.ExternalLinks) != 2 {
		t.Fatalf("want 2 links, got %d", len(item.ExternalLinks))
	}
	if item.ExternalLinks[0].Provider != "jira" || item.ExternalLinks[0].ExternalID != "PROJ-123" {
		t.Errorf("link mismatch: %+v", item.ExternalLinks[0])
	}

	if err := s.RemoveExternalLink(ctx, linkID); err != nil {
		t.Fatalf("RemoveExternalLink: %v", err)
	}
	item, _ = s.GetTodo(ctx, tid)
	if len(item.ExternalLinks) != 1 || item.ExternalLinks[0].Provider != "github" {
		t.Fatalf("after remove: %+v", item.ExternalLinks)
	}
}

func TestTodoKnowledgeRefs(t *testing.T) {
	s := newTestAnalysisStore(t)
	ctx := context.Background()
	lid, _ := s.CreateTodoList(ctx, TodoList{TeamID: "t", Name: "L"})
	tid, _ := s.CreateTodo(ctx, TodoItem{ListID: lid, TeamID: "t", Title: "apply technique"})
	tid2, _ := s.CreateTodo(ctx, TodoItem{ListID: lid, TeamID: "t", Title: "other"})

	if err := s.SetKnowledgeRefs(ctx, tid, []string{"entry-1", "entry-2"}); err != nil {
		t.Fatalf("SetKnowledgeRefs: %v", err)
	}
	s.SetKnowledgeRefs(ctx, tid2, []string{"entry-2"})

	item, _ := s.GetTodo(ctx, tid)
	if len(item.KnowledgeRefs) != 2 {
		t.Fatalf("want 2 refs, got %v", item.KnowledgeRefs)
	}

	// Replace semantics
	s.SetKnowledgeRefs(ctx, tid, []string{"entry-3"})
	item, _ = s.GetTodo(ctx, tid)
	if len(item.KnowledgeRefs) != 1 || item.KnowledgeRefs[0] != "entry-3" {
		t.Fatalf("replace failed: %v", item.KnowledgeRefs)
	}

	// Reverse lookup
	todos, err := s.ListTodosForEntry(ctx, "entry-2")
	if err != nil {
		t.Fatalf("ListTodosForEntry: %v", err)
	}
	if len(todos) != 1 || todos[0].ID != tid2 {
		t.Fatalf("reverse lookup: %+v", todos)
	}

	// Cascade: deleting the todo removes its refs
	s.DeleteTodo(ctx, tid2)
	todos, _ = s.ListTodosForEntry(ctx, "entry-2")
	if len(todos) != 0 {
		t.Fatalf("cascade failed: %+v", todos)
	}
}
```

- [ ] **Step 2: Run to verify failure** — `go test ./internal/storage/ -run 'TestTodoExternal|TestTodoKnowledge' -v` → compile error.

- [ ] **Step 3: Implement** — append to `internal/storage/todos_sqlite.go`:

```go
func (s *SQLiteStore) AddExternalLink(ctx context.Context, link ExternalLink) (string, error) {
	link.ID = uuid.NewString()
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO todo_external_links (id, todo_id, provider, external_id, url, external_status, synced_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)
	`, link.ID, link.TodoID, link.Provider, link.ExternalID, link.URL,
		link.ExternalStatus, fmtSQLiteTime(link.SyncedAt))
	if err != nil {
		return "", fmt.Errorf("insert external link: %w", err)
	}
	return link.ID, nil
}

func (s *SQLiteStore) RemoveExternalLink(ctx context.Context, linkID string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM todo_external_links WHERE id = ?`, linkID)
	if err != nil {
		return fmt.Errorf("remove external link: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("rows affected: %w", err)
	}
	if n == 0 {
		return fmt.Errorf("external link %q: %w", linkID, ErrNotFound)
	}
	return nil
}

func (s *SQLiteStore) SetKnowledgeRefs(ctx context.Context, todoID string, entryIDs []string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `DELETE FROM todo_knowledge_refs WHERE todo_id = ?`, todoID); err != nil {
		return fmt.Errorf("clear refs: %w", err)
	}
	for _, eid := range entryIDs {
		if _, err := tx.ExecContext(ctx,
			`INSERT OR IGNORE INTO todo_knowledge_refs (todo_id, entry_id) VALUES (?, ?)`, todoID, eid); err != nil {
			return fmt.Errorf("insert ref: %w", err)
		}
	}
	return tx.Commit()
}

func (s *SQLiteStore) ListTodosForEntry(ctx context.Context, entryID string) ([]TodoItem, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT `+todoItemCols+` FROM todo_items
		WHERE id IN (SELECT todo_id FROM todo_knowledge_refs WHERE entry_id = ?)
		ORDER BY created_at ASC`, entryID)
	if err != nil {
		return nil, fmt.Errorf("list todos for entry: %w", err)
	}
	defer rows.Close()
	items := []TodoItem{}
	for rows.Next() {
		it, err := scanTodoItemRow(rows.Scan)
		if err != nil {
			return nil, fmt.Errorf("scan todo: %w", err)
		}
		items = append(items, *it)
	}
	return items, rows.Err()
}

var _ TodoStore = (*SQLiteStore)(nil)
```

- [ ] **Step 4: Run tests** — `go test ./internal/storage/ -run TestTodo -v` → all PASS.
- [ ] **Step 5: Full verify** — `go build ./... && go test ./internal/storage/`.

---

### Task 4: Postgres `TodoStore` implementation

**Files:**
- Create: `internal/storage/postgres_todos.go`
- Modify: `internal/storage/postgres.go` (call `migrateTodos` in the migrate chain — find where `migrateRules` is called, around the `return fmt.Errorf("migrate rules: %w", err)` at ~line 150, and add the same call/check for todos immediately after)

**Interfaces:**
- Consumes: `*PostgresStore` (`postgres.go`), all Task-1 types. Port each SQLite method from `todos_sqlite.go` (it exists on disk — read it first).
- Produces: Postgres impls of every `TodoStore` method; `var _ TodoStore = (*PostgresStore)(nil)`.

**Porting rules (exact, apply to every method):**
1. Placeholders: `?` → `$1, $2, ...`. For dynamic queries (`QueryTodos`) use the `nextArg` closure pattern from `postgres_rules.go` `ListRules`.
2. Booleans: Postgres `BOOLEAN` — bind `bool` directly (no 0/1 int conversion), scan into `*bool`.
3. Timestamps: `TIMESTAMPTZ` — bind `time.Time`/`*time.Time` directly (no `fmtSQLiteTime`); scan into `time.Time` / `sql.NullTime` directly (no `parseTimestamp`).
4. `CURRENT_TIMESTAMP` → `NOW()`.
5. `INSERT OR IGNORE` → `INSERT ... ON CONFLICT (todo_id, entry_id) DO NOTHING`.
6. `strings.Repeat(",?", ...)` IN-lists → build with `nextArg` per element.
7. Everything else (method shape, error wrapping, `ErrNotFound` on zero rows affected, empty-slice returns) is IDENTICAL to the SQLite version.

- [ ] **Step 1: Write `migrateTodos`** at the top of `internal/storage/postgres_todos.go`:

```go
package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// migrateTodos creates the todo tables.
func (s *PostgresStore) migrateTodos(ctx context.Context) error {
	for _, stmt := range []string{
		`CREATE TABLE IF NOT EXISTS todo_lists (
			id          TEXT PRIMARY KEY,
			team_id     TEXT NOT NULL DEFAULT '',
			name        TEXT NOT NULL,
			description TEXT NOT NULL DEFAULT '',
			domain      TEXT NOT NULL DEFAULT '',
			color       TEXT NOT NULL DEFAULT '',
			archived    BOOLEAN NOT NULL DEFAULT FALSE,
			created_by  TEXT NOT NULL DEFAULT '',
			created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)`,
		`CREATE TABLE IF NOT EXISTS todo_items (
			id           TEXT PRIMARY KEY,
			list_id      TEXT NOT NULL REFERENCES todo_lists(id) ON DELETE CASCADE,
			team_id      TEXT NOT NULL DEFAULT '',
			title        TEXT NOT NULL,
			notes        TEXT NOT NULL DEFAULT '',
			status       TEXT NOT NULL DEFAULT 'open',
			priority     TEXT NOT NULL DEFAULT 'medium',
			created_by   TEXT NOT NULL DEFAULT '',
			assignee     TEXT NOT NULL DEFAULT '',
			due_date     TIMESTAMPTZ,
			completed_at TIMESTAMPTZ,
			position     INT NOT NULL DEFAULT 0,
			tags         TEXT NOT NULL DEFAULT '[]',
			created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			updated_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)`,
		`CREATE INDEX IF NOT EXISTS idx_todo_items_list ON todo_items(list_id)`,
		`CREATE INDEX IF NOT EXISTS idx_todo_items_team_status ON todo_items(team_id, status)`,
		`CREATE TABLE IF NOT EXISTS todo_external_links (
			id              TEXT PRIMARY KEY,
			todo_id         TEXT NOT NULL REFERENCES todo_items(id) ON DELETE CASCADE,
			provider        TEXT NOT NULL,
			external_id     TEXT NOT NULL DEFAULT '',
			url             TEXT NOT NULL DEFAULT '',
			external_status TEXT NOT NULL DEFAULT '',
			synced_at       TIMESTAMPTZ,
			created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)`,
		`CREATE INDEX IF NOT EXISTS idx_todo_links_todo ON todo_external_links(todo_id)`,
		`CREATE TABLE IF NOT EXISTS todo_knowledge_refs (
			todo_id  TEXT NOT NULL REFERENCES todo_items(id) ON DELETE CASCADE,
			entry_id TEXT NOT NULL,
			PRIMARY KEY (todo_id, entry_id)
		)`,
	} {
		if _, err := s.db.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("create todo tables: %w", err)
		}
	}
	return nil
}
```

Wire it into the Postgres migrate chain next to `migrateRules`:

```go
	if err := s.migrateTodos(ctx); err != nil {
		return fmt.Errorf("migrate todos: %w", err)
	}
```

- [ ] **Step 2: Port every method** from `todos_sqlite.go` using the porting rules above (same method set: `CreateTodoList`, `GetTodoList`, `ListTodoLists`, `UpdateTodoList`, `DeleteTodoList`, `CreateTodo`, `GetTodo`, `QueryTodos`, `UpdateTodo`, `CompleteTodo`, `DeleteTodo`, `AddExternalLink`, `RemoveExternalLink`, `SetKnowledgeRefs`, `ListTodosForEntry`). Representative example — `CreateTodo` in Postgres form (apply the same transformation everywhere):

```go
func (s *PostgresStore) CreateTodo(ctx context.Context, item TodoItem) (string, error) {
	item.ID = uuid.NewString()
	if item.Status == "" {
		item.Status = TodoStatusOpen
	}
	if item.Priority == "" {
		item.Priority = TodoPriorityMedium
	}
	if item.Tags == nil {
		item.Tags = []string{}
	}
	tagsJSON, err := json.Marshal(item.Tags)
	if err != nil {
		return "", fmt.Errorf("marshal tags: %w", err)
	}
	_, err = s.db.ExecContext(ctx, `
		INSERT INTO todo_items (id, list_id, team_id, title, notes, status, priority,
			created_by, assignee, due_date, position, tags)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10,
			(SELECT COALESCE(MAX(position), 0) + 1 FROM todo_items WHERE list_id = $2), $11)
	`, item.ID, item.ListID, item.TeamID, item.Title, item.Notes, string(item.Status),
		string(item.Priority), item.CreatedBy, item.Assignee, item.DueDate, string(tagsJSON))
	if err != nil {
		return "", fmt.Errorf("insert todo: %w", err)
	}
	return item.ID, nil
}
```

Postgres scan helper (nullable times use `sql.NullTime`, timestamps scan natively):

```go
func scanTodoItemRowPG(scan func(...any) error) (*TodoItem, error) {
	var it TodoItem
	var status, priority, tagsJSON string
	var due, completed sql.NullTime
	err := scan(&it.ID, &it.ListID, &it.TeamID, &it.Title, &it.Notes, &status, &priority,
		&it.CreatedBy, &it.Assignee, &due, &completed, &it.Position, &tagsJSON,
		&it.CreatedAt, &it.UpdatedAt)
	if err != nil {
		return nil, err
	}
	it.Status = TodoStatus(status)
	it.Priority = TodoPriority(priority)
	if due.Valid {
		t := due.Time
		it.DueDate = &t
	}
	if completed.Valid {
		t := completed.Time
		it.CompletedAt = &t
	}
	if err := json.Unmarshal([]byte(tagsJSON), &it.Tags); err != nil || it.Tags == nil {
		it.Tags = []string{}
	}
	it.ExternalLinks = []ExternalLink{}
	it.KnowledgeRefs = []string{}
	return &it, nil
}
```

`UpdateTodo` CASE expression in Postgres form: `completed_at = CASE WHEN $4 = 'done' THEN COALESCE(completed_at, NOW()) ELSE NULL END` (reuse the status placeholder number you bound for `status =`; count placeholders carefully). End the file with:

```go
var _ TodoStore = (*PostgresStore)(nil)
```

- [ ] **Step 3: Verify** — `go build ./... && go test ./internal/storage/` → compiles (the `var _` assertions prove interface completeness on both backends); all existing tests still pass. (Postgres has no unit-test harness in this repo — existing `postgres_rules.go` etc. have none; compile-check is the accepted bar.)

---

### Task 5: MCP tools — lists + items (`todo_lists`, `todo_list_create/update/delete`, `todo_add`, `todo_get`, `todo_query`, `todo_update`, `todo_complete`, `todo_delete`)

**Files:**
- Create: `internal/mcp/todo_tools.go`
- Create: `internal/mcp/todo_tools_test.go`

**Interfaces:**
- Consumes: `storage.TodoStore` (Tasks 1–3), `auth.GetTeamContext(ctx)` → `TeamContext{TeamID}` + `.EffectiveActorID()` (`internal/auth/middleware.go`), `logTool(name, handler)` (`internal/mcp/logging.go`), `mcplib.NewTool/NewToolResultText/NewToolResultError`, `req.GetString/GetInt` (see `rule_tools.go` for the exact idiom).
- Produces: `RegisterTodoTools(s *server.MCPServer, store storage.TodoStore)` and handler funcs `HandleTodoLists`, `HandleTodoListCreate`, `HandleTodoListUpdate`, `HandleTodoListDelete`, `HandleTodoAdd`, `HandleTodoGet`, `HandleTodoQuery`, `HandleTodoUpdate`, `HandleTodoComplete`, `HandleTodoDelete` — each `func(storage.TodoStore) func(context.Context, mcplib.CallToolRequest) (*mcplib.CallToolResult, error)`. Task 6 appends to this file and this Register func.

**Tool-description philosophy (the "telegraphing"):** every description teaches the LLM the workflow — when to use the tool, what to call first, what values mean. Copy the descriptions below verbatim; they are part of the design.

- [ ] **Step 1: Write failing tests** in `internal/mcp/todo_tools_test.go`. Follow the existing harness in `rule_tools_test.go` — read it first and mirror how it builds a store and a `mcplib.CallToolRequest` (there is an existing helper pattern for constructing requests with arguments; reuse it exactly). Test cases (package `mcp`):

```go
package mcp

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/dsandor/memory/internal/storage"
)

// newTodoTestStore returns a real SQLite store (in-temp-dir) — mirrors how
// rule_tools_test.go builds its store; adjust to that file's actual helper.
func newTodoTestStore(t *testing.T) *storage.SQLiteStore {
	t.Helper()
	s, err := storage.NewSQLiteStore(t.TempDir()+"/todo_test.db", 4)
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestHandleTodoAdd_CreatesListByName(t *testing.T) {
	s := newTodoTestStore(t)
	h := HandleTodoAdd(s)
	req := newToolRequest(map[string]any{ // use the request-builder idiom from rule_tools_test.go
		"title":     "Pull AAPL 10-Q",
		"list_name": "Q3 Review",
		"priority":  "high",
	})
	res, err := h(context.Background(), req)
	if err != nil {
		t.Fatalf("handler err: %v", err)
	}
	if res.IsError {
		t.Fatalf("tool error: %+v", res)
	}
	lists, _ := s.ListTodoLists(context.Background(), "", false)
	if len(lists) != 1 || lists[0].Name != "Q3 Review" {
		t.Fatalf("list not auto-created: %+v", lists)
	}
	items, _ := s.QueryTodos(context.Background(), storage.TodoFilter{ListID: lists[0].ID})
	if len(items) != 1 || items[0].Title != "Pull AAPL 10-Q" || items[0].Priority != storage.TodoPriorityHigh {
		t.Fatalf("item wrong: %+v", items)
	}
}

func TestHandleTodoAdd_RequiresTitle(t *testing.T) {
	s := newTodoTestStore(t)
	res, _ := HandleTodoAdd(s)(context.Background(), newToolRequest(map[string]any{"list_name": "X"}))
	if !res.IsError {
		t.Fatal("want error for missing title")
	}
}

func TestHandleTodoComplete_And_Query(t *testing.T) {
	s := newTodoTestStore(t)
	ctx := context.Background()
	lid, _ := s.CreateTodoList(ctx, storage.TodoList{Name: "L"})
	tid, _ := s.CreateTodo(ctx, storage.TodoItem{ListID: lid, Title: "finish"})

	res, _ := HandleTodoComplete(s)(ctx, newToolRequest(map[string]any{"id": tid}))
	if res.IsError {
		t.Fatalf("complete errored: %+v", res)
	}

	qres, _ := HandleTodoQuery(s)(ctx, newToolRequest(map[string]any{"status": "done"}))
	var items []storage.TodoItem
	text := toolResultText(t, qres) // helper: extract text content; mirror rule_tools_test.go
	if err := json.Unmarshal([]byte(text), &items); err != nil {
		t.Fatalf("unmarshal: %v (%s)", err, text)
	}
	if len(items) != 1 || items[0].ID != tid {
		t.Fatalf("query: %+v", items)
	}
}

func TestHandleTodoUpdate_InvalidStatus(t *testing.T) {
	s := newTodoTestStore(t)
	ctx := context.Background()
	lid, _ := s.CreateTodoList(ctx, storage.TodoList{Name: "L"})
	tid, _ := s.CreateTodo(ctx, storage.TodoItem{ListID: lid, Title: "x"})
	res, _ := HandleTodoUpdate(s)(ctx, newToolRequest(map[string]any{"id": tid, "status": "bogus"}))
	if !res.IsError || !strings.Contains(toolResultText(t, res), "status") {
		t.Fatalf("want invalid-status error, got %+v", res)
	}
}
```

If `newToolRequest`/`toolResultText` don't exist in the mcp test package, define them in this file modeled on how `rule_tools_test.go` constructs `mcplib.CallToolRequest` (`mcplib.CallToolRequest{Params: ...}` with an `Arguments` map) and extracts `TextContent`.

- [ ] **Step 2: Run to verify failure** — `go test ./internal/mcp/ -run TestHandleTodo -v` → compile error.

- [ ] **Step 3: Implement handlers** in `internal/mcp/todo_tools.go`:

```go
package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/dsandor/memory/internal/auth"
	"github.com/dsandor/memory/internal/storage"
	mcplib "github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// todoActor resolves team + stable actor identity for the calling client.
func todoActor(ctx context.Context) (teamID, actorID string) {
	tc := auth.GetTeamContext(ctx)
	return tc.TeamID, tc.EffectiveActorID()
}

// HandleTodoLists lists all todo lists for the caller's team.
func HandleTodoLists(store storage.TodoStore) func(context.Context, mcplib.CallToolRequest) (*mcplib.CallToolResult, error) {
	return func(ctx context.Context, req mcplib.CallToolRequest) (*mcplib.CallToolResult, error) {
		teamID, _ := todoActor(ctx)
		includeArchived := req.GetString("include_archived", "false") == "true"
		lists, err := store.ListTodoLists(ctx, teamID, includeArchived)
		if err != nil {
			return mcplib.NewToolResultError(fmt.Sprintf("list todo lists failed: %v", err)), nil
		}
		if lists == nil {
			lists = []storage.TodoList{}
		}
		data, err := json.Marshal(lists)
		if err != nil {
			return mcplib.NewToolResultError(fmt.Sprintf("marshal: %v", err)), nil
		}
		return mcplib.NewToolResultText(string(data)), nil
	}
}

// HandleTodoListCreate creates a new todo list.
func HandleTodoListCreate(store storage.TodoStore) func(context.Context, mcplib.CallToolRequest) (*mcplib.CallToolResult, error) {
	return func(ctx context.Context, req mcplib.CallToolRequest) (*mcplib.CallToolResult, error) {
		name := req.GetString("name", "")
		if name == "" {
			return mcplib.NewToolResultError("name is required"), nil
		}
		teamID, actorID := todoActor(ctx)
		id, err := store.CreateTodoList(ctx, storage.TodoList{
			TeamID:      teamID,
			Name:        name,
			Description: req.GetString("description", ""),
			Domain:      req.GetString("domain", ""),
			Color:       req.GetString("color", ""),
			CreatedBy:   actorID,
		})
		if err != nil {
			return mcplib.NewToolResultError(fmt.Sprintf("create todo list failed: %v", err)), nil
		}
		return mcplib.NewToolResultText(fmt.Sprintf(`{"id":%q,"name":%q}`, id, name)), nil
	}
}

// HandleTodoListUpdate updates name/description/domain/color/archived on a list.
func HandleTodoListUpdate(store storage.TodoStore) func(context.Context, mcplib.CallToolRequest) (*mcplib.CallToolResult, error) {
	return func(ctx context.Context, req mcplib.CallToolRequest) (*mcplib.CallToolResult, error) {
		id := req.GetString("id", "")
		if id == "" {
			return mcplib.NewToolResultError("id is required"), nil
		}
		teamID, _ := todoActor(ctx)
		list, err := store.GetTodoList(ctx, id)
		if err != nil {
			return mcplib.NewToolResultError(fmt.Sprintf("todo list not found: %v", err)), nil
		}
		if !auth.CanAccess(auth.GetTeamContext(ctx), list.TeamID) {
			return mcplib.NewToolResultError("todo list not found"), nil
		}
		_ = teamID
		if v := req.GetString("name", ""); v != "" {
			list.Name = v
		}
		if v := req.GetString("description", ""); v != "" {
			list.Description = v
		}
		if v := req.GetString("domain", ""); v != "" {
			list.Domain = v
		}
		if v := req.GetString("color", ""); v != "" {
			list.Color = v
		}
		switch req.GetString("archived", "") {
		case "true":
			list.Archived = true
		case "false":
			list.Archived = false
		}
		if err := store.UpdateTodoList(ctx, *list); err != nil {
			return mcplib.NewToolResultError(fmt.Sprintf("update todo list failed: %v", err)), nil
		}
		return mcplib.NewToolResultText(fmt.Sprintf("updated todo list %s", id)), nil
	}
}

// HandleTodoListDelete deletes a list and all its items.
func HandleTodoListDelete(store storage.TodoStore) func(context.Context, mcplib.CallToolRequest) (*mcplib.CallToolResult, error) {
	return func(ctx context.Context, req mcplib.CallToolRequest) (*mcplib.CallToolResult, error) {
		id := req.GetString("id", "")
		if id == "" {
			return mcplib.NewToolResultError("id is required"), nil
		}
		list, err := store.GetTodoList(ctx, id)
		if err != nil {
			return mcplib.NewToolResultError(fmt.Sprintf("todo list not found: %v", err)), nil
		}
		if !auth.CanAccess(auth.GetTeamContext(ctx), list.TeamID) {
			return mcplib.NewToolResultError("todo list not found"), nil
		}
		if err := store.DeleteTodoList(ctx, id); err != nil {
			return mcplib.NewToolResultError(fmt.Sprintf("delete todo list failed: %v", err)), nil
		}
		return mcplib.NewToolResultText(fmt.Sprintf("deleted todo list %s and its items", id)), nil
	}
}

// resolveOrCreateList returns the target list ID for todo_add: explicit list_id
// wins; otherwise list_name is matched case-insensitively within the team and
// auto-created if absent.
func resolveOrCreateList(ctx context.Context, store storage.TodoStore, teamID, actorID, listID, listName string) (string, error) {
	if listID != "" {
		return listID, nil
	}
	if listName == "" {
		return "", fmt.Errorf("either list_id or list_name is required")
	}
	lists, err := store.ListTodoLists(ctx, teamID, true)
	if err != nil {
		return "", err
	}
	for _, l := range lists {
		if strings.EqualFold(l.Name, listName) {
			return l.ID, nil
		}
	}
	return store.CreateTodoList(ctx, storage.TodoList{TeamID: teamID, Name: listName, CreatedBy: actorID})
}

// HandleTodoAdd creates a todo item, auto-creating the named list if needed.
func HandleTodoAdd(store storage.TodoStore) func(context.Context, mcplib.CallToolRequest) (*mcplib.CallToolResult, error) {
	return func(ctx context.Context, req mcplib.CallToolRequest) (*mcplib.CallToolResult, error) {
		title := req.GetString("title", "")
		if title == "" {
			return mcplib.NewToolResultError("title is required"), nil
		}
		teamID, actorID := todoActor(ctx)
		lid, err := resolveOrCreateList(ctx, store, teamID, actorID,
			req.GetString("list_id", ""), req.GetString("list_name", ""))
		if err != nil {
			return mcplib.NewToolResultError(err.Error()), nil
		}
		priority := req.GetString("priority", "medium")
		if !storage.ValidTodoPriority(priority) {
			return mcplib.NewToolResultError("invalid priority: must be low, medium, high, or urgent"), nil
		}
		item := storage.TodoItem{
			ListID:    lid,
			TeamID:    teamID,
			Title:     title,
			Notes:     req.GetString("notes", ""),
			Priority:  storage.TodoPriority(priority),
			CreatedBy: actorID,
			Assignee:  req.GetString("assignee", ""),
		}
		if ds := req.GetString("due_date", ""); ds != "" {
			t, err := time.Parse(time.RFC3339, ds)
			if err != nil {
				if t, err = time.Parse("2006-01-02", ds); err != nil {
					return mcplib.NewToolResultError("invalid due_date: use YYYY-MM-DD or RFC3339"), nil
				}
			}
			item.DueDate = &t
		}
		if ts := req.GetString("tags", ""); ts != "" {
			for _, tag := range strings.Split(ts, ",") {
				if tag = strings.TrimSpace(tag); tag != "" {
					item.Tags = append(item.Tags, tag)
				}
			}
		}
		id, err := store.CreateTodo(ctx, item)
		if err != nil {
			return mcplib.NewToolResultError(fmt.Sprintf("create todo failed: %v", err)), nil
		}
		return mcplib.NewToolResultText(fmt.Sprintf(`{"id":%q,"list_id":%q,"title":%q,"status":"open"}`, id, lid, title)), nil
	}
}

// HandleTodoGet fetches a full todo item including links and knowledge refs.
func HandleTodoGet(store storage.TodoStore) func(context.Context, mcplib.CallToolRequest) (*mcplib.CallToolResult, error) {
	return func(ctx context.Context, req mcplib.CallToolRequest) (*mcplib.CallToolResult, error) {
		id := req.GetString("id", "")
		if id == "" {
			return mcplib.NewToolResultError("id is required"), nil
		}
		item, err := store.GetTodo(ctx, id)
		if err != nil {
			return mcplib.NewToolResultError(fmt.Sprintf("todo not found: %v", err)), nil
		}
		if !auth.CanAccess(auth.GetTeamContext(ctx), item.TeamID) {
			return mcplib.NewToolResultError("todo not found"), nil
		}
		data, err := json.Marshal(item)
		if err != nil {
			return mcplib.NewToolResultError(fmt.Sprintf("marshal: %v", err)), nil
		}
		return mcplib.NewToolResultText(string(data)), nil
	}
}

// HandleTodoQuery searches todos with filters.
func HandleTodoQuery(store storage.TodoStore) func(context.Context, mcplib.CallToolRequest) (*mcplib.CallToolResult, error) {
	return func(ctx context.Context, req mcplib.CallToolRequest) (*mcplib.CallToolResult, error) {
		teamID, actorID := todoActor(ctx)
		f := storage.TodoFilter{
			TeamID: teamID,
			ListID: req.GetString("list_id", ""),
			Tag:    req.GetString("tag", ""),
			Query:  req.GetString("query", ""),
			Limit:  req.GetInt("limit", 50),
		}
		if st := req.GetString("status", ""); st != "" {
			for _, v := range strings.Split(st, ",") {
				v = strings.TrimSpace(v)
				if !storage.ValidTodoStatus(v) {
					return mcplib.NewToolResultError(fmt.Sprintf("invalid status %q", v)), nil
				}
				f.Status = append(f.Status, v)
			}
		}
		if p := req.GetString("priority", ""); p != "" {
			for _, v := range strings.Split(p, ",") {
				v = strings.TrimSpace(v)
				if !storage.ValidTodoPriority(v) {
					return mcplib.NewToolResultError(fmt.Sprintf("invalid priority %q", v)), nil
				}
				f.Priority = append(f.Priority, v)
			}
		}
		if a := req.GetString("assignee", ""); a != "" {
			if a == "me" {
				a = actorID
			}
			f.Assignee = a
		}
		if req.GetString("overdue", "") == "true" {
			now := time.Now().UTC()
			f.DueBefore = &now
			if len(f.Status) == 0 {
				f.Status = []string{"open", "in_progress", "blocked"}
			}
		}
		items, err := store.QueryTodos(ctx, f)
		if err != nil {
			return mcplib.NewToolResultError(fmt.Sprintf("query todos failed: %v", err)), nil
		}
		if items == nil {
			items = []storage.TodoItem{}
		}
		data, err := json.Marshal(items)
		if err != nil {
			return mcplib.NewToolResultError(fmt.Sprintf("marshal: %v", err)), nil
		}
		return mcplib.NewToolResultText(string(data)), nil
	}
}

// HandleTodoUpdate patches fields on a todo item.
func HandleTodoUpdate(store storage.TodoStore) func(context.Context, mcplib.CallToolRequest) (*mcplib.CallToolResult, error) {
	return func(ctx context.Context, req mcplib.CallToolRequest) (*mcplib.CallToolResult, error) {
		id := req.GetString("id", "")
		if id == "" {
			return mcplib.NewToolResultError("id is required"), nil
		}
		item, err := store.GetTodo(ctx, id)
		if err != nil {
			return mcplib.NewToolResultError(fmt.Sprintf("todo not found: %v", err)), nil
		}
		if !auth.CanAccess(auth.GetTeamContext(ctx), item.TeamID) {
			return mcplib.NewToolResultError("todo not found"), nil
		}
		if v := req.GetString("title", ""); v != "" {
			item.Title = v
		}
		if v := req.GetString("notes", ""); v != "" {
			item.Notes = v
		}
		if v := req.GetString("status", ""); v != "" {
			if !storage.ValidTodoStatus(v) {
				return mcplib.NewToolResultError("invalid status: must be open, in_progress, blocked, done, or cancelled"), nil
			}
			item.Status = storage.TodoStatus(v)
		}
		if v := req.GetString("priority", ""); v != "" {
			if !storage.ValidTodoPriority(v) {
				return mcplib.NewToolResultError("invalid priority: must be low, medium, high, or urgent"), nil
			}
			item.Priority = storage.TodoPriority(v)
		}
		if v := req.GetString("assignee", ""); v != "" {
			if v == "none" {
				item.Assignee = ""
			} else if v == "me" {
				_, actorID := todoActor(ctx)
				item.Assignee = actorID
			} else {
				item.Assignee = v
			}
		}
		if v := req.GetString("list_id", ""); v != "" {
			item.ListID = v
		}
		if ds := req.GetString("due_date", ""); ds != "" {
			if ds == "none" {
				item.DueDate = nil
			} else {
				t, err := time.Parse(time.RFC3339, ds)
				if err != nil {
					if t, err = time.Parse("2006-01-02", ds); err != nil {
						return mcplib.NewToolResultError("invalid due_date: use YYYY-MM-DD, RFC3339, or 'none'"), nil
					}
				}
				item.DueDate = &t
			}
		}
		if ts := req.GetString("tags", ""); ts != "" {
			item.Tags = nil
			for _, tag := range strings.Split(ts, ",") {
				if tag = strings.TrimSpace(tag); tag != "" {
					item.Tags = append(item.Tags, tag)
				}
			}
		}
		if err := store.UpdateTodo(ctx, *item); err != nil {
			return mcplib.NewToolResultError(fmt.Sprintf("update todo failed: %v", err)), nil
		}
		data, _ := json.Marshal(item)
		return mcplib.NewToolResultText(string(data)), nil
	}
}

// HandleTodoComplete marks a todo done.
func HandleTodoComplete(store storage.TodoStore) func(context.Context, mcplib.CallToolRequest) (*mcplib.CallToolResult, error) {
	return func(ctx context.Context, req mcplib.CallToolRequest) (*mcplib.CallToolResult, error) {
		id := req.GetString("id", "")
		if id == "" {
			return mcplib.NewToolResultError("id is required"), nil
		}
		item, err := store.GetTodo(ctx, id)
		if err != nil {
			return mcplib.NewToolResultError(fmt.Sprintf("todo not found: %v", err)), nil
		}
		if !auth.CanAccess(auth.GetTeamContext(ctx), item.TeamID) {
			return mcplib.NewToolResultError("todo not found"), nil
		}
		if err := store.CompleteTodo(ctx, id); err != nil {
			return mcplib.NewToolResultError(fmt.Sprintf("complete todo failed: %v", err)), nil
		}
		return mcplib.NewToolResultText(fmt.Sprintf("completed todo %s (%s)", id, item.Title)), nil
	}
}

// HandleTodoDelete removes a todo item.
func HandleTodoDelete(store storage.TodoStore) func(context.Context, mcplib.CallToolRequest) (*mcplib.CallToolResult, error) {
	return func(ctx context.Context, req mcplib.CallToolRequest) (*mcplib.CallToolResult, error) {
		id := req.GetString("id", "")
		if id == "" {
			return mcplib.NewToolResultError("id is required"), nil
		}
		item, err := store.GetTodo(ctx, id)
		if err != nil {
			return mcplib.NewToolResultError(fmt.Sprintf("todo not found: %v", err)), nil
		}
		if !auth.CanAccess(auth.GetTeamContext(ctx), item.TeamID) {
			return mcplib.NewToolResultError("todo not found"), nil
		}
		if err := store.DeleteTodo(ctx, id); err != nil {
			return mcplib.NewToolResultError(fmt.Sprintf("delete todo failed: %v", err)), nil
		}
		return mcplib.NewToolResultText(fmt.Sprintf("deleted todo %s", id)), nil
	}
}
```

- [ ] **Step 4: Write `RegisterTodoTools`** (same file; Task 6 appends the link tools to this func):

```go
// RegisterTodoTools adds the todo CRUD tools to an existing MCP server.
// Tool descriptions deliberately teach the calling LLM the intended workflow.
func RegisterTodoTools(s *server.MCPServer, store storage.TodoStore) {
	s.AddTool(
		mcplib.NewTool("todo_lists",
			mcplib.WithDescription("List the team's todo lists with open/total item counts. Call this FIRST when the user mentions todos, tasks, or action items — it shows what lists exist so you can add to the right one instead of creating duplicates."),
			mcplib.WithString("include_archived", mcplib.Description("'true' to include archived lists (default 'false')")),
		),
		logTool("todo_lists", HandleTodoLists(store)),
	)
	s.AddTool(
		mcplib.NewTool("todo_list_create",
			mcplib.WithDescription("Create a new todo list — a named container for related todo items (e.g. 'Q3 Earnings Review'). Check todo_lists first; prefer adding items to an existing list over creating near-duplicate lists."),
			mcplib.WithString("name", mcplib.Required(), mcplib.Description("List name — short and specific")),
			mcplib.WithString("description", mcplib.Description("What this list is for (markdown)")),
			mcplib.WithString("domain", mcplib.Description("Optional knowledge domain this list relates to")),
			mcplib.WithString("color", mcplib.Description("Optional UI accent color, hex like #38bdf8")),
		),
		logTool("todo_list_create", HandleTodoListCreate(store)),
	)
	s.AddTool(
		mcplib.NewTool("todo_list_update",
			mcplib.WithDescription("Rename, re-describe, or archive a todo list. Archiving hides a finished list without deleting its history — prefer archiving over deleting."),
			mcplib.WithString("id", mcplib.Required(), mcplib.Description("List UUID")),
			mcplib.WithString("name", mcplib.Description("New name")),
			mcplib.WithString("description", mcplib.Description("New description")),
			mcplib.WithString("domain", mcplib.Description("New domain association")),
			mcplib.WithString("color", mcplib.Description("New accent color")),
			mcplib.WithString("archived", mcplib.Description("'true' to archive, 'false' to unarchive")),
		),
		logTool("todo_list_update", HandleTodoListUpdate(store)),
	)
	s.AddTool(
		mcplib.NewTool("todo_list_delete",
			mcplib.WithDescription("Permanently delete a todo list AND all items in it. Destructive — confirm with the user first; consider todo_list_update with archived='true' instead."),
			mcplib.WithString("id", mcplib.Required(), mcplib.Description("List UUID")),
		),
		logTool("todo_list_delete", HandleTodoListDelete(store)),
	)
	s.AddTool(
		mcplib.NewTool("todo_add",
			mcplib.WithDescription("Create a todo item. Use when the user asks to track work, or when you complete an analysis that surfaces follow-up actions — capture them as todos so they aren't lost. Provide list_id (from todo_lists) or list_name (auto-creates the list if it doesn't exist). New items start with status 'open'."),
			mcplib.WithString("title", mcplib.Required(), mcplib.Description("Short actionable title, imperative voice (e.g. 'Pull AAPL 10-Q filing')")),
			mcplib.WithString("list_id", mcplib.Description("Target list UUID (preferred when known)")),
			mcplib.WithString("list_name", mcplib.Description("Target list by name; auto-created if absent")),
			mcplib.WithString("notes", mcplib.Description("Details, context, acceptance criteria (markdown)")),
			mcplib.WithString("priority", mcplib.Description("low | medium | high | urgent (default medium)")),
			mcplib.WithString("assignee", mcplib.Description("Actor ID of the person responsible; omit to leave unassigned")),
			mcplib.WithString("due_date", mcplib.Description("Due date: YYYY-MM-DD or RFC3339")),
			mcplib.WithString("tags", mcplib.Description("Comma-separated tags, e.g. 'earnings,aapl'")),
		),
		logTool("todo_add", HandleTodoAdd(store)),
	)
	s.AddTool(
		mcplib.NewTool("todo_get",
			mcplib.WithDescription("Fetch one todo item in full: notes, status, external tracker links (Jira/ServiceNow/GitHub/GitLab), and linked knowledge entries. Call before updating so you patch from current values."),
			mcplib.WithString("id", mcplib.Required(), mcplib.Description("Todo UUID")),
		),
		logTool("todo_get", HandleTodoGet(store)),
	)
	s.AddTool(
		mcplib.NewTool("todo_query",
			mcplib.WithDescription("Search todos with filters. Common patterns: assignee='me' + status='open,in_progress' for 'what am I working on'; overdue='true' for slipped work; query='...' for keyword search. Returns a JSON array."),
			mcplib.WithString("status", mcplib.Description("Comma-separated: open, in_progress, blocked, done, cancelled")),
			mcplib.WithString("assignee", mcplib.Description("Actor ID, or 'me' for the calling user's todos")),
			mcplib.WithString("priority", mcplib.Description("Comma-separated: low, medium, high, urgent")),
			mcplib.WithString("list_id", mcplib.Description("Restrict to one list")),
			mcplib.WithString("tag", mcplib.Description("Restrict to one tag")),
			mcplib.WithString("overdue", mcplib.Description("'true' = due date in the past and not done/cancelled")),
			mcplib.WithString("query", mcplib.Description("Keyword match on title and notes")),
			mcplib.WithNumber("limit", mcplib.Description("Max results (default 50)")),
		),
		logTool("todo_query", HandleTodoQuery(store)),
	)
	s.AddTool(
		mcplib.NewTool("todo_update",
			mcplib.WithDescription("Update fields on a todo. Status semantics: open = not started; in_progress = actively worked; blocked = waiting on something (say what in notes); done = finished (sets completed_at); cancelled = won't do. Moving status back from done clears completed_at. Only supplied fields change. Special values: assignee 'me' (caller) or 'none' (unassign); due_date 'none' clears it."),
			mcplib.WithString("id", mcplib.Required(), mcplib.Description("Todo UUID")),
			mcplib.WithString("title", mcplib.Description("New title")),
			mcplib.WithString("notes", mcplib.Description("New notes (replaces existing)")),
			mcplib.WithString("status", mcplib.Description("open | in_progress | blocked | done | cancelled")),
			mcplib.WithString("priority", mcplib.Description("low | medium | high | urgent")),
			mcplib.WithString("assignee", mcplib.Description("Actor ID, 'me', or 'none' to unassign")),
			mcplib.WithString("list_id", mcplib.Description("Move to a different list")),
			mcplib.WithString("due_date", mcplib.Description("YYYY-MM-DD, RFC3339, or 'none' to clear")),
			mcplib.WithString("tags", mcplib.Description("Comma-separated tags (replaces existing)")),
		),
		logTool("todo_update", HandleTodoUpdate(store)),
	)
	s.AddTool(
		mcplib.NewTool("todo_complete",
			mcplib.WithDescription("Mark a todo done (shortcut for todo_update status='done'). Call when the user says a task is finished, or when you complete work a todo tracks."),
			mcplib.WithString("id", mcplib.Required(), mcplib.Description("Todo UUID")),
		),
		logTool("todo_complete", HandleTodoComplete(store)),
	)
	s.AddTool(
		mcplib.NewTool("todo_delete",
			mcplib.WithDescription("Permanently delete a todo item. For finished work prefer todo_complete — deletion is for mistakes and duplicates, and it removes history."),
			mcplib.WithString("id", mcplib.Required(), mcplib.Description("Todo UUID")),
		),
		logTool("todo_delete", HandleTodoDelete(store)),
	)
}
```

- [ ] **Step 5: Run tests** — `go test ./internal/mcp/ -run TestHandleTodo -v` → PASS.
- [ ] **Step 6: Full verify** — `go build ./... && go test ./internal/mcp/`.

---

### Task 6: MCP link tools + resources + `manage_todos` prompt + server wiring

**Files:**
- Modify: `internal/mcp/todo_tools.go` (append handlers; extend `RegisterTodoTools`)
- Modify: `internal/mcp/todo_tools_test.go` (append tests)
- Modify: `cmd/server/main.go` (composite store interface at ~line 31 + `RegisterTodoTools` call next to `RegisterRuleTools` at ~line 254)

**Interfaces:**
- Consumes: Task 5's file/Register func; `resourceHandler` helper + `s.AddResource` pattern (`internal/mcp/resources.go`); `s.AddPrompt` pattern (`use_agent` in `internal/mcp/server.go` ~line 190).
- Produces: `HandleTodoLinkIssue`, `HandleTodoLinkKnowledge` handlers; resources `todos://mine`, `todos://overdue`; prompt `manage_todos`. Fully wired binary.

- [ ] **Step 1: Write failing tests** — append to `internal/mcp/todo_tools_test.go`:

```go
func TestHandleTodoLinkIssue(t *testing.T) {
	s := newTodoTestStore(t)
	ctx := context.Background()
	lid, _ := s.CreateTodoList(ctx, storage.TodoList{Name: "L"})
	tid, _ := s.CreateTodo(ctx, storage.TodoItem{ListID: lid, Title: "x"})

	res, _ := HandleTodoLinkIssue(s)(ctx, newToolRequest(map[string]any{
		"todo_id": tid, "provider": "jira", "external_id": "PROJ-9",
		"url": "https://jira.example.com/browse/PROJ-9", "external_status": "To Do",
	}))
	if res.IsError {
		t.Fatalf("link errored: %+v", res)
	}
	item, _ := s.GetTodo(ctx, tid)
	if len(item.ExternalLinks) != 1 || item.ExternalLinks[0].Provider != "jira" {
		t.Fatalf("link missing: %+v", item.ExternalLinks)
	}

	bad, _ := HandleTodoLinkIssue(s)(ctx, newToolRequest(map[string]any{
		"todo_id": tid, "provider": "asana", "external_id": "1",
	}))
	if !bad.IsError {
		t.Fatal("want error for unsupported provider")
	}
}

func TestHandleTodoLinkKnowledge(t *testing.T) {
	s := newTodoTestStore(t)
	ctx := context.Background()
	lid, _ := s.CreateTodoList(ctx, storage.TodoList{Name: "L"})
	tid, _ := s.CreateTodo(ctx, storage.TodoItem{ListID: lid, Title: "x"})

	res, _ := HandleTodoLinkKnowledge(s)(ctx, newToolRequest(map[string]any{
		"todo_id": tid, "entry_ids": "e1, e2",
	}))
	if res.IsError {
		t.Fatalf("link errored: %+v", res)
	}
	item, _ := s.GetTodo(ctx, tid)
	if len(item.KnowledgeRefs) != 2 {
		t.Fatalf("refs: %v", item.KnowledgeRefs)
	}

	// Empty entry_ids clears refs.
	HandleTodoLinkKnowledge(s)(ctx, newToolRequest(map[string]any{"todo_id": tid, "entry_ids": ""}))
	item, _ = s.GetTodo(ctx, tid)
	if len(item.KnowledgeRefs) != 0 {
		t.Fatalf("clear failed: %v", item.KnowledgeRefs)
	}
}
```

- [ ] **Step 2: Run to verify failure** — `go test ./internal/mcp/ -run TestHandleTodoLink -v` → compile error.

- [ ] **Step 3: Implement handlers** — append to `internal/mcp/todo_tools.go`:

```go
// HandleTodoLinkIssue attaches an external issue-tracker reference to a todo.
func HandleTodoLinkIssue(store storage.TodoStore) func(context.Context, mcplib.CallToolRequest) (*mcplib.CallToolResult, error) {
	return func(ctx context.Context, req mcplib.CallToolRequest) (*mcplib.CallToolResult, error) {
		todoID := req.GetString("todo_id", "")
		provider := req.GetString("provider", "")
		if todoID == "" || provider == "" {
			return mcplib.NewToolResultError("todo_id and provider are required"), nil
		}
		if !storage.ValidLinkProvider(provider) {
			return mcplib.NewToolResultError("invalid provider: must be jira, servicenow, github, gitlab, or other"), nil
		}
		item, err := store.GetTodo(ctx, todoID)
		if err != nil {
			return mcplib.NewToolResultError(fmt.Sprintf("todo not found: %v", err)), nil
		}
		if !auth.CanAccess(auth.GetTeamContext(ctx), item.TeamID) {
			return mcplib.NewToolResultError("todo not found"), nil
		}
		id, err := store.AddExternalLink(ctx, storage.ExternalLink{
			TodoID:         todoID,
			Provider:       provider,
			ExternalID:     req.GetString("external_id", ""),
			URL:            req.GetString("url", ""),
			ExternalStatus: req.GetString("external_status", ""),
		})
		if err != nil {
			return mcplib.NewToolResultError(fmt.Sprintf("link issue failed: %v", err)), nil
		}
		return mcplib.NewToolResultText(fmt.Sprintf(`{"link_id":%q,"todo_id":%q,"provider":%q}`, id, todoID, provider)), nil
	}
}

// HandleTodoLinkKnowledge replaces the set of knowledge entries a todo references.
func HandleTodoLinkKnowledge(store storage.TodoStore) func(context.Context, mcplib.CallToolRequest) (*mcplib.CallToolResult, error) {
	return func(ctx context.Context, req mcplib.CallToolRequest) (*mcplib.CallToolResult, error) {
		todoID := req.GetString("todo_id", "")
		if todoID == "" {
			return mcplib.NewToolResultError("todo_id is required"), nil
		}
		item, err := store.GetTodo(ctx, todoID)
		if err != nil {
			return mcplib.NewToolResultError(fmt.Sprintf("todo not found: %v", err)), nil
		}
		if !auth.CanAccess(auth.GetTeamContext(ctx), item.TeamID) {
			return mcplib.NewToolResultError("todo not found"), nil
		}
		var ids []string
		for _, e := range strings.Split(req.GetString("entry_ids", ""), ",") {
			if e = strings.TrimSpace(e); e != "" {
				ids = append(ids, e)
			}
		}
		if err := store.SetKnowledgeRefs(ctx, todoID, ids); err != nil {
			return mcplib.NewToolResultError(fmt.Sprintf("link knowledge failed: %v", err)), nil
		}
		return mcplib.NewToolResultText(fmt.Sprintf("todo %s now references %d knowledge entries", todoID, len(ids))), nil
	}
}
```

- [ ] **Step 4: Extend `RegisterTodoTools`** — append inside the func body:

```go
	s.AddTool(
		mcplib.NewTool("todo_link_issue",
			mcplib.WithDescription("Attach an external issue-tracker reference (Jira, ServiceNow, GitHub Issues, GitLab Issues) to a todo. Use when the user mentions a ticket number or pastes an issue URL alongside a task. This records the association only — it does not sync with the tracker."),
			mcplib.WithString("todo_id", mcplib.Required(), mcplib.Description("Todo UUID")),
			mcplib.WithString("provider", mcplib.Required(), mcplib.Description("jira | servicenow | github | gitlab | other")),
			mcplib.WithString("external_id", mcplib.Description("Issue key/number, e.g. 'PROJ-123' or '#456'")),
			mcplib.WithString("url", mcplib.Description("Deep link to the issue")),
			mcplib.WithString("external_status", mcplib.Description("Current status in the external tracker, e.g. 'In Review'")),
		),
		logTool("todo_link_issue", HandleTodoLinkIssue(store)),
	)
	s.AddTool(
		mcplib.NewTool("todo_link_knowledge",
			mcplib.WithDescription("Set which knowledge entries a todo references (REPLACES the current set). Use when a todo applies or produces tribal knowledge — e.g. 'apply the DCF prompt technique' should reference that technique's entry. Pass entry_ids='' to clear. Get entry IDs from knowledge_search or enrich_context results."),
			mcplib.WithString("todo_id", mcplib.Required(), mcplib.Description("Todo UUID")),
			mcplib.WithString("entry_ids", mcplib.Required(), mcplib.Description("Comma-separated knowledge entry UUIDs; empty string clears all refs")),
		),
		logTool("todo_link_knowledge", HandleTodoLinkKnowledge(store)),
	)

	// Resources — read-only views for quick context injection.
	s.AddResource(
		mcplib.NewResource("todos://mine", "My Open Todos",
			mcplib.WithResourceDescription("Open, in-progress, and blocked todos assigned to the calling user"),
			mcplib.WithMIMEType("application/json"),
		),
		resourceHandler(func(ctx context.Context, _ mcplib.ReadResourceRequest) (string, error) {
			teamID, actorID := todoActor(ctx)
			items, err := store.QueryTodos(ctx, storage.TodoFilter{
				TeamID: teamID, Assignee: actorID,
				Status: []string{"open", "in_progress", "blocked"},
			})
			if err != nil {
				return "", fmt.Errorf("query todos: %w", err)
			}
			data, err := json.Marshal(items)
			return string(data), err
		}),
	)
	s.AddResource(
		mcplib.NewResource("todos://overdue", "Overdue Todos",
			mcplib.WithResourceDescription("Team todos past their due date and not yet done or cancelled"),
			mcplib.WithMIMEType("application/json"),
		),
		resourceHandler(func(ctx context.Context, _ mcplib.ReadResourceRequest) (string, error) {
			teamID, _ := todoActor(ctx)
			now := time.Now().UTC()
			items, err := store.QueryTodos(ctx, storage.TodoFilter{
				TeamID: teamID, DueBefore: &now,
				Status: []string{"open", "in_progress", "blocked"},
			})
			if err != nil {
				return "", fmt.Errorf("query todos: %w", err)
			}
			data, err := json.Marshal(items)
			return string(data), err
		}),
	)

	// Templated resource: items of one list. Mirrors the agents://domain/{name}
	// registration in resources.go — use the same AddResourceTemplate API found there.
	s.AddResourceTemplate(
		mcplib.NewResourceTemplate("todos://list/{id}", "Todo List Items",
			mcplib.WithTemplateDescription("All items in one todo list, by list UUID"),
			mcplib.WithTemplateMIMEType("application/json"),
		),
		func(ctx context.Context, req mcplib.ReadResourceRequest) ([]mcplib.ResourceContents, error) {
			// Extract {id} the same way the agents://domain/{name} handler does in resources.go.
			id := strings.TrimPrefix(req.Params.URI, "todos://list/")
			list, err := store.GetTodoList(ctx, id)
			if err != nil {
				return nil, fmt.Errorf("todo list not found: %w", err)
			}
			if !auth.CanAccess(auth.GetTeamContext(ctx), list.TeamID) {
				return nil, fmt.Errorf("todo list not found")
			}
			items, err := store.QueryTodos(ctx, storage.TodoFilter{TeamID: list.TeamID, ListID: list.ID, Limit: -1})
			if err != nil {
				return nil, fmt.Errorf("query todos: %w", err)
			}
			data, err := json.Marshal(items)
			if err != nil {
				return nil, err
			}
			return []mcplib.ResourceContents{
				mcplib.TextResourceContents{URI: req.Params.URI, MIMEType: "application/json", Text: string(data)},
			}, nil
		},
	)

	// Prompt — teaches clients the intended workflow end to end.
	s.AddPrompt(
		mcplib.NewPrompt("manage_todos",
			mcplib.WithPromptDescription("Conventions for using the todo system effectively"),
		),
		func(ctx context.Context, req mcplib.GetPromptRequest) (*mcplib.GetPromptResult, error) {
			text := `You have access to a team todo system. Conventions:

1. DISCOVER FIRST: call todo_lists before creating anything — add items to existing lists; only create a list for a genuinely new stream of work.
2. CAPTURE FOLLOW-UPS: when analysis or discussion surfaces future actions, record them with todo_add (imperative titles, context in notes, realistic priority). Don't let action items vanish into chat history.
3. STATUS DISCIPLINE: open = not started; in_progress = actively worked; blocked = waiting (say on what in notes); done via todo_complete; cancelled = won't do. Keep status current as work progresses.
4. LINK CONTEXT: reference the knowledge entries a todo applies with todo_link_knowledge; attach tracker tickets (Jira/ServiceNow/GitHub/GitLab) with todo_link_issue when the user mentions them.
5. DAILY VIEW: todo_query with assignee='me' and status='open,in_progress' answers "what am I working on"; overdue='true' finds slipped work worth flagging.`
			return &mcplib.GetPromptResult{
				Description: "Todo system usage conventions",
				Messages: []mcplib.PromptMessage{
					{Role: mcplib.RoleUser, Content: mcplib.TextContent{Type: "text", Text: text}},
				},
			}, nil
		},
	)
```

- [ ] **Step 5: Activity feed from MCP** — the spec requires todo create/complete to appear in the activity feed regardless of entry point. Add to `internal/mcp/todo_tools.go`:

```go
// recordTodoEvent best-effort records an activity-feed event when the backing
// store also implements RecordActivity (the production composite store does;
// bare TodoStore test doubles may not — then this is a no-op).
func recordTodoEvent(ctx context.Context, store storage.TodoStore, eventType, todoID, title string) {
	rec, ok := store.(interface {
		RecordActivity(context.Context, storage.ActivityEvent) error
	})
	if !ok {
		return
	}
	_, actorID := todoActor(ctx)
	_ = rec.RecordActivity(ctx, storage.ActivityEvent{
		EventType: eventType,
		ActorID:   actorID,
		Metadata:  map[string]string{"todo_id": todoID, "title": title},
	})
}
```

Call it in `HandleTodoAdd` right after the successful `store.CreateTodo` (`recordTodoEvent(ctx, store, "todo_created", id, title)`) and in `HandleTodoComplete` after the successful `store.CompleteTodo` (`recordTodoEvent(ctx, store, "todo_completed", id, item.Title)`).

- [ ] **Step 6: Wire into `cmd/server/main.go`** — add `storage.TodoStore` to the composite store interface (~line 31, alongside `storage.RuleStore`), and add next to the other Register calls (~line 254):

```go
	internalmcp.RegisterTodoTools(mcpServer, store)
```

- [ ] **Step 7: Run tests** — `go test ./internal/mcp/ -run TestHandleTodo -v` → PASS.
- [ ] **Step 8: Full verify** — `go build ./... && go test ./...` → clean.

---

### Task 7: REST API — lists + items

**Files:**
- Create: `internal/web/todo_handlers.go`
- Create: `internal/web/todo_handlers_test.go`
- Modify: `internal/web/server.go` (`AllStore` interface ~line 55; routes in `routes()` inside the first authenticated `r.Group` alongside `/api/knowledge`)
- Modify: `internal/web/server_test.go` (mockStore must still satisfy AllStore — one line)

**Interfaces:**
- Consumes: `storage.TodoStore`; `auth.GetTeamContext(r.Context())` (`TeamID`, `.EffectiveActorID()`); `writeJSON(w, v)` + `writeError(w, status, code, msg)` (`internal/web/handlers.go` / `errors.go`); `chi.URLParam(r, "id")`.
- Produces: handlers `handleTodoListList/Create/Get/Update/Delete`, `handleTodoListItems`, `handleTodoQuery`, `handleTodoCreate/Get/Update/Delete/Complete`; routes below. JSON field names are the Go struct names (default marshaling — same as knowledge endpoints).

- [ ] **Step 1: Extend `AllStore`** in `internal/web/server.go`:

```go
type AllStore interface {
	storage.AgentStore
	storage.TeamStore
	storage.TodoStore
}
```

Then in `internal/web/server_test.go`, keep `mockStore` compiling by embedding the interface (line right inside `type mockStore struct {`):

```go
	storage.TodoStore // embedded nil interface — todo methods panic if called; todo tests use a real store
```

- [ ] **Step 2: Write failing tests** in `internal/web/todo_handlers_test.go` (package `web_test`). Model the harness on `visibility_handlers_test.go`: a struct embedding `mockStore` that delegates todo methods to a real `*storage.SQLiteStore`, `web.NewServer(fstest.MapFS{...}, store)`, and the same auth/request setup that file uses (read it and copy its request construction exactly — including any dev-bypass or Authorization header it sets):

```go
package web_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"testing/fstest"
	"time"

	"github.com/dsandor/memory/internal/storage"
	"github.com/dsandor/memory/internal/web"
)

// todoStore embeds mockStore for the AllStore surface but delegates todo
// methods to a real SQLite store so behavior is exercised end to end.
type todoStore struct {
	mockStore
	real *storage.SQLiteStore
}

func (t *todoStore) CreateTodoList(ctx context.Context, l storage.TodoList) (string, error) {
	return t.real.CreateTodoList(ctx, l)
}
func (t *todoStore) GetTodoList(ctx context.Context, id string) (*storage.TodoList, error) {
	return t.real.GetTodoList(ctx, id)
}
func (t *todoStore) ListTodoLists(ctx context.Context, teamID string, a bool) ([]storage.TodoList, error) {
	return t.real.ListTodoLists(ctx, teamID, a)
}
func (t *todoStore) UpdateTodoList(ctx context.Context, l storage.TodoList) error {
	return t.real.UpdateTodoList(ctx, l)
}
func (t *todoStore) DeleteTodoList(ctx context.Context, id string) error {
	return t.real.DeleteTodoList(ctx, id)
}
func (t *todoStore) CreateTodo(ctx context.Context, i storage.TodoItem) (string, error) {
	return t.real.CreateTodo(ctx, i)
}
func (t *todoStore) GetTodo(ctx context.Context, id string) (*storage.TodoItem, error) {
	return t.real.GetTodo(ctx, id)
}
func (t *todoStore) QueryTodos(ctx context.Context, f storage.TodoFilter) ([]storage.TodoItem, error) {
	return t.real.QueryTodos(ctx, f)
}
func (t *todoStore) UpdateTodo(ctx context.Context, i storage.TodoItem) error {
	return t.real.UpdateTodo(ctx, i)
}
func (t *todoStore) CompleteTodo(ctx context.Context, id string) error {
	return t.real.CompleteTodo(ctx, id)
}
func (t *todoStore) DeleteTodo(ctx context.Context, id string) error { return t.real.DeleteTodo(ctx, id) }
func (t *todoStore) AddExternalLink(ctx context.Context, l storage.ExternalLink) (string, error) {
	return t.real.AddExternalLink(ctx, l)
}
func (t *todoStore) RemoveExternalLink(ctx context.Context, id string) error {
	return t.real.RemoveExternalLink(ctx, id)
}
func (t *todoStore) SetKnowledgeRefs(ctx context.Context, id string, e []string) error {
	return t.real.SetKnowledgeRefs(ctx, id, e)
}
func (t *todoStore) ListTodosForEntry(ctx context.Context, e string) ([]storage.TodoItem, error) {
	return t.real.ListTodosForEntry(ctx, e)
}

func newTodoWebHarness(t *testing.T) (*web.Server, *todoStore) {
	t.Helper()
	real, err := storage.NewSQLiteStore(t.TempDir()+"/todo_web.db", 4)
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	t.Cleanup(func() { real.Close() })
	ts := &todoStore{real: real}
	staticFS := fstest.MapFS{
		"index.html": {Data: []byte("<html><body>app</body></html>"), Mode: 0444, ModTime: time.Now()},
	}
	return web.NewServer(staticFS, ts), ts
}

// doJSON performs an authenticated JSON request — copy auth details
// (headers / dev bypass) from visibility_handlers_test.go's requests.
func doJSON(t *testing.T, srv *web.Server, method, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		json.NewEncoder(&buf).Encode(body)
	}
	req := httptest.NewRequest(method, path, &buf)
	req.Header.Set("Content-Type", "application/json")
	// + the same auth setup visibility_handlers_test.go uses
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	return w
}

func TestTodoListEndpoints_CRUD(t *testing.T) {
	srv, _ := newTodoWebHarness(t)

	w := doJSON(t, srv, "POST", "/api/todo-lists", map[string]any{
		"Name": "Q3 Review", "Description": "prep", "Color": "#38bdf8",
	})
	if w.Code != http.StatusOK {
		t.Fatalf("create: %d %s", w.Code, w.Body)
	}
	var created struct{ ID string }
	json.NewDecoder(w.Body).Decode(&created)

	w = doJSON(t, srv, "GET", "/api/todo-lists", nil)
	var lists []map[string]any
	json.NewDecoder(w.Body).Decode(&lists)
	if len(lists) != 1 {
		t.Fatalf("list: %+v", lists)
	}

	w = doJSON(t, srv, "PUT", "/api/todo-lists/"+created.ID, map[string]any{"Name": "Q3 Final", "Archived": true})
	if w.Code != http.StatusOK {
		t.Fatalf("update: %d %s", w.Code, w.Body)
	}

	w = doJSON(t, srv, "DELETE", "/api/todo-lists/"+created.ID, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("delete: %d %s", w.Code, w.Body)
	}
}

func TestTodoItemEndpoints_CRUDAndComplete(t *testing.T) {
	srv, _ := newTodoWebHarness(t)
	w := doJSON(t, srv, "POST", "/api/todo-lists", map[string]any{"Name": "L"})
	var list struct{ ID string }
	json.NewDecoder(w.Body).Decode(&list)

	w = doJSON(t, srv, "POST", "/api/todos", map[string]any{
		"ListID": list.ID, "Title": "Pull 10-Q", "Priority": "high", "Assignee": "bob",
	})
	if w.Code != http.StatusOK {
		t.Fatalf("create item: %d %s", w.Code, w.Body)
	}
	var item struct{ ID string }
	json.NewDecoder(w.Body).Decode(&item)

	w = doJSON(t, srv, "GET", "/api/todos?status=open", nil)
	var items []map[string]any
	json.NewDecoder(w.Body).Decode(&items)
	if len(items) != 1 {
		t.Fatalf("query: %+v", items)
	}

	w = doJSON(t, srv, "POST", "/api/todos/"+item.ID+"/complete", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("complete: %d %s", w.Code, w.Body)
	}
	w = doJSON(t, srv, "GET", "/api/todos/"+item.ID, nil)
	var got map[string]any
	json.NewDecoder(w.Body).Decode(&got)
	if got["Status"] != "done" {
		t.Fatalf("status: %v", got["Status"])
	}

	if w = doJSON(t, srv, "DELETE", "/api/todos/"+item.ID, nil); w.Code != http.StatusOK {
		t.Fatalf("delete: %d", w.Code)
	}
}

func TestTodoCreate_Validation(t *testing.T) {
	srv, _ := newTodoWebHarness(t)
	if w := doJSON(t, srv, "POST", "/api/todos", map[string]any{"Title": ""}); w.Code != http.StatusBadRequest {
		t.Fatalf("want 400 for missing title, got %d", w.Code)
	}
	w := doJSON(t, srv, "POST", "/api/todo-lists", map[string]any{"Name": "L"})
	var list struct{ ID string }
	json.NewDecoder(w.Body).Decode(&list)
	if w := doJSON(t, srv, "POST", "/api/todos", map[string]any{
		"ListID": list.ID, "Title": "x", "Status": "bogus",
	}); w.Code != http.StatusBadRequest {
		t.Fatalf("want 400 for bad status, got %d", w.Code)
	}
}
```

- [ ] **Step 3: Run to verify failure** — `go test ./internal/web/ -run TestTodo -v` → compile error / 404s.

- [ ] **Step 4: Implement handlers** in `internal/web/todo_handlers.go`:

```go
package web

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/dsandor/memory/internal/auth"
	"github.com/dsandor/memory/internal/storage"
	"github.com/go-chi/chi/v5"
)

// todoListBody is the JSON payload for create/update of a list.
type todoListBody struct {
	Name        string
	Description string
	Domain      string
	Color       string
	Archived    bool
}

func (s *Server) handleTodoListList(w http.ResponseWriter, r *http.Request) {
	tc := auth.GetTeamContext(r.Context())
	includeArchived := r.URL.Query().Get("archived") == "true"
	lists, err := s.store.ListTodoLists(r.Context(), tc.TeamID, includeArchived)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "list todo lists failed")
		return
	}
	if lists == nil {
		lists = []storage.TodoList{}
	}
	writeJSON(w, lists)
}

func (s *Server) handleTodoListCreate(w http.ResponseWriter, r *http.Request) {
	var body todoListBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "invalid JSON body")
		return
	}
	if strings.TrimSpace(body.Name) == "" {
		writeError(w, http.StatusBadRequest, "bad_request", "Name is required")
		return
	}
	tc := auth.GetTeamContext(r.Context())
	id, err := s.store.CreateTodoList(r.Context(), storage.TodoList{
		TeamID: tc.TeamID, Name: body.Name, Description: body.Description,
		Domain: body.Domain, Color: body.Color, CreatedBy: tc.EffectiveActorID(),
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "create todo list failed")
		return
	}
	list, err := s.store.GetTodoList(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "fetch created list failed")
		return
	}
	writeJSON(w, list)
}

// fetchTodoListForTeam loads a list and 404s (without existence leak) when the
// caller's team cannot access it. Mirrors fetchEntryForTeam in handlers.go.
func (s *Server) fetchTodoListForTeam(w http.ResponseWriter, r *http.Request, id string) (*storage.TodoList, bool) {
	list, err := s.store.GetTodoList(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusNotFound, "not_found", "todo list not found")
		return nil, false
	}
	if !auth.CanAccess(auth.GetTeamContext(r.Context()), list.TeamID) {
		writeError(w, http.StatusNotFound, "not_found", "todo list not found")
		return nil, false
	}
	return list, true
}

func (s *Server) fetchTodoForTeam(w http.ResponseWriter, r *http.Request, id string) (*storage.TodoItem, bool) {
	item, err := s.store.GetTodo(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusNotFound, "not_found", "todo not found")
		return nil, false
	}
	if !auth.CanAccess(auth.GetTeamContext(r.Context()), item.TeamID) {
		writeError(w, http.StatusNotFound, "not_found", "todo not found")
		return nil, false
	}
	return item, true
}

func (s *Server) handleTodoListGet(w http.ResponseWriter, r *http.Request) {
	list, ok := s.fetchTodoListForTeam(w, r, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	writeJSON(w, list)
}

func (s *Server) handleTodoListUpdate(w http.ResponseWriter, r *http.Request) {
	list, ok := s.fetchTodoListForTeam(w, r, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	var body todoListBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "invalid JSON body")
		return
	}
	if strings.TrimSpace(body.Name) != "" {
		list.Name = body.Name
	}
	list.Description = body.Description
	list.Domain = body.Domain
	list.Color = body.Color
	list.Archived = body.Archived
	if err := s.store.UpdateTodoList(r.Context(), *list); err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "update todo list failed")
		return
	}
	updated, _ := s.store.GetTodoList(r.Context(), list.ID)
	writeJSON(w, updated)
}

func (s *Server) handleTodoListDelete(w http.ResponseWriter, r *http.Request) {
	list, ok := s.fetchTodoListForTeam(w, r, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	if err := s.store.DeleteTodoList(r.Context(), list.ID); err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "delete todo list failed")
		return
	}
	writeJSON(w, map[string]string{"deleted": list.ID})
}

func (s *Server) handleTodoListItems(w http.ResponseWriter, r *http.Request) {
	list, ok := s.fetchTodoListForTeam(w, r, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	items, err := s.store.QueryTodos(r.Context(), storage.TodoFilter{
		TeamID: list.TeamID, ListID: list.ID, Limit: -1,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "list items failed")
		return
	}
	if items == nil {
		items = []storage.TodoItem{}
	}
	writeJSON(w, items)
}

// todoItemBody is the JSON payload for create/update of an item.
// PATCH SEMANTICS on update: nil pointer = field unchanged; non-nil = set.
// DueDate is a *string: nil = unchanged, "" = clear, else RFC3339/YYYY-MM-DD.
type todoItemBody struct {
	ListID   *string
	Title    *string
	Notes    *string
	Status   *string
	Priority *string
	Assignee *string
	DueDate  *string
	Position *int
	Tags     []string
}

// parseDueDate accepts RFC3339 or YYYY-MM-DD.
func parseDueDate(s string) (*time.Time, error) {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		if t, err = time.Parse("2006-01-02", s); err != nil {
			return nil, err
		}
	}
	return &t, nil
}

func strOr(p *string, def string) string {
	if p == nil {
		return def
	}
	return *p
}

func (s *Server) handleTodoQuery(w http.ResponseWriter, r *http.Request) {
	tc := auth.GetTeamContext(r.Context())
	q := r.URL.Query()
	f := storage.TodoFilter{
		TeamID: tc.TeamID,
		ListID: q.Get("list_id"),
		Tag:    q.Get("tag"),
		Query:  q.Get("q"),
		Limit:  -1,
	}
	if v := q.Get("status"); v != "" {
		f.Status = strings.Split(v, ",")
	}
	if v := q.Get("priority"); v != "" {
		f.Priority = strings.Split(v, ",")
	}
	if v := q.Get("assignee"); v != "" {
		if v == "me" {
			v = tc.EffectiveActorID()
		}
		f.Assignee = v
	}
	if q.Get("overdue") == "true" {
		now := time.Now().UTC()
		f.DueBefore = &now
		if len(f.Status) == 0 {
			f.Status = []string{"open", "in_progress", "blocked"}
		}
	}
	if v := q.Get("entry_id"); v != "" {
		// Reverse lookup: todos referencing a knowledge entry (team-filtered below).
		items, err := s.store.ListTodosForEntry(r.Context(), v)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal", "query todos failed")
			return
		}
		out := []storage.TodoItem{}
		for _, it := range items {
			if auth.CanAccess(tc, it.TeamID) {
				out = append(out, it)
			}
		}
		writeJSON(w, out)
		return
	}
	items, err := s.store.QueryTodos(r.Context(), f)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "query todos failed")
		return
	}
	if items == nil {
		items = []storage.TodoItem{}
	}
	writeJSON(w, items)
}

func (s *Server) handleTodoCreate(w http.ResponseWriter, r *http.Request) {
	var body todoItemBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "invalid JSON body")
		return
	}
	title := strings.TrimSpace(strOr(body.Title, ""))
	if title == "" {
		writeError(w, http.StatusBadRequest, "bad_request", "Title is required")
		return
	}
	listID := strOr(body.ListID, "")
	if listID == "" {
		writeError(w, http.StatusBadRequest, "bad_request", "ListID is required")
		return
	}
	status := strOr(body.Status, "")
	if status != "" && !storage.ValidTodoStatus(status) {
		writeError(w, http.StatusBadRequest, "bad_request", "invalid Status")
		return
	}
	priority := strOr(body.Priority, "")
	if priority != "" && !storage.ValidTodoPriority(priority) {
		writeError(w, http.StatusBadRequest, "bad_request", "invalid Priority")
		return
	}
	var due *time.Time
	if ds := strOr(body.DueDate, ""); ds != "" {
		var err error
		if due, err = parseDueDate(ds); err != nil {
			writeError(w, http.StatusBadRequest, "bad_request", "invalid DueDate: use YYYY-MM-DD or RFC3339")
			return
		}
	}
	list, ok := s.fetchTodoListForTeam(w, r, listID)
	if !ok {
		return
	}
	tc := auth.GetTeamContext(r.Context())
	id, err := s.store.CreateTodo(r.Context(), storage.TodoItem{
		ListID: list.ID, TeamID: list.TeamID, Title: title, Notes: strOr(body.Notes, ""),
		Status: storage.TodoStatus(status), Priority: storage.TodoPriority(priority),
		CreatedBy: tc.EffectiveActorID(), Assignee: strOr(body.Assignee, ""),
		DueDate: due, Tags: body.Tags,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "create todo failed")
		return
	}
	item, err := s.store.GetTodo(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "fetch created todo failed")
		return
	}
	s.recordTodoActivity(r, "todo_created", item)
	writeJSON(w, item)
}

func (s *Server) handleTodoGet(w http.ResponseWriter, r *http.Request) {
	item, ok := s.fetchTodoForTeam(w, r, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	writeJSON(w, item)
}

func (s *Server) handleTodoUpdate(w http.ResponseWriter, r *http.Request) {
	item, ok := s.fetchTodoForTeam(w, r, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	var body todoItemBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "invalid JSON body")
		return
	}
	// Patch semantics: only non-nil fields change (a drag-move sends just Status).
	if body.Status != nil {
		if !storage.ValidTodoStatus(*body.Status) {
			writeError(w, http.StatusBadRequest, "bad_request", "invalid Status")
			return
		}
		item.Status = storage.TodoStatus(*body.Status)
	}
	if body.Priority != nil {
		if !storage.ValidTodoPriority(*body.Priority) {
			writeError(w, http.StatusBadRequest, "bad_request", "invalid Priority")
			return
		}
		item.Priority = storage.TodoPriority(*body.Priority)
	}
	if body.Title != nil && strings.TrimSpace(*body.Title) != "" {
		item.Title = *body.Title
	}
	if body.Notes != nil {
		item.Notes = *body.Notes
	}
	if body.Assignee != nil {
		item.Assignee = *body.Assignee
	}
	if body.DueDate != nil {
		if *body.DueDate == "" {
			item.DueDate = nil
		} else {
			due, err := parseDueDate(*body.DueDate)
			if err != nil {
				writeError(w, http.StatusBadRequest, "bad_request", "invalid DueDate: use YYYY-MM-DD, RFC3339, or empty to clear")
				return
			}
			item.DueDate = due
		}
	}
	if body.Tags != nil {
		item.Tags = body.Tags
	}
	if body.Position != nil {
		item.Position = *body.Position
	}
	if body.ListID != nil && *body.ListID != "" && *body.ListID != item.ListID {
		// Moving lists — verify the target list is in-team too.
		if _, ok := s.fetchTodoListForTeam(w, r, *body.ListID); !ok {
			return
		}
		item.ListID = *body.ListID
	}
	if err := s.store.UpdateTodo(r.Context(), *item); err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "update todo failed")
		return
	}
	updated, _ := s.store.GetTodo(r.Context(), item.ID)
	writeJSON(w, updated)
}

func (s *Server) handleTodoComplete(w http.ResponseWriter, r *http.Request) {
	item, ok := s.fetchTodoForTeam(w, r, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	if err := s.store.CompleteTodo(r.Context(), item.ID); err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "complete todo failed")
		return
	}
	updated, _ := s.store.GetTodo(r.Context(), item.ID)
	s.recordTodoActivity(r, "todo_completed", updated)
	writeJSON(w, updated)
}

func (s *Server) handleTodoDelete(w http.ResponseWriter, r *http.Request) {
	item, ok := s.fetchTodoForTeam(w, r, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	if err := s.store.DeleteTodo(r.Context(), item.ID); err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "delete todo failed")
		return
	}
	writeJSON(w, map[string]string{"deleted": item.ID})
}

// recordTodoActivity emits a feed event; failures are non-fatal (best effort).
func (s *Server) recordTodoActivity(r *http.Request, eventType string, item *storage.TodoItem) {
	if item == nil {
		return
	}
	tc := auth.GetTeamContext(r.Context())
	_ = s.store.RecordActivity(r.Context(), storage.ActivityEvent{
		EventType: eventType,
		ActorID:   tc.EffectiveActorID(),
		Metadata:  map[string]string{"todo_id": item.ID, "title": item.Title, "team_id": item.TeamID},
	})
}
```

- [ ] **Step 5: Register routes** in `internal/web/server.go` `routes()`, inside the first authenticated group (after the `/api/knowledge` block):

```go
		r.Get("/api/todo-lists", s.handleTodoListList)
		r.Post("/api/todo-lists", s.handleTodoListCreate)
		r.Get("/api/todo-lists/{id}", s.handleTodoListGet)
		r.Put("/api/todo-lists/{id}", s.handleTodoListUpdate)
		r.Delete("/api/todo-lists/{id}", s.handleTodoListDelete)
		r.Get("/api/todo-lists/{id}/items", s.handleTodoListItems)
		r.Get("/api/todos", s.handleTodoQuery)
		r.Post("/api/todos", s.handleTodoCreate)
		r.Get("/api/todos/{id}", s.handleTodoGet)
		r.Put("/api/todos/{id}", s.handleTodoUpdate)
		r.Delete("/api/todos/{id}", s.handleTodoDelete)
		r.Post("/api/todos/{id}/complete", s.handleTodoComplete)
```

- [ ] **Step 6: Run tests** — `go test ./internal/web/ -run TestTodo -v` → PASS.
- [ ] **Step 7: Full verify** — `go build ./... && go test ./internal/web/`.

---

### Task 8: REST API — external links + knowledge refs

**Files:**
- Modify: `internal/web/todo_handlers.go` (append)
- Modify: `internal/web/todo_handlers_test.go` (append)
- Modify: `internal/web/server.go` (append routes)

**Interfaces:**
- Consumes: Task 7's helpers (`fetchTodoForTeam`, `todoStore` test harness).
- Produces: `handleTodoLinkAdd`, `handleTodoLinkRemove`, `handleTodoRefsSet`. Routes: `POST /api/todos/{id}/links`, `DELETE /api/todos/{id}/links/{linkId}`, `PUT /api/todos/{id}/knowledge-refs`.

- [ ] **Step 1: Write failing tests** — append to `internal/web/todo_handlers_test.go`:

```go
func TestTodoLinkEndpoints(t *testing.T) {
	srv, _ := newTodoWebHarness(t)
	w := doJSON(t, srv, "POST", "/api/todo-lists", map[string]any{"Name": "L"})
	var list struct{ ID string }
	json.NewDecoder(w.Body).Decode(&list)
	w = doJSON(t, srv, "POST", "/api/todos", map[string]any{"ListID": list.ID, "Title": "x"})
	var item struct{ ID string }
	json.NewDecoder(w.Body).Decode(&item)

	w = doJSON(t, srv, "POST", "/api/todos/"+item.ID+"/links", map[string]any{
		"Provider": "github", "ExternalID": "#42", "URL": "https://github.com/o/r/issues/42",
	})
	if w.Code != http.StatusOK {
		t.Fatalf("add link: %d %s", w.Code, w.Body)
	}
	var link struct{ ID string }
	json.NewDecoder(w.Body).Decode(&link)

	if w = doJSON(t, srv, "POST", "/api/todos/"+item.ID+"/links", map[string]any{
		"Provider": "trello",
	}); w.Code != http.StatusBadRequest {
		t.Fatalf("want 400 for bad provider, got %d", w.Code)
	}

	if w = doJSON(t, srv, "DELETE", "/api/todos/"+item.ID+"/links/"+link.ID, nil); w.Code != http.StatusOK {
		t.Fatalf("remove link: %d", w.Code)
	}
}

func TestTodoKnowledgeRefEndpoints(t *testing.T) {
	srv, ts := newTodoWebHarness(t)
	w := doJSON(t, srv, "POST", "/api/todo-lists", map[string]any{"Name": "L"})
	var list struct{ ID string }
	json.NewDecoder(w.Body).Decode(&list)
	w = doJSON(t, srv, "POST", "/api/todos", map[string]any{"ListID": list.ID, "Title": "x"})
	var item struct{ ID string }
	json.NewDecoder(w.Body).Decode(&item)

	if w = doJSON(t, srv, "PUT", "/api/todos/"+item.ID+"/knowledge-refs", map[string]any{
		"EntryIDs": []string{"e1", "e2"},
	}); w.Code != http.StatusOK {
		t.Fatalf("set refs: %d %s", w.Code, w.Body)
	}
	got, _ := ts.real.GetTodo(context.Background(), item.ID)
	if len(got.KnowledgeRefs) != 2 {
		t.Fatalf("refs: %v", got.KnowledgeRefs)
	}

	// Reverse lookup through the query endpoint.
	w = doJSON(t, srv, "GET", "/api/todos?entry_id=e1", nil)
	var items []map[string]any
	json.NewDecoder(w.Body).Decode(&items)
	if len(items) != 1 {
		t.Fatalf("entry_id lookup: %+v", items)
	}
}
```

- [ ] **Step 2: Run to verify failure** — `go test ./internal/web/ -run 'TestTodoLink|TestTodoKnowledge' -v` → 404/405 failures.

- [ ] **Step 3: Implement** — append to `internal/web/todo_handlers.go`:

```go
func (s *Server) handleTodoLinkAdd(w http.ResponseWriter, r *http.Request) {
	item, ok := s.fetchTodoForTeam(w, r, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	var body struct {
		Provider       string
		ExternalID     string
		URL            string
		ExternalStatus string
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "invalid JSON body")
		return
	}
	if !storage.ValidLinkProvider(body.Provider) {
		writeError(w, http.StatusBadRequest, "bad_request", "invalid Provider: jira, servicenow, github, gitlab, or other")
		return
	}
	id, err := s.store.AddExternalLink(r.Context(), storage.ExternalLink{
		TodoID: item.ID, Provider: body.Provider, ExternalID: body.ExternalID,
		URL: body.URL, ExternalStatus: body.ExternalStatus,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "add link failed")
		return
	}
	writeJSON(w, map[string]string{"ID": id})
}

func (s *Server) handleTodoLinkRemove(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.fetchTodoForTeam(w, r, chi.URLParam(r, "id")); !ok {
		return
	}
	if err := s.store.RemoveExternalLink(r.Context(), chi.URLParam(r, "linkId")); err != nil {
		writeError(w, http.StatusNotFound, "not_found", "link not found")
		return
	}
	writeJSON(w, map[string]string{"deleted": chi.URLParam(r, "linkId")})
}

func (s *Server) handleTodoRefsSet(w http.ResponseWriter, r *http.Request) {
	item, ok := s.fetchTodoForTeam(w, r, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	var body struct{ EntryIDs []string }
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "invalid JSON body")
		return
	}
	if err := s.store.SetKnowledgeRefs(r.Context(), item.ID, body.EntryIDs); err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "set refs failed")
		return
	}
	updated, _ := s.store.GetTodo(r.Context(), item.ID)
	writeJSON(w, updated)
}
```

Routes (same group as Task 7):

```go
		r.Post("/api/todos/{id}/links", s.handleTodoLinkAdd)
		r.Delete("/api/todos/{id}/links/{linkId}", s.handleTodoLinkRemove)
		r.Put("/api/todos/{id}/knowledge-refs", s.handleTodoRefsSet)
```

- [ ] **Step 4: Run tests** — `go test ./internal/web/ -run TestTodo -v` → all PASS.
- [ ] **Step 5: Full verify** — `go build ./... && go test ./...` → clean.

---

### Task 9: Frontend API client

**Files:**
- Modify: `web/src/lib/api.ts` (append types + functions; reuse the existing `apiFetch`/`BASE` wrapper at the top of the file)

**Interfaces:**
- Consumes: `apiFetch(url, init)` — injects Authorization + X-Team-Id and 401-redirects.
- Produces (exact exports used by Tasks 10–12): `TodoList`, `TodoItem`, `TodoExternalLink` interfaces; `listTodoLists`, `createTodoList`, `updateTodoList`, `deleteTodoList`, `queryTodos`, `createTodo`, `getTodo`, `updateTodo`, `completeTodo`, `deleteTodo`, `addTodoLink`, `removeTodoLink`, `setTodoKnowledgeRefs`, `todosForEntry`.

- [ ] **Step 1: Append to `web/src/lib/api.ts`** (field names match Go's default JSON marshaling — Go struct field names, as with `KnowledgeEntry` above):

```ts
// ---------- Todos ----------

export interface TodoExternalLink {
  ID: string
  TodoID: string
  Provider: 'jira' | 'servicenow' | 'github' | 'gitlab' | 'other'
  ExternalID: string
  URL: string
  ExternalStatus: string
  SyncedAt: string | null
  CreatedAt: string
}

export interface TodoItem {
  ID: string
  ListID: string
  TeamID: string
  Title: string
  Notes: string
  Status: 'open' | 'in_progress' | 'blocked' | 'done' | 'cancelled'
  Priority: 'low' | 'medium' | 'high' | 'urgent'
  CreatedBy: string
  Assignee: string
  DueDate: string | null
  CompletedAt: string | null
  Position: number
  Tags: string[] | null
  ExternalLinks: TodoExternalLink[] | null
  KnowledgeRefs: string[] | null
  CreatedAt: string
  UpdatedAt: string
}

export interface TodoList {
  ID: string
  TeamID: string
  Name: string
  Description: string
  Domain: string
  Color: string
  Archived: boolean
  CreatedBy: string
  CreatedAt: string
  UpdatedAt: string
  OpenCount: number
  TotalCount: number
}

export interface TodoQueryFilters {
  list_id?: string
  status?: string   // comma-separated
  assignee?: string // 'me' supported
  priority?: string // comma-separated
  tag?: string
  overdue?: boolean
  q?: string
  entry_id?: string
}

async function jsonOrThrow<T>(r: Response): Promise<T> {
  if (!r.ok) {
    const body = await r.json().catch(() => ({ error: `HTTP ${r.status}` }))
    throw new Error(body.error ?? `HTTP ${r.status}`)
  }
  return r.json()
}

export async function listTodoLists(includeArchived = false): Promise<TodoList[]> {
  return jsonOrThrow(await apiFetch(`${BASE}/todo-lists${includeArchived ? '?archived=true' : ''}`))
}

export async function createTodoList(body: Partial<Pick<TodoList, 'Name' | 'Description' | 'Domain' | 'Color'>>): Promise<TodoList> {
  return jsonOrThrow(await apiFetch(`${BASE}/todo-lists`, {
    method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(body),
  }))
}

export async function updateTodoList(id: string, body: Partial<Pick<TodoList, 'Name' | 'Description' | 'Domain' | 'Color' | 'Archived'>>): Promise<TodoList> {
  return jsonOrThrow(await apiFetch(`${BASE}/todo-lists/${id}`, {
    method: 'PUT', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(body),
  }))
}

export async function deleteTodoList(id: string): Promise<void> {
  await jsonOrThrow(await apiFetch(`${BASE}/todo-lists/${id}`, { method: 'DELETE' }))
}

export async function queryTodos(filters: TodoQueryFilters = {}): Promise<TodoItem[]> {
  const params = new URLSearchParams()
  for (const [k, v] of Object.entries(filters)) {
    if (v !== undefined && v !== '' && v !== false) params.set(k, String(v))
  }
  const qs = params.toString()
  return jsonOrThrow(await apiFetch(`${BASE}/todos${qs ? `?${qs}` : ''}`))
}

// PATCH semantics: omitted field = unchanged. DueDate: omit = unchanged,
// '' = clear, else RFC3339 or YYYY-MM-DD.
export type TodoItemPatch = Partial<Pick<TodoItem,
  'ListID' | 'Title' | 'Notes' | 'Status' | 'Priority' | 'Assignee' | 'Position' | 'Tags'>> & {
  DueDate?: string
}

export async function createTodo(body: TodoItemPatch & { ListID: string; Title: string }): Promise<TodoItem> {
  return jsonOrThrow(await apiFetch(`${BASE}/todos`, {
    method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(body),
  }))
}

export async function getTodo(id: string): Promise<TodoItem> {
  return jsonOrThrow(await apiFetch(`${BASE}/todos/${id}`))
}

export async function updateTodo(id: string, body: TodoItemPatch): Promise<TodoItem> {
  return jsonOrThrow(await apiFetch(`${BASE}/todos/${id}`, {
    method: 'PUT', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(body),
  }))
}

export async function completeTodo(id: string): Promise<TodoItem> {
  return jsonOrThrow(await apiFetch(`${BASE}/todos/${id}/complete`, { method: 'POST' }))
}

export async function deleteTodo(id: string): Promise<void> {
  await jsonOrThrow(await apiFetch(`${BASE}/todos/${id}`, { method: 'DELETE' }))
}

export async function addTodoLink(todoId: string, body: { Provider: string; ExternalID?: string; URL?: string; ExternalStatus?: string }): Promise<{ ID: string }> {
  return jsonOrThrow(await apiFetch(`${BASE}/todos/${todoId}/links`, {
    method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(body),
  }))
}

export async function removeTodoLink(todoId: string, linkId: string): Promise<void> {
  await jsonOrThrow(await apiFetch(`${BASE}/todos/${todoId}/links/${linkId}`, { method: 'DELETE' }))
}

export async function setTodoKnowledgeRefs(todoId: string, entryIds: string[]): Promise<TodoItem> {
  return jsonOrThrow(await apiFetch(`${BASE}/todos/${todoId}/knowledge-refs`, {
    method: 'PUT', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ EntryIDs: entryIds }),
  }))
}

export async function todosForEntry(entryId: string): Promise<TodoItem[]> {
  return queryTodos({ entry_id: entryId })
}
```

**NOTE (updated during Task 7 review):** the list-update endpoint uses PATCH semantics like items — omitted fields are unchanged. `updateTodoList` may safely send partial bodies. Add this comment above `updateTodoList` in api.ts:

```ts
// PATCH semantics: omitted fields are unchanged server-side.
```

- [ ] **Step 2: Verify** — `cd web && npm run build` → clean build (types compile; nothing imports the new functions yet).

---

### Task 10: Todos page — Kanban board, list view, filters, quick add

**Files:**
- Create: `web/src/pages/Todos.tsx` (page state: lists sidebar, filters, view toggle, data loading)
- Create: `web/src/components/todo/TodoBoard.tsx` (4-column Kanban, HTML5 drag-and-drop)
- Create: `web/src/components/todo/TodoCard.tsx` (card visuals)
- Create: `web/src/components/todo/TodoTable.tsx` (list view)
- Create: `web/src/components/todo/todoTheme.ts` (shared color/label maps)

**Interfaces:**
- Consumes: Task 9 api functions/types; MUI components; lucide-react icons (already a dependency).
- Produces: `<Todos />` default export (Task 12 routes it); `TodoBoard({ items, onMove, onOpen })`, `TodoCard({ item, onOpen, draggable })`, `TodoTable({ items, onOpen, onComplete })`, and `todoTheme.ts` exports `STATUS_COLUMNS`, `statusLabel`, `priorityColor`, `providerLabel`, `dueTone` (Task 11/12 import these). `onOpen(item)` opens the detail drawer (wired in Task 11 — until then Todos.tsx keeps a `selected` state and renders nothing for it).

**Design language (match the existing dark theme):** cards on `background.paper` with `border: '1px solid'`, `borderColor: 'divider'`, `borderRadius: 1`; 13px body text; muted secondary text `#94a3b8`; accent colors only for status/priority signals.

- [ ] **Step 1: Write `web/src/components/todo/todoTheme.ts`:**

```ts
import type { TodoItem } from '@/lib/api'

export const STATUS_COLUMNS = [
  { key: 'open', label: 'Open', color: '#94a3b8' },
  { key: 'in_progress', label: 'In Progress', color: '#38bdf8' },
  { key: 'blocked', label: 'Blocked', color: '#f87171' },
  { key: 'done', label: 'Done', color: '#4ade80' },
] as const

export function statusLabel(s: TodoItem['Status']): string {
  const found = STATUS_COLUMNS.find(c => c.key === s)
  return found ? found.label : s === 'cancelled' ? 'Cancelled' : s
}

export const PRIORITY_ORDER = ['urgent', 'high', 'medium', 'low'] as const

export function priorityColor(p: TodoItem['Priority']): string {
  switch (p) {
    case 'urgent': return '#ef4444'
    case 'high': return '#f97316'
    case 'medium': return '#eab308'
    default: return '#64748b'
  }
}

export function providerLabel(p: string): string {
  switch (p) {
    case 'jira': return 'Jira'
    case 'servicenow': return 'ServiceNow'
    case 'github': return 'GitHub'
    case 'gitlab': return 'GitLab'
    default: return 'Link'
  }
}

// dueTone: 'overdue' (red), 'soon' (amber, within 48h), 'normal', or null (no due date / done).
export function dueTone(item: TodoItem): 'overdue' | 'soon' | 'normal' | null {
  if (!item.DueDate || item.Status === 'done' || item.Status === 'cancelled') return null
  const due = new Date(item.DueDate).getTime()
  const now = Date.now()
  if (due < now) return 'overdue'
  if (due < now + 48 * 3600 * 1000) return 'soon'
  return 'normal'
}
```

- [ ] **Step 2: Write `web/src/components/todo/TodoCard.tsx`:**

```tsx
import Box from '@mui/material/Box'
import Chip from '@mui/material/Chip'
import Tooltip from '@mui/material/Tooltip'
import Typography from '@mui/material/Typography'
import { CalendarClock, Link2, BookOpen, User } from 'lucide-react'
import type { TodoItem } from '@/lib/api'
import { priorityColor, providerLabel, dueTone } from './todoTheme'

export default function TodoCard({ item, onOpen, draggable = true }: {
  item: TodoItem
  onOpen: (item: TodoItem) => void
  draggable?: boolean
}) {
  const tone = dueTone(item)
  const dueColor = tone === 'overdue' ? '#f87171' : tone === 'soon' ? '#fbbf24' : '#94a3b8'
  const links = item.ExternalLinks ?? []
  const refs = item.KnowledgeRefs ?? []
  return (
    <Box
      draggable={draggable}
      onDragStart={(e) => e.dataTransfer.setData('text/todo-id', item.ID)}
      onClick={() => onOpen(item)}
      sx={{
        p: 1.25, mb: 1, cursor: 'pointer', borderRadius: 1,
        bgcolor: 'background.paper', border: '1px solid', borderColor: 'divider',
        borderLeft: `3px solid ${priorityColor(item.Priority)}`,
        '&:hover': { borderColor: 'primary.main' },
        opacity: item.Status === 'done' || item.Status === 'cancelled' ? 0.6 : 1,
      }}
    >
      <Typography sx={{ fontSize: 13, fontWeight: 500, lineHeight: 1.35 }} color="text.primary">
        {item.Title}
      </Typography>
      <Box sx={{ display: 'flex', alignItems: 'center', gap: 1, mt: 0.75, flexWrap: 'wrap' }}>
        <Chip
          size="small"
          label={item.Priority}
          sx={{
            height: 18, fontSize: 10, textTransform: 'uppercase', fontWeight: 600,
            color: priorityColor(item.Priority), bgcolor: 'transparent',
            border: `1px solid ${priorityColor(item.Priority)}44`,
          }}
        />
        {item.DueDate && tone && (
          <Box sx={{ display: 'flex', alignItems: 'center', gap: 0.5, color: dueColor }}>
            <CalendarClock size={12} />
            <Typography sx={{ fontSize: 11 }}>{new Date(item.DueDate).toLocaleDateString()}</Typography>
          </Box>
        )}
        {item.Assignee && (
          <Tooltip title={item.Assignee}>
            <Box sx={{ display: 'flex', alignItems: 'center', gap: 0.5, color: '#94a3b8' }}>
              <User size={12} />
              <Typography sx={{ fontSize: 11, maxWidth: 90, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>
                {item.Assignee}
              </Typography>
            </Box>
          </Tooltip>
        )}
        {links.length > 0 && (
          <Tooltip title={links.map(l => `${providerLabel(l.Provider)} ${l.ExternalID}`).join(', ')}>
            <Box sx={{ display: 'flex', alignItems: 'center', gap: 0.5, color: '#94a3b8' }}>
              <Link2 size={12} />
              <Typography sx={{ fontSize: 11 }}>{links.length}</Typography>
            </Box>
          </Tooltip>
        )}
        {refs.length > 0 && (
          <Tooltip title={`${refs.length} linked knowledge entries`}>
            <Box sx={{ display: 'flex', alignItems: 'center', gap: 0.5, color: '#94a3b8' }}>
              <BookOpen size={12} />
              <Typography sx={{ fontSize: 11 }}>{refs.length}</Typography>
            </Box>
          </Tooltip>
        )}
      </Box>
    </Box>
  )
}
```

- [ ] **Step 3: Write `web/src/components/todo/TodoBoard.tsx`** (native HTML5 DnD — no new deps):

```tsx
import { useState } from 'react'
import Box from '@mui/material/Box'
import Typography from '@mui/material/Typography'
import type { TodoItem } from '@/lib/api'
import { STATUS_COLUMNS } from './todoTheme'
import TodoCard from './TodoCard'

export default function TodoBoard({ items, onMove, onOpen }: {
  items: TodoItem[]
  onMove: (id: string, status: TodoItem['Status']) => void
  onOpen: (item: TodoItem) => void
}) {
  const [dragOver, setDragOver] = useState<string | null>(null)
  return (
    <Box sx={{ display: 'flex', gap: 2, alignItems: 'flex-start', overflowX: 'auto', pb: 1 }}>
      {STATUS_COLUMNS.map(col => {
        const colItems = items.filter(i => i.Status === col.key)
        return (
          <Box
            key={col.key}
            onDragOver={(e) => { e.preventDefault(); setDragOver(col.key) }}
            onDragLeave={() => setDragOver(null)}
            onDrop={(e) => {
              e.preventDefault()
              setDragOver(null)
              const id = e.dataTransfer.getData('text/todo-id')
              if (id) onMove(id, col.key as TodoItem['Status'])
            }}
            sx={{
              flex: '1 1 0', minWidth: 240, borderRadius: 1.5, p: 1.25,
              bgcolor: dragOver === col.key ? 'rgba(255,255,255,0.06)' : 'rgba(255,255,255,0.02)',
              border: '1px solid', borderColor: dragOver === col.key ? col.color : 'divider',
              transition: 'border-color 120ms, background-color 120ms',
            }}
          >
            <Box sx={{ display: 'flex', alignItems: 'center', gap: 1, mb: 1.25, px: 0.5 }}>
              <Box sx={{ width: 8, height: 8, borderRadius: '50%', bgcolor: col.color }} />
              <Typography sx={{ fontSize: 12, fontWeight: 600, textTransform: 'uppercase', letterSpacing: 0.5 }} color="text.secondary">
                {col.label}
              </Typography>
              <Typography sx={{ fontSize: 12, ml: 'auto' }} color="text.secondary">{colItems.length}</Typography>
            </Box>
            {colItems.map(item => <TodoCard key={item.ID} item={item} onOpen={onOpen} />)}
            {colItems.length === 0 && (
              <Typography sx={{ fontSize: 12, textAlign: 'center', py: 2 }} color="text.secondary">
                Drop items here
              </Typography>
            )}
          </Box>
        )
      })}
    </Box>
  )
}
```

- [ ] **Step 4: Write `web/src/components/todo/TodoTable.tsx`:**

```tsx
import Box from '@mui/material/Box'
import Checkbox from '@mui/material/Checkbox'
import Chip from '@mui/material/Chip'
import Table from '@mui/material/Table'
import TableBody from '@mui/material/TableBody'
import TableCell from '@mui/material/TableCell'
import TableHead from '@mui/material/TableHead'
import TableRow from '@mui/material/TableRow'
import Typography from '@mui/material/Typography'
import type { TodoItem } from '@/lib/api'
import { priorityColor, statusLabel, dueTone } from './todoTheme'

export default function TodoTable({ items, onOpen, onComplete }: {
  items: TodoItem[]
  onOpen: (item: TodoItem) => void
  onComplete: (item: TodoItem) => void
}) {
  return (
    <Table size="small">
      <TableHead>
        <TableRow>
          <TableCell padding="checkbox" />
          <TableCell>Title</TableCell>
          <TableCell>Status</TableCell>
          <TableCell>Priority</TableCell>
          <TableCell>Assignee</TableCell>
          <TableCell>Due</TableCell>
        </TableRow>
      </TableHead>
      <TableBody>
        {items.map(item => {
          const tone = dueTone(item)
          return (
            <TableRow key={item.ID} hover sx={{ cursor: 'pointer' }} onClick={() => onOpen(item)}>
              <TableCell padding="checkbox" onClick={(e) => e.stopPropagation()}>
                <Checkbox
                  size="small"
                  checked={item.Status === 'done'}
                  onChange={() => onComplete(item)}
                />
              </TableCell>
              <TableCell>
                <Typography sx={{ fontSize: 13, textDecoration: item.Status === 'done' ? 'line-through' : 'none' }}>
                  {item.Title}
                </Typography>
              </TableCell>
              <TableCell><Typography sx={{ fontSize: 12 }} color="text.secondary">{statusLabel(item.Status)}</Typography></TableCell>
              <TableCell>
                <Chip size="small" label={item.Priority} sx={{
                  height: 18, fontSize: 10, textTransform: 'uppercase', fontWeight: 600,
                  color: priorityColor(item.Priority), bgcolor: 'transparent',
                  border: `1px solid ${priorityColor(item.Priority)}44`,
                }} />
              </TableCell>
              <TableCell><Typography sx={{ fontSize: 12 }} color="text.secondary">{item.Assignee || '—'}</Typography></TableCell>
              <TableCell>
                <Typography sx={{ fontSize: 12, color: tone === 'overdue' ? '#f87171' : tone === 'soon' ? '#fbbf24' : '#94a3b8' }}>
                  {item.DueDate ? new Date(item.DueDate).toLocaleDateString() : '—'}
                </Typography>
              </TableCell>
            </TableRow>
          )
        })}
        {items.length === 0 && (
          <TableRow>
            <TableCell colSpan={6}>
              <Box sx={{ py: 3, textAlign: 'center' }}>
                <Typography sx={{ fontSize: 13 }} color="text.secondary">No todos match the current filters</Typography>
              </Box>
            </TableCell>
          </TableRow>
        )}
      </TableBody>
    </Table>
  )
}
```

- [ ] **Step 5: Write `web/src/pages/Todos.tsx`** — the page: left rail of lists (+ create), filter bar (Assignee Me/All toggle, priority select, overdue toggle, search), board/list view toggle, quick-add row, cancelled section collapsed. Complete component:

```tsx
import { useCallback, useEffect, useMemo, useState } from 'react'
import Box from '@mui/material/Box'
import Button from '@mui/material/Button'
import IconButton from '@mui/material/IconButton'
import InputAdornment from '@mui/material/InputAdornment'
import MenuItem from '@mui/material/MenuItem'
import TextField from '@mui/material/TextField'
import ToggleButton from '@mui/material/ToggleButton'
import ToggleButtonGroup from '@mui/material/ToggleButtonGroup'
import Typography from '@mui/material/Typography'
import Snackbar from '@mui/material/Snackbar'
import { Plus, Search, LayoutGrid, List as ListIcon, ListTodo } from 'lucide-react'
import {
  TodoItem, TodoList, listTodoLists, createTodoList, queryTodos,
  createTodo, updateTodo, completeTodo,
} from '@/lib/api'
import TodoBoard from '@/components/todo/TodoBoard'
import TodoTable from '@/components/todo/TodoTable'
import TodoDetailDrawer from '@/components/todo/TodoDetailDrawer'

export default function Todos() {
  const [lists, setLists] = useState<TodoList[]>([])
  const [items, setItems] = useState<TodoItem[]>([])
  const [activeList, setActiveList] = useState<string>('all')
  const [view, setView] = useState<'board' | 'list'>('board')
  const [mineOnly, setMineOnly] = useState(false)
  const [priority, setPriority] = useState('')
  const [search, setSearch] = useState('')
  const [quickTitle, setQuickTitle] = useState('')
  const [selected, setSelected] = useState<TodoItem | null>(null)
  const [error, setError] = useState('')

  const load = useCallback(async () => {
    try {
      const [ls, its] = await Promise.all([
        listTodoLists(),
        queryTodos({
          list_id: activeList === 'all' ? undefined : activeList,
          assignee: mineOnly ? 'me' : undefined,
          priority: priority || undefined,
          q: search || undefined,
        }),
      ])
      setLists(ls)
      setItems(its)
    } catch (e) {
      setError(e instanceof Error ? e.message : 'load failed')
    }
  }, [activeList, mineOnly, priority, search])

  useEffect(() => { load() }, [load])

  const visible = useMemo(() => items.filter(i => i.Status !== 'cancelled'), [items])

  const handleMove = async (id: string, status: TodoItem['Status']) => {
    // Optimistic column move; reconcile with server response.
    setItems(prev => prev.map(i => (i.ID === id ? { ...i, Status: status } : i)))
    try {
      const updated = await updateTodo(id, { Status: status })
      setItems(prev => prev.map(i => (i.ID === id ? updated : i)))
    } catch (e) {
      setError(e instanceof Error ? e.message : 'update failed')
      load()
    }
  }

  const handleComplete = async (item: TodoItem) => {
    try {
      const updated = item.Status === 'done'
        ? await updateTodo(item.ID, { Status: 'open' })
        : await completeTodo(item.ID)
      setItems(prev => prev.map(i => (i.ID === item.ID ? updated : i)))
    } catch (e) {
      setError(e instanceof Error ? e.message : 'update failed')
    }
  }

  const handleQuickAdd = async () => {
    const title = quickTitle.trim()
    if (!title) return
    try {
      let listId = activeList
      if (listId === 'all') {
        listId = lists[0]?.ID ?? (await createTodoList({ Name: 'General' })).ID
      }
      const created = await createTodo({ ListID: listId, Title: title })
      setQuickTitle('')
      setItems(prev => [...prev, created])
      load()
    } catch (e) {
      setError(e instanceof Error ? e.message : 'create failed')
    }
  }

  const handleNewList = async () => {
    const name = window.prompt('New list name')
    if (!name?.trim()) return
    try {
      const created = await createTodoList({ Name: name.trim() })
      setLists(prev => [...prev, created])
      setActiveList(created.ID)
    } catch (e) {
      setError(e instanceof Error ? e.message : 'create list failed')
    }
  }

  return (
    <Box sx={{ display: 'flex', gap: 2.5, height: '100%' }}>
      {/* Lists rail */}
      <Box sx={{ width: 220, flexShrink: 0 }}>
        <Box sx={{ display: 'flex', alignItems: 'center', mb: 1.5 }}>
          <Typography variant="h6" sx={{ fontSize: 16, fontWeight: 600 }}>Todos</Typography>
          <IconButton size="small" onClick={handleNewList} sx={{ ml: 'auto' }} title="New list">
            <Plus size={16} />
          </IconButton>
        </Box>
        {[{ ID: 'all', Name: 'All lists', OpenCount: lists.reduce((n, l) => n + l.OpenCount, 0) } as Pick<TodoList, 'ID' | 'Name' | 'OpenCount'>, ...lists].map(l => (
          <Box
            key={l.ID}
            onClick={() => setActiveList(l.ID)}
            sx={{
              display: 'flex', alignItems: 'center', gap: 1, px: 1.25, py: 0.75, mb: 0.25,
              borderRadius: 1, cursor: 'pointer', fontSize: 13,
              bgcolor: activeList === l.ID ? 'rgba(255,255,255,0.08)' : 'transparent',
              '&:hover': { bgcolor: 'rgba(255,255,255,0.05)' },
            }}
          >
            <ListTodo size={14} color={(l as TodoList).Color || '#94a3b8'} />
            <Typography sx={{ fontSize: 13, flex: 1, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>
              {l.Name}
            </Typography>
            <Typography sx={{ fontSize: 11 }} color="text.secondary">{l.OpenCount}</Typography>
          </Box>
        ))}
      </Box>

      {/* Main area */}
      <Box sx={{ flex: 1, minWidth: 0, display: 'flex', flexDirection: 'column', gap: 1.5 }}>
        {/* Filter bar */}
        <Box sx={{ display: 'flex', alignItems: 'center', gap: 1.5, flexWrap: 'wrap' }}>
          <TextField
            size="small" placeholder="Search todos…" value={search}
            onChange={(e) => setSearch(e.target.value)}
            slotProps={{ input: { startAdornment: (
              <InputAdornment position="start"><Search size={14} /></InputAdornment>
            ) } }}
            sx={{ width: 220 }}
          />
          <ToggleButtonGroup size="small" exclusive value={mineOnly ? 'me' : 'all'}
            onChange={(_, v) => v && setMineOnly(v === 'me')}>
            <ToggleButton value="all" sx={{ fontSize: 12, px: 1.5 }}>All</ToggleButton>
            <ToggleButton value="me" sx={{ fontSize: 12, px: 1.5 }}>Mine</ToggleButton>
          </ToggleButtonGroup>
          <TextField select size="small" value={priority} onChange={(e) => setPriority(e.target.value)}
            sx={{ width: 140 }} label="Priority">
            <MenuItem value="">Any</MenuItem>
            <MenuItem value="urgent">Urgent</MenuItem>
            <MenuItem value="high">High</MenuItem>
            <MenuItem value="medium">Medium</MenuItem>
            <MenuItem value="low">Low</MenuItem>
          </TextField>
          <ToggleButtonGroup size="small" exclusive value={view} onChange={(_, v) => v && setView(v)} sx={{ ml: 'auto' }}>
            <ToggleButton value="board" title="Board view"><LayoutGrid size={14} /></ToggleButton>
            <ToggleButton value="list" title="List view"><ListIcon size={14} /></ToggleButton>
          </ToggleButtonGroup>
        </Box>

        {/* Quick add */}
        <Box sx={{ display: 'flex', gap: 1 }}>
          <TextField
            size="small" fullWidth placeholder="Add a todo… (Enter to save)"
            value={quickTitle}
            onChange={(e) => setQuickTitle(e.target.value)}
            onKeyDown={(e) => { if (e.key === 'Enter') handleQuickAdd() }}
          />
          <Button variant="contained" size="small" onClick={handleQuickAdd} startIcon={<Plus size={14} />}>
            Add
          </Button>
        </Box>

        {/* Content */}
        <Box sx={{ flex: 1, overflow: 'auto' }}>
          {view === 'board'
            ? <TodoBoard items={visible} onMove={handleMove} onOpen={setSelected} />
            : <TodoTable items={visible} onOpen={setSelected} onComplete={handleComplete} />}
        </Box>
      </Box>

      <TodoDetailDrawer
        item={selected}
        lists={lists}
        onClose={() => setSelected(null)}
        onChanged={(updated) => {
          if (updated) setItems(prev => prev.map(i => (i.ID === updated.ID ? updated : i)))
          else load() // deleted
        }}
      />
      <Snackbar open={!!error} autoHideDuration={4000} onClose={() => setError('')} message={error} />
    </Box>
  )
}
```

Until Task 11 exists, create a placeholder `web/src/components/todo/TodoDetailDrawer.tsx` so the build passes:

```tsx
import type { TodoItem, TodoList } from '@/lib/api'

// Placeholder — replaced by the full drawer in the next task.
export default function TodoDetailDrawer(_props: {
  item: TodoItem | null
  lists: TodoList[]
  onClose: () => void
  onChanged: (updated: TodoItem | null) => void
}) {
  return null
}
```

- [ ] **Step 6: Verify** — `cd web && npm run build` → clean. (Page not yet routed; Task 12 wires it. If the build flags the unused page import rule, that's fine — nothing imports it yet, Vite doesn't error on unimported files.)

---

### Task 11: Todo detail drawer — edit, external links, knowledge refs

**Files:**
- Modify: `web/src/components/todo/TodoDetailDrawer.tsx` (replace the Task 10 placeholder with the real component)

**Interfaces:**
- Consumes: api functions `getTodo`, `updateTodo`, `deleteTodo`, `addTodoLink`, `removeTodoLink`, `setTodoKnowledgeRefs`; `searchKnowledge`-style lookup — check `web/src/lib/api.ts` for the existing knowledge list/search function (used by KnowledgeBrowser) and reuse it for the ref picker; `todoTheme.ts` helpers.
- Produces: the drawer already wired in Task 10 (`item`, `lists`, `onClose`, `onChanged`).

- [ ] **Step 1: Replace the placeholder** with the full drawer:

```tsx
import { useEffect, useState } from 'react'
import Box from '@mui/material/Box'
import Button from '@mui/material/Button'
import Chip from '@mui/material/Chip'
import Divider from '@mui/material/Divider'
import Drawer from '@mui/material/Drawer'
import IconButton from '@mui/material/IconButton'
import MenuItem from '@mui/material/MenuItem'
import TextField from '@mui/material/TextField'
import Typography from '@mui/material/Typography'
import { ExternalLink as ExternalLinkIcon, Trash2, X, Plus, BookOpen } from 'lucide-react'
import { Link as RouterLink } from 'react-router-dom'
import {
  TodoItem, TodoList, getTodo, updateTodo, deleteTodo,
  addTodoLink, removeTodoLink, setTodoKnowledgeRefs,
} from '@/lib/api'
import { providerLabel, priorityColor } from './todoTheme'

export default function TodoDetailDrawer({ item, lists, onClose, onChanged }: {
  item: TodoItem | null
  lists: TodoList[]
  onClose: () => void
  onChanged: (updated: TodoItem | null) => void
}) {
  const [full, setFull] = useState<TodoItem | null>(null)
  const [draft, setDraft] = useState<TodoItem | null>(null)
  const [linkProvider, setLinkProvider] = useState('jira')
  const [linkExternalID, setLinkExternalID] = useState('')
  const [linkURL, setLinkURL] = useState('')
  const [refInput, setRefInput] = useState('')
  const [busy, setBusy] = useState(false)

  // Reload the full item (with links + refs) whenever the drawer opens.
  useEffect(() => {
    if (!item) { setFull(null); setDraft(null); return }
    getTodo(item.ID).then(f => { setFull(f); setDraft(f) }).catch(() => { setFull(item); setDraft(item) })
  }, [item])

  if (!item || !draft) return null

  const save = async () => {
    setBusy(true)
    try {
      const updated = await updateTodo(draft.ID, {
        ListID: draft.ListID, Title: draft.Title, Notes: draft.Notes,
        Status: draft.Status, Priority: draft.Priority, Assignee: draft.Assignee,
        DueDate: draft.DueDate ?? '', // '' clears the due date server-side
        Tags: draft.Tags ?? [],
      })
      setFull(updated); setDraft(updated); onChanged(updated)
    } finally {
      setBusy(false)
    }
  }

  const remove = async () => {
    if (!window.confirm('Delete this todo? This cannot be undone.')) return
    await deleteTodo(draft.ID)
    onChanged(null)
    onClose()
  }

  const addLink = async () => {
    if (!linkExternalID.trim() && !linkURL.trim()) return
    await addTodoLink(draft.ID, {
      Provider: linkProvider, ExternalID: linkExternalID.trim(), URL: linkURL.trim(),
    })
    const refreshed = await getTodo(draft.ID)
    setFull(refreshed); setDraft(refreshed); onChanged(refreshed)
    setLinkExternalID(''); setLinkURL('')
  }

  const dropLink = async (linkId: string) => {
    await removeTodoLink(draft.ID, linkId)
    const refreshed = await getTodo(draft.ID)
    setFull(refreshed); setDraft(refreshed); onChanged(refreshed)
  }

  const addRef = async () => {
    const id = refInput.trim()
    if (!id) return
    const next = [...(full?.KnowledgeRefs ?? []), id]
    const refreshed = await setTodoKnowledgeRefs(draft.ID, next)
    setFull(refreshed); setDraft(refreshed); onChanged(refreshed)
    setRefInput('')
  }

  const dropRef = async (id: string) => {
    const next = (full?.KnowledgeRefs ?? []).filter(r => r !== id)
    const refreshed = await setTodoKnowledgeRefs(draft.ID, next)
    setFull(refreshed); setDraft(refreshed); onChanged(refreshed)
  }

  return (
    <Drawer anchor="right" open={!!item} onClose={onClose}
      slotProps={{ paper: { sx: { width: 440, p: 2.5, bgcolor: 'background.paper' } } }}>
      <Box sx={{ display: 'flex', alignItems: 'center', mb: 2 }}>
        <Typography sx={{ fontSize: 15, fontWeight: 600, flex: 1 }}>Todo details</Typography>
        <IconButton size="small" onClick={remove} title="Delete todo"><Trash2 size={16} /></IconButton>
        <IconButton size="small" onClick={onClose}><X size={16} /></IconButton>
      </Box>

      <Box sx={{ display: 'flex', flexDirection: 'column', gap: 1.75 }}>
        <TextField label="Title" size="small" fullWidth value={draft.Title}
          onChange={(e) => setDraft({ ...draft, Title: e.target.value })} />
        <TextField label="Notes (markdown)" size="small" fullWidth multiline minRows={3} value={draft.Notes}
          onChange={(e) => setDraft({ ...draft, Notes: e.target.value })} />
        <Box sx={{ display: 'flex', gap: 1.5 }}>
          <TextField select label="Status" size="small" fullWidth value={draft.Status}
            onChange={(e) => setDraft({ ...draft, Status: e.target.value as TodoItem['Status'] })}>
            <MenuItem value="open">Open</MenuItem>
            <MenuItem value="in_progress">In Progress</MenuItem>
            <MenuItem value="blocked">Blocked</MenuItem>
            <MenuItem value="done">Done</MenuItem>
            <MenuItem value="cancelled">Cancelled</MenuItem>
          </TextField>
          <TextField select label="Priority" size="small" fullWidth value={draft.Priority}
            onChange={(e) => setDraft({ ...draft, Priority: e.target.value as TodoItem['Priority'] })}>
            {(['urgent', 'high', 'medium', 'low'] as const).map(p => (
              <MenuItem key={p} value={p}>
                <Box component="span" sx={{ color: priorityColor(p), textTransform: 'capitalize' }}>{p}</Box>
              </MenuItem>
            ))}
          </TextField>
        </Box>
        <Box sx={{ display: 'flex', gap: 1.5 }}>
          <TextField select label="List" size="small" fullWidth value={draft.ListID}
            onChange={(e) => setDraft({ ...draft, ListID: e.target.value })}>
            {lists.map(l => <MenuItem key={l.ID} value={l.ID}>{l.Name}</MenuItem>)}
          </TextField>
          <TextField label="Assignee" size="small" fullWidth value={draft.Assignee}
            onChange={(e) => setDraft({ ...draft, Assignee: e.target.value })} />
        </Box>
        <TextField label="Due date" type="date" size="small"
          slotProps={{ inputLabel: { shrink: true } }}
          value={draft.DueDate ? draft.DueDate.slice(0, 10) : ''}
          onChange={(e) => setDraft({ ...draft, DueDate: e.target.value ? new Date(e.target.value + 'T12:00:00Z').toISOString() : null })} />
        <TextField label="Tags (comma-separated)" size="small" fullWidth
          value={(draft.Tags ?? []).join(', ')}
          onChange={(e) => setDraft({ ...draft, Tags: e.target.value.split(',').map(t => t.trim()).filter(Boolean) })} />
        <Button variant="contained" size="small" onClick={save} disabled={busy}>Save changes</Button>
      </Box>

      <Divider sx={{ my: 2.5 }} />

      {/* External tracker links */}
      <Typography sx={{ fontSize: 13, fontWeight: 600, mb: 1 }}>Issue tracker links</Typography>
      {(full?.ExternalLinks ?? []).map(l => (
        <Box key={l.ID} sx={{ display: 'flex', alignItems: 'center', gap: 1, mb: 0.75, p: 1, borderRadius: 1, border: '1px solid', borderColor: 'divider' }}>
          <Chip size="small" label={providerLabel(l.Provider)} sx={{ height: 18, fontSize: 10 }} />
          <Typography sx={{ fontSize: 12, flex: 1 }}>{l.ExternalID || l.URL}</Typography>
          {l.ExternalStatus && <Typography sx={{ fontSize: 11 }} color="text.secondary">{l.ExternalStatus}</Typography>}
          {l.URL && (
            <IconButton size="small" component="a" href={l.URL} target="_blank" rel="noopener">
              <ExternalLinkIcon size={13} />
            </IconButton>
          )}
          <IconButton size="small" onClick={() => dropLink(l.ID)}><X size={13} /></IconButton>
        </Box>
      ))}
      <Box sx={{ display: 'flex', gap: 1, mt: 1 }}>
        <TextField select size="small" value={linkProvider} onChange={(e) => setLinkProvider(e.target.value)} sx={{ width: 130 }}>
          {['jira', 'servicenow', 'github', 'gitlab', 'other'].map(p => (
            <MenuItem key={p} value={p}>{providerLabel(p)}</MenuItem>
          ))}
        </TextField>
        <TextField size="small" placeholder="ID (PROJ-123)" value={linkExternalID}
          onChange={(e) => setLinkExternalID(e.target.value)} sx={{ width: 110 }} />
        <TextField size="small" placeholder="URL" value={linkURL} onChange={(e) => setLinkURL(e.target.value)} sx={{ flex: 1 }} />
        <IconButton size="small" onClick={addLink}><Plus size={15} /></IconButton>
      </Box>

      <Divider sx={{ my: 2.5 }} />

      {/* Knowledge refs */}
      <Typography sx={{ fontSize: 13, fontWeight: 600, mb: 1 }}>Linked knowledge</Typography>
      {(full?.KnowledgeRefs ?? []).map(id => (
        <Box key={id} sx={{ display: 'flex', alignItems: 'center', gap: 1, mb: 0.75, p: 1, borderRadius: 1, border: '1px solid', borderColor: 'divider' }}>
          <BookOpen size={13} color="#94a3b8" />
          <Typography component={RouterLink} to={`/knowledge/${id}`} sx={{ fontSize: 12, flex: 1, color: '#93c5fd', textDecoration: 'none', '&:hover': { textDecoration: 'underline' } }}>
            {id}
          </Typography>
          <IconButton size="small" onClick={() => dropRef(id)}><X size={13} /></IconButton>
        </Box>
      ))}
      <Box sx={{ display: 'flex', gap: 1, mt: 1 }}>
        <TextField size="small" placeholder="Knowledge entry ID" value={refInput}
          onChange={(e) => setRefInput(e.target.value)} sx={{ flex: 1 }} />
        <IconButton size="small" onClick={addRef}><Plus size={15} /></IconButton>
      </Box>
    </Drawer>
  )
}
```

**Optional polish (do it if `web/src/lib/api.ts` already exposes a knowledge list/search function used by KnowledgeBrowser):** replace the plain "Knowledge entry ID" TextField with an MUI `Autocomplete` that searches entries by title and stores the selected entry's ID. Show entry titles instead of raw IDs in the refs list by batch-fetching titles. If no such function exists, keep the ID input — do NOT build a new search endpoint for this.

- [ ] **Step 2: Verify** — `cd web && npm run build` → clean.

---

### Task 12: Wiring — route, nav, Dashboard widget, Knowledge Detail panel

**Files:**
- Modify: `web/src/App.tsx` (import + route)
- Modify: `web/src/components/Layout.tsx` (nav item)
- Modify: `web/src/pages/Dashboard.tsx` (My Todos widget)
- Modify: `web/src/pages/KnowledgeDetail.tsx` (Related Todos panel)

**Interfaces:**
- Consumes: `<Todos />` (Task 10), `queryTodos`, `completeTodo`, `todosForEntry` (Task 9), `statusLabel`/`priorityColor`/`dueTone` (`todoTheme.ts`).

- [ ] **Step 1: Route** — in `web/src/App.tsx` add `import Todos from './pages/Todos'` and, inside the `Layout` route children next to `<Route path="knowledge" ...>`:

```tsx
          <Route path="todos" element={<Todos />} />
```

- [ ] **Step 2: Nav** — in `web/src/components/Layout.tsx`, add `ListTodo` to the lucide-react import and insert into `baseNav` after the Knowledge entry:

```tsx
  { to: '/todos', label: 'Todos', Icon: ListTodo },
```

- [ ] **Step 3: Dashboard widget** — in `web/src/pages/Dashboard.tsx`, add a "My Todos" card. Read the file first and mirror the EXACT card/widget wrapper used by the existing Trending widget (same Paper/Box styling, heading typography, and grid placement). Widget content:

```tsx
// State + load (place with the other dashboard state/effects):
const [myTodos, setMyTodos] = useState<TodoItem[]>([])
useEffect(() => {
  queryTodos({ assignee: 'me', status: 'open,in_progress,blocked' })
    .then(setMyTodos)
    .catch(() => setMyTodos([]))
}, [])
const overdueCount = myTodos.filter(t => dueTone(t) === 'overdue').length

// Render (inside the widget card, after a heading like the Trending widget's):
<Box sx={{ display: 'flex', alignItems: 'baseline', gap: 1, mb: 1 }}>
  <Typography sx={{ fontSize: 24, fontWeight: 700 }}>{myTodos.length}</Typography>
  <Typography sx={{ fontSize: 12 }} color="text.secondary">open</Typography>
  {overdueCount > 0 && (
    <Typography sx={{ fontSize: 12, color: '#f87171', ml: 1 }}>{overdueCount} overdue</Typography>
  )}
</Box>
{myTodos.slice(0, 5).map(t => (
  <Box key={t.ID} component={RouterLink} to="/todos" sx={{
    display: 'flex', alignItems: 'center', gap: 1, py: 0.5,
    textDecoration: 'none', color: 'inherit', '&:hover': { opacity: 0.8 },
  }}>
    <Box sx={{ width: 6, height: 6, borderRadius: '50%', bgcolor: priorityColor(t.Priority), flexShrink: 0 }} />
    <Typography sx={{ fontSize: 12, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>
      {t.Title}
    </Typography>
  </Box>
))}
{myTodos.length === 0 && (
  <Typography sx={{ fontSize: 12 }} color="text.secondary">Nothing assigned to you 🎉</Typography>
)}
```

Imports to add: `queryTodos`, `TodoItem` from `@/lib/api`; `priorityColor`, `dueTone` from `@/components/todo/todoTheme`; `Link as RouterLink` from `react-router-dom` (if not present).

- [ ] **Step 4: Knowledge Detail panel** — in `web/src/pages/KnowledgeDetail.tsx`, next to the existing "Similar entries" panel (mirror its card styling exactly), add a "Related todos" panel:

```tsx
// State + load (entry id variable name: match the component's existing route-param variable):
const [relatedTodos, setRelatedTodos] = useState<TodoItem[]>([])
useEffect(() => {
  if (!id) return
  todosForEntry(id).then(setRelatedTodos).catch(() => setRelatedTodos([]))
}, [id])

// Render — only when non-empty:
{relatedTodos.length > 0 && (
  /* same card wrapper as Similar entries */
  <>
    <Typography sx={{ fontSize: 13, fontWeight: 600, mb: 1 }}>Related todos</Typography>
    {relatedTodos.map(t => (
      <Box key={t.ID} component={RouterLink} to="/todos" sx={{
        display: 'flex', alignItems: 'center', gap: 1, py: 0.5,
        textDecoration: 'none', color: 'inherit', '&:hover': { opacity: 0.8 },
      }}>
        <Box sx={{ width: 6, height: 6, borderRadius: '50%', bgcolor: priorityColor(t.Priority) }} />
        <Typography sx={{ fontSize: 12, flex: 1, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>
          {t.Title}
        </Typography>
        <Typography sx={{ fontSize: 11 }} color="text.secondary">{statusLabel(t.Status)}</Typography>
      </Box>
    ))}
  </>
)}
```

Imports: `todosForEntry`, `TodoItem` from `@/lib/api`; `priorityColor`, `statusLabel` from `@/components/todo/todoTheme`.

- [ ] **Step 5: Verify** — `cd web && npm run build` → clean production build.

---

### Task 13: End-to-end verification + docs

**Files:**
- Modify: `README.md` (MCP tools table + REST endpoints list — add the todo tools/endpoints wherever the existing ones are documented)
- Modify: `CHANGELOG.md` (new entry at top)
- Modify: `.planning/STATE.md` and `.planning/ROADMAP.md` (add Phase 11 — TODO Subsystem — `complete`, linking the spec + this plan)

- [ ] **Step 1: Full backend verification:**

```bash
go build ./... && go test ./...
```
Expected: all packages PASS.

- [ ] **Step 2: Full frontend verification:**

```bash
cd web && npm run build
```
Expected: clean Vite production build.

- [ ] **Step 3: Manual smoke test** — start the server (`go run ./cmd/server` with the same env as `run.sh`, or SQLite defaults), then:
  1. Open the web UI → Todos nav appears → create a list → add a todo → drag it to In Progress → open drawer → add a Jira link `PROJ-1` → save.
  2. `GET /api/todos?status=in_progress` (with auth header) returns the item with the link.
  3. Dashboard shows the My Todos widget.
  4. Via MCP (stdio or HTTP): call `todo_lists`, `todo_add`, `todo_complete` — verify results and that the web UI reflects them.

- [ ] **Step 4: Docs** — README (tools + endpoints), CHANGELOG entry ("Added: TODO subsystem — todo lists/items, 12 MCP tools, todos:// resources, manage_todos prompt, REST API, Kanban web UI, knowledge refs, external tracker link schema"), `.planning` Phase 11 row + notes.

- [ ] **Step 5: Report** — summarize test counts, build results, and any deviations from this plan. DO NOT commit.








---

## Follow-up (user-approved 2026-07-21): within-column drag-to-reorder

### Task 14: Backend reorder — storage + REST + MCP position parity

**Files:**
- Modify: `internal/storage/todos.go` (interface + sentinel error), `internal/storage/todos_sqlite.go`, `internal/storage/postgres_todos.go`, `internal/storage/todos_test.go`
- Modify: `internal/web/todo_handlers.go`, `internal/web/todo_handlers_test.go`, `internal/web/server.go` (route)
- Modify: `internal/mcp/todo_tools.go` (todo_update position param), `internal/mcp/todo_tools_test.go`

**Interfaces:**
- Produces: `ReorderTodo(ctx context.Context, todoID, afterID string) error` on TodoStore (afterID "" = move to top of its list); `var ErrDifferentList = errors.New("todos are in different lists")` in todos.go; REST `POST /api/todos/{id}/reorder` body `{"AfterID": "<id or empty>"}` returning the updated item; MCP `todo_update` gains optional numeric `position`.

**Storage semantics (both backends, transactional):**
```go
// ReorderTodo places todoID immediately after afterID within the same list,
// renumbering the whole list to dense 1..N positions. afterID == "" moves the
// todo to the top. Returns ErrNotFound if either todo is missing and
// ErrDifferentList if afterID belongs to another list.
```
Algorithm inside one transaction: load todoID's list_id (ErrNotFound if absent); if afterID != "", load its list_id (ErrNotFound / ErrDifferentList checks); `SELECT id FROM todo_items WHERE list_id = ? ORDER BY position ASC, created_at ASC`; rebuild the slice with todoID removed and re-inserted immediately after afterID (index 0 when afterID == ""); loop `UPDATE todo_items SET position = ? WHERE id = ?` for every id whose position changed; also bump `updated_at` on the moved todo only. Commit.

- [ ] Storage tests (TDD first): reorder middle→top (afterID ""), top→after-last, ErrNotFound (bogus todo, bogus afterID), ErrDifferentList, and dense-renumber assertion (positions exactly 1..N after reorder). Verify: `go test ./internal/storage/ -run TestTodoReorder -v`.
- [ ] REST handler `handleTodoReorder`: fetchTodoForTeam on {id}; decode `{AfterID string}`; if AfterID != "" fetchTodoForTeam on it too and 400 `"AfterID must be a todo in the same list"` when `ErrDifferentList` (or pre-check ListID mismatch); map ErrNotFound→404; on success return updated item via GetTodo. Route `r.Post("/api/todos/{id}/reorder", s.handleTodoReorder)` next to the other todo routes. Tests: happy path (order verified via list-items endpoint), cross-team afterID → 404, different-list afterID → 400.
- [ ] MCP: `todo_update` tool gains `mcplib.WithNumber("position", mcplib.Description("Explicit position within the list (1-based). Prefer leaving ordering to the UI; set only when the user asks."))`; handler: `if p := req.GetInt("position", 0); p > 0 { item.Position = p }`. One test case.
- [ ] Full verify: `go build ./... && go test ./...` clean; gofmt on touched files.

### Task 15: Board UI reorder + docs

**Files:**
- Modify: `web/src/lib/api.ts` (reorderTodo), `web/src/components/todo/TodoBoard.tsx`, `web/src/components/todo/TodoCard.tsx`, `web/src/pages/Todos.tsx`
- Modify: `README.md` (endpoint row), `CHANGELOG.md` (bullet), `.planning/ROADMAP.md` (Phase 11 note)

**Behavior:**
- api.ts: `reorderTodo(id: string, afterId: string): Promise<TodoItem>` → POST `{AfterID: afterId}`.
- TodoCard: accept optional `dropIndicator?: boolean` prop — when true render a 2px accent line (primary color) above the card; add `onDragOver`/`onDrop` passthrough props (card-level handlers call `e.preventDefault()` + `e.stopPropagation()` so the column's own handlers don't double-fire).
- TodoBoard: track `dragOverCard: string | null`; hovering a card shows its indicator; dropping ON a card means "insert before hovered card": compute `afterId` = the previous card in that column's rendered order that belongs to THE SAME ListID as the dragged item ('' if none), and only offer the indicator when the hovered card's ListID matches the dragged item's ListID (in "All lists" view, cross-list card hover falls back to plain column drop = status move only, no indicator). Dropping on empty column space keeps existing behavior (status move). Callback: `onReorder(id, afterId, newStatus)`.
- Todos.tsx `handleReorder`: optimistic local reorder of `items`; if status changed, `await updateTodo(id, {Status: newStatus})`; then `await reorderTodo(id, afterId)`; reconcile with the returned item + `load()` on error (mirror handleMove's pattern). Track the dragged item's ListID via the existing dataTransfer payload — extend it to `text/todo-id` unchanged plus `text/todo-list-id` set in TodoCard's dragStart.
- [ ] Verify: `cd web && npm run build` clean; manual logic self-review (same-list computation, All-lists fallback).
- [ ] Docs: README todo-endpoints table += reorder row; CHANGELOG Phase 11 entry += "drag-to-reorder within columns (dense per-list renumbering)"; ROADMAP Phase 11 notes += reorder bullet.
