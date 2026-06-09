package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// migrateAgents creates the agent-related tables (agents, agent_versions).
func (s *PostgresStore) migrateAgents(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS agents (
			id            TEXT PRIMARY KEY,
			domain        TEXT NOT NULL UNIQUE,
			version       INT NOT NULL DEFAULT 1,
			status        TEXT NOT NULL DEFAULT 'draft',
			system_prompt TEXT NOT NULL DEFAULT '',
			instructions  TEXT NOT NULL DEFAULT '',
			anti_patterns TEXT NOT NULL DEFAULT '',
			source_refs   JSONB NOT NULL DEFAULT '[]',
			cluster_id    TEXT NOT NULL DEFAULT '',
			team_id       TEXT NOT NULL DEFAULT '',
			created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			updated_at    TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)
	`)
	if err != nil {
		return fmt.Errorf("create agents table: %w", err)
	}

	_, err = s.db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS agent_versions (
			id            TEXT PRIMARY KEY,
			agent_id      TEXT NOT NULL REFERENCES agents(id),
			version       INT NOT NULL,
			system_prompt TEXT NOT NULL DEFAULT '',
			instructions  TEXT NOT NULL DEFAULT '',
			anti_patterns TEXT NOT NULL DEFAULT '',
			changelog     TEXT NOT NULL DEFAULT '',
			team_id       TEXT NOT NULL DEFAULT '',
			created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			UNIQUE(agent_id, version)
		)
	`)
	if err != nil {
		return fmt.Errorf("create agent_versions table: %w", err)
	}

	return nil
}

// UpsertAgent inserts or updates an agent. If a.ID is empty, it looks up by domain first.
func (s *PostgresStore) UpsertAgent(ctx context.Context, a Agent) (string, error) {
	if a.ID == "" {
		existing, err := s.GetAgentByDomain(ctx, a.Domain)
		if err != nil {
			return "", fmt.Errorf("lookup agent by domain: %w", err)
		}
		if existing != nil {
			a.ID = existing.ID
		} else {
			a.ID = uuid.NewString()
		}
	}

	if a.SourceRefs == nil {
		a.SourceRefs = []string{}
	}
	refsJSON, err := json.Marshal(a.SourceRefs)
	if err != nil {
		return "", fmt.Errorf("marshal source_refs: %w", err)
	}

	_, err = s.db.ExecContext(ctx, `
		INSERT INTO agents (id, domain, version, status, system_prompt, instructions, anti_patterns, source_refs, cluster_id)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		ON CONFLICT (id) DO UPDATE SET
			version       = EXCLUDED.version,
			status        = EXCLUDED.status,
			system_prompt = EXCLUDED.system_prompt,
			instructions  = EXCLUDED.instructions,
			anti_patterns = EXCLUDED.anti_patterns,
			source_refs   = EXCLUDED.source_refs,
			cluster_id    = EXCLUDED.cluster_id,
			updated_at    = NOW()
	`, a.ID, a.Domain, a.Version, string(a.Status),
		a.SystemPrompt, a.Instructions, a.AntiPatterns,
		string(refsJSON), a.ClusterID)
	if err != nil {
		return "", fmt.Errorf("upsert agent: %w", err)
	}
	return a.ID, nil
}

// GetAgent returns the agent with the given ID, or nil if not found.
func (s *PostgresStore) GetAgent(ctx context.Context, id string) (*Agent, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, domain, version, status, system_prompt, instructions, anti_patterns,
		       source_refs, cluster_id, created_at, updated_at
		FROM agents WHERE id = $1
	`, id)
	a, err := scanAgentPG(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("get agent: %w", err)
	}
	return a, nil
}

// GetAgentByDomain returns the agent with the given domain, or nil if not found.
func (s *PostgresStore) GetAgentByDomain(ctx context.Context, domain string) (*Agent, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, domain, version, status, system_prompt, instructions, anti_patterns,
		       source_refs, cluster_id, created_at, updated_at
		FROM agents WHERE domain = $1
	`, domain)
	a, err := scanAgentPG(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("get agent by domain: %w", err)
	}
	return a, nil
}

// ListAgents returns all agents ordered by domain ascending.
func (s *PostgresStore) ListAgents(ctx context.Context) ([]Agent, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, domain, version, status, system_prompt, instructions, anti_patterns,
		       source_refs, cluster_id, created_at, updated_at
		FROM agents ORDER BY domain ASC
	`)
	if err != nil {
		return nil, fmt.Errorf("list agents: %w", err)
	}
	defer rows.Close()

	var agents []Agent
	for rows.Next() {
		a, err := scanAgentPG(rows)
		if err != nil {
			return nil, fmt.Errorf("scan agent: %w", err)
		}
		agents = append(agents, *a)
	}
	return agents, rows.Err()
}

// PublishAgent sets the agent's status to 'published'.
func (s *PostgresStore) PublishAgent(ctx context.Context, id string) error {
	res, err := s.db.ExecContext(ctx,
		`UPDATE agents SET status = 'published', updated_at = NOW() WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("publish agent: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("agent %q: %w", id, ErrNotFound)
	}
	return nil
}

// StoreAgentVersion inserts a versioned snapshot of an agent. Idempotent on (agent_id, version).
func (s *PostgresStore) StoreAgentVersion(ctx context.Context, v AgentVersion) error {
	v.ID = uuid.NewString()
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO agent_versions (id, agent_id, version, system_prompt, instructions, anti_patterns, changelog)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		ON CONFLICT (agent_id, version) DO NOTHING
	`, v.ID, v.AgentID, v.Version, v.SystemPrompt, v.Instructions, v.AntiPatterns, v.Changelog)
	if err != nil {
		return fmt.Errorf("store agent version: %w", err)
	}
	return nil
}

// ListAgentVersions returns all version records for the given agent, ordered by version ascending.
func (s *PostgresStore) ListAgentVersions(ctx context.Context, agentID string) ([]AgentVersion, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, agent_id, version, system_prompt, instructions, anti_patterns, changelog, created_at
		FROM agent_versions WHERE agent_id = $1 ORDER BY version ASC
	`, agentID)
	if err != nil {
		return nil, fmt.Errorf("list agent versions: %w", err)
	}
	defer rows.Close()

	var versions []AgentVersion
	for rows.Next() {
		var v AgentVersion
		var createdAt time.Time
		if err := rows.Scan(
			&v.ID, &v.AgentID, &v.Version,
			&v.SystemPrompt, &v.Instructions, &v.AntiPatterns,
			&v.Changelog, &createdAt,
		); err != nil {
			return nil, fmt.Errorf("scan agent version: %w", err)
		}
		v.CreatedAt = createdAt
		versions = append(versions, v)
	}
	return versions, rows.Err()
}

// scanAgentPG scans an agent row from the PostgreSQL agents table.
func scanAgentPG(row rowScanner) (*Agent, error) {
	var a Agent
	var refsRaw []byte
	var createdAt, updatedAt time.Time
	err := row.Scan(
		&a.ID, &a.Domain, &a.Version, &a.Status,
		&a.SystemPrompt, &a.Instructions, &a.AntiPatterns,
		&refsRaw, &a.ClusterID, &createdAt, &updatedAt,
	)
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(refsRaw, &a.SourceRefs); err != nil {
		a.SourceRefs = []string{}
	}
	a.CreatedAt = createdAt
	a.UpdatedAt = updatedAt
	return &a, nil
}
