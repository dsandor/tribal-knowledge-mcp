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

var _ FTSessionStore = (*SQLiteStore)(nil)

func (s *SQLiteStore) migrateFTSessions() error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS ft_sessions (
			id                TEXT PRIMARY KEY,
			team_id           TEXT NOT NULL DEFAULT '',
			user_id           TEXT NOT NULL DEFAULT '',
			client            TEXT NOT NULL DEFAULT '',
			project           TEXT NOT NULL DEFAULT '',
			task_summary      TEXT NOT NULL DEFAULT '',
			domain            TEXT NOT NULL DEFAULT '',
			status            TEXT NOT NULL DEFAULT 'open',
			outcome_rating    INTEGER,
			outcome_note      TEXT NOT NULL DEFAULT '',
			train_eligible    INTEGER NOT NULL DEFAULT 1,
			redaction_status  TEXT NOT NULL DEFAULT 'raw',
			metadata_json     TEXT NOT NULL DEFAULT '',
			started_at        DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			completed_at      DATETIME,
			created_at        DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at        DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE INDEX IF NOT EXISTS idx_ft_sessions_team_time ON ft_sessions(team_id, started_at)`,
		`CREATE INDEX IF NOT EXISTS idx_ft_sessions_train ON ft_sessions(train_eligible, redaction_status, status)`,
		`CREATE TABLE IF NOT EXISTS ft_turns (
			id              TEXT PRIMARY KEY,
			session_id      TEXT NOT NULL REFERENCES ft_sessions(id) ON DELETE CASCADE,
			seq             INTEGER NOT NULL,
			role            TEXT NOT NULL,
			kind            TEXT NOT NULL,
			content         TEXT NOT NULL,
			content_hash    TEXT NOT NULL DEFAULT '',
			model           TEXT NOT NULL DEFAULT '',
			token_estimate  INTEGER NOT NULL DEFAULT 0,
			entry_ids_json  TEXT NOT NULL DEFAULT '[]',
			rule_ids_json   TEXT NOT NULL DEFAULT '[]',
			agent_id        TEXT NOT NULL DEFAULT '',
			tool_name       TEXT NOT NULL DEFAULT '',
			parent_turn_id  TEXT NOT NULL DEFAULT '',
			created_at      DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			UNIQUE(session_id, seq)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_ft_turns_session ON ft_turns(session_id, seq)`,
		`CREATE TABLE IF NOT EXISTS ft_preferences (
			id              TEXT PRIMARY KEY,
			session_id      TEXT NOT NULL REFERENCES ft_sessions(id) ON DELETE CASCADE,
			turn_id         TEXT NOT NULL DEFAULT '',
			prompt_turn_id  TEXT NOT NULL DEFAULT '',
			chosen_text     TEXT NOT NULL,
			rejected_text   TEXT NOT NULL DEFAULT '',
			source          TEXT NOT NULL,
			rating          INTEGER,
			entry_id        TEXT NOT NULL DEFAULT '',
			user_id         TEXT NOT NULL DEFAULT '',
			created_at      DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE INDEX IF NOT EXISTS idx_ft_prefs_session ON ft_preferences(session_id)`,
		`CREATE TABLE IF NOT EXISTS ft_session_knowledge (
			session_id  TEXT NOT NULL REFERENCES ft_sessions(id) ON DELETE CASCADE,
			entry_id    TEXT NOT NULL,
			role        TEXT NOT NULL,
			PRIMARY KEY (session_id, entry_id, role)
		)`,
	}
	for _, stmt := range stmts {
		if _, err := s.db.Exec(stmt); err != nil {
			snippet := stmt
			if len(snippet) > 60 {
				snippet = snippet[:60]
			}
			return fmt.Errorf("ft sessions migrate: %w: sql=%s", err, snippet)
		}
	}
	return nil
}

func (s *SQLiteStore) CreateFTSession(ctx context.Context, sess FTSession) (string, error) {
	if sess.ID == "" {
		sess.ID = uuid.NewString()
	}
	if sess.Status == "" {
		sess.Status = FTSessionOpen
	}
	if sess.RedactionStatus == "" {
		sess.RedactionStatus = "raw"
	}
	train := 0
	if sess.TrainEligible {
		train = 1
	}
	now := time.Now().UTC()
	if sess.StartedAt.IsZero() {
		sess.StartedAt = now
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO ft_sessions (
			id, team_id, user_id, client, project, task_summary, domain, status,
			outcome_rating, outcome_note, train_eligible, redaction_status, metadata_json,
			started_at, completed_at, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, sess.ID, sess.TeamID, sess.UserID, sess.Client, sess.Project, sess.TaskSummary, sess.Domain, sess.Status,
		nullableInt(sess.OutcomeRating), sess.OutcomeNote, train, sess.RedactionStatus, sess.MetadataJSON,
		sess.StartedAt.UTC().Format(time.RFC3339Nano), fmtSQLiteTime(sess.CompletedAt),
		now.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano))
	if err != nil {
		return "", fmt.Errorf("create ft session: %w", err)
	}
	return sess.ID, nil
}

func (s *SQLiteStore) GetFTSession(ctx context.Context, id string) (*FTSession, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, team_id, user_id, client, project, task_summary, domain, status,
			outcome_rating, outcome_note, train_eligible, redaction_status, metadata_json,
			started_at, completed_at, created_at, updated_at
		FROM ft_sessions WHERE id = ?`, id)
	sess, err := scanFTSession(row.Scan)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("get ft session: %w", err)
	}
	return sess, nil
}

