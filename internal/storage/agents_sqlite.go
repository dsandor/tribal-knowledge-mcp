package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"
)

func (s *SQLiteStore) UpsertAgent(ctx context.Context, a Agent) (string, error) {
	// If no ID given, look up by domain so callers don't need to track IDs.
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
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			version       = excluded.version,
			status        = excluded.status,
			system_prompt = excluded.system_prompt,
			instructions  = excluded.instructions,
			anti_patterns = excluded.anti_patterns,
			source_refs   = excluded.source_refs,
			cluster_id    = excluded.cluster_id,
			updated_at    = CURRENT_TIMESTAMP
	`, a.ID, a.Domain, a.Version, string(a.Status),
		a.SystemPrompt, a.Instructions, a.AntiPatterns,
		string(refsJSON), a.ClusterID)
	if err != nil {
		return "", fmt.Errorf("upsert agent: %w", err)
	}
	return a.ID, nil
}

func (s *SQLiteStore) GetAgent(ctx context.Context, id string) (*Agent, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, domain, version, status, system_prompt, instructions, anti_patterns,
		       source_refs, cluster_id, created_at, updated_at
		FROM agents WHERE id = ?
	`, id)
	a, err := scanAgent(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return a, nil
}

func (s *SQLiteStore) GetAgentByDomain(ctx context.Context, domain string) (*Agent, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, domain, version, status, system_prompt, instructions, anti_patterns,
		       source_refs, cluster_id, created_at, updated_at
		FROM agents WHERE domain = ?
	`, domain)
	a, err := scanAgent(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return a, nil
}

func (s *SQLiteStore) ListAgents(ctx context.Context) ([]Agent, error) {
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
		a, err := scanAgent(rows)
		if err != nil {
			return nil, fmt.Errorf("scan agent: %w", err)
		}
		agents = append(agents, *a)
	}
	return agents, rows.Err()
}

func (s *SQLiteStore) PublishAgent(ctx context.Context, id string) error {
	res, err := s.db.ExecContext(ctx,
		"UPDATE agents SET status = 'published', updated_at = CURRENT_TIMESTAMP WHERE id = ?", id)
	if err != nil {
		return fmt.Errorf("publish agent: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("agent %q: %w", id, ErrNotFound)
	}
	return nil
}

func (s *SQLiteStore) StoreAgentVersion(ctx context.Context, v AgentVersion) error {
	// ID is always generated; the caller's ID field is ignored — each version gets a fresh UUID.
	v.ID = uuid.NewString()
	_, err := s.db.ExecContext(ctx, `
		INSERT OR IGNORE INTO agent_versions (id, agent_id, version, system_prompt, instructions, anti_patterns, changelog)
		VALUES (?, ?, ?, ?, ?, ?, ?)
	`, v.ID, v.AgentID, v.Version, v.SystemPrompt, v.Instructions, v.AntiPatterns, v.Changelog)
	if err != nil {
		return fmt.Errorf("store agent version: %w", err)
	}
	return nil
}

func (s *SQLiteStore) ListAgentVersions(ctx context.Context, agentID string) ([]AgentVersion, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, agent_id, version, system_prompt, instructions, anti_patterns, changelog, created_at
		FROM agent_versions WHERE agent_id = ? ORDER BY version ASC
	`, agentID)
	if err != nil {
		return nil, fmt.Errorf("list agent versions: %w", err)
	}
	defer rows.Close()

	var versions []AgentVersion
	for rows.Next() {
		var v AgentVersion
		var createdAt string
		if err := rows.Scan(&v.ID, &v.AgentID, &v.Version,
			&v.SystemPrompt, &v.Instructions, &v.AntiPatterns,
			&v.Changelog, &createdAt); err != nil {
			return nil, fmt.Errorf("scan agent version: %w", err)
		}
		v.CreatedAt = parseTimestamp(createdAt)
		versions = append(versions, v)
	}
	return versions, rows.Err()
}

// Ensure *SQLiteStore implements AgentStore at compile time.
var _ AgentStore = (*SQLiteStore)(nil)

func scanAgent(row rowScanner) (*Agent, error) {
	var a Agent
	var refsJSON, createdAt, updatedAt string
	err := row.Scan(&a.ID, &a.Domain, &a.Version, &a.Status,
		&a.SystemPrompt, &a.Instructions, &a.AntiPatterns,
		&refsJSON, &a.ClusterID, &createdAt, &updatedAt)
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal([]byte(refsJSON), &a.SourceRefs); err != nil {
		a.SourceRefs = []string{}
	}
	a.CreatedAt = parseTimestamp(createdAt)
	a.UpdatedAt = parseTimestamp(updatedAt)
	return &a, nil
}
