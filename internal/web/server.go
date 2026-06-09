package web

import (
	"io/fs"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	chimw "github.com/go-chi/chi/v5/middleware"

	"github.com/dsandor/memory/internal/auth"
	"github.com/dsandor/memory/internal/llm"
	"github.com/dsandor/memory/internal/storage"
)

// AllStore is the storage interface required by the HTTP API.
// *storage.SQLiteStore satisfies this.
type AllStore interface {
	storage.AgentStore
	storage.TeamStore
}

// Server wraps a chi router with REST API routes and SPA static serving.
type Server struct {
	store           AllStore
	router          *chi.Mux
	staticFS        fs.FS
	triggerPipeline chan<- struct{} // optional; set by WithPipelineTrigger
	oidcSecret      string
	devBypassAuth   bool       // skips auth middleware — development only
	rateLimitRPS    int        // 0 means disabled
	trustXFF        bool       // only enable when deployed behind a known reverse proxy
	agentLLM        llm.Client // optional; enables the /refactor endpoint
}

// NewServer wires all routes and returns a ready Server.
// staticFS should be the built React dist (typically fs.Sub of the embedded FS).
func NewServer(staticFS fs.FS, store AllStore) *Server {
	s := &Server{
		store:    store,
		router:   chi.NewRouter(),
		staticFS: staticFS,
	}
	s.routes()
	return s
}

// WithPipelineTrigger sets the channel used to manually trigger the pipeline.
func (s *Server) WithPipelineTrigger(ch chan<- struct{}) *Server {
	s.triggerPipeline = ch
	return s
}

// WithOIDCSecret sets the OIDC client secret for the OIDC callback handler.
func (s *Server) WithOIDCSecret(secret string) *Server {
	s.oidcSecret = secret
	return s
}

// WithAgentLLM sets the LLM client used by the agent refactor endpoint.
func (s *Server) WithAgentLLM(client llm.Client) *Server {
	s.agentLLM = client
	return s
}

// WithDevBypass disables auth middleware so every request runs as superadmin.
// Never enable this in production.
func (s *Server) WithDevBypass(bypass bool) *Server {
	s.devBypassAuth = bypass
	// Routes were already wired in NewServer; rewire with updated flag.
	s.router = chi.NewRouter()
	s.routes()
	return s
}

// WithRateLimitRPS enables a per-IP token bucket rate limiter at the given
// requests-per-second. A value of 0 disables rate limiting (default).
func (s *Server) WithRateLimitRPS(rps int) *Server {
	s.rateLimitRPS = rps
	s.router = chi.NewRouter()
	s.routes()
	return s
}

// WithTrustXFF opts into using X-Forwarded-For for IP extraction in the rate
// limiter. Only enable this when the server is deployed behind a known reverse
// proxy — without it, clients can spoof their IP by setting the header.
func (s *Server) WithTrustXFF(trust bool) *Server {
	s.trustXFF = trust
	s.router = chi.NewRouter()
	s.routes()
	return s
}

// effectiveAuthMW returns RequireAuth normally, or a pass-through that injects
// superadmin context when DEV_BYPASS_AUTH is enabled.
func (s *Server) effectiveAuthMW() func(http.Handler) http.Handler {
	if s.devBypassAuth {
		return func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				next.ServeHTTP(w, r.WithContext(auth.InjectSuperadmin(r.Context())))
			})
		}
	}
	return auth.RequireAuth(s.store)
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.router.ServeHTTP(w, r)
}

