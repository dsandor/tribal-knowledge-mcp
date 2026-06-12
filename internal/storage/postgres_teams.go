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

// migrateTeams creates the team/user/session/apikey/settings/auth/activity tables.
func (s *PostgresStore) migrateTeams(ctx context.Context) error {
	stmts := []struct {
		name string
		sql  string
	}{
		{"teams", `
			CREATE TABLE IF NOT EXISTS teams (
				id              TEXT PRIMARY KEY,
				name            TEXT NOT NULL DEFAULT '',
				domain_patterns JSONB NOT NULL DEFAULT '[]',
				enabled         BOOLEAN NOT NULL DEFAULT TRUE,
				created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
			)`},
		{"users", `
			CREATE TABLE IF NOT EXISTS users (
				id                TEXT PRIMARY KEY,
				team_id           TEXT NOT NULL DEFAULT '',
				email             TEXT NOT NULL DEFAULT '',
				name              TEXT NOT NULL DEFAULT '',
				external_id       TEXT NOT NULL DEFAULT '',
				password_hash     TEXT NOT NULL DEFAULT '',
				role              TEXT NOT NULL DEFAULT 'member',
				manually_assigned BOOLEAN NOT NULL DEFAULT FALSE,
				created_at        TIMESTAMPTZ NOT NULL DEFAULT NOW()
			)`},
		{"users_email_uq", `CREATE UNIQUE INDEX IF NOT EXISTS users_email_uq ON users(email) WHERE email <> ''`},
		{"users_ext_id_uq", `CREATE UNIQUE INDEX IF NOT EXISTS users_ext_id_uq ON users(external_id) WHERE external_id <> ''`},
		{"sessions", `
			CREATE TABLE IF NOT EXISTS sessions (
				id          TEXT PRIMARY KEY,
				user_id     TEXT NOT NULL,
				token_hash  TEXT UNIQUE NOT NULL,
				expires_at  TIMESTAMPTZ NOT NULL,
				created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
			)`},
		{"api_keys", `
			CREATE TABLE IF NOT EXISTS api_keys (
				id           TEXT PRIMARY KEY,
				team_id      TEXT NOT NULL DEFAULT '',
				user_id      TEXT NOT NULL DEFAULT '',
				key_type     TEXT NOT NULL DEFAULT 'team',
				name         TEXT NOT NULL DEFAULT '',
				key_hash     TEXT UNIQUE NOT NULL,
				raw_key      TEXT NOT NULL DEFAULT '',
				role         TEXT NOT NULL DEFAULT 'member',
				created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
				last_used_at TIMESTAMPTZ NULL
			)`},
		{"api_keys_raw_key", `ALTER TABLE api_keys ADD COLUMN IF NOT EXISTS raw_key TEXT NOT NULL DEFAULT ''`},
		{"team_settings", `
			CREATE TABLE IF NOT EXISTS team_settings (
				team_id              TEXT PRIMARY KEY,
				domains              JSONB NOT NULL DEFAULT '[]',
				cluster_threshold    FLOAT NOT NULL DEFAULT 0.85,
				pipeline_min_entries INT NOT NULL DEFAULT 10,
				agent_model          TEXT NOT NULL DEFAULT 'claude-haiku-4-5-20251001',
				anthropic_api_key    TEXT NOT NULL DEFAULT '',
				anthropic_model      TEXT NOT NULL DEFAULT '',
				ollama_url           TEXT NOT NULL DEFAULT '',
				ollama_model         TEXT NOT NULL DEFAULT '',
				llm_provider         TEXT NOT NULL DEFAULT '',
				ollama_llm_model     TEXT NOT NULL DEFAULT '',
				updated_at           TIMESTAMPTZ NOT NULL DEFAULT NOW()
			)`},
		{"team_settings_anthropic_api_key", `ALTER TABLE team_settings ADD COLUMN IF NOT EXISTS anthropic_api_key TEXT NOT NULL DEFAULT ''`},
		{"team_settings_anthropic_model", `ALTER TABLE team_settings ADD COLUMN IF NOT EXISTS anthropic_model    TEXT NOT NULL DEFAULT ''`},
		{"team_settings_ollama_url", `ALTER TABLE team_settings ADD COLUMN IF NOT EXISTS ollama_url         TEXT NOT NULL DEFAULT ''`},
		{"team_settings_ollama_model", `ALTER TABLE team_settings ADD COLUMN IF NOT EXISTS ollama_model       TEXT NOT NULL DEFAULT ''`},
		{"team_settings_llm_provider", `ALTER TABLE team_settings ADD COLUMN IF NOT EXISTS llm_provider       TEXT NOT NULL DEFAULT ''`},
		{"team_settings_ollama_llm_model", `ALTER TABLE team_settings ADD COLUMN IF NOT EXISTS ollama_llm_model   TEXT NOT NULL DEFAULT ''`},
		{"team_settings_ai_touchpoints", `ALTER TABLE team_settings ADD COLUMN IF NOT EXISTS ai_touchpoints TEXT NOT NULL DEFAULT '{}'`},
		{"auth_config", `
			CREATE TABLE IF NOT EXISTS auth_config (
				id                INT PRIMARY KEY DEFAULT 1 CHECK (id = 1),
				provider          TEXT NOT NULL DEFAULT 'local',
				oidc_issuer       TEXT NOT NULL DEFAULT '',
				oidc_client_id    TEXT NOT NULL DEFAULT '',
				oidc_redirect_url TEXT NOT NULL DEFAULT '',
				updated_at        TIMESTAMPTZ NOT NULL DEFAULT NOW()
			)`},
		{"auth_config_seed", `INSERT INTO auth_config DEFAULT VALUES ON CONFLICT DO NOTHING`},
		{"activity_log", `
			CREATE TABLE IF NOT EXISTS activity_log (
				id          TEXT PRIMARY KEY,
				team_id     TEXT NOT NULL DEFAULT '',
				key_id      TEXT NOT NULL DEFAULT '',
				user_id     TEXT NOT NULL DEFAULT '',
				action      TEXT NOT NULL DEFAULT '',
				entity_type TEXT NOT NULL DEFAULT '',
				entity_id   TEXT NOT NULL DEFAULT '',
				created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
			)`},
		{"activity_team_time_idx", `CREATE INDEX IF NOT EXISTS activity_team_time_idx ON activity_log(team_id, created_at DESC)`},
	}

	for _, st := range stmts {
		if _, err := s.db.ExecContext(ctx, st.sql); err != nil {
			return fmt.Errorf("migrate teams (%s): %w", st.name, err)
		}
	}
	return nil
}

