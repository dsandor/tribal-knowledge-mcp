package storage

import (
	"context"
	"time"
)

type AgentStatus string

const (
	AgentStatusDraft     AgentStatus = "draft"
	AgentStatusPublished AgentStatus = "published"
)

type Agent struct {
	ID           string
	Domain       string
	Version      int
	Status       AgentStatus
	SystemPrompt string
	Instructions string
	AntiPatterns string
	SourceRefs   []string
	ClusterID    string
	TeamID       string
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

type AgentVersion struct {
	ID           string
	AgentID      string
	Version      int
	SystemPrompt string
	Instructions string
	AntiPatterns string
	Changelog    string
	CreatedAt    time.Time
}

// AgentStore extends AnalysisStore with agent generation and versioning methods.
type AgentStore interface {
	AnalysisStore
	UpsertAgent(ctx context.Context, agent Agent) (string, error)
	GetAgent(ctx context.Context, id string) (*Agent, error)
	// GetAgentByDomain looks up the latest agent for a domain, scoped by team.
	//
	// Lookup semantics:
	//   - teamID non-empty: exact match on (domain, team_id) first; if not found,
	//     falls back to a legacy row with team_id="" (visible-to-all policy).
	//   - teamID empty: returns any matching agent regardless of team (dev / single-
	//     tenant fallback; behaves like the old unscoped query).
	GetAgentByDomain(ctx context.Context, domain, teamID string) (*Agent, error)
	ListAgents(ctx context.Context, teamID string) ([]Agent, error)
	PublishAgent(ctx context.Context, id string) error
	StoreAgentVersion(ctx context.Context, version AgentVersion) error
	ListAgentVersions(ctx context.Context, agentID string) ([]AgentVersion, error)
}
