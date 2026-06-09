package config

import (
	"fmt"
	"os"
	"strconv"
	"time"
)

type Config struct {
	DBPath             string
	OllamaURL          string
	OllamaModel        string
	TeamID             string
	EmbeddingDim       int
	AnthropicAPIKey    string
	AnthropicModel     string
	AgentModel         string
	PipelineInterval   time.Duration
	PipelineMinEntries int
	ClusterThreshold   float64
	HTTPAddr           string
	SuperadminKey      string
	OIDCClientSecret   string
	MCPHTTPAddr        string
	MCPHTTPPath        string
	DatabaseURL        string // DATABASE_URL — if non-empty, uses PostgreSQL instead of SQLite
	DevBypassAuth      bool   // DEV_BYPASS_AUTH=true — skip auth middleware (never use in production)
	LogLevel           string // debug | info | warn | error  (default: info)
	RateLimitRPS       int    // RATE_LIMIT_RPS — per-IP token bucket limit (default: 60)
	TrustXFF           bool   // TRUST_XFF=true — honor X-Forwarded-For for rate limiting (only set when behind a known reverse proxy)
}

func Load() (Config, error) {
	dim := 768
	if v := os.Getenv("EMBEDDING_DIM"); v != "" {
		parsed, err := strconv.Atoi(v)
		if err != nil {
			return Config{}, fmt.Errorf("invalid EMBEDDING_DIM %q: must be a positive integer", v)
		}
		if parsed <= 0 {
			return Config{}, fmt.Errorf("invalid EMBEDDING_DIM %d: must be positive", parsed)
		}
		dim = parsed
	}

	minEntries := 10
	if v := os.Getenv("PIPELINE_MIN_ENTRIES"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n <= 0 {
			return Config{}, fmt.Errorf("invalid PIPELINE_MIN_ENTRIES %q: must be a positive integer", v)
		}
		minEntries = n
	}

	interval := time.Hour
	if v := os.Getenv("PIPELINE_INTERVAL"); v != "" {
		d, err := time.ParseDuration(v)
		if err != nil || d <= 0 {
			return Config{}, fmt.Errorf("invalid PIPELINE_INTERVAL %q: must be a positive duration (e.g. 30m, 2h)", v)
		}
		interval = d
	}

	clusterThresh := 0.85
	if v := os.Getenv("CLUSTER_THRESHOLD"); v != "" {
		f, err := strconv.ParseFloat(v, 64)
		if err != nil || f <= 0 || f > 1 {
			return Config{}, fmt.Errorf("invalid CLUSTER_THRESHOLD %q: must be a float in (0,1]", v)
		}
		clusterThresh = f
	}

	logLevel := envOrDefault("LOG_LEVEL", "info")
	switch logLevel {
	case "debug", "info", "warn", "error":
		// valid
	default:
		return Config{}, fmt.Errorf("invalid LOG_LEVEL %q: must be debug, info, warn, or error", logLevel)
	}

	rateLimitRPS := 60
	if v := os.Getenv("RATE_LIMIT_RPS"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n <= 0 {
			return Config{}, fmt.Errorf("invalid RATE_LIMIT_RPS %q: must be a positive integer", v)
		}
		rateLimitRPS = n
	}

	return Config{
		DBPath:             envOrDefault("DATABASE_PATH", "knowledge.db"),
		OllamaURL:          envOrDefault("OLLAMA_URL", "http://localhost:11434"),
		OllamaModel:        envOrDefault("OLLAMA_MODEL", "nomic-embed-text"),
		TeamID:             envOrDefault("TEAM_ID", "default"),
		EmbeddingDim:       dim,
		AnthropicAPIKey:    os.Getenv("ANTHROPIC_API_KEY"),
		AnthropicModel:     envOrDefault("ANTHROPIC_MODEL", "claude-haiku-4-5-20251001"),
		AgentModel:         envOrDefault("AGENT_MODEL", "claude-sonnet-4-6"),
		PipelineInterval:   interval,
		PipelineMinEntries: minEntries,
		ClusterThreshold:   clusterThresh,
		HTTPAddr:           envOrDefault("HTTP_ADDR", ":8080"),
		SuperadminKey:      os.Getenv("SUPERADMIN_KEY"),
		OIDCClientSecret:   os.Getenv("OIDC_CLIENT_SECRET"),
		MCPHTTPAddr:        os.Getenv("MCP_HTTP_ADDR"),
		MCPHTTPPath:        envOrDefault("MCP_HTTP_PATH", "/mcp"),
		DatabaseURL:        os.Getenv("DATABASE_URL"),
		DevBypassAuth:      os.Getenv("DEV_BYPASS_AUTH") == "true",
		LogLevel:           logLevel,
		RateLimitRPS:       rateLimitRPS,
		TrustXFF:           os.Getenv("TRUST_XFF") == "true",
	}, nil
}

func envOrDefault(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
