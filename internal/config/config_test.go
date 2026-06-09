package config_test

import (
	"testing"
	"time"

	"github.com/dsandor/memory/internal/config"
)

func TestLoad_Defaults(t *testing.T) {
	t.Setenv("DATABASE_PATH", "")
	t.Setenv("OLLAMA_URL", "")
	t.Setenv("OLLAMA_MODEL", "")
	t.Setenv("TEAM_ID", "")
	t.Setenv("EMBEDDING_DIM", "")

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("Load() returned unexpected error: %v", err)
	}

	if cfg.DBPath != "knowledge.db" {
		t.Errorf("DBPath: got %q, want %q", cfg.DBPath, "knowledge.db")
	}
	if cfg.OllamaURL != "http://localhost:11434" {
		t.Errorf("OllamaURL: got %q, want %q", cfg.OllamaURL, "http://localhost:11434")
	}
	if cfg.OllamaModel != "nomic-embed-text" {
		t.Errorf("OllamaModel: got %q, want %q", cfg.OllamaModel, "nomic-embed-text")
	}
	if cfg.TeamID != "default" {
		t.Errorf("TeamID: got %q, want %q", cfg.TeamID, "default")
	}
	if cfg.EmbeddingDim != 768 {
		t.Errorf("EmbeddingDim: got %d, want 768", cfg.EmbeddingDim)
	}
}

func TestLoad_EnvOverrides(t *testing.T) {
	t.Setenv("DATABASE_PATH", "/tmp/test.db")
	t.Setenv("OLLAMA_URL", "http://myollama:11434")
	t.Setenv("OLLAMA_MODEL", "mxbai-embed-large")
	t.Setenv("TEAM_ID", "acme")
	t.Setenv("EMBEDDING_DIM", "1024")

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("Load() returned unexpected error: %v", err)
	}

	if cfg.DBPath != "/tmp/test.db" {
		t.Errorf("DBPath: got %q, want %q", cfg.DBPath, "/tmp/test.db")
	}
	if cfg.OllamaURL != "http://myollama:11434" {
		t.Errorf("OllamaURL: got %q, want %q", cfg.OllamaURL, "http://myollama:11434")
	}
	if cfg.OllamaModel != "mxbai-embed-large" {
		t.Errorf("OllamaModel: got %q, want %q", cfg.OllamaModel, "mxbai-embed-large")
	}
	if cfg.TeamID != "acme" {
		t.Errorf("TeamID: got %q, want %q", cfg.TeamID, "acme")
	}
	if cfg.EmbeddingDim != 1024 {
		t.Errorf("EmbeddingDim: got %d, want 1024", cfg.EmbeddingDim)
	}
}

func TestLoad_InvalidEmbeddingDim(t *testing.T) {
	t.Setenv("EMBEDDING_DIM", "notanumber")

	_, err := config.Load()
	if err == nil {
		t.Fatal("Load() expected an error for invalid EMBEDDING_DIM, got nil")
	}
}

func TestLoad_PipelineDefaults(t *testing.T) {
	for _, k := range []string{"PIPELINE_MIN_ENTRIES", "PIPELINE_INTERVAL", "CLUSTER_THRESHOLD", "ANTHROPIC_MODEL", "ANTHROPIC_API_KEY"} {
		t.Setenv(k, "")
	}
	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.PipelineMinEntries != 10 {
		t.Errorf("PipelineMinEntries = %d, want 10", cfg.PipelineMinEntries)
	}
	if cfg.PipelineInterval != time.Hour {
		t.Errorf("PipelineInterval = %v, want 1h", cfg.PipelineInterval)
	}
	if cfg.ClusterThreshold != 0.85 {
		t.Errorf("ClusterThreshold = %v, want 0.85", cfg.ClusterThreshold)
	}
	if cfg.AnthropicModel != "claude-haiku-4-5-20251001" {
		t.Errorf("AnthropicModel = %q, want claude-haiku-4-5-20251001", cfg.AnthropicModel)
	}
	if cfg.AnthropicAPIKey != "" {
		t.Errorf("AnthropicAPIKey should be empty when env not set")
	}
}

