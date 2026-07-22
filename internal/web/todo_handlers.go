package web

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/dsandor/memory/internal/auth"
	"github.com/dsandor/memory/internal/live"
	"github.com/dsandor/memory/internal/storage"
	"github.com/go-chi/chi/v5"
)

// todoListBody is the JSON payload for create/update of a list.
// PATCH SEMANTICS on update: nil pointer = field unchanged; non-nil = set.
type todoListBody struct {
	Name        *string
	Description *string
	Domain      *string
	Color       *string
	Archived    *bool
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
	name := strOr(body.Name, "")
	if strings.TrimSpace(name) == "" {
		writeError(w, http.StatusBadRequest, "bad_request", "Name is required")
		return
	}
	tc := auth.GetTeamContext(r.Context())
	id, err := s.store.CreateTodoList(r.Context(), storage.TodoList{
		TeamID: tc.TeamID, Name: name, Description: strOr(body.Description, ""),
		Domain: strOr(body.Domain, ""), Color: strOr(body.Color, ""), CreatedBy: tc.EffectiveActorID(),
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
	if body.Name != nil && strings.TrimSpace(*body.Name) != "" {
		list.Name = *body.Name
	}
	if body.Description != nil {
		list.Description = *body.Description
	}
	if body.Domain != nil {
		list.Domain = *body.Domain
	}
	if body.Color != nil {
		list.Color = *body.Color
	}
	if body.Archived != nil {
		list.Archived = *body.Archived
	}
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
		// Moving lists — verify the target list is in-team too, and stamp the
		// item's TeamID to match (mirrors the MCP layer's parity stamp for
		// cross-team moves).
		targetList, ok := s.fetchTodoListForTeam(w, r, *body.ListID)
		if !ok {
			return
		}
		item.ListID = *body.ListID
		item.TeamID = targetList.TeamID
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

// todoReorderBody is the JSON payload for POST /api/todos/{id}/reorder.
// AfterID == "" moves the todo to the top of its list.
type todoReorderBody struct {
	AfterID string
}

// handleTodoReorder drag-moves a todo to sit immediately after AfterID within
// its own list (or to the top when AfterID is empty), renumbering the whole
// list to dense 1..N positions.
func (s *Server) handleTodoReorder(w http.ResponseWriter, r *http.Request) {
	item, ok := s.fetchTodoForTeam(w, r, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	var body todoReorderBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "invalid JSON body")
		return
	}
	if body.AfterID != "" {
		afterItem, ok := s.fetchTodoForTeam(w, r, body.AfterID)
		if !ok {
			return
		}
		// Pre-check: catch a cross-list AfterID before it ever reaches the
		// store, so the 400 is returned even if the store's own
		// ErrDifferentList check below never fires.
		if afterItem.ListID != item.ListID {
			writeError(w, http.StatusBadRequest, "bad_request", "AfterID must be a todo in the same list")
			return
		}
	}
	if err := s.store.ReorderTodo(r.Context(), item.ID, body.AfterID); err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "todo not found")
			return
		}
		if errors.Is(err, storage.ErrDifferentList) {
			writeError(w, http.StatusBadRequest, "bad_request", "AfterID must be a todo in the same list")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal", "reorder todo failed")
		return
	}
	updated, err := s.store.GetTodo(r.Context(), item.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "fetch reordered todo failed")
		return
	}
	writeJSON(w, updated)
}

// recordTodoActivity emits a feed event; failures are non-fatal (best effort).
// It also publishes to the live SSE hub (mirrors handleKnowledgeStore in
// handlers.go) so connected clients see todo activity in real time instead of
// only after their next snapshot reconnect. publishLive is a no-op when no
// hub is configured.
func (s *Server) recordTodoActivity(r *http.Request, eventType string, item *storage.TodoItem) {
	if item == nil {
		return
	}
	tc := auth.GetTeamContext(r.Context())
	_ = s.store.RecordActivity(r.Context(), storage.ActivityEvent{
		EventType: eventType,
		ActorID:   tc.EffectiveActorID(),
		Metadata:  map[string]string{"todo_id": item.ID, "title": item.Title},
	})

	actorID := tc.UserID
	if actorID == "" {
		actorID = tc.KeyID
	}
	// Meta carries the same {todo_id, title} shape RecordActivity persists so
	// eventLabel/eventIcon on the frontend (which read event.meta, not the
	// top-level title field) render identically whether the event arrived via
	// live push or a snapshot reconnect.
	s.publishLive(live.LiveEvent{
		Type:   eventType,
		TeamID: item.TeamID,
		Title:  live.CapFragment(item.Title),
		Actor:  live.ActorRef{ID: actorID, Display: tc.Display},
		Meta:   map[string]string{"todo_id": item.ID, "title": item.Title},
	})
}

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
	if body.URL != "" {
		lower := strings.ToLower(body.URL)
		if !strings.HasPrefix(lower, "http://") && !strings.HasPrefix(lower, "https://") {
			writeError(w, http.StatusBadRequest, "bad_request", "invalid URL: must be http(s)")
			return
		}
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
	item, ok := s.fetchTodoForTeam(w, r, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	if err := s.store.RemoveExternalLink(r.Context(), item.ID, chi.URLParam(r, "linkId")); err != nil {
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
	// Cross-tenant guard: an EntryID that resolves to an existing entry the
	// caller's team cannot access is rejected. Nonexistent IDs remain
	// permitted (no FK; a lookup miss is treated as inert junk, not a leak).
	tc := auth.GetTeamContext(r.Context())
	for _, id := range body.EntryIDs {
		entry, err := s.store.GetEntry(r.Context(), id)
		if err != nil {
			continue
		}
		if !auth.CanAccess(tc, entry.TeamID) {
			writeError(w, http.StatusNotFound, "not_found", "knowledge entry not found")
			return
		}
	}
	if err := s.store.SetKnowledgeRefs(r.Context(), item.ID, body.EntryIDs); err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "set refs failed")
		return
	}
	updated, _ := s.store.GetTodo(r.Context(), item.ID)
	writeJSON(w, updated)
}