func (s *SQLiteStore) ListFTSessions(ctx context.Context, filter FTSessionFilter) ([]FTSession, error) {
	q := `SELECT id, team_id, user_id, client, project, task_summary, domain, status,
		outcome_rating, outcome_note, train_eligible, redaction_status, metadata_json,
		started_at, completed_at, created_at, updated_at
		FROM ft_sessions WHERE 1=1`
	var args []any
	if filter.TeamID != "" {
		q += ` AND team_id = ?`
		args = append(args, filter.TeamID)
	}
	if filter.UserID != "" {
		q += ` AND user_id = ?`
		args = append(args, filter.UserID)
	}
	if filter.Status != "" {
		q += ` AND status = ?`
		args = append(args, filter.Status)
	}
	if filter.Domain != "" {
		q += ` AND domain = ?`
		args = append(args, filter.Domain)
	}
	if filter.TrainEligibleOnly {
		q += ` AND train_eligible = 1 AND redaction_status != 'blocked'`
	}
	if filter.MinOutcomeRating > 0 {
		q += ` AND outcome_rating IS NOT NULL AND outcome_rating >= ?`
		args = append(args, filter.MinOutcomeRating)
	}
	if filter.Since != nil {
		q += ` AND started_at >= ?`
		args = append(args, filter.Since.UTC().Format(time.RFC3339Nano))
	}
	if filter.Until != nil {
		q += ` AND started_at <= ?`
		args = append(args, filter.Until.UTC().Format(time.RFC3339Nano))
	}
	q += ` ORDER BY started_at DESC`
	limit := filter.Limit
	if limit <= 0 {
		limit = 100
	}
	q += ` LIMIT ? OFFSET ?`
	args = append(args, limit, filter.Offset)

	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("list ft sessions: %w", err)
	}
	defer rows.Close()
	out := []FTSession{}
	for rows.Next() {
		sess, err := scanFTSession(rows.Scan)
		if err != nil {
			return nil, fmt.Errorf("scan ft session: %w", err)
		}
		out = append(out, *sess)
	}
	return out, rows.Err()
}

