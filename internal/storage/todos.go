package storage

import (
	"context"
	"errors"
	"time"
)

// ErrDifferentList is returned by ReorderTodo when afterID belongs to a
// different list than todoID.
var ErrDifferentList = errors.New("todos are in different lists")

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
	// ReorderTodo places todoID immediately after afterID within the same list,
	// renumbering the whole list to dense 1..N positions. afterID == "" moves
	// the todo to the top. Returns ErrNotFound if either todo is missing and
	// ErrDifferentList if afterID belongs to another list.
	ReorderTodo(ctx context.Context, todoID, afterID string) error

	// Links
	AddExternalLink(ctx context.Context, link ExternalLink) (string, error)
	// RemoveExternalLink deletes an external link, scoped to the owning todo so
	// callers cannot delete another todo's link by guessing its ID. Returns
	// ErrNotFound if no link with that ID exists under todoID.
	RemoveExternalLink(ctx context.Context, todoID, linkID string) error
	SetKnowledgeRefs(ctx context.Context, todoID string, entryIDs []string) error
	ListTodosForEntry(ctx context.Context, entryID string) ([]TodoItem, error)
}
