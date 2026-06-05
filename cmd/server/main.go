package main

import (
	"log"

	"github.com/dsandor/memory/internal/config"
	"github.com/dsandor/memory/internal/embedding"
	internalmcp "github.com/dsandor/memory/internal/mcp"
	"github.com/dsandor/memory/internal/storage"
	"github.com/mark3labs/mcp-go/server"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("load config: %v", err)
	}

	store, err := storage.NewSQLiteStore(cfg.DBPath, cfg.EmbeddingDim)
	if err != nil {
		log.Fatalf("open storage: %v", err)
	}
	defer store.Close()

	embedder := embedding.NewOllamaEmbedder(cfg.OllamaURL, cfg.OllamaModel)

	mcpServer := internalmcp.NewMCPServer(store, embedder)

	if err := server.ServeStdio(mcpServer); err != nil {
		log.Fatalf("serve: %v", err)
	}
}
