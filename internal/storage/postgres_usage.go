package storage

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
)

// migrateUsage creates the usage_events, outcome_ratings, and feed_activity tables
// (PostgreSQL). feed_activity is a separate table from the existing activity_log,
// which is an audit log for API-key/action events; feed_activity carries richer
// Phase 8 event_type + metadata payloads.
func (s *PostgresStore) migrateUsage(ctx context.Context) error {
	stmts := []struct {
		name string
		sql  string
	}{
		{"usage_events", `
			CREATE TABLE IF NOT EXISTS usage_events (
				id             TEXT PRIMARY KEY,
				entry_id       TEXT NOT NULL REFERENCES entries(id) ON DELETE CASCADE,
				user_id        TEXT NOT NULL DEFAULT '',
				tool           TEXT NOT NULL DEFAULT '',
				selected_index INT  NOT NULL DEFAULT 0,
				created_at     TIMESTAMPTZ NOT NULL DEFAULT NOW()
			)`},
		{"outcome_ratings", `
			CREATE TABLE IF NOT EXISTS outcome_ratings (
				id         TEXT PRIMARY KEY,
				entry_id   TEXT NOT NULL REFERENCES entries(id) ON DELETE CASCADE,
				user_id    TEXT NOT NULL DEFAULT '',
				rating     INT  NOT NULL CHECK(rating BETWEEN 1 AND 5),
				note       TEXT NOT NULL DEFAULT '',
				created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
			)`},
		{"feed_activity", `
			CREATE TABLE IF NOT EXISTS feed_activity (
				id         TEXT PRIMARY KEY,
				team_id    TEXT NOT NULL DEFAULT '',
				event_type TEXT NOT NULL DEFAULT '',
				actor_id   TEXT NOT NULL DEFAULT '',
				entry_id   TEXT NOT NULL DEFAULT '',
				metadata   JSONB NOT NULL DEFAULT '{}',
				created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
			)`},
		{"idx_usage_entry", `CREATE INDEX IF NOT EXISTS idx_usage_entry ON usage_events(entry_id)`},
		{"idx_usage_created", `CREATE INDEX IF NOT EXISTS idx_usage_created ON usage_events(created_at)`},
		{"idx_outcome_entry", `CREATE INDEX IF NOT EXISTS idx_outcome_entry ON outcome_ratings(entry_id)`},
		{"idx_feed_team_created", `CREATE INDEX IF NOT EXISTS idx_feed_team_created ON feed_activity(team_id, created_at DESC)`},
	}
	for _, st := range stmts {
		if _, err := s.db.ExecContext(ctx, st.sql); err != nil {
			return fmt.Errorf("migrate usage (%s): %w", st.name, err)
		}
	}
	return nil
}

// ── Phase 8: Usage tracking ───────────────────────────────────────────────────

func (s *PostgresStore) RecordUsage(ctx context.Context, event UsageEvent) error {
	id := event.ID
	if id == "" {
		id = uuid.NewString()
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO usage_events (id, entry_id, user_id, tool, selected_index)
		VALUES ($1, $2, $3, $4, $5)
	`, id, event.EntryID, event.UserID, event.Tool, event.SelectedIndex)
	if err != nil {
		return fmt.Errorf("record usage: %w", err)
	}
	return nil
}

func (s *PostgresStore) RecordOutcome(ctx context.Context, rating OutcomeRating) error {
	id := rating.ID
	if id == "" {
		id = uuid.NewString()
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO outcome_ratings (id, entry_id, user_id, rating, note)
		VALUES ($1, $2, $3, $4, $5)
	`, id, rating.EntryID, rating.UserID, rating.Rating, rating.Note)
	if err != nil {
		return fmt.Errorf("record outcome: %w", err)
	}
	return nil
}