// ── Teams ─────────────────────────────────────────────────────────────────────

func (s *PostgresStore) CreateTeam(ctx context.Context, t Team) (string, error) {
	id := uuid.NewString()
	patternsJSON, err := json.Marshal(t.DomainPatterns)
	if err != nil {
		return "", fmt.Errorf("marshal domain_patterns: %w", err)
	}
	_, err = s.db.ExecContext(ctx, `
		INSERT INTO teams (id, name, domain_patterns, enabled)
		VALUES ($1, $2, $3, $4)
	`, id, t.Name, string(patternsJSON), t.Enabled)
	if err != nil {
		return "", fmt.Errorf("insert team: %w", err)
	}
	return id, nil
}

func (s *PostgresStore) GetTeam(ctx context.Context, id string) (*Team, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, name, domain_patterns, enabled, created_at
		FROM teams WHERE id = $1
	`, id)
	team, err := scanTeamPG(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("team %q: %w", id, ErrNotFound)
		}
		return nil, fmt.Errorf("get team: %w", err)
	}
	return team, nil
}

func (s *PostgresStore) ListTeams(ctx context.Context) ([]Team, error) {
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
		team, err := scanTeamPG(rows)
		if err != nil {
			return nil, fmt.Errorf("scan team: %w", err)
		}
		teams = append(teams, *team)
	}
	return teams, rows.Err()
}

func (s *PostgresStore) UpdateTeam(ctx context.Context, id, name string, domainPatterns []string) error {
	patternsJSON, err := json.Marshal(domainPatterns)
	if err != nil {
		return fmt.Errorf("marshal domain_patterns: %w", err)
	}
	res, err := s.db.ExecContext(ctx,
		"UPDATE teams SET name = $1, domain_patterns = $2 WHERE id = $3",
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

func (s *PostgresStore) SetTeamEnabled(ctx context.Context, id string, enabled bool) error {
	res, err := s.db.ExecContext(ctx,
		"UPDATE teams SET enabled = $1 WHERE id = $2",
		enabled, id,
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

func (s *PostgresStore) DeleteTeam(ctx context.Context, id string) error {
	// Delete the team_settings row first (no FK, just cleanup).
	if _, err := s.db.ExecContext(ctx, "DELETE FROM team_settings WHERE team_id = $1", id); err != nil {
		return fmt.Errorf("delete team_settings: %w", err)
	}
	res, err := s.db.ExecContext(ctx, "DELETE FROM teams WHERE id = $1", id)
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
func (s *PostgresStore) TeamDataCounts(ctx context.Context, id string) (TeamDataCounts, error) {
	var c TeamDataCounts
	queries := []struct {
		dest *int
		sql  string
	}{
		{&c.Users, "SELECT COUNT(*) FROM users WHERE team_id = $1"},
		{&c.APIKeys, "SELECT COUNT(*) FROM api_keys WHERE team_id = $1"},
		{&c.Entries, "SELECT COUNT(*) FROM entries WHERE team_id = $1"},
		{&c.Clusters, "SELECT COUNT(*) FROM clusters WHERE team_id = $1"},
		{&c.Agents, "SELECT COUNT(*) FROM agents WHERE team_id = $1"},
		{&c.Rules, "SELECT COUNT(*) FROM rules WHERE team_id = $1"},
	}
	for _, q := range queries {
		if err := s.db.QueryRowContext(ctx, q.sql, id).Scan(q.dest); err != nil {
			return TeamDataCounts{}, fmt.Errorf("team data counts: %w", err)
		}
	}
	return c, nil
}

// DeleteTeamMigrate transactionally migrates all data from team id to targetID,
// handles agent domain conflicts, deletes source team_settings, and deletes the source team.
func (s *PostgresStore) DeleteTeamMigrate(ctx context.Context, id, targetID string) (TeamMigrationSummary, error) {
	if id == targetID {
		return TeamMigrationSummary{}, fmt.Errorf("source and target team must be different")
	}

	// Validate target exists.
	var exists int
	if err := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM teams WHERE id = $1", targetID).Scan(&exists); err != nil {
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

	// Delete agent_versions for conflicting agents.
	_, err = tx.ExecContext(ctx, `
		DELETE FROM agent_versions WHERE agent_id IN (
			SELECT a.id FROM agents a
			WHERE a.team_id = $1
			  AND EXISTS (
			    SELECT 1 FROM agents b
			    WHERE b.team_id = $2 AND b.domain = a.domain
			  )
		)
	`, id, targetID)
	if err != nil {
		return TeamMigrationSummary{}, fmt.Errorf("delete conflicting agent_versions: %w", err)
	}

	// Delete conflicting agents from source, capture skipped count.
	resSkip, err := tx.ExecContext(ctx, `
		DELETE FROM agents
		WHERE team_id = $1
		  AND domain IN (
		    SELECT domain FROM agents WHERE team_id = $2
		  )
	`, id, targetID)
	if err != nil {
		return TeamMigrationSummary{}, fmt.Errorf("delete conflicting agents: %w", err)
	}
	skipped, _ := resSkip.RowsAffected()
	sum.AgentsSkipped = int(skipped)

	// Move non-conflicting agents.
	resAgents, err := tx.ExecContext(ctx,
		"UPDATE agents SET team_id = $1 WHERE team_id = $2", targetID, id)
	if err != nil {
		return TeamMigrationSummary{}, fmt.Errorf("migrate agents: %w", err)
	}
	moved, _ := resAgents.RowsAffected()
	sum.Agents = int(moved)

	// Move agent_versions for remaining agents.
	if _, err := tx.ExecContext(ctx,
		"UPDATE agent_versions SET team_id = $1 WHERE team_id = $2", targetID, id); err != nil {
		return TeamMigrationSummary{}, fmt.Errorf("migrate agent_versions: %w", err)
	}

	type tableUpdate struct {
		dest *int
		sql  string
	}
	updates := []tableUpdate{
		{&sum.Users, "UPDATE users SET team_id = $1 WHERE team_id = $2"},
		{&sum.APIKeys, "UPDATE api_keys SET team_id = $1 WHERE team_id = $2"},
		{&sum.Entries, "UPDATE entries SET team_id = $1 WHERE team_id = $2"},
		{&sum.Clusters, "UPDATE clusters SET team_id = $1 WHERE team_id = $2"},
		{&sum.Rules, "UPDATE rules SET team_id = $1 WHERE team_id = $2"},
		{nil, "UPDATE dataset_snapshots SET team_id = $1 WHERE team_id = $2"},
		{nil, "UPDATE pipeline_runs SET team_id = $1 WHERE team_id = $2"},
		{nil, "UPDATE feed_activity SET team_id = $1 WHERE team_id = $2"},
		{nil, "UPDATE activity_log SET team_id = $1 WHERE team_id = $2"},
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

	// Delete source team_settings.
	if _, err := tx.ExecContext(ctx, "DELETE FROM team_settings WHERE team_id = $1", id); err != nil {
		return TeamMigrationSummary{}, fmt.Errorf("delete source team_settings: %w", err)
	}

	// Delete source team row.
	res, err := tx.ExecContext(ctx, "DELETE FROM teams WHERE id = $1", id)
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

// ── Users ─────────────────────────────────────────────────────────────────────

func (s *PostgresStore) UpsertUser(ctx context.Context, u User) (string, error) {
	existing, err := s.GetUserByEmail(ctx, u.Email)
	if err != nil && !errors.Is(err, ErrNotFound) {
		return "", fmt.Errorf("upsert user lookup: %w", err)
	}
	if existing != nil {
		_, err = s.db.ExecContext(ctx,
			"UPDATE users SET name = $1, external_id = $2, role = $3 WHERE id = $4",
			u.Name, u.ExternalID, u.Role, existing.ID,
		)
		if err != nil {
			return "", fmt.Errorf("update user: %w", err)
		}
		return existing.ID, nil
	}

	id := uuid.NewString()
	teamID := u.TeamID // empty string is fine — column has NOT NULL DEFAULT ''
	_, err = s.db.ExecContext(ctx, `
		INSERT INTO users (id, team_id, email, name, external_id, password_hash, role, manually_assigned)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
	`, id, teamID, u.Email, u.Name, u.ExternalID, u.PasswordHash, u.Role, u.ManuallyAssigned)
	if err != nil {
		return "", fmt.Errorf("insert user: %w", err)
	}
	return id, nil
}

func (s *PostgresStore) GetUserByID(ctx context.Context, id string) (*User, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, team_id, email, name, external_id, password_hash, role, manually_assigned, created_at
		FROM users WHERE id = $1
	`, id)
	u, err := scanUserPG(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("user %q: %w", id, ErrNotFound)
		}
		return nil, fmt.Errorf("get user by id: %w", err)
	}
	return u, nil
}

