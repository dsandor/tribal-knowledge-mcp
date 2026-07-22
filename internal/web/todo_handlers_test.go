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
func (t *todoStore) DeleteTodo(ctx context.Context, id string) error {
	return t.real.DeleteTodo(ctx, id)
}
func (t *todoStore) ReorderTodo(ctx context.Context, todoID, afterID string) error {
	return t.real.ReorderTodo(ctx, todoID, afterID)
}
func (t *todoStore) AddExternalLink(ctx context.Context, l storage.ExternalLink) (string, error) {
	return t.real.AddExternalLink(ctx, l)
}
func (t *todoStore) RemoveExternalLink(ctx context.Context, todoID, linkID string) error {
	return t.real.RemoveExternalLink(ctx, todoID, linkID)
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

// doJSON performs an authenticated JSON request. Auth follows the same
// pattern as server_test.go's authRequest / visibility_handlers_test.go's
// requests: a Bearer token that mockStore.GetAPIKeyByHash accepts
// unconditionally (any hash resolves to a valid team-scoped admin key), plus
// a Content-Type header for bodies that carry one.
func doJSON(t *testing.T, srv *web.Server, method, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			t.Fatalf("encode body: %v", err)
		}
	}
	req := httptest.NewRequest(method, path, &buf)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer test-token")
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

func TestTodoUpdate_PatchPreservesFields(t *testing.T) {
	srv, _ := newTodoWebHarness(t)
	w := doJSON(t, srv, "POST", "/api/todo-lists", map[string]any{"Name": "L"})
	var list struct{ ID string }
	json.NewDecoder(w.Body).Decode(&list)

	w = doJSON(t, srv, "POST", "/api/todos", map[string]any{
		"ListID": list.ID, "Title": "Pull 10-Q", "Notes": "check footnotes",
		"Assignee": "bob", "DueDate": "2026-08-01", "Tags": []string{"a", "b"},
		"Priority": "high",
	})
	if w.Code != http.StatusOK {
		t.Fatalf("create item: %d %s", w.Code, w.Body)
	}
	var item struct{ ID string }
	json.NewDecoder(w.Body).Decode(&item)

	// PATCH with only Status set — everything else should be preserved.
	w = doJSON(t, srv, "PUT", "/api/todos/"+item.ID, map[string]any{"Status": "done"})
	if w.Code != http.StatusOK {
		t.Fatalf("update status: %d %s", w.Code, w.Body)
	}
	w = doJSON(t, srv, "GET", "/api/todos/"+item.ID, nil)
	var got map[string]any
	json.NewDecoder(w.Body).Decode(&got)
	if got["Status"] != "done" {
		t.Fatalf("status: %v", got["Status"])
	}
	if got["CompletedAt"] == nil {
		t.Fatalf("want CompletedAt set, got nil: %+v", got)
	}
	if got["Notes"] != "check footnotes" {
		t.Fatalf("notes not preserved: %v", got["Notes"])
	}
	if got["Assignee"] != "bob" {
		t.Fatalf("assignee not preserved: %v", got["Assignee"])
	}
	if got["Priority"] != "high" {
		t.Fatalf("priority not preserved: %v", got["Priority"])
	}
	tags, _ := got["Tags"].([]any)
	if len(tags) != 2 || tags[0] != "a" || tags[1] != "b" {
		t.Fatalf("tags not preserved: %v", got["Tags"])
	}
	if got["DueDate"] == nil {
		t.Fatalf("want DueDate still set, got nil: %+v", got)
	}

	// PATCH clearing DueDate only.
	w = doJSON(t, srv, "PUT", "/api/todos/"+item.ID, map[string]any{"DueDate": ""})
	if w.Code != http.StatusOK {
		t.Fatalf("clear due date: %d %s", w.Code, w.Body)
	}
	w = doJSON(t, srv, "GET", "/api/todos/"+item.ID, nil)
	json.NewDecoder(w.Body).Decode(&got)
	if got["DueDate"] != nil {
		t.Fatalf("want DueDate cleared, got %v", got["DueDate"])
	}
	if got["Notes"] != "check footnotes" {
		t.Fatalf("notes not preserved after due date clear: %v", got["Notes"])
	}
	if got["Assignee"] != "bob" {
		t.Fatalf("assignee not preserved after due date clear: %v", got["Assignee"])
	}

	// PATCH clearing Tags only.
	w = doJSON(t, srv, "PUT", "/api/todos/"+item.ID, map[string]any{"Tags": []string{}})
	if w.Code != http.StatusOK {
		t.Fatalf("clear tags: %d %s", w.Code, w.Body)
	}
	w = doJSON(t, srv, "GET", "/api/todos/"+item.ID, nil)
	json.NewDecoder(w.Body).Decode(&got)
	tags, _ = got["Tags"].([]any)
	if len(tags) != 0 {
		t.Fatalf("want Tags cleared, got %v", got["Tags"])
	}
	if got["Notes"] != "check footnotes" {
		t.Fatalf("notes not preserved after tags clear: %v", got["Notes"])
	}
	if got["Assignee"] != "bob" {
		t.Fatalf("assignee not preserved after tags clear: %v", got["Assignee"])
	}
}

