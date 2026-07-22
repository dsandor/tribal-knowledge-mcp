package storage

import (
	"context"
	"errors"
	"testing"
	"time"
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

// TestTodoCreate_CompletedAtInvariant guards the invariant that a todo's
// CompletedAt is set if and only if it is created directly in a terminal
// "done" status — e.g. via the REST create endpoint, which accepts any valid
// Status up front rather than always starting at "open".
func TestTodoCreate_CompletedAtInvariant(t *testing.T) {
	s := newTestAnalysisStore(t)
	ctx := context.Background()
	lid, _ := s.CreateTodoList(ctx, TodoList{TeamID: "t", Name: "L"})

	doneID, err := s.CreateTodo(ctx, TodoItem{
		ListID: lid, TeamID: "t", Title: "already done", Status: TodoStatusDone,
	})
	if err != nil {
		t.Fatalf("CreateTodo (done): %v", err)
	}
	done, err := s.GetTodo(ctx, doneID)
	if err != nil {
		t.Fatalf("GetTodo (done): %v", err)
	}
	if done.Status != TodoStatusDone || done.CompletedAt == nil {
		t.Errorf("todo created with Status=done should have CompletedAt set: %+v", done)
	}

	openID, err := s.CreateTodo(ctx, TodoItem{
		ListID: lid, TeamID: "t", Title: "default status",
	})
	if err != nil {
		t.Fatalf("CreateTodo (open): %v", err)
	}
	open, err := s.GetTodo(ctx, openID)
	if err != nil {
		t.Fatalf("GetTodo (open): %v", err)
	}
	if open.Status != TodoStatusOpen || open.CompletedAt != nil {
		t.Errorf("todo created with default status should have nil CompletedAt: %+v", open)
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

func TestTodoExternalLinks(t *testing.T) {
	s := newTestAnalysisStore(t)
	ctx := context.Background()
	lid, _ := s.CreateTodoList(ctx, TodoList{TeamID: "t", Name: "L"})
	tid, _ := s.CreateTodo(ctx, TodoItem{ListID: lid, TeamID: "t", Title: "x"})
	otherTid, _ := s.CreateTodo(ctx, TodoItem{ListID: lid, TeamID: "t", Title: "other"})

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

	// IDOR guard: removing the link through the wrong todo (or a fake ID) must
	// fail with ErrNotFound and leave the link untouched.
	if err := s.RemoveExternalLink(ctx, otherTid, linkID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("want ErrNotFound removing link via wrong todo, got %v", err)
	}
	item, _ = s.GetTodo(ctx, tid)
	if len(item.ExternalLinks) != 2 {
		t.Fatalf("link removed despite wrong todo scope: %+v", item.ExternalLinks)
	}

	if err := s.RemoveExternalLink(ctx, tid, linkID); err != nil {
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

	// Direct check: the FK cascade must have removed the child rows themselves.
	var n int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM todo_knowledge_refs WHERE todo_id = ?`, tid2).Scan(&n); err != nil {
		t.Fatalf("count refs: %v", err)
	}
	if n != 0 {
		t.Fatalf("cascade left %d orphan refs for deleted todo", n)
	}
}

// assertDensePositions fetches every item in listID and asserts their
// positions are exactly the dense set 1..N (order not asserted here — callers
// check ordering separately via titles/ids).
func assertDensePositions(t *testing.T, s *SQLiteStore, listID string, n int) {
	t.Helper()
	items, err := s.QueryTodos(context.Background(), TodoFilter{ListID: listID, Limit: -1})
	if err != nil {
		t.Fatalf("QueryTodos: %v", err)
	}
	if len(items) != n {
		t.Fatalf("want %d items, got %d", n, len(items))
	}
	seen := map[int]bool{}
	for _, it := range items {
		seen[it.Position] = true
	}
	for i := 1; i <= n; i++ {
		if !seen[i] {
			t.Fatalf("position %d missing from dense renumber; items: %+v", i, items)
		}
	}
}

// orderedTitles returns the titles of a list's items in position order.
func orderedTitles(t *testing.T, s *SQLiteStore, listID string) []string {
	t.Helper()
	items, err := s.QueryTodos(context.Background(), TodoFilter{ListID: listID, Limit: -1})
	if err != nil {
		t.Fatalf("QueryTodos: %v", err)
	}
	titles := make([]string, len(items))
	for i, it := range items {
		titles[i] = it.Title
	}
	return titles
}

func TestTodoReorder_MiddleToTop(t *testing.T) {
	s := newTestAnalysisStore(t)
	ctx := context.Background()
	lid, _ := s.CreateTodoList(ctx, TodoList{TeamID: "t", Name: "L"})
	a, _ := s.CreateTodo(ctx, TodoItem{ListID: lid, TeamID: "t", Title: "a"})
	b, _ := s.CreateTodo(ctx, TodoItem{ListID: lid, TeamID: "t", Title: "b"})
	c, _ := s.CreateTodo(ctx, TodoItem{ListID: lid, TeamID: "t", Title: "c"})
	_ = a

	// Move "c" (currently last) to the top via afterID "".
	if err := s.ReorderTodo(ctx, c, ""); err != nil {
		t.Fatalf("ReorderTodo: %v", err)
	}
	got := orderedTitles(t, s, lid)
	want := []string{"c", "a", "b"}
	if len(got) != len(want) {
		t.Fatalf("order: %v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("order = %v, want %v", got, want)
		}
	}
	assertDensePositions(t, s, lid, 3)
	_ = b
}

func TestTodoReorder_TopToAfterLast(t *testing.T) {
	s := newTestAnalysisStore(t)
	ctx := context.Background()
	lid, _ := s.CreateTodoList(ctx, TodoList{TeamID: "t", Name: "L"})
	a, _ := s.CreateTodo(ctx, TodoItem{ListID: lid, TeamID: "t", Title: "a"})
	b, _ := s.CreateTodo(ctx, TodoItem{ListID: lid, TeamID: "t", Title: "b"})
	c, _ := s.CreateTodo(ctx, TodoItem{ListID: lid, TeamID: "t", Title: "c"})

	// Move "a" (currently first) to right after "c" (currently last).
	if err := s.ReorderTodo(ctx, a, c); err != nil {
		t.Fatalf("ReorderTodo: %v", err)
	}
	got := orderedTitles(t, s, lid)
	want := []string{"b", "c", "a"}
	if len(got) != len(want) {
		t.Fatalf("order: %v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("order = %v, want %v", got, want)
		}
	}
	assertDensePositions(t, s, lid, 3)
	_ = b
}

func TestTodoReorder_ErrNotFound(t *testing.T) {
	s := newTestAnalysisStore(t)
	ctx := context.Background()
	lid, _ := s.CreateTodoList(ctx, TodoList{TeamID: "t", Name: "L"})
	a, _ := s.CreateTodo(ctx, TodoItem{ListID: lid, TeamID: "t", Title: "a"})

	if err := s.ReorderTodo(ctx, "bogus-todo", ""); !errors.Is(err, ErrNotFound) {
		t.Fatalf("want ErrNotFound for bogus todoID, got %v", err)
	}
	if err := s.ReorderTodo(ctx, a, "bogus-after"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("want ErrNotFound for bogus afterID, got %v", err)
	}
}

func TestTodoReorder_ErrDifferentList(t *testing.T) {
	s := newTestAnalysisStore(t)
	ctx := context.Background()
	lid1, _ := s.CreateTodoList(ctx, TodoList{TeamID: "t", Name: "L1"})
	lid2, _ := s.CreateTodoList(ctx, TodoList{TeamID: "t", Name: "L2"})
	a, _ := s.CreateTodo(ctx, TodoItem{ListID: lid1, TeamID: "t", Title: "a"})
	x, _ := s.CreateTodo(ctx, TodoItem{ListID: lid2, TeamID: "t", Title: "x"})

	if err := s.ReorderTodo(ctx, a, x); !errors.Is(err, ErrDifferentList) {
		t.Fatalf("want ErrDifferentList, got %v", err)
	}
}

func TestTodoReorder_DenseRenumberAfterGap(t *testing.T) {
	// Deleting an item leaves a position gap; reordering afterward must still
	// yield an exactly dense 1..N sequence.
	s := newTestAnalysisStore(t)
	ctx := context.Background()
	lid, _ := s.CreateTodoList(ctx, TodoList{TeamID: "t", Name: "L"})
	a, _ := s.CreateTodo(ctx, TodoItem{ListID: lid, TeamID: "t", Title: "a"})
	b, _ := s.CreateTodo(ctx, TodoItem{ListID: lid, TeamID: "t", Title: "b"})
	c, _ := s.CreateTodo(ctx, TodoItem{ListID: lid, TeamID: "t", Title: "c"})
	d, _ := s.CreateTodo(ctx, TodoItem{ListID: lid, TeamID: "t", Title: "d"})
	if err := s.DeleteTodo(ctx, b); err != nil {
		t.Fatalf("DeleteTodo: %v", err)
	}
	// list is now a(1), c(3), d(4) — a gap at position 2.

	if err := s.ReorderTodo(ctx, d, a); err != nil {
		t.Fatalf("ReorderTodo: %v", err)
	}
	got := orderedTitles(t, s, lid)
	want := []string{"a", "d", "c"}
	if len(got) != len(want) {
		t.Fatalf("order: %v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("order = %v, want %v", got, want)
		}
	}
	assertDensePositions(t, s, lid, 3)
	_ = c
}
