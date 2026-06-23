package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"time"

	"github.com/google/uuid"
)

// Compile-time check that *SQLiteStore implements TeamStore.
var _ TeamStore = (*SQLiteStore)(nil)

// ── Teams ────────────────────────────────────────────────────────────────────

func (s *SQLiteStore) CreateTeam(ctx context.Context, t Team) (string, error) {
	id := t.ID
	if id == "" {
		id = uuid.NewString()
	}
	patternsJSON, err := json.Marshal(t.DomainPatterns)
	if err != nil {
		return "", fmt.Errorf("marshal domain_patterns: %w", err)
	}
	_, err = s.db.ExecContext(ctx, `
		INSERT INTO teams (id, name, domain_patterns, enabled)
		VALUES (?, ?, ?, ?)
	`, id, t.Name, string(patternsJSON), boolToInt(t.Enabled))
	if err != nil {
		return "", fmt.Errorf("insert team: %w", err)
	}
	return id, nil
}

func (s *SQLiteStore) GetTeam(ctx context.Context, id string) (*Team, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, name, domain_patterns, enabled, created_at
		FROM teams WHERE id = ?
	`, id)
	team, err := scanTeam(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("team %q: %w", id, ErrNotFound)
		}
		return nil, fmt.Errorf("get team: %w", err)
	}
	return team, nil
}

func (s *SQLiteStore) ListTeams(ctx context.Context) ([]Team, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, name, domain_patterns, enabled, created_at
		FROM teams ORDER BY name
	`)
	if err != nil {
		return nil, fmt.Errorf("list teams: %w", err)
	}
	defer rows.Close()

	var teams []Team
	for rows.Next() {
		team, err := scanTeam(rows)
		if err != nil {
			return nil, fmt.Errorf("scan team: %w", err)
		}
		teams = append(teams, *team)
	}
	return teams, rows.Err()
}

