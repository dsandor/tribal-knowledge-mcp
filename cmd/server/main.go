package main

import (
	"context"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/dsandor/memory/internal/aiconfig"
	"github.com/dsandor/memory/internal/auth"
	"github.com/dsandor/memory/internal/config"
	"github.com/dsandor/memory/internal/embedding"
	"github.com/dsandor/memory/internal/live"
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

	// Single-team deployments converge legacy team-less rows onto the one
	// real team. Multi-team installs are left untouched (ownership unknown).
	{
		bfCtx := context.Background()
		if teams, err := store.ListTeams(bfCtx); err == nil && len(teams) == 1 {
			if err := store.BackfillTeamID(bfCtx, teams[0].ID); err != nil {
				slog.Error("team backfill failed", "err", err)
			} else {
				slog.Info("team backfill applied", "team", teams[0].ID)
			}
		}

		// Any run still "running" at boot belongs to a dead process — mark it
		// failed so the runs list is honest and the next interval run starts clean.
		if n, err := store.MarkInterruptedRuns(bfCtx); err != nil {
			slog.Error("mark interrupted pipeline runs failed", "err", err)
		} else if n > 0 {
			slog.Info("marked interrupted pipeline runs", "count", n)
		}
	}

	// Build the AI sources layer. Clients are resolved per call so saved team
	// settings (AI config) take effect immediately without a restart.
	envDefaults := aiconfig.EnvDefaults{
		AnthropicAPIKey: cfg.AnthropicAPIKey,
		AnthropicModel:  cfg.AnthropicModel,
		AgentModel:      cfg.AgentModel,
		OllamaURL:       cfg.OllamaURL,
		OllamaModel:     cfg.OllamaModel,
		LLMProvider:     cfg.LLMProvider,
		OllamaLLMModel:  cfg.OllamaLLMModel,
	}
	resolver := aiconfig.NewResolver(store, envDefaults)
	src := &aiconfig.Sources{
		Resolver:    resolver,
		LLM:         llm.NewProvider(),
		Embed:       embedding.NewProvider(),
		DefaultTeam: cfg.TeamID,
	}

	triggerCh := make(chan struct{}, 1)

	// Construct the live event bus and presence tracker once, shared by the
	// web server (SSE stream + producers) and, later, MCP-side producers.
	liveHub := live.NewHub()
	presence := live.NewPresence(60 * time.Second)

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
		WithTrustXFF(cfg.TrustXFF).
		WithLive(liveHub, presence).
		WithAISources(src)

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

	// Presence sweep: every 10s evict stale entries and publish presence deltas
	// to the live hub so SSE clients learn who joined or left.
	go func() {
		ticker := time.NewTicker(10 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				deltas := presence.Sweep()
				for team, delta := range deltas {
					if len(delta.Joined) == 0 && len(delta.Left) == 0 {
						continue
					}
					online := presence.OnlineCount(team, false)
					liveHub.Publish(live.LiveEvent{
						Type:   live.TypePresence,
						TeamID: team,
						Meta: map[string]string{
							"online_count": strconv.Itoa(online),
							"joined":       strconv.Itoa(len(delta.Joined)),
							"left":         strconv.Itoa(len(delta.Left)),
						},
						CreatedAt: time.Now().UTC(),
					})
				}
			case <-ctx.Done():
				return
			}
		}
	}()

	mcpServer := internalmcp.NewMCPServer(store, src, liveHub)
	internalmcp.RegisterAnalysisTools(mcpServer, store)
	internalmcp.RegisterRuleTools(mcpServer, store)
	internalmcp.RegisterAgentTools(mcpServer, store)
	internalmcp.RegisterKnowledgeExtTools(mcpServer, store, src, liveHub)
	internalmcp.RegisterUsageTools(mcpServer, store, liveHub)
	internalmcp.RegisterResources(mcpServer, store)
	internalmcp.RegisterPromptSuggest(mcpServer, store, src)
	internalmcp.RegisterEnrichContext(mcpServer, store, src, liveHub)

	// Pipeline always starts; it skips gracefully (logs) when no effective API key.
	p := pipeline.New(store, src, pipeline.Config{
		MinEntries:       cfg.PipelineMinEntries,
		Interval:         cfg.PipelineInterval,
		ClusterThreshold: cfg.ClusterThreshold,
	}).
		WithAgentGeneration(store).
		WithWeakSignalImprovement().
		WithAutoTagBackfill().
		WithLivePublish(liveHub)

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
