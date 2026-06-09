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
	GetAgentByDomain(ctx context.Context, domain string) (*Agent, error)
	ListAgents(ctx context.Context) ([]Agent, error)
	PublishAgent(ctx context.Context, id string) error
	StoreAgentVersion(ctx context.Context, version AgentVersion) error
	ListAgentVersions(ctx context.Context, agentID string) ([]AgentVersion, error)
}