func (s *SQLiteStore) UpdateTeam(ctx context.Context, id, name string, domainPatterns []string) error {
	patternsJSON, err := json.Marshal(domainPatterns)
	if err != nil {
		return fmt.Errorf("marshal domain_patterns: %w", err)
	}
	res, err := s.db.ExecContext(ctx,
		"UPDATE teams SET name = ?, domain_patterns = ? WHERE id = ?",
		name, string(patternsJSON), id,
	)
	if err != nil {
		return fmt.Errorf("update team: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("team %q: %w", id, ErrNotFound)
	}
	return nil
}

func (s *SQLiteStore) SetTeamEnabled(ctx context.Context, id string, enabled bool) error {
	res, err := s.db.ExecContext(ctx,
		"UPDATE teams SET enabled = ? WHERE id = ?",
		boolToInt(enabled), id,
	)
	if err != nil {
		return fmt.Errorf("set team enabled: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("team %q: %w", id, ErrNotFound)
	}
	return nil
}

func (s *SQLiteStore) DeleteTeam(ctx context.Context, id string) error {
	// Delete the team_settings row first (no FK, just cleanup).
	if _, err := s.db.ExecContext(ctx, "DELETE FROM team_settings WHERE team_id = ?", id); err != nil {
		return fmt.Errorf("delete team_settings: %w", err)
	}
	res, err := s.db.ExecContext(ctx, "DELETE FROM teams WHERE id = ?", id)
	if err != nil {
		return fmt.Errorf("delete team: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("team %q: %w", id, ErrNotFound)
	}
	return nil
}

// TeamDataCounts returns per-category record counts for the given team.
func (s *SQLiteStore) TeamDataCounts(ctx context.Context, id string) (TeamDataCounts, error) {
	var c TeamDataCounts
	queries := []struct {
		dest *int
		sql  string
	}{
		{&c.Users, "SELECT COUNT(*) FROM users WHERE team_id = ?"},
		{&c.APIKeys, "SELECT COUNT(*) FROM api_keys WHERE team_id = ?"},
		{&c.Entries, "SELECT COUNT(*) FROM entries WHERE team_id = ?"},
		{&c.Clusters, "SELECT COUNT(*) FROM clusters WHERE team_id = ?"},
		{&c.Agents, "SELECT COUNT(*) FROM agents WHERE team_id = ?"},
		{&c.Rules, "SELECT COUNT(*) FROM rules WHERE team_id = ?"},
	}
	for _, q := range queries {
		if err := s.db.QueryRowContext(ctx, q.sql, id).Scan(q.dest); err != nil {
			return TeamDataCounts{}, fmt.Errorf("team data counts: %w", err)
		}
	}
	return c, nil
}

// DeleteTeamMigrate transactionally migrates all data from team id to targetID,
// handles agent domain conflicts (source's conflicting agents are deleted), deletes the
// source team_settings row, and finally deletes the source team row.
func (s *SQLiteStore) DeleteTeamMigrate(ctx context.Context, id, targetID string) (TeamMigrationSummary, error) {
	if id == targetID {
		return TeamMigrationSummary{}, fmt.Errorf("source and target team must be different")
	}

	// Validate target exists.
	var exists int
	if err := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM teams WHERE id = ?", targetID).Scan(&exists); err != nil {
		return TeamMigrationSummary{}, fmt.Errorf("check target team: %w", err)
	}
	if exists == 0 {
		return TeamMigrationSummary{}, fmt.Errorf("target team %q: %w", targetID, ErrBadTarget)
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return TeamMigrationSummary{}, fmt.Errorf("begin transaction: %w", err)
	}
	defer tx.Rollback()

	var sum TeamMigrationSummary

	// ── Agent conflict resolution ─────────────────────────────────────────────
	// Delete agent_versions for conflicting agents (source agents whose domain
	// already exists in target).
	_, err = tx.ExecContext(ctx, `
		DELETE FROM agent_versions WHERE agent_id IN (
			SELECT a.id FROM agents a
			WHERE a.team_id = ?
			  AND EXISTS (
			    SELECT 1 FROM agents b
			    WHERE b.team_id = ? AND b.domain = a.domain
			  )
		)
	`, id, targetID)
	if err != nil {
		return TeamMigrationSummary{}, fmt.Errorf("delete conflicting agent_versions: %w", err)
	}

	// Delete conflicting agents from source and capture skipped count.
	resSkip, err := tx.ExecContext(ctx, `
		DELETE FROM agents
		WHERE team_id = ?
		  AND domain IN (
		    SELECT domain FROM agents WHERE team_id = ?
		  )
	`, id, targetID)
	if err != nil {
		return TeamMigrationSummary{}, fmt.Errorf("delete conflicting agents: %w", err)
	}
	skipped, _ := resSkip.RowsAffected()
	sum.AgentsSkipped = int(skipped)

	// ── Move non-conflicting agents ───────────────────────────────────────────
	resAgents, err := tx.ExecContext(ctx,
		"UPDATE agents SET team_id = ? WHERE team_id = ?", targetID, id)
	if err != nil {
		return TeamMigrationSummary{}, fmt.Errorf("migrate agents: %w", err)
	}
	moved, _ := resAgents.RowsAffected()
	sum.Agents = int(moved)

	// Move agent_versions for remaining (non-conflicting) agents.
	if _, err := tx.ExecContext(ctx,
		"UPDATE agent_versions SET team_id = ? WHERE team_id = ?", targetID, id); err != nil {
		return TeamMigrationSummary{}, fmt.Errorf("migrate agent_versions: %w", err)
	}

	// ── Move other team_id-bearing tables ────────────────────────────────────
	type tableUpdate struct {
		dest *int
		sql  string
	}
	updates := []tableUpdate{
		{&sum.Users, "UPDATE users SET team_id = ? WHERE team_id = ?"},
		{&sum.APIKeys, "UPDATE api_keys SET team_id = ? WHERE team_id = ?"},
		{&sum.Entries, "UPDATE entries SET team_id = ? WHERE team_id = ?"},
		{&sum.Clusters, "UPDATE clusters SET team_id = ? WHERE team_id = ?"},
		{&sum.Rules, "UPDATE rules SET team_id = ? WHERE team_id = ?"},
		{nil, "UPDATE dataset_snapshots SET team_id = ? WHERE team_id = ?"},
		{nil, "UPDATE pipeline_runs SET team_id = ? WHERE team_id = ?"},
		{nil, "UPDATE feed_activity SET team_id = ? WHERE team_id = ?"},
		{nil, "UPDATE activity_log SET team_id = ? WHERE team_id = ?"},
	}
	for _, u := range updates {
		res, err := tx.ExecContext(ctx, u.sql, targetID, id)
		if err != nil {
			return TeamMigrationSummary{}, fmt.Errorf("migrate table: %w", err)
		}
		if u.dest != nil {
			n, _ := res.RowsAffected()
			*u.dest = int(n)
		}
	}

	// ── Delete source team_settings ──────────────────────────────────────────
	if _, err := tx.ExecContext(ctx, "DELETE FROM team_settings WHERE team_id = ?", id); err != nil {
		return TeamMigrationSummary{}, fmt.Errorf("delete source team_settings: %w", err)
	}

	// ── Delete source team row ───────────────────────────────────────────────
	res, err := tx.ExecContext(ctx, "DELETE FROM teams WHERE id = ?", id)
	if err != nil {
		return TeamMigrationSummary{}, fmt.Errorf("delete source team: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return TeamMigrationSummary{}, fmt.Errorf("team %q: %w", id, ErrNotFound)
	}

	if err := tx.Commit(); err != nil {
		return TeamMigrationSummary{}, fmt.Errorf("commit migration: %w", err)
	}
	return sum, nil
}

// ── Users ────────────────────────────────────────────────────────────────────

func (s *SQLiteStore) UpsertUser(ctx context.Context, u User) (string, error) {
	existing, err := s.GetUserByEmail(ctx, u.Email)
	if err != nil && !errors.Is(err, ErrNotFound) {
		return "", fmt.Errorf("upsert user lookup: %w", err)
	}
	if existing != nil {
		// Update mutable fields.
		_, err = s.db.ExecContext(ctx,
			"UPDATE users SET name = ?, external_id = ?, role = ? WHERE id = ?",
			u.Name, u.ExternalID, u.Role, existing.ID,
		)
		if err != nil {
			return "", fmt.Errorf("update user: %w", err)
		}
		return existing.ID, nil
	}

	id := uuid.NewString()
	_, err = s.db.ExecContext(ctx, `
		INSERT INTO users (id, team_id, email, name, external_id, password_hash, role, manually_assigned)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`, id, nullIfEmpty(u.TeamID), u.Email, u.Name, u.ExternalID, u.PasswordHash, u.Role, boolToInt(u.ManuallyAssigned))
	if err != nil {
		return "", fmt.Errorf("insert user: %w", err)
	}
	return id, nil
}

func (s *SQLiteStore) GetUserByID(ctx context.Context, id string) (*User, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, COALESCE(team_id,''), email, name, external_id, password_hash, role, manually_assigned, created_at
		FROM users WHERE id = ?
	`, id)
	u, err := scanUser(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("user %q: %w", id, ErrNotFound)
		}
		return nil, fmt.Errorf("get user by id: %w", err)
	}
	return u, nil
}

func (s *SQLiteStore) GetUserByEmail(ctx context.Context, email string) (*User, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, COALESCE(team_id,''), email, name, external_id, password_hash, role, manually_assigned, created_at
		FROM users WHERE email = ?
	`, email)
	u, err := scanUser(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("user %q: %w", email, ErrNotFound)
		}
		return nil, fmt.Errorf("get user by email: %w", err)
	}
	return u, nil
}

func (s *SQLiteStore) GetUserByExternalID(ctx context.Context, externalID string) (*User, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, COALESCE(team_id,''), email, name, external_id, password_hash, role, manually_assigned, created_at
		FROM users WHERE external_id = ?
	`, externalID)
	u, err := scanUser(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("user with external_id %q: %w", externalID, ErrNotFound)
		}
		return nil, fmt.Errorf("get user by external id: %w", err)
	}
	return u, nil
}

func (s *SQLiteStore) ListUsers(ctx context.Context, teamID string) ([]User, error) {
	var rows *sql.Rows
	var err error
	if teamID == "" {
		rows, err = s.db.QueryContext(ctx, `
			SELECT id, COALESCE(team_id,''), email, name, external_id, password_hash, role, manually_assigned, created_at
			FROM users ORDER BY email
		`)
	} else {
		rows, err = s.db.QueryContext(ctx, `
			SELECT id, COALESCE(team_id,''), email, name, external_id, password_hash, role, manually_assigned, created_at
			FROM users WHERE team_id = ? ORDER BY email
		`, teamID)
	}
	if err != nil {
		return nil, fmt.Errorf("list users: %w", err)
	}
	defer rows.Close()

	var users []User
	for rows.Next() {
		u, err := scanUser(rows)
		if err != nil {
			return nil, fmt.Errorf("scan user: %w", err)
		}
		users = append(users, *u)
	}
	return users, rows.Err()
}

func (s *SQLiteStore) AssignUserToTeam(ctx context.Context, userID, teamID, role string) error {
	res, err := s.db.ExecContext(ctx,
		"UPDATE users SET team_id = ?, role = ?, manually_assigned = 1 WHERE id = ?",
		teamID, role, userID,
	)
	if err != nil {
		return fmt.Errorf("assign user to team: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("user %q: %w", userID, ErrNotFound)
	}
	return nil
}

func (s *SQLiteStore) AutoAssignUserToTeam(ctx context.Context, userID, teamID, role string) error {
	_, err := s.db.ExecContext(ctx,
		"UPDATE users SET team_id = ?, role = ? WHERE id = ? AND manually_assigned = 0",
		teamID, role, userID,
	)
	if err != nil {
		return fmt.Errorf("auto-assign user to team: %w", err)
	}
	// Zero rows affected means the user was manually assigned; leave them be.
	return nil
}

// ClaimFirstSuperadmin promotes userID to superadmin iff no superadmin user
// currently exists. The conditional UPDATE is atomic, so it is safe to call on
// every login. Returns true only when this call performed the promotion.
func (s *SQLiteStore) ClaimFirstSuperadmin(ctx context.Context, userID string) (bool, error) {
	res, err := s.db.ExecContext(ctx,
		`UPDATE users SET role = 'superadmin', manually_assigned = 1
		 WHERE id = ? AND NOT EXISTS (SELECT 1 FROM users WHERE role = 'superadmin')`,
		userID,
	)
	if err != nil {
		return false, fmt.Errorf("claim first superadmin: %w", err)
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

func (s *SQLiteStore) ResolveTeamByEmail(ctx context.Context, email string) (*Team, error) {
	teams, err := s.ListTeams(ctx)
	if err != nil {
		return nil, fmt.Errorf("resolve team by email: %w", err)
	}
	for i := range teams {
		t := &teams[i]
		if !t.Enabled {
			continue
		}
		for _, pattern := range t.DomainPatterns {
			re, err := regexp.Compile(pattern)
			if err != nil {
				// Skip invalid patterns rather than hard-failing.
				continue
			}
			if re.MatchString(email) {
				return t, nil
			}
		}
	}
	return nil, nil
}

// ── API Keys ─────────────────────────────────────────────────────────────────

func (s *SQLiteStore) CreateAPIKey(ctx context.Context, key APIKey) error {
	id := key.ID
	if id == "" {
		id = uuid.NewString()
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO api_keys (id, team_id, user_id, key_type, name, key_hash, raw_key, role)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`, id, nullIfEmpty(key.TeamID), nullIfEmpty(key.UserID), key.KeyType, key.Name, key.KeyHash, key.RawKey, key.Role)
	if err != nil {
		return fmt.Errorf("create api key: %w", err)
	}
	return nil
}

