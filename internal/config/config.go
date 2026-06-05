package config

import (
	"fmt"
	"os"
	"strconv"
)

type Config struct {
	DBPath       string
	OllamaURL    string
	OllamaModel  string
	TeamID       string
	EmbeddingDim int
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
	return Config{
		DBPath:       envOrDefault("DATABASE_PATH", "knowledge.db"),
		OllamaURL:    envOrDefault("OLLAMA_URL", "http://localhost:11434"),
		OllamaModel:  envOrDefault("OLLAMA_MODEL", "nomic-embed-text"),
		TeamID:       envOrDefault("TEAM_ID", "default"),
		EmbeddingDim: dim,
	}, nil
}

func envOrDefault(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