func (s *SQLiteStore) UpdateFTSession(ctx context.Context, sess FTSession) error {
	train := 0
	if sess.TrainEligible {
		train = 1
	}
	res, err := s.db.ExecContext(ctx, `
		UPDATE ft_sessions SET
			team_id=?, user_id=?, client=?, project=?, task_summary=?, domain=?, status=?,
			outcome_rating=?, outcome_note=?, train_eligible=?, redaction_status=?, metadata_json=?,
			completed_at=?, updated_at=?
		WHERE id=?`,
		sess.TeamID, sess.UserID, sess.Client, sess.Project, sess.TaskSummary, sess.Domain, sess.Status,
		nullableInt(sess.OutcomeRating), sess.OutcomeNote, train, sess.RedactionStatus, sess.MetadataJSON,
		fmtSQLiteTime(sess.CompletedAt), time.Now().UTC().Format(time.RFC3339Nano), sess.ID)
	if err != nil {
		return fmt.Errorf("update ft session: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *SQLiteStore) CompleteFTSession(ctx context.Context, id string, outcomeRating *int, outcomeNote string, status string) error {
	if status == "" {
		status = FTSessionCompleted
	}
	if !ValidFTSessionStatus(status) || status == FTSessionOpen {
		return fmt.Errorf("%w: status must be completed or abandoned", ErrInvalid)
	}
	now := time.Now().UTC()
	res, err := s.db.ExecContext(ctx, `
		UPDATE ft_sessions SET
			status=?, outcome_rating=?, outcome_note=?, completed_at=?, updated_at=?
		WHERE id=?`,
		status, nullableInt(outcomeRating), outcomeNote, now.Format(time.RFC3339Nano),
		now.Format(time.RFC3339Nano), id)
	if err != nil {
		return fmt.Errorf("complete ft session: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *SQLiteStore) AddFTTurn(ctx context.Context, t FTTurn) (string, error) {
	if t.ID == "" {
		t.ID = uuid.NewString()
	}
	if t.Seq < 0 {
		seq, err := s.NextFTTurnSeq(ctx, t.SessionID)
		if err != nil {
			return "", err
		}
		t.Seq = seq
	}
	if t.ContentHash == "" && t.Content != "" {
		t.ContentHash = sha256Hex(t.Content)
	}
	entryJSON, err := json.Marshal(nonNilStrings(t.EntryIDs))
	if err != nil {
		return "", fmt.Errorf("marshal entry_ids: %w", err)
	}
	ruleJSON, err := json.Marshal(nonNilStrings(t.RuleIDs))
	if err != nil {
		return "", fmt.Errorf("marshal rule_ids: %w", err)
	}
	_, err = s.db.ExecContext(ctx, `
		INSERT INTO ft_turns (
			id, session_id, seq, role, kind, content, content_hash, model, token_estimate,
			entry_ids_json, rule_ids_json, agent_id, tool_name, parent_turn_id, created_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, t.ID, t.SessionID, t.Seq, t.Role, t.Kind, t.Content, t.ContentHash, t.Model, t.TokenEstimate,
		string(entryJSON), string(ruleJSON), t.AgentID, t.ToolName, t.ParentTurnID,
		time.Now().UTC().Format(time.RFC3339Nano))
	if err != nil {
		return "", fmt.Errorf("add ft turn: %w", err)
	}
	// Best-effort knowledge links from entry IDs.
	for _, eid := range t.EntryIDs {
		if eid == "" {
			continue
		}
		_ = s.LinkFTSessionKnowledge(ctx, t.SessionID, eid, FTKnowRetrieved)
	}
	return t.ID, nil
}

func (s *SQLiteStore) ListFTTurns(ctx context.Context, sessionID string) ([]FTTurn, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, session_id, seq, role, kind, content, content_hash, model, token_estimate,
			entry_ids_json, rule_ids_json, agent_id, tool_name, parent_turn_id, created_at
		FROM ft_turns WHERE session_id = ? ORDER BY seq ASC`, sessionID)
	if err != nil {
		return nil, fmt.Errorf("list ft turns: %w", err)
	}
	defer rows.Close()
	out := []FTTurn{}
	for rows.Next() {
		t, err := scanFTTurn(rows.Scan)
		if err != nil {
			return nil, fmt.Errorf("scan ft turn: %w", err)
		}
		out = append(out, *t)
	}
	return out, rows.Err()
}

func (s *SQLiteStore) NextFTTurnSeq(ctx context.Context, sessionID string) (int, error) {
	var max sql.NullInt64
	err := s.db.QueryRowContext(ctx,
		`SELECT MAX(seq) FROM ft_turns WHERE session_id = ?`, sessionID).Scan(&max)
	if err != nil {
		return 0, fmt.Errorf("next ft turn seq: %w", err)
	}
	if !max.Valid {
		return 0, nil
	}
	return int(max.Int64) + 1, nil
}

func (s *SQLiteStore) AddFTPreference(ctx context.Context, p FTPreference) (string, error) {
	if p.ID == "" {
		p.ID = uuid.NewString()
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO ft_preferences (
			id, session_id, turn_id, prompt_turn_id, chosen_text, rejected_text,
			source, rating, entry_id, user_id, created_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, p.ID, p.SessionID, p.TurnID, p.PromptTurnID, p.ChosenText, p.RejectedText,
		p.Source, nullableInt(p.Rating), p.EntryID, p.UserID,
		time.Now().UTC().Format(time.RFC3339Nano))
	if err != nil {
		return "", fmt.Errorf("add ft preference: %w", err)
	}
	return p.ID, nil
}

func (s *SQLiteStore) ListFTPreferences(ctx context.Context, sessionID string) ([]FTPreference, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, session_id, turn_id, prompt_turn_id, chosen_text, rejected_text,
			source, rating, entry_id, user_id, created_at
		FROM ft_preferences WHERE session_id = ? ORDER BY created_at ASC`, sessionID)
	if err != nil {
		return nil, fmt.Errorf("list ft preferences: %w", err)
	}
	defer rows.Close()
	return scanFTPreferences(rows)
}

func (s *SQLiteStore) ListFTPreferencesExport(ctx context.Context, filter FTSessionFilter) ([]FTPreference, error) {
	q := `SELECT p.id, p.session_id, p.turn_id, p.prompt_turn_id, p.chosen_text, p.rejected_text,
		p.source, p.rating, p.entry_id, p.user_id, p.created_at
		FROM ft_preferences p
		INNER JOIN ft_sessions s ON s.id = p.session_id
		WHERE 1=1`
	var args []any
	if filter.TeamID != "" {
		q += ` AND s.team_id = ?`
		args = append(args, filter.TeamID)
	}
	if filter.TrainEligibleOnly {
		q += ` AND s.train_eligible = 1 AND s.redaction_status != 'blocked'`
	}
	if filter.MinOutcomeRating > 0 {
		q += ` AND s.outcome_rating IS NOT NULL AND s.outcome_rating >= ?`
		args = append(args, filter.MinOutcomeRating)
	}
	if filter.Since != nil {
		q += ` AND s.started_at >= ?`
		args = append(args, filter.Since.UTC().Format(time.RFC3339Nano))
	}
	if filter.Until != nil {
		q += ` AND s.started_at <= ?`
		args = append(args, filter.Until.UTC().Format(time.RFC3339Nano))
	}
	q += ` ORDER BY p.created_at ASC`
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("list ft preferences export: %w", err)
	}
	defer rows.Close()
	return scanFTPreferences(rows)
}

func (s *SQLiteStore) LinkFTSessionKnowledge(ctx context.Context, sessionID, entryID, role string) error {
	if sessionID == "" || entryID == "" || role == "" {
		return fmt.Errorf("%w: session_id, entry_id, and role required", ErrInvalid)
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT OR IGNORE INTO ft_session_knowledge (session_id, entry_id, role)
		VALUES (?, ?, ?)`, sessionID, entryID, role)
	if err != nil {
		return fmt.Errorf("link ft session knowledge: %w", err)
	}
	return nil
}

func (s *SQLiteStore) ListFTSessionKnowledge(ctx context.Context, sessionID string) ([]FTSessionKnowledge, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT session_id, entry_id, role FROM ft_session_knowledge
		WHERE session_id = ? ORDER BY entry_id, role`, sessionID)
	if err != nil {
		return nil, fmt.Errorf("list ft session knowledge: %w", err)
	}
	defer rows.Close()
	out := []FTSessionKnowledge{}
	for rows.Next() {
		var k FTSessionKnowledge
		if err := rows.Scan(&k.SessionID, &k.EntryID, &k.Role); err != nil {
			return nil, fmt.Errorf("scan ft session knowledge: %w", err)
		}
		out = append(out, k)
	}
	return out, rows.Err()
}

// --- scanners / helpers ---

func scanFTSession(scan func(...any) error) (*FTSession, error) {
	var sess FTSession
	var outcome sql.NullInt64
	var completed sql.NullString
	var train int
	var started, created, updated string
	err := scan(
		&sess.ID, &sess.TeamID, &sess.UserID, &sess.Client, &sess.Project, &sess.TaskSummary,
		&sess.Domain, &sess.Status, &outcome, &sess.OutcomeNote, &train, &sess.RedactionStatus,
		&sess.MetadataJSON, &started, &completed, &created, &updated,
	)
	if err != nil {
		return nil, err
	}
	sess.TrainEligible = train != 0
	if outcome.Valid {
		v := int(outcome.Int64)
		sess.OutcomeRating = &v
	}
	sess.StartedAt = parseTimestamp(started)
	sess.CreatedAt = parseTimestamp(created)
	sess.UpdatedAt = parseTimestamp(updated)
	if completed.Valid && completed.String != "" {
		t := parseTimestamp(completed.String)
		sess.CompletedAt = &t
	}
	return &sess, nil
}

func scanFTTurn(scan func(...any) error) (*FTTurn, error) {
	var t FTTurn
	var entryJSON, ruleJSON, created string
	err := scan(
		&t.ID, &t.SessionID, &t.Seq, &t.Role, &t.Kind, &t.Content, &t.ContentHash,
		&t.Model, &t.TokenEstimate, &entryJSON, &ruleJSON, &t.AgentID, &t.ToolName,
		&t.ParentTurnID, &created,
	)
	if err != nil {
		return nil, err
	}
	t.EntryIDs = decodeStringSlice(entryJSON)
	t.RuleIDs = decodeStringSlice(ruleJSON)
	t.CreatedAt = parseTimestamp(created)
	return &t, nil
}

func scanFTPreferences(rows *sql.Rows) ([]FTPreference, error) {
	out := []FTPreference{}
	for rows.Next() {
		var p FTPreference
		var rating sql.NullInt64
		var created string
		if err := rows.Scan(
			&p.ID, &p.SessionID, &p.TurnID, &p.PromptTurnID, &p.ChosenText, &p.RejectedText,
			&p.Source, &rating, &p.EntryID, &p.UserID, &created,
		); err != nil {
			return nil, fmt.Errorf("scan ft preference: %w", err)
		}
		if rating.Valid {
			v := int(rating.Int64)
			p.Rating = &v
		}
		p.CreatedAt = parseTimestamp(created)
		out = append(out, p)
	}
	return out, rows.Err()
}

func nullableInt(p *int) any {
	if p == nil {
		return nil
	}
	return *p
}

func nonNilStrings(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

func decodeStringSlice(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return []string{}
	}
	var out []string
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return []string{}
	}
	if out == nil {
		return []string{}
	}
	return out
}