func (s *PostgresStore) GetUserByEmail(ctx context.Context, email string) (*User, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, team_id, email, name, external_id, password_hash, role, manually_assigned, created_at
		FROM users WHERE email = $1
	`, email)
	u, err := scanUserPG(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("user %q: %w", email, ErrNotFound)
		}
		return nil, fmt.Errorf("get user by email: %w", err)
	}
	return u, nil
}

func (s *PostgresStore) GetUserByExternalID(ctx context.Context, externalID string) (*User, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, team_id, email, name, external_id, password_hash, role, manually_assigned, created_at
		FROM users WHERE external_id = $1
	`, externalID)
	u, err := scanUserPG(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("user with external_id %q: %w", externalID, ErrNotFound)
		}
		return nil, fmt.Errorf("get user by external id: %w", err)
	}
	return u, nil
}

func (s *PostgresStore) ListUsers(ctx context.Context, teamID string) ([]User, error) {
	var rows *sql.Rows
	var err error
	if teamID == "" {
		rows, err = s.db.QueryContext(ctx, `
			SELECT id, team_id, email, name, external_id, password_hash, role, manually_assigned, created_at
			FROM users ORDER BY email
		`)
	} else {
		rows, err = s.db.QueryContext(ctx, `
			SELECT id, team_id, email, name, external_id, password_hash, role, manually_assigned, created_at
			FROM users WHERE team_id = $1 ORDER BY email
		`, teamID)
	}
	if err != nil {
		return nil, fmt.Errorf("list users: %w", err)
	}
	defer rows.Close()

	var users []User
	for rows.Next() {
		u, err := scanUserPG(rows)
		if err != nil {
			return nil, fmt.Errorf("scan user: %w", err)
		}
		users = append(users, *u)
	}
	return users, rows.Err()
}

