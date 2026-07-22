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

func (s *PostgresStore) CreateTodoList(ctx context.Context, list TodoList) (string, error) {
	list.ID = uuid.NewString()
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO todo_lists (id, team_id, name, description, domain, color, archived, created_by)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
	`, list.ID, list.TeamID, list.Name, list.Description, list.Domain, list.Color, list.Archived, list.CreatedBy)
	if err != nil {
		return "", fmt.Errorf("insert todo list: %w", err)
	}
	return list.ID, nil
}

// scanTodoListRowPG scans a todo_lists row from PostgreSQL. BOOLEAN scans directly
// into bool and TIMESTAMPTZ scans directly into time.Time (no string parsing needed).
func scanTodoListRowPG(scan func(...any) error) (*TodoList, error) {
	var l TodoList
	var createdAt, updatedAt time.Time
	err := scan(&l.ID, &l.TeamID, &l.Name, &l.Description, &l.Domain, &l.Color,
		&l.Archived, &l.CreatedBy, &createdAt, &updatedAt, &l.TotalCount, &l.OpenCount)
	if err != nil {
		return nil, err
	}
	l.CreatedAt = createdAt
	l.UpdatedAt = updatedAt
	return &l, nil
}

func (s *PostgresStore) GetTodoList(ctx context.Context, id string) (*TodoList, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT `+todoListCols+` FROM todo_lists l `+todoListCountJoin+` WHERE l.id = $1`, id)
	l, err := scanTodoListRowPG(row.Scan)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("get todo list: %w", err)
	}
	return l, nil
}

func (s *PostgresStore) ListTodoLists(ctx context.Context, teamID string, includeArchived bool) ([]TodoList, error) {
	query := `SELECT ` + todoListCols + ` FROM todo_lists l ` + todoListCountJoin + ` WHERE l.team_id = $1`
	if !includeArchived {
		query += ` AND l.archived = FALSE`
	}
	query += ` ORDER BY l.created_at ASC`
	rows, err := s.db.QueryContext(ctx, query, teamID)
	if err != nil {
		return nil, fmt.Errorf("list todo lists: %w", err)
	}
	defer rows.Close()
	lists := []TodoList{}
	for rows.Next() {
		l, err := scanTodoListRowPG(rows.Scan)
		if err != nil {
			return nil, fmt.Errorf("scan todo list: %w", err)
		}
		lists = append(lists, *l)
	}
	return lists, rows.Err()
}

func (s *PostgresStore) UpdateTodoList(ctx context.Context, list TodoList) error {
	res, err := s.db.ExecContext(ctx, `
		UPDATE todo_lists
		SET name = $1, description = $2, domain = $3, color = $4, archived = $5,
		    updated_at = NOW()
		WHERE id = $6
	`, list.Name, list.Description, list.Domain, list.Color, list.Archived, list.ID)
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

func (s *PostgresStore) DeleteTodoList(ctx context.Context, id string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM todo_lists WHERE id = $1`, id)
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

// scanTodoItemRowPG scans a todo_items row from PostgreSQL. Nullable timestamp
// columns scan into sql.NullTime; NOT NULL timestamp columns scan directly into
// time.Time (no string parsing needed, unlike the SQLite variant).
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
	// Placeholders: $1=id $2=list_id $3=team_id $4=title $5=notes $6=status
	// $7=priority $8=created_by $9=assignee $10=due_date $11=tags. The
	// completed_at CASE reuses $6 (status) rather than binding it a second
	// time — Postgres allows repeated placeholder numbers.
	_, err = s.db.ExecContext(ctx, `
		INSERT INTO todo_items (id, list_id, team_id, title, notes, status, priority,
			created_by, assignee, due_date, position, tags, completed_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10,
			(SELECT COALESCE(MAX(position), 0) + 1 FROM todo_items WHERE list_id = $2), $11,
			CASE WHEN $6 = 'done' THEN NOW() ELSE NULL END)
	`, item.ID, item.ListID, item.TeamID, item.Title, item.Notes, string(item.Status),
		string(item.Priority), item.CreatedBy, item.Assignee, item.DueDate, string(tagsJSON))
	if err != nil {
		return "", fmt.Errorf("insert todo: %w", err)
	}
	return item.ID, nil
}

func (s *PostgresStore) GetTodo(ctx context.Context, id string) (*TodoItem, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT `+todoItemCols+` FROM todo_items WHERE id = $1`, id)
	it, err := scanTodoItemRowPG(row.Scan)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("get todo: %w", err)
	}

	// External links
	lrows, err := s.db.QueryContext(ctx, `
		SELECT id, todo_id, provider, external_id, url, external_status, synced_at, created_at
		FROM todo_external_links WHERE todo_id = $1 ORDER BY created_at ASC`, id)
	if err != nil {
		return nil, fmt.Errorf("get todo links: %w", err)
	}
	defer lrows.Close()
	for lrows.Next() {
		var l ExternalLink
		var synced sql.NullTime
		var createdAt time.Time
		if err := lrows.Scan(&l.ID, &l.TodoID, &l.Provider, &l.ExternalID, &l.URL,
			&l.ExternalStatus, &synced, &createdAt); err != nil {
			return nil, fmt.Errorf("scan link: %w", err)
		}
		if synced.Valid {
			t := synced.Time
			l.SyncedAt = &t
		}
		l.CreatedAt = createdAt
		it.ExternalLinks = append(it.ExternalLinks, l)
	}
	if err := lrows.Err(); err != nil {
		return nil, err
	}

	// Knowledge refs
	krows, err := s.db.QueryContext(ctx,
		`SELECT entry_id FROM todo_knowledge_refs WHERE todo_id = $1 ORDER BY entry_id`, id)
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