func (s *SQLiteStore) GetAPIKeyByHash(ctx context.Context, hash string) (*APIKey, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, COALESCE(team_id,''), COALESCE(user_id,''), key_type, name, key_hash, COALESCE(raw_key,''), role, created_at, last_used_at
		FROM api_keys WHERE key_hash = ?
	`, hash)
	k, err := scanAPIKey(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("api key: %w", ErrNotFound)
		}
		return nil, fmt.Errorf("get api key by hash: %w", err)
	}
	return k, nil
}

func (s *SQLiteStore) ListAPIKeys(ctx context.Context, teamID string) ([]APIKey, error) {
	var rows *sql.Rows
	var err error
	if teamID == "" {
		rows, err = s.db.QueryContext(ctx, `
			SELECT id, COALESCE(team_id,''), COALESCE(user_id,''), key_type, name, key_hash, COALESCE(raw_key,''), role, created_at, last_used_at
			FROM api_keys ORDER BY created_at DESC
		`)
	} else {
		rows, err = s.db.QueryContext(ctx, `
			SELECT id, COALESCE(team_id,''), COALESCE(user_id,''), key_type, name, key_hash, COALESCE(raw_key,''), role, created_at, last_used_at
			FROM api_keys WHERE team_id = ? ORDER BY created_at DESC
		`, teamID)
	}
	if err != nil {
		return nil, fmt.Errorf("list api keys: %w", err)
	}
	defer rows.Close()

	var keys []APIKey
	for rows.Next() {
		k, err := scanAPIKey(rows)
		if err != nil {
			return nil, fmt.Errorf("scan api key: %w", err)
		}
		keys = append(keys, *k)
	}
	return keys, rows.Err()
}

func (s *SQLiteStore) RevokeAPIKey(ctx context.Context, id string) error {
	res, err := s.db.ExecContext(ctx, "DELETE FROM api_keys WHERE id = ?", id)
	if err != nil {
		return fmt.Errorf("revoke api key: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("api key %q: %w", id, ErrNotFound)
	}
	return nil
}

func (s *SQLiteStore) TouchAPIKey(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx,
		"UPDATE api_keys SET last_used_at = ? WHERE id = ?",
		time.Now().UTC().Format(time.RFC3339), id,
	)
	if err != nil {
		return fmt.Errorf("touch api key: %w", err)
	}
	return nil
}

// ── Sessions ─────────────────────────────────────────────────────────────────

func (s *SQLiteStore) CreateSession(ctx context.Context, sess Session) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO sessions (id, user_id, token_hash, expires_at)
		VALUES (?, ?, ?, ?)
	`, sess.ID, sess.UserID, sess.TokenHash, sess.ExpiresAt.UTC().Format(time.RFC3339))
	if err != nil {
		return fmt.Errorf("create session: %w", err)
	}
	return nil
}