func (s *PostgresStore) AssignUserToTeam(ctx context.Context, userID, teamID, role string) error {
	res, err := s.db.ExecContext(ctx,
		"UPDATE users SET team_id = $1, role = $2, manually_assigned = TRUE WHERE id = $3",
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

func (s *PostgresStore) ResolveTeamByEmail(ctx context.Context, email string) (*Team, error) {
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
				continue
			}
			if re.MatchString(email) {
				return t, nil
			}
		}
	}
	return nil, nil
}

// ── API Keys ──────────────────────────────────────────────────────────────────

func (s *PostgresStore) CreateAPIKey(ctx context.Context, key APIKey) error {
	id := key.ID
	if id == "" {
		id = uuid.NewString()
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO api_keys (id, team_id, user_id, key_type, name, key_hash, raw_key, role)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
	`, id, key.TeamID, key.UserID, key.KeyType, key.Name, key.KeyHash, key.RawKey, key.Role)
	if err != nil {
		return fmt.Errorf("create api key: %w", err)
	}
	return nil
}

func (s *PostgresStore) GetAPIKeyByHash(ctx context.Context, hash string) (*APIKey, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, team_id, user_id, key_type, name, key_hash, raw_key, role, created_at, last_used_at
		FROM api_keys WHERE key_hash = $1
	`, hash)
	k, err := scanAPIKeyPG(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("api key: %w", ErrNotFound)
		}
		return nil, fmt.Errorf("get api key by hash: %w", err)
	}
	return k, nil
}

