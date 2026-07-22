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
			created_by, assignee, due_date, position, tags, completed_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?,
			(SELECT COALESCE(MAX(position), 0) + 1 FROM todo_items WHERE list_id = ?), ?,
			CASE WHEN ? = 'done' THEN CURRENT_TIMESTAMP ELSE NULL END)
	`, item.ID, item.ListID, item.TeamID, item.Title, item.Notes, string(item.Status),
		string(item.Priority), item.CreatedBy, item.Assignee, fmtSQLiteTime(item.DueDate),
		item.ListID, string(tagsJSON), string(item.Status))
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

// ReorderTodo places todoID immediately after afterID within the same list,
// renumbering the whole list to dense 1..N positions. See TodoStore.ReorderTodo.
func (s *SQLiteStore) ReorderTodo(ctx context.Context, todoID, afterID string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	var listID string
	if err := tx.QueryRowContext(ctx, `SELECT list_id FROM todo_items WHERE id = ?`, todoID).Scan(&listID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		return fmt.Errorf("lookup todo: %w", err)
	}
	if afterID != "" {
		var afterListID string
		if err := tx.QueryRowContext(ctx, `SELECT list_id FROM todo_items WHERE id = ?`, afterID).Scan(&afterListID); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return ErrNotFound
			}
			return fmt.Errorf("lookup after todo: %w", err)
		}
		if afterListID != listID {
			return ErrDifferentList
		}
	}

	rows, err := tx.QueryContext(ctx,
		`SELECT id, position FROM todo_items WHERE list_id = ? ORDER BY position ASC, created_at ASC`, listID)
	if err != nil {
		return fmt.Errorf("load list items: %w", err)
	}
	// origPos records each item's current position so the renumbering loop
	// below only writes rows whose position actually changed.
	origPos := map[string]int{}
	ordered := []string{}
	for rows.Next() {
		var id string
		var pos int
		if err := rows.Scan(&id, &pos); err != nil {
			rows.Close()
			return fmt.Errorf("scan item: %w", err)
		}
		origPos[id] = pos
		if id != todoID {
			ordered = append(ordered, id)
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate list items: %w", err)
	}
	rows.Close()

	insertAt := 0
	if afterID != "" {
		for i, id := range ordered {
			if id == afterID {
				insertAt = i + 1
				break
			}
		}
	}
	ordered = append(ordered[:insertAt], append([]string{todoID}, ordered[insertAt:]...)...)

	for i, id := range ordered {
		newPos := i + 1
		if origPos[id] == newPos {
			continue
		}
		if id == todoID {
			if _, err := tx.ExecContext(ctx,
				`UPDATE todo_items SET position = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?`,
				newPos, id); err != nil {
				return fmt.Errorf("update position: %w", err)
			}
		} else {
			if _, err := tx.ExecContext(ctx,
				`UPDATE todo_items SET position = ? WHERE id = ?`, newPos, id); err != nil {
				return fmt.Errorf("update position: %w", err)
			}
		}
	}

	return tx.Commit()
}

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

func (s *SQLiteStore) RemoveExternalLink(ctx context.Context, todoID, linkID string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM todo_external_links WHERE id = ? AND todo_id = ?`, linkID, todoID)
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