func (s *SQLiteStore) GetSession(ctx context.Context, tokenHash string) (*Session, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, user_id, token_hash, expires_at, created_at
		FROM sessions WHERE token_hash = ?
	`, tokenHash)
	var sess Session
	var expiresAt, createdAt string
	if err := row.Scan(&sess.ID, &sess.UserID, &sess.TokenHash, &expiresAt, &createdAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("session: %w", ErrNotFound)
		}
		return nil, fmt.Errorf("get session: %w", err)
	}
	sess.ExpiresAt = parseTimestamp(expiresAt)
	sess.CreatedAt = parseTimestamp(createdAt)
	return &sess, nil
}

func (s *SQLiteStore) DeleteSession(ctx context.Context, tokenHash string) error {
	res, err := s.db.ExecContext(ctx, "DELETE FROM sessions WHERE token_hash = ?", tokenHash)
	if err != nil {
		return fmt.Errorf("delete session: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("session: %w", ErrNotFound)
	}
	return nil
}

// ── Team Settings ─────────────────────────────────────────────────────────────

func (s *SQLiteStore) GetTeamSettings(ctx context.Context, teamID string) (*TeamSettings, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT team_id, domains, cluster_threshold, pipeline_min_entries, agent_model,
		       anthropic_api_key, anthropic_model, ollama_url, ollama_model,
		       llm_provider, ollama_llm_model, ai_touchpoints,
		       embedding_max_tokens, chunk_overlap_tokens, max_chunks, updated_at
		FROM team_settings WHERE team_id = ?
	`, teamID)
	var ts TeamSettings
	var domainsJSON, aiTouchpointsJSON, updatedAt string
	err := row.Scan(
		&ts.TeamID, &domainsJSON, &ts.ClusterThreshold, &ts.PipelineMinEntries, &ts.AgentModel,
		&ts.AnthropicAPIKey, &ts.AnthropicModel, &ts.OllamaURL, &ts.OllamaModel,
		&ts.LLMProvider, &ts.OllamaLLMModel, &aiTouchpointsJSON,
		&ts.EmbeddingMaxTokens, &ts.ChunkOverlapTokens, &ts.MaxChunks, &updatedAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			// Return defaults for teams that have not configured settings yet.
			return &TeamSettings{
				TeamID:             teamID,
				Domains:            []string{},
				ClusterThreshold:   0.85,
				PipelineMinEntries: 10,
				AgentModel:         "claude-haiku-4-5-20251001",
				AnthropicAPIKey:    "",
				AnthropicModel:     "",
				OllamaURL:          "",
				OllamaModel:        "",
				LLMProvider:        "",
				OllamaLLMModel:     "",
				AITouchpoints:      map[string]AITouchpoint{},
			}, nil
		}
		return nil, fmt.Errorf("get team settings: %w", err)
	}
	if err := json.Unmarshal([]byte(domainsJSON), &ts.Domains); err != nil {
		ts.Domains = []string{}
	}
	if err := json.Unmarshal([]byte(aiTouchpointsJSON), &ts.AITouchpoints); err != nil {
		ts.AITouchpoints = map[string]AITouchpoint{}
	}
	ts.UpdatedAt = parseTimestamp(updatedAt)
	return &ts, nil
}

