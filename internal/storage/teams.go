package storage

import (
	"context"
	"time"
)

type Team struct {
	ID             string    `json:"id"`
	Name           string    `json:"name"`
	DomainPatterns []string  `json:"domain_patterns"`
	Enabled        bool      `json:"enabled"`
	CreatedAt      time.Time `json:"created_at"`
}

type User struct {
	ID               string
	TeamID           string // empty until assigned
	Email            string
	Name             string
	ExternalID       string // OIDC subject claim; empty for local auth
	PasswordHash     string // bcrypt; only for local auth
	Role             string // member|curator|admin|superadmin
	ManuallyAssigned bool
	CreatedAt        time.Time
}

type Session struct {
	ID        string
	UserID    string
	TokenHash string
	ExpiresAt time.Time
	CreatedAt time.Time
}

const (
	APIKeyTypeTeam = "team"
	APIKeyTypeUser = "user"
)

// UnassignedTeamID is the reserved team that users land in when no team's
// whitelist (domain_patterns) matches their email. Seeded at startup.
const UnassignedTeamID = "unassigned"

type APIKey struct {
	ID         string
	TeamID     string // empty for superadmin keys
	UserID     string // empty for team keys
	KeyType    string // "team" | "user"
	Name       string
	KeyHash    string
	RawKey     string // plaintext key, retained so it can be re-copied from the UI
	Role       string // member|curator|admin|superadmin
	CreatedAt  time.Time
	LastUsedAt *time.Time
}

// AITouchpoint configures the provider and model for one AI touchpoint.
// Valid providers: "anthropic" | "ollama". Empty Model uses the provider's
// touchpoint default (see aiconfig.LLMForTouchpoint).
type AITouchpoint struct {
	Provider string `json:"provider"`
	Model    string `json:"model"`
}

type TeamSettings struct {
	TeamID             string
	Domains            []string // domain taxonomy labels
	ClusterThreshold   float64
	PipelineMinEntries int
	AgentModel         string `json:"agent_model"`
	AnthropicAPIKey    string `json:"anthropic_api_key"`
	AnthropicModel     string `json:"anthropic_model"`
	OllamaURL          string `json:"ollama_url"`
	OllamaModel        string `json:"ollama_model"`
	LLMProvider        string `json:"llm_provider"`     // "" | "anthropic" | "ollama"; empty means anthropic
	OllamaLLMModel     string `json:"ollama_llm_model"` // chat model; distinct from OllamaModel (embeddings)
	// Per-team embedding/chunking config. 0 means "unset → fall back to env default".
	EmbeddingMaxTokens int `json:"embedding_max_tokens"`
	ChunkOverlapTokens int `json:"chunk_overlap_tokens"`
	MaxChunks          int `json:"max_chunks"`
	// AITouchpoints maps touchpoint name to per-touchpoint AI config.
	// Valid keys: "analysis", "agents", "improvement", "enrichment".
	AITouchpoints map[string]AITouchpoint `json:"ai_touchpoints"`
	UpdatedAt     time.Time
}

// EnrichmentPrefs holds a user's per-user enrichment tuning. Scalars are
// "resolved" values together with a *Set flag indicating whether the user has
// an explicit override (true) or the value should fall back to the deployment
// default (false). Rule lists are bucketed by kind.
type EnrichmentPrefs struct {
	MinRelevance float64
	MaxMemories  int
	LLMRewrite   bool
	// Source flags: true when the scalar is a per-user override (not the default).
	MinRelevanceSet bool
	MaxMemoriesSet  bool
	LLMRewriteSet   bool

	AllowDomains  []string
	DenyDomains   []string
	AllowTags     []string
	DenyTags      []string
	PinnedEntries []string
}

type AuthConfig struct {
	Provider        string // "local" | "oidc"
	OIDCIssuer      string
	OIDCClientID    string
	OIDCRedirectURL string
	UpdatedAt       time.Time
}

// EmbeddingConfig is the deployment-wide (singleton) embedding provider
// configuration. There is exactly one row (id=1) in the embedding_config table.
type EmbeddingConfig struct {
	Provider      string // "openai" | "ollama"
	Model         string
	OpenAIAPIKey  string
	OpenAIBaseURL string
	OllamaURL     string
	Dimension     int
	UpdatedAt     time.Time
}

type ActivityEntry struct {
	ID         string
	TeamID     string
	KeyID      string
	UserID     string
	Action     string // e.g. "knowledge.store", "prompt.enhance"
	EntityType string
	EntityID   string
	CreatedAt  time.Time
}

// TeamDataCounts reports how many team-owned records exist for a given team ID.
// Used by the handler to guard against deleting a non-empty team.
type TeamDataCounts struct {
	Users    int `json:"users"`
	APIKeys  int `json:"api_keys"`
	Entries  int `json:"entries"`
	Clusters int `json:"clusters"`
	Agents   int `json:"agents"`
	Rules    int `json:"rules"`
}

// TeamMigrationSummary reports what DeleteTeamMigrate moved or skipped.
type TeamMigrationSummary struct {
	Users         int `json:"users"`
	APIKeys       int `json:"api_keys"`
	Entries       int `json:"entries"`
	Clusters      int `json:"clusters"`
	Agents        int `json:"agents"`
	AgentsSkipped int `json:"agents_skipped"`
	Rules         int `json:"rules"`
}