func (s *PostgresStore) QueryTodos(ctx context.Context, filter TodoFilter) ([]TodoItem, error) {
	limit := filter.Limit
	if limit == 0 {
		limit = 50
	}

	args := []any{}
	n := 0
	nextArg := func(v any) string {
		n++
		args = append(args, v)
		return fmt.Sprintf("$%d", n)
	}

	query := `SELECT ` + todoItemCols + ` FROM todo_items WHERE 1=1`
	if filter.TeamID != "" {
		query += ` AND team_id = ` + nextArg(filter.TeamID)
	}
	if filter.ListID != "" {
		query += ` AND list_id = ` + nextArg(filter.ListID)
	}
	if len(filter.Status) > 0 {
		placeholders := make([]string, len(filter.Status))
		for i, st := range filter.Status {
			placeholders[i] = nextArg(st)
		}
		query += ` AND status IN (` + strings.Join(placeholders, ",") + `)`
	}
	if filter.Assignee != "" {
		query += ` AND assignee = ` + nextArg(filter.Assignee)
	}
	if len(filter.Priority) > 0 {
		placeholders := make([]string, len(filter.Priority))
		for i, p := range filter.Priority {
			placeholders[i] = nextArg(p)
		}
		query += ` AND priority IN (` + strings.Join(placeholders, ",") + `)`
	}
	if filter.Tag != "" {
		// tags stored as JSON array text: match the quoted element.
		query += ` AND tags LIKE ` + nextArg(`%"`+filter.Tag+`"%`)
	}
	if filter.DueBefore != nil {
		query += ` AND due_date IS NOT NULL AND due_date < ` + nextArg(*filter.DueBefore)
	}
	if filter.Query != "" {
		q := "%" + filter.Query + "%"
		p1 := nextArg(q)
		p2 := nextArg(q)
		query += ` AND (title LIKE ` + p1 + ` OR notes LIKE ` + p2 + `)`
	}
	query += ` ORDER BY position ASC, created_at ASC`
	if limit > 0 {
		query += ` LIMIT ` + nextArg(limit)
	}

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query todos: %w", err)
	}
	defer rows.Close()
	items := []TodoItem{}
	for rows.Next() {
		it, err := scanTodoItemRowPG(rows.Scan)
		if err != nil {
			return nil, fmt.Errorf("scan todo: %w", err)
		}
		items = append(items, *it)
	}
	return items, rows.Err()
}