func (s *SQLiteStore) PutTeamSettings(ctx context.Context, ts TeamSettings) error {
	domainsJSON, err := json.Marshal(ts.Domains)
	if err != nil {
		return fmt.Errorf("marshal domains: %w", err)
	}
	tps := ts.AITouchpoints
	if tps == nil {
		tps = map[string]AITouchpoint{}
	}
	aiTouchpointsJSON, err := json.Marshal(tps)
	if err != nil {
		return fmt.Errorf("marshal ai_touchpoints: %w", err)
	}
	_, err = s.db.ExecContext(ctx, `
		INSERT INTO team_settings (
			team_id, domains, cluster_threshold, pipeline_min_entries, agent_model,
			anthropic_api_key, anthropic_model, ollama_url, ollama_model,
			llm_provider, ollama_llm_model, ai_touchpoints,
			embedding_max_tokens, chunk_overlap_tokens, max_chunks, updated_at
		)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP)
		ON CONFLICT(team_id) DO UPDATE SET
			domains              = excluded.domains,
			cluster_threshold    = excluded.cluster_threshold,
			pipeline_min_entries = excluded.pipeline_min_entries,
			agent_model          = excluded.agent_model,
			anthropic_api_key    = excluded.anthropic_api_key,
			anthropic_model      = excluded.anthropic_model,
			ollama_url           = excluded.ollama_url,
			ollama_model         = excluded.ollama_model,
			llm_provider         = excluded.llm_provider,
			ollama_llm_model     = excluded.ollama_llm_model,
			ai_touchpoints       = excluded.ai_touchpoints,
			embedding_max_tokens = excluded.embedding_max_tokens,
			chunk_overlap_tokens = excluded.chunk_overlap_tokens,
			max_chunks           = excluded.max_chunks,
			updated_at           = CURRENT_TIMESTAMP
	`, ts.TeamID, string(domainsJSON), ts.ClusterThreshold, ts.PipelineMinEntries, ts.AgentModel,
		ts.AnthropicAPIKey, ts.AnthropicModel, ts.OllamaURL, ts.OllamaModel,
		ts.LLMProvider, ts.OllamaLLMModel, string(aiTouchpointsJSON),
		ts.EmbeddingMaxTokens, ts.ChunkOverlapTokens, ts.MaxChunks)
	if err != nil {
		return fmt.Errorf("put team settings: %w", err)
	}
	return nil
}