// TeamStore handles teams, users, sessions, API keys, settings, auth config, and activity log.
// *SQLiteStore implements this interface.
type TeamStore interface {
	// Teams
	CreateTeam(ctx context.Context, t Team) (string, error)
	GetTeam(ctx context.Context, id string) (*Team, error)
	ListTeams(ctx context.Context) ([]Team, error)
	UpdateTeam(ctx context.Context, id, name string, domainPatterns []string) error
	SetTeamEnabled(ctx context.Context, id string, enabled bool) error
	DeleteTeam(ctx context.Context, id string) error
	// TeamDataCounts returns counts of team-owned records for the given team ID.
	TeamDataCounts(ctx context.Context, id string) (TeamDataCounts, error)
	// DeleteTeamMigrate transactionally migrates all data from team id to targetID,
	// then deletes the source team. Returns a summary of what was moved/skipped.
	DeleteTeamMigrate(ctx context.Context, id, targetID string) (TeamMigrationSummary, error)

	// Users
	UpsertUser(ctx context.Context, u User) (string, error)
	GetUserByID(ctx context.Context, id string) (*User, error)
	GetUserByEmail(ctx context.Context, email string) (*User, error)
	GetUserByExternalID(ctx context.Context, externalID string) (*User, error)
	ListUsers(ctx context.Context, teamID string) ([]User, error)
	AssignUserToTeam(ctx context.Context, userID, teamID, role string) error
	// SetUserRole updates only the user's role, leaving their home team
	// (team_id) untouched. Returns a wrapped ErrNotFound if no such user.
	SetUserRole(ctx context.Context, userID, role string) error
	// AutoAssignUserToTeam assigns a user to a team without marking the
	// assignment as manual, and only if the user has not been manually
	// assigned by an admin. Used by the OIDC login flow for whitelist-based
	// grouping. A no-op (no error) when the user is manually assigned.
	AutoAssignUserToTeam(ctx context.Context, userID, teamID, role string) error
	// ResolveTeamByEmail returns the first enabled team whose domain_patterns
	// contains a regex matching email; returns nil (no error) if none match.
	ResolveTeamByEmail(ctx context.Context, email string) (*Team, error)
	// Team memberships (multi-team). The user's home team (users.team_id) is an
	// implicit membership and cannot be removed via RemoveTeamMember.
	AddTeamMember(ctx context.Context, userID, teamID string) error
	RemoveTeamMember(ctx context.Context, userID, teamID string) error
	ListUserTeams(ctx context.Context, userID string) ([]Team, error)
	IsTeamMember(ctx context.Context, userID, teamID string) (bool, error)
	// ClaimFirstSuperadmin promotes userID to superadmin iff no superadmin user
	// currently exists, so the first user to sign in on a fresh deployment owns
	// it. Atomic and idempotent; returns true only when it performed the
	// promotion.
	ClaimFirstSuperadmin(ctx context.Context, userID string) (bool, error)

	// API keys
	CreateAPIKey(ctx context.Context, key APIKey) error
	GetAPIKeyByHash(ctx context.Context, hash string) (*APIKey, error)
	ListAPIKeys(ctx context.Context, teamID string) ([]APIKey, error)
	RevokeAPIKey(ctx context.Context, id string) error
	TouchAPIKey(ctx context.Context, id string) error // async last_used_at

	// Sessions
	CreateSession(ctx context.Context, s Session) error
	GetSession(ctx context.Context, tokenHash string) (*Session, error)
	DeleteSession(ctx context.Context, tokenHash string) error

	// Team settings
	GetTeamSettings(ctx context.Context, teamID string) (*TeamSettings, error)
	PutTeamSettings(ctx context.Context, s TeamSettings) error

	// Auth config (singleton row)
	GetAuthConfig(ctx context.Context) (*AuthConfig, error)
	PutAuthConfig(ctx context.Context, c AuthConfig) error

	// Embedding config (singleton row, deployment-wide)
	GetEmbeddingConfig(ctx context.Context) (*EmbeddingConfig, error)
	PutEmbeddingConfig(ctx context.Context, cfg EmbeddingConfig) error

	// Enrichment preferences (per-user, keyed by EffectiveActorID). Scalars unset by
	// the user are returned NOT-set so callers can apply deployment defaults.
	GetEnrichmentPrefs(ctx context.Context, userID string) (*EnrichmentPrefs, error)
	PutEnrichmentPrefs(ctx context.Context, userID string, minRel *float64, maxMem *int, llmRewrite *bool) error
	ReplaceEnrichmentRules(ctx context.Context, userID, kind string, values []string) error
	AddEnrichmentRule(ctx context.Context, userID, kind, value string) error
	RemoveEnrichmentRule(ctx context.Context, userID, kind, value string) error

	// Activity log
	LogActivity(ctx context.Context, e ActivityEntry) error
	QueryActivity(ctx context.Context, teamID string, limit int) ([]ActivityEntry, error)
}
