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

// isHTTPURL reports whether s starts with an http(s) scheme, case-insensitive.
// Used to reject stored javascript:/data: URIs on external link URLs before
// they can be persisted and later rendered as an href (stored XSS guard).
func isHTTPURL(s string) bool {
	lower := strings.ToLower(s)
	return strings.HasPrefix(lower, "http://") || strings.HasPrefix(lower, "https://")
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
		list, err := store.GetTodoList(ctx, id)
		if err != nil {
			return mcplib.NewToolResultError(fmt.Sprintf("todo list not found: %v", err)), nil
		}
		if !auth.CanAccess(auth.GetTeamContext(ctx), list.TeamID) {
			return mcplib.NewToolResultError("todo list not found"), nil
		}
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

// resolveOrCreateList returns the target list ID and its owning team for
// todo_add: explicit list_id wins, but only after an ownership check (the
// caller's team must be able to access the list's team) — otherwise it
// returns "todo list not found" without leaking whether the ID exists.
// Absent an explicit list_id, list_name is matched case-insensitively within
// the caller's team and auto-created if absent.
func resolveOrCreateList(ctx context.Context, store storage.TodoStore, teamID, actorID, listID, listName string) (resolvedListID, resolvedTeamID string, err error) {
	if listID != "" {
		list, err := store.GetTodoList(ctx, listID)
		if err != nil {
			return "", "", fmt.Errorf("todo list not found")
		}
		if !auth.CanAccess(auth.GetTeamContext(ctx), list.TeamID) {
			return "", "", fmt.Errorf("todo list not found")
		}
		return list.ID, list.TeamID, nil
	}
	if listName == "" {
		return "", "", fmt.Errorf("either list_id or list_name is required")
	}
	lists, err := store.ListTodoLists(ctx, teamID, true)
	if err != nil {
		return "", "", err
	}
	for _, l := range lists {
		if strings.EqualFold(l.Name, listName) {
			return l.ID, teamID, nil
		}
	}
	id, err := store.CreateTodoList(ctx, storage.TodoList{TeamID: teamID, Name: listName, CreatedBy: actorID})
	if err != nil {
		return "", "", err
	}
	return id, teamID, nil
}

// HandleTodoAdd creates a todo item, auto-creating the named list if needed.
func HandleTodoAdd(store storage.TodoStore) func(context.Context, mcplib.CallToolRequest) (*mcplib.CallToolResult, error) {
	return func(ctx context.Context, req mcplib.CallToolRequest) (*mcplib.CallToolResult, error) {
		title := req.GetString("title", "")
		if title == "" {
			return mcplib.NewToolResultError("title is required"), nil
		}
		teamID, actorID := todoActor(ctx)
		lid, resolvedTeamID, err := resolveOrCreateList(ctx, store, teamID, actorID,
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
			TeamID:    resolvedTeamID,
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
		recordTodoEvent(ctx, store, "todo_created", id, title)
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
					return mcplib.NewToolResultError(fmt.Sprintf("invalid status %q: must be open, in_progress, blocked, done, or cancelled", v)), nil
				}
				f.Status = append(f.Status, v)
			}
		}
		if p := req.GetString("priority", ""); p != "" {
			for _, v := range strings.Split(p, ",") {
				v = strings.TrimSpace(v)
				if !storage.ValidTodoPriority(v) {
					return mcplib.NewToolResultError(fmt.Sprintf("invalid priority %q: must be low, medium, high, or urgent", v)), nil
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
			targetList, err := store.GetTodoList(ctx, v)
			if err != nil {
				return mcplib.NewToolResultError("todo list not found"), nil
			}
			if !auth.CanAccess(auth.GetTeamContext(ctx), targetList.TeamID) {
				return mcplib.NewToolResultError("todo list not found"), nil
			}
			item.ListID = v
			item.TeamID = targetList.TeamID
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
		if p := req.GetInt("position", 0); p > 0 {
			item.Position = p
		}
		if err := store.UpdateTodo(ctx, *item); err != nil {
			return mcplib.NewToolResultError(fmt.Sprintf("update todo failed: %v", err)), nil
		}
		data, err := json.Marshal(item)
		if err != nil {
			return mcplib.NewToolResultError(fmt.Sprintf("marshal: %v", err)), nil
		}
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
		recordTodoEvent(ctx, store, "todo_completed", id, item.Title)
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
		url := req.GetString("url", "")
		if url != "" && !isHTTPURL(url) {
			return mcplib.NewToolResultError("invalid url: must be http(s)"), nil
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
			URL:            url,
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
		// Cross-tenant guard: only applied when the backing store also exposes
		// GetEntry (the production composite store does; bare TodoStore test
		// doubles may not — then validation is skipped). An EntryID that
		// resolves to an existing entry the caller's team cannot access is
		// rejected; nonexistent IDs remain permitted (no FK; a lookup miss is
		// inert junk, not a leak).
		if entryStore, ok := store.(interface {
			GetEntry(context.Context, string) (*storage.KnowledgeEntry, error)
		}); ok {
			tc := auth.GetTeamContext(ctx)
			for _, id := range ids {
				entry, err := entryStore.GetEntry(ctx, id)
				if err != nil {
					continue
				}
				if !auth.CanAccess(tc, entry.TeamID) {
					return mcplib.NewToolResultError(fmt.Sprintf("knowledge entry not found: %s", id)), nil
				}
			}
		}
		if err := store.SetKnowledgeRefs(ctx, todoID, ids); err != nil {
			return mcplib.NewToolResultError(fmt.Sprintf("link knowledge failed: %v", err)), nil
		}
		return mcplib.NewToolResultText(fmt.Sprintf("todo %s now references %d knowledge entries", todoID, len(ids))), nil
	}
}

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
			mcplib.WithNumber("position", mcplib.Description("Explicit position within the list (1-based). Prefer leaving ordering to the UI; set only when the user asks.")),
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
	// registration in resources.go — same AddResourceTemplate + resourceTemplateHandler
	// API, extracting {id} via extractPathParam.
	s.AddResourceTemplate(
		mcplib.NewResourceTemplate("todos://list/{id}", "Todo List Items",
			mcplib.WithTemplateDescription("All items in one todo list, by list UUID"),
			mcplib.WithTemplateMIMEType("application/json"),
		),
		resourceTemplateHandler(func(ctx context.Context, req mcplib.ReadResourceRequest) (string, error) {
			id := extractPathParam(req.Params.URI, "todos://list/")
			list, err := store.GetTodoList(ctx, id)
			if err != nil {
				return "", fmt.Errorf("todo list not found: %w", err)
			}
			if !auth.CanAccess(auth.GetTeamContext(ctx), list.TeamID) {
				return "", fmt.Errorf("todo list not found")
			}
			items, err := store.QueryTodos(ctx, storage.TodoFilter{TeamID: list.TeamID, ListID: list.ID, Limit: -1})
			if err != nil {
				return "", fmt.Errorf("query todos: %w", err)
			}
			data, err := json.Marshal(items)
			if err != nil {
				return "", err
			}
			return string(data), nil
		}),
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
}
