package main

import (
	"context"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/dsandor/memory/internal/auth"
	"github.com/dsandor/memory/internal/config"
	"github.com/dsandor/memory/internal/embedding"
	"github.com/dsandor/memory/internal/llm"
	internalmcp "github.com/dsandor/memory/internal/mcp"
	"github.com/dsandor/memory/internal/pipeline"
	"github.com/dsandor/memory/internal/storage"
	"github.com/dsandor/memory/internal/web"
	"github.com/joho/godotenv"
	"github.com/mark3labs/mcp-go/server"
)

// combinedStore is the full storage interface satisfied by both SQLiteStore and PostgresStore.
type combinedStore interface {
	storage.AgentStore
	storage.TeamStore
	storage.RuleStore
}

func main() {
	// Load a .env file (if present) before reading configuration, so the documented
	// .env workflow actually populates the process environment. Real environment
	// variables always win: godotenv.Load does not override variables already set.
	// Override the file location with DOTENV_PATH if needed.
	envFile := os.Getenv("DOTENV_PATH")
	if envFile == "" {
		envFile = ".env"
	}
	var dotenvLoaded bool
	var dotenvErr error
	if _, statErr := os.Stat(envFile); statErr == nil {
		if dotenvErr = godotenv.Load(envFile); dotenvErr == nil {
			dotenvLoaded = true
		}
	}

	cfg, err := config.Load()
	if err != nil {
		slog.Error("load config", "err", err)
		os.Exit(1)
	}

	// Set up structured JSON logger based on configured log level.
	var logLevel slog.Level
	switch cfg.LogLevel {
	case "debug":
		logLevel = slog.LevelDebug
	case "warn":
		logLevel = slog.LevelWarn
	case "error":
		logLevel = slog.LevelError
	default:
		logLevel = slog.LevelInfo
	}
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: logLevel})))

	// Report .env loading now that the logger is configured.
	switch {
	case dotenvErr != nil:
		slog.Warn("failed to load .env file", "path", envFile, "err", dotenvErr)
	case dotenvLoaded:
		slog.Info("loaded environment from .env file", "path", envFile)
	}

	var store combinedStore
	if cfg.DatabaseURL != "" {
		pgStore, err := storage.NewPostgresStore(cfg.DatabaseURL, cfg.EmbeddingDim)
		if err != nil {
			slog.Error("open postgres storage", "err", err)
			os.Exit(1)
		}
		defer pgStore.Close()
		store = pgStore
		slog.Info("using PostgreSQL storage")
	} else {
		sqliteStore, err := storage.NewSQLiteStore(cfg.DBPath, cfg.EmbeddingDim)
		if err != nil {
			slog.Error("open sqlite storage", "err", err)
			os.Exit(1)
		}
		defer sqliteStore.Close()
		store = sqliteStore
		slog.Info("using SQLite storage", "path", cfg.DBPath)
	}

	if cfg.SuperadminKey != "" {
		bootstrapSuperadmin(store, cfg.SuperadminKey)
	}

	embedder := embedding.NewOllamaEmbedder(cfg.OllamaURL, cfg.OllamaModel)

	triggerCh := make(chan struct{}, 1)

	staticFS, err := fs.Sub(web.DistFS, "dist")
	if err != nil {
		slog.Error("sub embedded fs", "err", err)
		os.Exit(1)
	}
	webServer := web.NewServer(staticFS, store).
		WithOIDCSecret(cfg.OIDCClientSecret).
		WithPipelineTrigger(triggerCh).
		WithDevBypass(cfg.DevBypassAuth).
		WithRateLimitRPS(cfg.RateLimitRPS).
		WithTrustXFF(cfg.TrustXFF)
	if cfg.AnthropicAPIKey != "" {
		webServer = webServer.WithAgentLLM(llm.NewAnthropicClient(cfg.AnthropicAPIKey, cfg.AgentModel))
	}

	httpServer := &http.Server{
		Addr:    cfg.HTTPAddr,
		Handler: webServer,
	}
	go func() {
		slog.Info("HTTP server listening", "addr", cfg.HTTPAddr)
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("HTTP server error", "err", err)
		}
	}()

	// Signal-aware context for graceful shutdown
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	mcpServer := internalmcp.NewMCPServer(store, embedder)
	internalmcp.RegisterAnalysisTools(mcpServer, store)
	internalmcp.RegisterRuleTools(mcpServer, store)
	internalmcp.RegisterAgentTools(mcpServer, store)
	internalmcp.RegisterKnowledgeExtTools(mcpServer, store, embedder)
	internalmcp.RegisterUsageTools(mcpServer, store)
	internalmcp.RegisterResources(mcpServer, store)

	var llmClient llm.Client
	if cfg.AnthropicAPIKey != "" {
		llmClient = llm.NewAnthropicClient(cfg.AnthropicAPIKey, cfg.AnthropicModel)
		agentLLMClient := llm.NewAnthropicClient(cfg.AnthropicAPIKey, cfg.AgentModel)
		internalmcp.RegisterPromptSuggest(mcpServer, store, embedder, llmClient)

		improvementLLMClient := llm.NewAnthropicClient(cfg.AnthropicAPIKey, "claude-haiku-4-5-20251001")

		p := pipeline.New(store, llmClient, pipeline.Config{
			MinEntries:       cfg.PipelineMinEntries,
			Interval:         cfg.PipelineInterval,
			ClusterThreshold: cfg.ClusterThreshold,
		}).
			WithAgentGeneration(store, agentLLMClient).
			WithWeakSignalImprovement(improvementLLMClient, cfg.TeamID)

		p.Start(ctx)

		go func() {
			for {
				select {
				case <-triggerCh:
					p.TriggerNow()
				case <-ctx.Done():
					return
				}
			}
		}()
	} else {
		internalmcp.RegisterPromptSuggest(mcpServer, store, embedder, nil)
	}
	internalmcp.RegisterEnrichContext(mcpServer, store, embedder, llmClient)

	if cfg.MCPHTTPAddr != "" {
		internalmcp.StartRemoteMCP(mcpServer, cfg.MCPHTTPAddr, cfg.MCPHTTPPath, store)
	}

	// Run MCP stdio in a goroutine so signal handling is not blocked.
	mcpDone := make(chan struct{})
	go func() {
		defer close(mcpDone)
		if err := server.ServeStdio(mcpServer); err != nil {
			slog.Error("MCP stdio serve error", "err", err)
		}
	}()

	// Block until signal or MCP exit.
	select {
	case <-ctx.Done():
		slog.Info("shutdown signal received")
	case <-mcpDone:
		slog.Info("MCP stdio transport closed")
	}

	// Shutdown HTTP server with a 15-second drain window.
	shutCtx, shutCancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer shutCancel()
	slog.Info("draining HTTP server", "timeout_s", 15)
	if err := httpServer.Shutdown(shutCtx); err != nil {
		slog.Warn("HTTP shutdown incomplete", "err", err)
	} else {
		slog.Info("HTTP server stopped cleanly")
	}

	// Wait for MCP goroutine if it hasn't finished yet.
	<-mcpDone
	slog.Info("server exit")
}

func bootstrapSuperadmin(store storage.TeamStore, rawKey string) {
	ctx := context.Background()
	hash := auth.HashSHA256(rawKey)
	existing, _ := store.GetAPIKeyByHash(ctx, hash)
	if existing != nil {
		return
	}
	err := store.CreateAPIKey(ctx, storage.APIKey{
		KeyType: storage.APIKeyTypeTeam,
		Name:    "superadmin-bootstrap",
		KeyHash: hash,
		Role:    "superadmin",
	})
	if err != nil {
		slog.Warn("failed to bootstrap superadmin key", "err", err)
		return
	}
	slog.Info("superadmin API key bootstrapped")
}