// maxBodySize limits POST and PUT request bodies to 1 MiB.
func maxBodySize(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost || r.Method == http.MethodPut {
			r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) routes() {
	r := s.router
	r.Use(slogRequestLogger)
	r.Use(chimw.Recoverer)
	r.Use(maxBodySize)
	if s.rateLimitRPS > 0 {
		r.Use(NewRateLimiter(s.rateLimitRPS, s.trustXFF))
	}

	// Health — public
	r.Get("/health", s.handleHealth)

	// Auth — public
	r.Post("/auth/login", s.handleLogin)
	r.Get("/auth/oidc/login", s.handleOIDCLogin)
	r.Get("/auth/oidc/callback", s.handleOIDCCallback)
	r.Post("/auth/logout", s.handleLogout)

	authMW := s.effectiveAuthMW()

	// Member routes
	r.Group(func(r chi.Router) {
		r.Use(authMW)
		r.Get("/api/stats", s.handleStats)
		r.Get("/api/knowledge", s.handleKnowledgeList)
		r.Get("/api/knowledge/export", s.handleKnowledgeExport)
		r.Post("/api/knowledge", s.handleKnowledgeStore)
		r.Get("/api/knowledge/{id}", s.handleKnowledgeGet)
		r.Put("/api/knowledge/{id}", s.handleKnowledgeUpdate)
		r.Delete("/api/knowledge/{id}", s.handleKnowledgeDelete)
		r.Put("/api/knowledge/{id}/rate", s.handleKnowledgeRate)
		r.Get("/api/clusters", s.handleClusterList)
		r.Get("/api/clusters/{id}", s.handleClusterGet)
		r.Get("/api/clusters/{id}/summary", s.handleClusterSummary)
		r.Get("/api/datasets", s.handleDatasetList)
		r.Get("/api/datasets/{id}/export", s.handleDatasetExport)
		r.Get("/api/agents", s.handleAgentList)
		r.Get("/api/agents/bulk-export", s.handleAgentBulkExport)
		r.Get("/api/agents/domain/{domain}/latest", s.handleAgentLatestByDomain)
		r.Get("/api/agents/{id}", s.handleAgentGet)
		r.Get("/api/agents/{id}/export", s.handleAgentExport)
		r.Post("/api/agents/{id}/refactor", s.handleAgentRefactor)
		r.Get("/api/pipeline/status", s.handlePipelineStatus)
		r.Get("/api/analytics/usage", s.handleUsage)
		r.Get("/api/analytics/gaps", s.handleGaps)
		r.Get("/api/analytics/contributions", s.handleContributions)
		r.Get("/api/knowledge/trending", s.handleKnowledgeTrending)
		r.Get("/api/activity", s.handleActivityFeed)
	})

	// Curator routes
	r.Group(func(r chi.Router) {
		r.Use(authMW)
		r.Use(auth.RequireCurator())
		r.Put("/api/knowledge/{id}/approve", s.handleKnowledgeApprove)
		r.Put("/api/knowledge/{id}/reject", s.handleKnowledgeReject)
		r.Post("/api/knowledge/batch-approve", s.handleBatchApprove)
		r.Post("/api/knowledge/batch-reject", s.handleBatchReject)
		r.Put("/api/agents/{id}/publish", s.handleAgentPublish)
		r.Post("/api/knowledge/import", s.handleKnowledgeImport)
	})

	// Admin routes
	r.Group(func(r chi.Router) {
		r.Use(authMW)
		r.Use(auth.RequireAdmin())
		r.Post("/api/pipeline/trigger", s.handlePipelineTrigger)
		r.Get("/api/pipeline/runs", s.handleListPipelineRuns)
		r.Get("/api/api-keys", s.handleListAPIKeys)
		r.Post("/api/api-keys", s.handleCreateAPIKey)
		r.Delete("/api/api-keys/{id}", s.handleRevokeAPIKey)
		r.Get("/api/users", s.handleListUsers)
		r.Post("/api/users", s.handleAssignUser)
		r.Put("/api/users/{id}/role", s.handleSetUserRole)
		r.Get("/api/settings", s.handleGetSettings)
		r.Put("/api/settings", s.handlePutSettings)
	})

	// Superadmin routes
	r.Group(func(r chi.Router) {
		r.Use(authMW)
		r.Use(auth.RequireSuperadmin())
		r.Get("/api/admin/teams", s.handleListTeams)
		r.Post("/api/admin/teams", s.handleCreateTeam)
		r.Put("/api/admin/teams/{id}", s.handleUpdateTeam)
		r.Put("/api/admin/teams/{id}/enabled", s.handleSetTeamEnabled)
		r.Delete("/api/admin/teams/{id}", s.handleDeleteTeam)
		r.Get("/api/admin/teams/{id}/users", s.handleListTeamUsers)
		r.Get("/api/admin/users", s.handleAdminListAllUsers)
		r.Put("/api/admin/users/{id}/team", s.handleAdminAssignUserTeam)
		r.Get("/api/admin/auth-config", s.handleGetAuthConfig)
		r.Put("/api/admin/auth-config", s.handlePutAuthConfig)
	})

	// SPA fallback
	r.Get("/*", s.handleStatic)
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	type componentStatus struct {
		OK    bool   `json:"ok"`
		Error string `json:"error,omitempty"`
	}
	type healthResponse struct {
		Status     string                     `json:"status"`
		Components map[string]componentStatus `json:"components"`
	}

	ctx := r.Context()
	components := map[string]componentStatus{}
	allOK := true

	// Storage ping
	if err := s.store.Ping(ctx); err != nil {
		components["storage"] = componentStatus{OK: false, Error: err.Error()}
		allOK = false
	} else {
		components["storage"] = componentStatus{OK: true}
	}

	status := "ok"
	if !allOK {
		status = "degraded"
	}
	if !allOK {
		w.WriteHeader(http.StatusServiceUnavailable)
	}
	writeJSON(w, healthResponse{Status: status, Components: components})
}

func (s *Server) handleStatic(w http.ResponseWriter, r *http.Request) {
	if strings.HasPrefix(r.URL.Path, "/api/") || strings.HasPrefix(r.URL.Path, "/auth/") {
		http.NotFound(w, r)
		return
	}
	path := strings.TrimPrefix(r.URL.Path, "/")
	if path == "" {
		path = "index.html"
	}
	if s.staticFS == nil {
		http.NotFound(w, r)
		return
	}
	_, err := s.staticFS.Open(path)
	if err != nil {
		http.ServeFileFS(w, r, s.staticFS, "index.html")
		return
	}
	http.FileServerFS(s.staticFS).ServeHTTP(w, r)
}

// slogRequestLogger is a chi middleware that logs HTTP requests via slog (stderr).
// It replaces chimw.Logger which writes to os.Stdout and would corrupt the MCP stdio transport.
func slogRequestLogger(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ww := chimw.NewWrapResponseWriter(w, r.ProtoMajor)
		start := time.Now()
		defer func() {
			slog.Info("http",
				"method", r.Method,
				"path", r.URL.Path,
				"status", ww.Status(),
				"bytes", ww.BytesWritten(),
				"duration_ms", time.Since(start).Milliseconds(),
			)
		}()
		next.ServeHTTP(ww, r)
	})
}
