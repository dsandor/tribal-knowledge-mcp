package config_test

import (
	"testing"

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