func (s *PostgresStore) ListAPIKeys(ctx context.Context, teamID string) ([]APIKey, error) {
	var rows *sql.Rows
	var err error
	if teamID == "" {
		rows, err = s.db.QueryContext(ctx, `
			SELECT id, team_id, user_id, key_type, name, key_hash, raw_key, role, created_at, last_used_at
			FROM api_keys ORDER BY created_at DESC
		`)
	} else {
		rows, err = s.db.QueryContext(ctx, `
			SELECT id, team_id, user_id, key_type, name, key_hash, raw_key, role, created_at, last_used_at
			FROM api_keys WHERE team_id = $1 ORDER BY created_at DESC
		`, teamID)
	}
	if err != nil {
		return nil, fmt.Errorf("list api keys: %w", err)
	}
	defer rows.Close()

	var keys []APIKey
	for rows.Next() {
		k, err := scanAPIKeyPG(rows)
		if err != nil {
			return nil, fmt.Errorf("scan api key: %w", err)
		}
		keys = append(keys, *k)
	}
	return keys, rows.Err()
}

func (s *PostgresStore) RevokeAPIKey(ctx context.Context, id string) error {
	res, err := s.db.ExecContext(ctx, "DELETE FROM api_keys WHERE id = $1", id)
	if err != nil {
		return fmt.Errorf("revoke api key: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("api key %q: %w", id, ErrNotFound)
	}
	return nil
}

func (s *PostgresStore) TouchAPIKey(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx,
		"UPDATE api_keys SET last_used_at = NOW() WHERE id = $1",
		id,
	)
	if err != nil {
		return fmt.Errorf("touch api key: %w", err)
	}
	return nil
}

// ── Sessions ──────────────────────────────────────────────────────────────────

