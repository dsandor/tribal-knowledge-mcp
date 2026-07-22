package mcp

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	mcplib "github.com/mark3labs/mcp-go/mcp"

	"github.com/dsandor/memory/internal/auth"
	"github.com/dsandor/memory/internal/storage"
)

// newToolRequest builds a mcplib.CallToolRequest carrying the given arguments,
// mirroring the callReq helper in tools_test.go (package mcp_test) adapted to
// take a map directly, as this file lives in package mcp (white-box).
func newToolRequest(args map[string]any) mcplib.CallToolRequest {
	req := mcplib.CallToolRequest{}
	req.Params.Arguments = args
	return req
}

// toolResultText extracts the text content from a tool result, mirroring the
// textContent helper in tools_test.go.
func toolResultText(t *testing.T, res *mcplib.CallToolResult) string {
	t.Helper()
	if res == nil || len(res.Content) == 0 {
		t.Fatalf("empty result content")
	}
	tc, ok := res.Content[0].(mcplib.TextContent)
	if !ok {
		t.Fatalf("content not text: %+v", res.Content[0])
	}
	return tc.Text
}

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

// TestHandleTodoUpdate_Position verifies todo_update's optional numeric
// position param patches item.Position directly (independent of the
// ReorderTodo dense-renumber algorithm exercised by the storage/REST layers).
func TestHandleTodoUpdate_Position(t *testing.T) {
	s := newTodoTestStore(t)
	ctx := context.Background()
	lid, _ := s.CreateTodoList(ctx, storage.TodoList{Name: "L"})
	tid, _ := s.CreateTodo(ctx, storage.TodoItem{ListID: lid, Title: "x"})

	res, err := HandleTodoUpdate(s)(ctx, newToolRequest(map[string]any{"id": tid, "position": 5}))
	if err != nil {
		t.Fatalf("handler err: %v", err)
	}
	if res.IsError {
		t.Fatalf("update errored: %+v", res)
	}
	item, err := s.GetTodo(ctx, tid)
	if err != nil {
		t.Fatalf("GetTodo: %v", err)
	}
	if item.Position != 5 {
		t.Fatalf("position = %d, want 5", item.Position)
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

// todoCtxWithTeam returns a context carrying the given teamID as a
// TeamContext, mirroring the ctxWithTeam helper in team_isolation_test.go
// (package mcp_test) — that helper lives in a different Go package
// (mcp_test) so its unexported symbol isn't visible from this
// white-box (package mcp) test file; auth.WithTestTeamContext is the
// exported mechanism both rely on.
func todoCtxWithTeam(teamID string) context.Context {
	return auth.WithTestTeamContext(context.Background(), auth.TeamContext{
		TeamID: teamID,
		UserID: "test-user-" + teamID,
		Role:   "member",
	})
}

// TestTodoTools_TeamIsolation verifies that todo_add, todo_update, and
// todo_get all refuse cross-team access to a list/todo owned by another
// team, returning a not-found-shaped error rather than leaking existence
// or allowing the write.
func TestTodoTools_TeamIsolation(t *testing.T) {
	s := newTodoTestStore(t)
	bg := context.Background()

	// A list owned by team-b, created directly via the store (bypassing any
	// handler-level team stamping).
	teamBListID, err := s.CreateTodoList(bg, storage.TodoList{TeamID: "team-b", Name: "Team B List"})
	if err != nil {
		t.Fatalf("create team-b list: %v", err)
	}

	teamACtx := todoCtxWithTeam("team-a")

	t.Run("todo_add into another team's list", func(t *testing.T) {
		res, err := HandleTodoAdd(s)(teamACtx, newToolRequest(map[string]any{
			"title":   "sneaky item",
			"list_id": teamBListID,
		}))
		if err != nil {
			t.Fatalf("handler err: %v", err)
		}
		if !res.IsError || !strings.Contains(toolResultText(t, res), "not found") {
			t.Fatalf("want IsError with 'not found', got %+v", res)
		}
	})

	t.Run("todo_update moving a todo into another team's list", func(t *testing.T) {
		// A todo owned by team-a, in a team-a list.
		teamAListID, err := s.CreateTodoList(bg, storage.TodoList{TeamID: "team-a", Name: "Team A List"})
		if err != nil {
			t.Fatalf("create team-a list: %v", err)
		}
		tid, err := s.CreateTodo(bg, storage.TodoItem{ListID: teamAListID, TeamID: "team-a", Title: "team-a todo"})
		if err != nil {
			t.Fatalf("create team-a todo: %v", err)
		}

		res, err := HandleTodoUpdate(s)(teamACtx, newToolRequest(map[string]any{
			"id":      tid,
			"list_id": teamBListID,
		}))
		if err != nil {
			t.Fatalf("handler err: %v", err)
		}
		if !res.IsError || !strings.Contains(toolResultText(t, res), "not found") {
			t.Fatalf("want IsError with 'not found', got %+v", res)
		}
	})

	t.Run("todo_get on another team's todo", func(t *testing.T) {
		tid, err := s.CreateTodo(bg, storage.TodoItem{ListID: teamBListID, TeamID: "team-b", Title: "team-b todo"})
		if err != nil {
			t.Fatalf("create team-b todo: %v", err)
		}

		res, err := HandleTodoGet(s)(teamACtx, newToolRequest(map[string]any{"id": tid}))
		if err != nil {
			t.Fatalf("handler err: %v", err)
		}
		if !res.IsError || !strings.Contains(toolResultText(t, res), "not found") {
			t.Fatalf("want IsError with 'not found', got %+v", res)
		}
	})

	t.Run("todo_link_knowledge referencing another team's entry", func(t *testing.T) {
		teamAListID, err := s.CreateTodoList(bg, storage.TodoList{TeamID: "team-a", Name: "Team A List 2"})
		if err != nil {
			t.Fatalf("create team-a list: %v", err)
		}
		tid, err := s.CreateTodo(bg, storage.TodoItem{ListID: teamAListID, TeamID: "team-a", Title: "team-a todo 2"})
		if err != nil {
			t.Fatalf("create team-a todo: %v", err)
		}

		// A knowledge entry that genuinely exists but belongs to team-b.
		// embedding dim must match the store's configured dim (4, per
		// newTodoTestStore).
		entryID, err := s.StoreEntry(bg, storage.KnowledgeEntry{
			Type: storage.KTDomainFact, Title: "team-b secret", Content: "shh", TeamID: "team-b",
		}, []float32{0, 0, 0, 0})
		if err != nil {
			t.Fatalf("store team-b entry: %v", err)
		}

		res, err := HandleTodoLinkKnowledge(s)(teamACtx, newToolRequest(map[string]any{
			"todo_id": tid, "entry_ids": entryID,
		}))
		if err != nil {
			t.Fatalf("handler err: %v", err)
		}
		if !res.IsError || !strings.Contains(toolResultText(t, res), "not found") {
			t.Fatalf("want IsError with 'not found' for cross-team entry ref, got %+v", res)
		}
		item, _ := s.GetTodo(bg, tid)
		if len(item.KnowledgeRefs) != 0 {
			t.Fatalf("refs changed despite rejected cross-team write: %v", item.KnowledgeRefs)
		}
	})
}

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

	// A javascript: URL must be rejected (stored XSS guard) rather than
	// persisted — the web drawer renders link URLs as an <a href>.
	xss, _ := HandleTodoLinkIssue(s)(ctx, newToolRequest(map[string]any{
		"todo_id": tid, "provider": "jira", "external_id": "PROJ-10",
		"url": "javascript:alert(1)",
	}))
	if !xss.IsError || !strings.Contains(toolResultText(t, xss), "url") {
		t.Fatalf("want error for javascript: URL, got %+v", xss)
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