func TestLoad_InvalidPipelineInterval(t *testing.T) {
	t.Setenv("PIPELINE_INTERVAL", "not-a-duration")
	_, err := config.Load()
	if err == nil {
		t.Fatal("expected error for invalid PIPELINE_INTERVAL")
	}
}

func TestLoad_InvalidClusterThreshold(t *testing.T) {
	t.Setenv("CLUSTER_THRESHOLD", "1.5")
	_, err := config.Load()
	if err == nil {
		t.Fatal("expected error for CLUSTER_THRESHOLD > 1")
	}
}

func TestLoad_InvalidPipelineMinEntries(t *testing.T) {
	t.Setenv("PIPELINE_MIN_ENTRIES", "0")
	_, err := config.Load()
	if err == nil {
		t.Fatal("expected error for PIPELINE_MIN_ENTRIES=0")
	}
}

func TestLoad_AgentModelDefault(t *testing.T) {
	t.Setenv("AGENT_MODEL", "")
	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.AgentModel != "claude-sonnet-4-6" {
		t.Errorf("AgentModel = %q, want claude-sonnet-4-6", cfg.AgentModel)
	}
}

func TestHTTPAddrDefault(t *testing.T) {
	t.Setenv("HTTP_ADDR", "")
	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.HTTPAddr != ":8080" {
		t.Errorf("HTTPAddr = %q, want :8080", cfg.HTTPAddr)
	}
}

func TestConfig_NewFields(t *testing.T) {
	t.Setenv("SUPERADMIN_KEY", "test-superadmin-key")
	t.Setenv("OIDC_CLIENT_SECRET", "test-secret")
	t.Setenv("MCP_HTTP_ADDR", ":9090")
	t.Setenv("MCP_HTTP_PATH", "/mcp/v1")

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.SuperadminKey != "test-superadmin-key" {
		t.Errorf("SuperadminKey = %q, want %q", cfg.SuperadminKey, "test-superadmin-key")
	}
	if cfg.OIDCClientSecret != "test-secret" {
		t.Errorf("OIDCClientSecret = %q, want %q", cfg.OIDCClientSecret, "test-secret")
	}
	if cfg.MCPHTTPAddr != ":9090" {
		t.Errorf("MCPHTTPAddr = %q, want :9090", cfg.MCPHTTPAddr)
	}
	if cfg.MCPHTTPPath != "/mcp/v1" {
		t.Errorf("MCPHTTPPath = %q, want /mcp/v1", cfg.MCPHTTPPath)
	}
}

func TestConfig_MCPDefaults(t *testing.T) {
	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.MCPHTTPAddr != "" {
		t.Errorf("MCPHTTPAddr default should be empty, got %q", cfg.MCPHTTPAddr)
	}
	if cfg.MCPHTTPPath != "/mcp" {
		t.Errorf("MCPHTTPPath default = %q, want /mcp", cfg.MCPHTTPPath)
	}
}

func TestLoad_LogLevel(t *testing.T) {
	t.Setenv("LOG_LEVEL", "debug")
	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.LogLevel != "debug" {
		t.Errorf("LogLevel = %q, want debug", cfg.LogLevel)
	}
}

func TestLoad_LogLevel_Invalid(t *testing.T) {
	t.Setenv("LOG_LEVEL", "verbose")
	_, err := config.Load()
	if err == nil {
		t.Fatal("expected error for invalid LOG_LEVEL")
	}
}

func TestLoad_LogLevel_Default(t *testing.T) {
	t.Setenv("LOG_LEVEL", "")
	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.LogLevel != "info" {
		t.Errorf("LogLevel default = %q, want info", cfg.LogLevel)
	}
}

func TestLoad_LogLevel_AllValid(t *testing.T) {
	for _, level := range []string{"debug", "info", "warn", "error"} {
		t.Run(level, func(t *testing.T) {
			t.Setenv("LOG_LEVEL", level)
			cfg, err := config.Load()
			if err != nil {
				t.Fatalf("Load: %v", err)
			}
			if cfg.LogLevel != level {
				t.Errorf("LogLevel = %q, want %q", cfg.LogLevel, level)
			}
		})
	}
}