func (s *PostgresStore) CreateSession(ctx context.Context, sess Session) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO sessions (id, user_id, token_hash, expires_at)
		VALUES ($1, $2, $3, $4)
	`, sess.ID, sess.UserID, sess.TokenHash, sess.ExpiresAt)
	if err != nil {
		return fmt.Errorf("create session: %w", err)
	}
	return nil
}

func (s *PostgresStore) GetSession(ctx context.Context, tokenHash string) (*Session, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, user_id, token_hash, expires_at, created_at
		FROM sessions WHERE token_hash = $1
	`, tokenHash)
	var sess Session
	var expiresAt, createdAt time.Time
	if err := row.Scan(&sess.ID, &sess.UserID, &sess.TokenHash, &expiresAt, &createdAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("session: %w", ErrNotFound)
		}
		return nil, fmt.Errorf("get session: %w", err)
	}
	sess.ExpiresAt = expiresAt
	sess.CreatedAt = createdAt
	return &sess, nil
}

func (s *PostgresStore) DeleteSession(ctx context.Context, tokenHash string) error {
	res, err := s.db.ExecContext(ctx, "DELETE FROM sessions WHERE token_hash = $1", tokenHash)
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

func (s *PostgresStore) GetTeamSettings(ctx context.Context, teamID string) (*TeamSettings, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT team_id, domains, cluster_threshold, pipeline_min_entries, agent_model,
		       anthropic_api_key, anthropic_model, ollama_url, ollama_model,
		       llm_provider, ollama_llm_model, ai_touchpoints, updated_at
		FROM team_settings WHERE team_id = $1
	`, teamID)
	var ts TeamSettings
	var domainsRaw, aiTouchpointsRaw []byte
	var updatedAt time.Time
	err := row.Scan(
		&ts.TeamID, &domainsRaw, &ts.ClusterThreshold, &ts.PipelineMinEntries, &ts.AgentModel,
		&ts.AnthropicAPIKey, &ts.AnthropicModel, &ts.OllamaURL, &ts.OllamaModel,
		&ts.LLMProvider, &ts.OllamaLLMModel, &aiTouchpointsRaw, &updatedAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
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
	if err := json.Unmarshal(domainsRaw, &ts.Domains); err != nil {
		ts.Domains = []string{}
	}
	if err := json.Unmarshal(aiTouchpointsRaw, &ts.AITouchpoints); err != nil {
		ts.AITouchpoints = map[string]AITouchpoint{}
	}
	ts.UpdatedAt = updatedAt
	return &ts, nil
}

func (s *PostgresStore) PutTeamSettings(ctx context.Context, ts TeamSettings) error {
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
			llm_provider, ollama_llm_model, ai_touchpoints, updated_at
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, NOW())
		ON CONFLICT (team_id) DO UPDATE SET
			domains              = EXCLUDED.domains,
			cluster_threshold    = EXCLUDED.cluster_threshold,
			pipeline_min_entries = EXCLUDED.pipeline_min_entries,
			agent_model          = EXCLUDED.agent_model,
			anthropic_api_key    = EXCLUDED.anthropic_api_key,
			anthropic_model      = EXCLUDED.anthropic_model,
			ollama_url           = EXCLUDED.ollama_url,
			ollama_model         = EXCLUDED.ollama_model,
			llm_provider         = EXCLUDED.llm_provider,
			ollama_llm_model     = EXCLUDED.ollama_llm_model,
			ai_touchpoints       = EXCLUDED.ai_touchpoints,
			updated_at           = NOW()
	`, ts.TeamID, string(domainsJSON), ts.ClusterThreshold, ts.PipelineMinEntries, ts.AgentModel,
		ts.AnthropicAPIKey, ts.AnthropicModel, ts.OllamaURL, ts.OllamaModel,
		ts.LLMProvider, ts.OllamaLLMModel, string(aiTouchpointsJSON))
	if err != nil {
		return fmt.Errorf("put team settings: %w", err)
	}
	return nil
}

// ── Auth Config ───────────────────────────────────────────────────────────────