func TestTodoListUpdate_PatchPreservesFields(t *testing.T) {
	srv, _ := newTodoWebHarness(t)
	w := doJSON(t, srv, "POST", "/api/todo-lists", map[string]any{
		"Name": "Q3 Review", "Description": "prep", "Color": "#38bdf8",
	})
	if w.Code != http.StatusOK {
		t.Fatalf("create: %d %s", w.Code, w.Body)
	}
	var created struct{ ID string }
	json.NewDecoder(w.Body).Decode(&created)

	w = doJSON(t, srv, "PUT", "/api/todo-lists/"+created.ID, map[string]any{"Archived": true})
	if w.Code != http.StatusOK {
		t.Fatalf("update: %d %s", w.Code, w.Body)
	}

	w = doJSON(t, srv, "GET", "/api/todo-lists/"+created.ID, nil)
	var got map[string]any
	json.NewDecoder(w.Body).Decode(&got)
	if got["Name"] != "Q3 Review" {
		t.Fatalf("name not preserved: %v", got["Name"])
	}
	if got["Description"] != "prep" {
		t.Fatalf("description not preserved: %v", got["Description"])
	}
	if got["Color"] != "#38bdf8" {
		t.Fatalf("color not preserved: %v", got["Color"])
	}
	if got["Archived"] != true {
		t.Fatalf("archived not applied: %v", got["Archived"])
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

// TestTodoLinkAdd_RejectsNonHTTPURL verifies that a javascript: (or any
// non-http(s)) URL on an external link is rejected with 400 rather than
// stored, since the web drawer renders link URLs directly as an <a href>.
func TestTodoLinkAdd_RejectsNonHTTPURL(t *testing.T) {
	srv, _ := newTodoWebHarness(t)
	w := doJSON(t, srv, "POST", "/api/todo-lists", map[string]any{"Name": "L"})
	var list struct{ ID string }
	json.NewDecoder(w.Body).Decode(&list)
	w = doJSON(t, srv, "POST", "/api/todos", map[string]any{"ListID": list.ID, "Title": "x"})
	var item struct{ ID string }
	json.NewDecoder(w.Body).Decode(&item)

	w = doJSON(t, srv, "POST", "/api/todos/"+item.ID+"/links", map[string]any{
		"Provider": "other", "URL": "javascript:alert(1)",
	})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("want 400 for javascript: URL, got %d %s", w.Code, w.Body)
	}
}

// TestTodoLinkRemove_IDOR verifies that deleting a link scoped to a different
// todo (guessing a valid linkId that belongs to someone else's todo) 404s
// instead of deleting the link.
func TestTodoLinkRemove_IDOR(t *testing.T) {
	srv, ts := newTodoWebHarness(t)
	w := doJSON(t, srv, "POST", "/api/todo-lists", map[string]any{"Name": "L"})
	var list struct{ ID string }
	json.NewDecoder(w.Body).Decode(&list)

	w = doJSON(t, srv, "POST", "/api/todos", map[string]any{"ListID": list.ID, "Title": "todo A"})
	var todoA struct{ ID string }
	json.NewDecoder(w.Body).Decode(&todoA)

	w = doJSON(t, srv, "POST", "/api/todos", map[string]any{"ListID": list.ID, "Title": "todo B"})
	var todoB struct{ ID string }
	json.NewDecoder(w.Body).Decode(&todoB)

	w = doJSON(t, srv, "POST", "/api/todos/"+todoB.ID+"/links", map[string]any{
		"Provider": "github", "ExternalID": "#7", "URL": "https://github.com/o/r/issues/7",
	})
	if w.Code != http.StatusOK {
		t.Fatalf("add link to todo B: %d %s", w.Code, w.Body)
	}
	var linkB struct{ ID string }
	json.NewDecoder(w.Body).Decode(&linkB)

	// Attempt to delete todo B's link by scoping the request to todo A.
	w = doJSON(t, srv, "DELETE", "/api/todos/"+todoA.ID+"/links/"+linkB.ID, nil)
	if w.Code != http.StatusNotFound {
		t.Fatalf("want 404 removing another todo's link, got %d %s", w.Code, w.Body)
	}

	got, err := ts.real.GetTodo(context.Background(), todoB.ID)
	if err != nil {
		t.Fatalf("GetTodo: %v", err)
	}
	if len(got.ExternalLinks) != 1 {
		t.Fatalf("todo B's link was deleted via IDOR: %+v", got.ExternalLinks)
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

// TestTodoRefsSet_CrossTenantRejected verifies that referencing an existing
// knowledge entry owned by another team is rejected with 404, and that the
// todo's refs are left unchanged (the rejected write does not partially
// apply). doJSON authenticates as team "test-team" (mockStore.GetAPIKeyByHash).
func TestTodoRefsSet_CrossTenantRejected(t *testing.T) {
	srv, ts := newTodoWebHarness(t)
	w := doJSON(t, srv, "POST", "/api/todo-lists", map[string]any{"Name": "L"})
	var list struct{ ID string }
	json.NewDecoder(w.Body).Decode(&list)
	w = doJSON(t, srv, "POST", "/api/todos", map[string]any{"ListID": list.ID, "Title": "x"})
	var item struct{ ID string }
	json.NewDecoder(w.Body).Decode(&item)

	// Seed an existing entry owned by a different team.
	ts.entries = append(ts.entries, storage.KnowledgeEntry{ID: "other-team-entry", TeamID: "other-team"})

	// Baseline: set some refs the caller can legitimately reach (nonexistent
	// IDs remain permissive).
	if w = doJSON(t, srv, "PUT", "/api/todos/"+item.ID+"/knowledge-refs", map[string]any{
		"EntryIDs": []string{"e1"},
	}); w.Code != http.StatusOK {
		t.Fatalf("baseline set refs: %d %s", w.Code, w.Body)
	}

	w = doJSON(t, srv, "PUT", "/api/todos/"+item.ID+"/knowledge-refs", map[string]any{
		"EntryIDs": []string{"other-team-entry"},
	})
	if w.Code != http.StatusNotFound {
		t.Fatalf("want 404 for cross-tenant entry ref, got %d %s", w.Code, w.Body)
	}

	got, err := ts.real.GetTodo(context.Background(), item.ID)
	if err != nil {
		t.Fatalf("GetTodo: %v", err)
	}
	if len(got.KnowledgeRefs) != 1 || got.KnowledgeRefs[0] != "e1" {
		t.Fatalf("refs changed despite rejected cross-tenant write: %v", got.KnowledgeRefs)
	}
}

// TestTodoReorder_HappyPath verifies a drag-move to a new position is
// reflected in the list-items endpoint's ordering.
func TestTodoReorder_HappyPath(t *testing.T) {
	srv, _ := newTodoWebHarness(t)
	w := doJSON(t, srv, "POST", "/api/todo-lists", map[string]any{"Name": "L"})
	var list struct{ ID string }
	json.NewDecoder(w.Body).Decode(&list)

	var ids []string
	for _, title := range []string{"a", "b", "c"} {
		w = doJSON(t, srv, "POST", "/api/todos", map[string]any{"ListID": list.ID, "Title": title})
		var item struct{ ID string }
		json.NewDecoder(w.Body).Decode(&item)
		ids = append(ids, item.ID)
	}

	// Move "a" (ids[0]) to sit right after "c" (ids[2]).
	w = doJSON(t, srv, "POST", "/api/todos/"+ids[0]+"/reorder", map[string]any{"AfterID": ids[2]})
	if w.Code != http.StatusOK {
		t.Fatalf("reorder: %d %s", w.Code, w.Body)
	}
	var updated map[string]any
	json.NewDecoder(w.Body).Decode(&updated)
	if updated["ID"] != ids[0] {
		t.Fatalf("reorder response is not the moved item: %+v", updated)
	}

	w = doJSON(t, srv, "GET", "/api/todo-lists/"+list.ID+"/items", nil)
	var items []map[string]any
	json.NewDecoder(w.Body).Decode(&items)
	if len(items) != 3 {
		t.Fatalf("want 3 items, got %+v", items)
	}
	gotOrder := []string{items[0]["Title"].(string), items[1]["Title"].(string), items[2]["Title"].(string)}
	wantOrder := []string{"b", "c", "a"}
	for i := range wantOrder {
		if gotOrder[i] != wantOrder[i] {
			t.Fatalf("order = %v, want %v", gotOrder, wantOrder)
		}
	}

	// Move "b" (ids[1]) to the top via AfterID "".
	w = doJSON(t, srv, "POST", "/api/todos/"+ids[1]+"/reorder", map[string]any{"AfterID": ""})
	if w.Code != http.StatusOK {
		t.Fatalf("reorder to top: %d %s", w.Code, w.Body)
	}
	w = doJSON(t, srv, "GET", "/api/todo-lists/"+list.ID+"/items", nil)
	json.NewDecoder(w.Body).Decode(&items)
	gotOrder = []string{items[0]["Title"].(string), items[1]["Title"].(string), items[2]["Title"].(string)}
	if gotOrder[0] != "b" {
		t.Fatalf("want b at top, got %v", gotOrder)
	}
}

// TestTodoReorder_CrossTeamAfterID404 verifies that an AfterID belonging to a
// todo the caller's team cannot access 404s without leaking existence.
func TestTodoReorder_CrossTeamAfterID404(t *testing.T) {
	srv, ts := newTodoWebHarness(t)
	ctx := context.Background()
	w := doJSON(t, srv, "POST", "/api/todo-lists", map[string]any{"Name": "L"})
	var list struct{ ID string }
	json.NewDecoder(w.Body).Decode(&list)
	w = doJSON(t, srv, "POST", "/api/todos", map[string]any{"ListID": list.ID, "Title": "mine"})
	var item struct{ ID string }
	json.NewDecoder(w.Body).Decode(&item)

	// A todo owned by a different team, created directly via the real store
	// (bypassing the HTTP layer, which always authenticates as "test-team").
	otherListID, err := ts.real.CreateTodoList(ctx, storage.TodoList{TeamID: "other-team", Name: "Other"})
	if err != nil {
		t.Fatalf("create other-team list: %v", err)
	}
	otherTodoID, err := ts.real.CreateTodo(ctx, storage.TodoItem{ListID: otherListID, TeamID: "other-team", Title: "not yours"})
	if err != nil {
		t.Fatalf("create other-team todo: %v", err)
	}

	w = doJSON(t, srv, "POST", "/api/todos/"+item.ID+"/reorder", map[string]any{"AfterID": otherTodoID})
	if w.Code != http.StatusNotFound {
		t.Fatalf("want 404 for cross-team AfterID, got %d %s", w.Code, w.Body)
	}
}

// TestTodoReorder_DifferentList400 verifies that an AfterID in a different
// (but same-team, accessible) list is rejected with 400 rather than silently
// moving the todo across lists.
func TestTodoReorder_DifferentList400(t *testing.T) {
	srv, _ := newTodoWebHarness(t)
	w := doJSON(t, srv, "POST", "/api/todo-lists", map[string]any{"Name": "L1"})
	var list1 struct{ ID string }
	json.NewDecoder(w.Body).Decode(&list1)
	w = doJSON(t, srv, "POST", "/api/todo-lists", map[string]any{"Name": "L2"})
	var list2 struct{ ID string }
	json.NewDecoder(w.Body).Decode(&list2)

	w = doJSON(t, srv, "POST", "/api/todos", map[string]any{"ListID": list1.ID, "Title": "a"})
	var itemA struct{ ID string }
	json.NewDecoder(w.Body).Decode(&itemA)
	w = doJSON(t, srv, "POST", "/api/todos", map[string]any{"ListID": list2.ID, "Title": "x"})
	var itemX struct{ ID string }
	json.NewDecoder(w.Body).Decode(&itemX)

	w = doJSON(t, srv, "POST", "/api/todos/"+itemA.ID+"/reorder", map[string]any{"AfterID": itemX.ID})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("want 400 for cross-list AfterID, got %d %s", w.Code, w.Body)
	}
}
