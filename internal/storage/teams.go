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

type TeamSettings struct {
	TeamID             string
	Domains            []string // domain taxonomy labels
	ClusterThreshold   float64
	PipelineMinEntries int
	AgentModel         string
	AnthropicAPIKey    string    `json:"anthropic_api_key"`
	AnthropicModel     string    `json:"anthropic_model"`
	OllamaURL          string    `json:"ollama_url"`
	OllamaModel        string    `json:"ollama_model"`
	UpdatedAt          time.Time
}

type AuthConfig struct {
	Provider        string // "local" | "oidc"
	OIDCIssuer      string
	OIDCClientID    string
	OIDCRedirectURL string
	UpdatedAt       time.Time
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

	// Users
	UpsertUser(ctx context.Context, u User) (string, error)
	GetUserByEmail(ctx context.Context, email string) (*User, error)
	GetUserByExternalID(ctx context.Context, externalID string) (*User, error)
	ListUsers(ctx context.Context, teamID string) ([]User, error)
	AssignUserToTeam(ctx context.Context, userID, teamID, role string) error
	// ResolveTeamByEmail returns the first enabled team whose domain_patterns
	// contains a regex matching email; returns nil (no error) if none match.
	ResolveTeamByEmail(ctx context.Context, email string) (*Team, error)

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

	// Activity log
	LogActivity(ctx context.Context, e ActivityEntry) error
	QueryActivity(ctx context.Context, teamID string, limit int) ([]ActivityEntry, error)
}