func (s *PostgresStore) GetTrendingEntries(ctx context.Context, teamID string, days int, limit int) ([]TrendingEntry, error) {
	if limit <= 0 {
		limit = 10
	}
	// signal_score = (avg_outcome * 2 + usage_count * 0.5) * (1 + ln(1 + usage_count))
	const query = `
		SELECT
			e.id, e.type, e.title, e.content, e.description, e.domain, e.tags, e.auto_tags,
			e.author, e.team, e.created_at, e.updated_at, e.version,
			e.rating, e.usage_count, e.team_id, e.status,
			COALESCE(u.cnt7,  0)        AS usage_count_7d,
			COALESCE(u.cnt30, 0)        AS usage_count_30d,
			COALESCE(o.avg_rating, 0.0) AS avg_outcome,
			(
				(COALESCE(o.avg_rating, 0.0) * 2 + COALESCE(u.cnt30, 0) * 0.5)
				* (1 + ln(1 + COALESCE(u.cnt30, 0)))
			) AS signal_score
		FROM entries e
		LEFT JOIN (
			SELECT
				entry_id,
				COUNT(*) FILTER (WHERE created_at >= NOW() - INTERVAL '7 days')  AS cnt7,
				COUNT(*) FILTER (WHERE created_at >= NOW() - INTERVAL '1 day' * $1) AS cnt30
			FROM usage_events
			WHERE created_at >= NOW() - INTERVAL '1 day' * $1
			GROUP BY entry_id
		) u ON u.entry_id = e.id
		LEFT JOIN (
			SELECT entry_id, AVG(rating::FLOAT) AS avg_rating
			FROM outcome_ratings
			GROUP BY entry_id
		) o ON o.entry_id = e.id
		WHERE (u.entry_id IS NOT NULL OR o.entry_id IS NOT NULL)
		  AND ($2 = '' OR e.team_id = $2)
		ORDER BY signal_score DESC
		LIMIT $3
	`
	rows, err := s.db.QueryContext(ctx, query, days, teamID, limit)
	if err != nil {
		return nil, fmt.Errorf("get trending entries: %w", err)
	}
	defer rows.Close()

	var results []TrendingEntry
	for rows.Next() {
		var t TrendingEntry
		var tagsRaw []byte
		var autoTagsRaw []byte
		if err := rows.Scan(
			&t.ID, &t.Type, &t.Title, &t.Content, &t.Description, &t.Domain, &tagsRaw, &autoTagsRaw,
			&t.Author, &t.Team, &t.CreatedAt, &t.UpdatedAt, &t.Version,
			&t.Rating, &t.UsageCount, &t.TeamID, &t.Status,
			&t.UsageCount7d, &t.UsageCount30d, &t.AvgOutcome, &t.SignalScore,
		); err != nil {
			return nil, fmt.Errorf("scan trending entry: %w", err)
		}
		if err := json.Unmarshal(tagsRaw, &t.Tags); err != nil {
			t.Tags = []string{}
		}
		if err := json.Unmarshal(autoTagsRaw, &t.AutoTags); err != nil {
			t.AutoTags = []string{}
		}
		results = append(results, t)
	}
	return results, rows.Err()
}

func (s *PostgresStore) GetWeakSignalEntries(ctx context.Context, teamID string, minRatings int, maxAvgOutcome float64) ([]KnowledgeEntry, error) {
	const query = `
		SELECT
			e.id, e.type, e.title, e.content, e.description, e.domain, e.tags, e.auto_tags,
			e.author, e.team, e.created_at, e.updated_at, e.version,
			e.rating, e.usage_count, e.team_id, e.status
		FROM entries e
		INNER JOIN (
			SELECT entry_id, COUNT(*) AS cnt, AVG(rating::FLOAT) AS avg_rating
			FROM outcome_ratings
			GROUP BY entry_id
			HAVING COUNT(*) >= $1 AND AVG(rating::FLOAT) <= $2
		) o ON o.entry_id = e.id
		WHERE ($3 = '' OR e.team_id = $3)
		ORDER BY o.avg_rating ASC
	`
	rows, err := s.db.QueryContext(ctx, query, minRatings, maxAvgOutcome, teamID)
	if err != nil {
		return nil, fmt.Errorf("get weak signal entries: %w", err)
	}
	defer rows.Close()

	var entries []KnowledgeEntry
	for rows.Next() {
		entry, err := scanEntryPG(rows)
		if err != nil {
			return nil, fmt.Errorf("scan weak signal entry: %w", err)
		}
		entries = append(entries, *entry)
	}
	return entries, rows.Err()
}

// ── Phase 8: Activity feed ────────────────────────────────────────────────────

func (s *PostgresStore) RecordActivity(ctx context.Context, event ActivityEvent) error {
	id := event.ID
	if id == "" {
		id = uuid.NewString()
	}
	metaJSON, err := json.Marshal(event.Metadata)
	if err != nil {
		return fmt.Errorf("marshal metadata: %w", err)
	}
	_, err = s.db.ExecContext(ctx, `
		INSERT INTO feed_activity (id, team_id, event_type, actor_id, entry_id, metadata)
		VALUES ($1, '', $2, $3, $4, $5)
	`, id, event.EventType, event.ActorID, event.EntryID, string(metaJSON))
	if err != nil {
		return fmt.Errorf("record activity: %w", err)
	}
	return nil
}

func (s *PostgresStore) ListActivity(ctx context.Context, teamID string, limit int, offset int) ([]ActivityEvent, error) {
	if limit <= 0 {
		limit = 50
	}
	const query = `
		SELECT id, event_type, actor_id, entry_id, metadata, created_at
		FROM feed_activity
		WHERE ($1 = '' OR team_id = $1)
		ORDER BY created_at DESC
		LIMIT $2 OFFSET $3
	`
	rows, err := s.db.QueryContext(ctx, query, teamID, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("list activity: %w", err)
	}
	defer rows.Close()

	var events []ActivityEvent
	for rows.Next() {
		var ev ActivityEvent
		var metaRaw []byte
		if err := rows.Scan(&ev.ID, &ev.EventType, &ev.ActorID, &ev.EntryID, &metaRaw, &ev.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan activity event: %w", err)
		}
		if err := json.Unmarshal(metaRaw, &ev.Metadata); err != nil {
			ev.Metadata = map[string]string{}
		}
		events = append(events, ev)
	}
	return events, rows.Err()
}