func (s *PostgresStore) GetAuthConfig(ctx context.Context) (*AuthConfig, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT provider, oidc_issuer, oidc_client_id, oidc_redirect_url, updated_at
		FROM auth_config WHERE id = 1
	`)
	var cfg AuthConfig
	var updatedAt time.Time
	err := row.Scan(&cfg.Provider, &cfg.OIDCIssuer, &cfg.OIDCClientID, &cfg.OIDCRedirectURL, &updatedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return &AuthConfig{Provider: "local"}, nil
		}
		return nil, fmt.Errorf("get auth config: %w", err)
	}
	cfg.UpdatedAt = updatedAt
	return &cfg, nil
}

func (s *PostgresStore) PutAuthConfig(ctx context.Context, c AuthConfig) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO auth_config (id, provider, oidc_issuer, oidc_client_id, oidc_redirect_url, updated_at)
		VALUES (1, $1, $2, $3, $4, NOW())
		ON CONFLICT (id) DO UPDATE SET
			provider          = EXCLUDED.provider,
			oidc_issuer       = EXCLUDED.oidc_issuer,
			oidc_client_id    = EXCLUDED.oidc_client_id,
			oidc_redirect_url = EXCLUDED.oidc_redirect_url,
			updated_at        = NOW()
	`, c.Provider, c.OIDCIssuer, c.OIDCClientID, c.OIDCRedirectURL)
	if err != nil {
		return fmt.Errorf("put auth config: %w", err)
	}
	return nil
}

// ── Activity Log ──────────────────────────────────────────────────────────────

func (s *PostgresStore) LogActivity(ctx context.Context, e ActivityEntry) error {
	id := e.ID
	if id == "" {
		id = uuid.NewString()
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO activity_log (id, team_id, key_id, user_id, action, entity_type, entity_id)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
	`, id, e.TeamID, e.KeyID, e.UserID, e.Action, e.EntityType, e.EntityID)
	if err != nil {
		return fmt.Errorf("log activity: %w", err)
	}
	return nil
}

func (s *PostgresStore) QueryActivity(ctx context.Context, teamID string, limit int) ([]ActivityEntry, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, team_id, key_id, user_id, action, entity_type, entity_id, created_at
		FROM activity_log
		WHERE team_id = $1
		ORDER BY created_at DESC
		LIMIT $2
	`, teamID, limit)
	if err != nil {
		return nil, fmt.Errorf("query activity: %w", err)
	}
	defer rows.Close()

	var entries []ActivityEntry
	for rows.Next() {
		var e ActivityEntry
		var createdAt time.Time
		if err := rows.Scan(&e.ID, &e.TeamID, &e.KeyID, &e.UserID, &e.Action, &e.EntityType, &e.EntityID, &createdAt); err != nil {
			return nil, fmt.Errorf("scan activity entry: %w", err)
		}
		e.CreatedAt = createdAt
		entries = append(entries, e)
	}
	return entries, rows.Err()
}

// ── Scan helpers ──────────────────────────────────────────────────────────────

func scanTeamPG(row rowScanner) (*Team, error) {
	var t Team
	var patternsRaw []byte
	var createdAt time.Time
	if err := row.Scan(&t.ID, &t.Name, &patternsRaw, &t.Enabled, &createdAt); err != nil {
		return nil, err
	}
	if err := json.Unmarshal(patternsRaw, &t.DomainPatterns); err != nil {
		t.DomainPatterns = []string{}
	}
	t.CreatedAt = createdAt
	return &t, nil
}

func scanUserPG(row rowScanner) (*User, error) {
	var u User
	var createdAt time.Time
	if err := row.Scan(
		&u.ID, &u.TeamID, &u.Email, &u.Name, &u.ExternalID,
		&u.PasswordHash, &u.Role, &u.ManuallyAssigned, &createdAt,
	); err != nil {
		return nil, err
	}
	u.CreatedAt = createdAt
	return &u, nil
}

func scanAPIKeyPG(row rowScanner) (*APIKey, error) {
	var k APIKey
	var createdAt time.Time
	var lastUsedAt sql.NullTime
	if err := row.Scan(
		&k.ID, &k.TeamID, &k.UserID, &k.KeyType, &k.Name,
		&k.KeyHash, &k.RawKey, &k.Role, &createdAt, &lastUsedAt,
	); err != nil {
		return nil, err
	}
	k.CreatedAt = createdAt
	if lastUsedAt.Valid {
		t := lastUsedAt.Time
		k.LastUsedAt = &t
	}
	return &k, nil
}