// ── Auth Config ───────────────────────────────────────────────────────────────

func (s *SQLiteStore) GetAuthConfig(ctx context.Context) (*AuthConfig, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT provider, oidc_issuer, oidc_client_id, oidc_redirect_url, updated_at
		FROM auth_config WHERE id = 1
	`)
	var cfg AuthConfig
	var updatedAt string
	err := row.Scan(&cfg.Provider, &cfg.OIDCIssuer, &cfg.OIDCClientID, &cfg.OIDCRedirectURL, &updatedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return &AuthConfig{Provider: "local"}, nil
		}
		return nil, fmt.Errorf("get auth config: %w", err)
	}
	cfg.UpdatedAt = parseTimestamp(updatedAt)
	return &cfg, nil
}

func (s *SQLiteStore) PutAuthConfig(ctx context.Context, c AuthConfig) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO auth_config (id, provider, oidc_issuer, oidc_client_id, oidc_redirect_url, updated_at)
		VALUES (1, ?, ?, ?, ?, CURRENT_TIMESTAMP)
		ON CONFLICT(id) DO UPDATE SET
			provider          = excluded.provider,
			oidc_issuer       = excluded.oidc_issuer,
			oidc_client_id    = excluded.oidc_client_id,
			oidc_redirect_url = excluded.oidc_redirect_url,
			updated_at        = CURRENT_TIMESTAMP
	`, c.Provider, c.OIDCIssuer, c.OIDCClientID, c.OIDCRedirectURL)
	if err != nil {
		return fmt.Errorf("put auth config: %w", err)
	}
	return nil
}