func (s *PostgresStore) UpdateTodo(ctx context.Context, item TodoItem) error {
	if item.Tags == nil {
		item.Tags = []string{}
	}
	tagsJSON, err := json.Marshal(item.Tags)
	if err != nil {
		return fmt.Errorf("marshal tags: %w", err)
	}
	// Placeholders: $1=list_id $2=title $3=notes $4=status $5=priority $6=assignee
	// $7=due_date $8=position $9=tags $10=id. The CASE reuses $4 (status) rather
	// than binding it a second time — Postgres allows repeated placeholder numbers.
	res, err := s.db.ExecContext(ctx, `
		UPDATE todo_items
		SET list_id = $1, title = $2, notes = $3, status = $4, priority = $5, assignee = $6,
		    due_date = $7, position = $8, tags = $9,
		    completed_at = CASE
		        WHEN $4 = 'done' THEN COALESCE(completed_at, NOW())
		        ELSE NULL END,
		    updated_at = NOW()
		WHERE id = $10
	`, item.ListID, item.Title, item.Notes, string(item.Status), string(item.Priority),
		item.Assignee, item.DueDate, item.Position, string(tagsJSON), item.ID)
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

func (s *PostgresStore) CompleteTodo(ctx context.Context, id string) error {
	res, err := s.db.ExecContext(ctx, `
		UPDATE todo_items
		SET status = 'done', completed_at = NOW(), updated_at = NOW()
		WHERE id = $1
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

func (s *PostgresStore) DeleteTodo(ctx context.Context, id string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM todo_items WHERE id = $1`, id)
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
func (s *PostgresStore) ReorderTodo(ctx context.Context, todoID, afterID string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	var listID string
	if err := tx.QueryRowContext(ctx, `SELECT list_id FROM todo_items WHERE id = $1`, todoID).Scan(&listID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		return fmt.Errorf("lookup todo: %w", err)
	}
	if afterID != "" {
		var afterListID string
		if err := tx.QueryRowContext(ctx, `SELECT list_id FROM todo_items WHERE id = $1`, afterID).Scan(&afterListID); err != nil {
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
		`SELECT id, position FROM todo_items WHERE list_id = $1 ORDER BY position ASC, created_at ASC`, listID)
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
				`UPDATE todo_items SET position = $1, updated_at = NOW() WHERE id = $2`,
				newPos, id); err != nil {
				return fmt.Errorf("update position: %w", err)
			}
		} else {
			if _, err := tx.ExecContext(ctx,
				`UPDATE todo_items SET position = $1 WHERE id = $2`, newPos, id); err != nil {
				return fmt.Errorf("update position: %w", err)
			}
		}
	}

	return tx.Commit()
}

func (s *PostgresStore) AddExternalLink(ctx context.Context, link ExternalLink) (string, error) {
	link.ID = uuid.NewString()
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO todo_external_links (id, todo_id, provider, external_id, url, external_status, synced_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
	`, link.ID, link.TodoID, link.Provider, link.ExternalID, link.URL,
		link.ExternalStatus, link.SyncedAt)
	if err != nil {
		return "", fmt.Errorf("insert external link: %w", err)
	}
	return link.ID, nil
}

func (s *PostgresStore) RemoveExternalLink(ctx context.Context, todoID, linkID string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM todo_external_links WHERE id = $1 AND todo_id = $2`, linkID, todoID)
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

func (s *PostgresStore) SetKnowledgeRefs(ctx context.Context, todoID string, entryIDs []string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `DELETE FROM todo_knowledge_refs WHERE todo_id = $1`, todoID); err != nil {
		return fmt.Errorf("clear refs: %w", err)
	}
	for _, eid := range entryIDs {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO todo_knowledge_refs (todo_id, entry_id) VALUES ($1, $2) ON CONFLICT (todo_id, entry_id) DO NOTHING`,
			todoID, eid); err != nil {
			return fmt.Errorf("insert ref: %w", err)
		}
	}
	return tx.Commit()
}

func (s *PostgresStore) ListTodosForEntry(ctx context.Context, entryID string) ([]TodoItem, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT `+todoItemCols+` FROM todo_items
		WHERE id IN (SELECT todo_id FROM todo_knowledge_refs WHERE entry_id = $1)
		ORDER BY created_at ASC`, entryID)
	if err != nil {
		return nil, fmt.Errorf("list todos for entry: %w", err)
	}
	defer rows.Close()
	items := []TodoItem{}
	for rows.Next() {
		it, err := scanTodoItemRowPG(rows.Scan)
		if err != nil {
			return nil, fmt.Errorf("scan todo: %w", err)
		}
		items = append(items, *it)
	}
	return items, rows.Err()
}

var _ TodoStore = (*PostgresStore)(nil)