// ── Activity Log ──────────────────────────────────────────────────────────────

func (s *SQLiteStore) LogActivity(ctx context.Context, e ActivityEntry) error {
	id := e.ID
	if id == "" {
		id = uuid.NewString()
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO activity_log (id, team_id, key_id, user_id, action, entity_type, entity_id)
		VALUES (?, ?, ?, ?, ?, ?, ?)
	`, id, e.TeamID, e.KeyID, e.UserID, e.Action, e.EntityType, e.EntityID)
	if err != nil {
		return fmt.Errorf("log activity: %w", err)
	}
	return nil
}

func (s *SQLiteStore) QueryActivity(ctx context.Context, teamID string, limit int) ([]ActivityEntry, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, team_id, key_id, user_id, action, entity_type, entity_id, created_at
		FROM activity_log
		WHERE team_id = ?
		ORDER BY created_at DESC
		LIMIT ?
	`, teamID, limit)
	if err != nil {
		return nil, fmt.Errorf("query activity: %w", err)
	}
	defer rows.Close()

	var entries []ActivityEntry
	for rows.Next() {
		var e ActivityEntry
		var createdAt string
		if err := rows.Scan(&e.ID, &e.TeamID, &e.KeyID, &e.UserID, &e.Action, &e.EntityType, &e.EntityID, &createdAt); err != nil {
			return nil, fmt.Errorf("scan activity entry: %w", err)
		}
		e.CreatedAt = parseTimestamp(createdAt)
		entries = append(entries, e)
	}
	return entries, rows.Err()
}

// ── Scan helpers ──────────────────────────────────────────────────────────────

type scanner interface {
	Scan(dest ...any) error
}

func scanTeam(row scanner) (*Team, error) {
	var t Team
	var patternsJSON string
	var enabledInt int
	var createdAt string
	if err := row.Scan(&t.ID, &t.Name, &patternsJSON, &enabledInt, &createdAt); err != nil {
		return nil, err
	}
	if err := json.Unmarshal([]byte(patternsJSON), &t.DomainPatterns); err != nil {
		t.DomainPatterns = []string{}
	}
	t.Enabled = enabledInt != 0
	t.CreatedAt = parseTimestamp(createdAt)
	return &t, nil
}

func scanUser(row scanner) (*User, error) {
	var u User
	var manuallyAssignedInt int
	var createdAt string
	if err := row.Scan(
		&u.ID, &u.TeamID, &u.Email, &u.Name, &u.ExternalID,
		&u.PasswordHash, &u.Role, &manuallyAssignedInt, &createdAt,
	); err != nil {
		return nil, err
	}
	u.ManuallyAssigned = manuallyAssignedInt != 0
	u.CreatedAt = parseTimestamp(createdAt)
	return &u, nil
}

func scanAPIKey(row scanner) (*APIKey, error) {
	var k APIKey
	var createdAt string
	var lastUsedAt *string
	if err := row.Scan(
		&k.ID, &k.TeamID, &k.UserID, &k.KeyType, &k.Name,
		&k.KeyHash, &k.RawKey, &k.Role, &createdAt, &lastUsedAt,
	); err != nil {
		return nil, err
	}
	k.CreatedAt = parseTimestamp(createdAt)
	if lastUsedAt != nil {
		t := parseTimestamp(*lastUsedAt)
		k.LastUsedAt = &t
	}
	return &k, nil
}

// ── Misc helpers ──────────────────────────────────────────────────────────────

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func nullIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}
