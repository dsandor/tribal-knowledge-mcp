package web

import (
	"archive/zip"
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	agentpkg "github.com/dsandor/memory/internal/agent"
	"github.com/dsandor/memory/internal/auth"
	"github.com/dsandor/memory/internal/embedding"
	"github.com/dsandor/memory/internal/live"
	"github.com/dsandor/memory/internal/storage"
	tagspkg "github.com/dsandor/memory/internal/tags"
	"github.com/dsandor/memory/internal/visibility"
)

// publishLive publishes a live event to the hub if one is configured.
// It is a no-op when the hub is nil. The event's ID is set to a new UUID if
// empty, and CreatedAt is set to now (UTC) if zero.
func (s *Server) publishLive(ev live.LiveEvent) {
	if s.hub == nil {
		return
	}
	if ev.ID == "" {
		ev.ID = uuid.NewString()
	}
	if ev.CreatedAt.IsZero() {
		ev.CreatedAt = time.Now().UTC()
	}
	s.hub.Publish(ev)
}

// StatsResponse is the JSON shape returned by GET /api/stats.
type StatsResponse struct {
	KnowledgeCount  int     `json:"knowledge_count"`
	ClusterCount    int     `json:"cluster_count"`
	AgentCount      int     `json:"agent_count"`
	PipelineStatus  string  `json:"pipeline_status"`
	PipelineLastRun *string `json:"pipeline_last_run"`
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

func (s *Server) handleStats(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	tc := auth.GetTeamContext(ctx)

	count, err := s.store.CountEntries(ctx, tc.ListScopeTeamID())
	if err != nil {
		writeError(w, 500, "internal_error", fmt.Sprintf("count entries: %v", err))
		return
	}
	clusters, err := s.store.ListClusters(ctx, tc.ListScopeTeamID())
	if err != nil {
		writeError(w, 500, "internal_error", fmt.Sprintf("list clusters: %v", err))
		return
	}
	agents, err := s.store.ListAgents(ctx, tc.ListScopeTeamID())
	if err != nil {
		writeError(w, 500, "internal_error", fmt.Sprintf("list agents: %v", err))
		return
	}
	run, err := s.store.GetLatestPipelineRun(ctx, tc.ListScopeTeamID())
	if err != nil {
		writeError(w, 500, "internal_error", fmt.Sprintf("get pipeline run: %v", err))
		return
	}

	resp := StatsResponse{
		KnowledgeCount: count,
		ClusterCount:   len(clusters),
		AgentCount:     len(agents),
		PipelineStatus: "idle",
	}
	if run != nil {
		resp.PipelineStatus = run.Status
		t := run.StartedAt.Format("2006-01-02T15:04:05Z")
		resp.PipelineLastRun = &t
	}
	writeJSON(w, resp)
}

// callerVisibility builds the calling user's compiled suppression RuleSet.
// It keys off a stable effective actor identity (user id, else API key id, else
// "local") so the per-user filter works for team-scoped keys and dev/no-auth
// setups, not only session/user tokens. It returns a zero (no-op) RuleSet —
// which hides nothing — when the rules cannot be loaded. Owner identities
// (id/email/name) are included so a user's own entries are never hidden.
func (s *Server) callerVisibility(ctx context.Context, tc auth.TeamContext) visibility.RuleSet {
	actorID := tc.EffectiveActorID()
	rules, err := s.store.ListVisibilityRules(ctx, actorID)
	if err != nil {
		slog.Warn("visibility filter failing open: could not load rules",
			"actor_id", actorID, "error", err)
		return visibility.RuleSet{}
	}
	identities := []string{actorID}
	// When the actor is a real user, include their email/name so their own
	// entries are exempt. For key-id / "local" actors GetUserByID fails and we
	// fall back to the actor id alone.
	if u, err := s.store.GetUserByID(ctx, actorID); err == nil && u != nil {
		identities = append(identities, u.Email, u.Name)
	}
	return visibility.Compile(rules, identities...)
}

func (s *Server) handleKnowledgeList(w http.ResponseWriter, r *http.Request) {
	tc := auth.GetTeamContext(r.Context())
	q := r.URL.Query()
	limit, _ := strconv.Atoi(q.Get("limit"))
	if limit == 0 {
		limit = 20
	}
	offset, _ := strconv.Atoi(q.Get("offset"))

	filter := storage.ListFilter{
		Domain: q.Get("domain"),
		Status: q.Get("status"),
		Type:   storage.KnowledgeType(q.Get("type")),
		Tag:    q.Get("tag"),
		Limit:  limit,
		Offset: offset,
		Search: q.Get("search"),
		TeamID: tc.ListScopeTeamID(),
	}
	entries, err := s.store.ListEntries(r.Context(), filter)
	if err != nil {
		writeError(w, 500, "internal_error", fmt.Sprintf("list entries: %v", err))
		return
	}
	// Per-user suppression (no-op when the caller is not user-scoped).
	entries = visibility.FilterEntries(s.callerVisibility(r.Context(), tc), entries)
	if entries == nil {
		entries = []storage.KnowledgeEntry{}
	}
	writeJSON(w, entries)
}

// fetchEntryForTeam loads an entry and enforces team access for the caller.
// It writes the appropriate 404/500/403 response and returns ok=false when the
// caller may not proceed. Entry TeamID is immutable after creation, so there is
// no TOCTOU window between this check and a subsequent mutation.
func (s *Server) fetchEntryForTeam(w http.ResponseWriter, r *http.Request, id string) (*storage.KnowledgeEntry, bool) {
	entry, err := s.store.GetEntry(r.Context(), id)
	if err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			writeError(w, 404, "not_found", "entry not found")
			return nil, false
		}
		writeError(w, 500, "internal_error", fmt.Sprintf("get entry: %v", err))
		return nil, false
	}
	tc := auth.GetTeamContext(r.Context())
	if !auth.CanAccess(tc, entry.TeamID) {
		writeError(w, 403, "forbidden", "entry belongs to another team")
		return nil, false
	}
	return entry, true
}

func (s *Server) handleKnowledgeGet(w http.ResponseWriter, r *http.Request) {
	entry, ok := s.fetchEntryForTeam(w, r, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	writeJSON(w, entry)
}

func (s *Server) handleKnowledgeRate(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	// Access check before body decode so cross-team probers get 403, not 400.
	if _, ok := s.fetchEntryForTeam(w, r, id); !ok {
		return
	}
	var body struct {
		Rating float64 `json:"rating"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, 400, "bad_request", "invalid JSON body")
		return
	}
	if body.Rating < 0 || body.Rating > 5 {
		writeError(w, 400, "bad_request", "rating must be between 0 and 5")
		return
	}
	if err := s.store.RateEntry(r.Context(), id, body.Rating); err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			writeError(w, 404, "not_found", "entry not found")
			return
		}
		writeError(w, 500, "internal_error", fmt.Sprintf("rate entry: %v", err))
		return
	}
	writeJSON(w, map[string]any{"ok": true})
}

func (s *Server) handleClusterList(w http.ResponseWriter, r *http.Request) {
	tc := auth.GetTeamContext(r.Context())
	clusters, err := s.store.ListClusters(r.Context(), tc.ListScopeTeamID())
	if err != nil {
		writeError(w, 500, "internal_error", fmt.Sprintf("list clusters: %v", err))
		return
	}
	if clusters == nil {
		clusters = []storage.Cluster{}
	}
	writeJSON(w, clusters)
}

func (s *Server) handleDatasetList(w http.ResponseWriter, r *http.Request) {
	tc := auth.GetTeamContext(r.Context())
	snaps, err := s.store.ListSnapshots(r.Context(), tc.ListScopeTeamID())
	if err != nil {
		writeError(w, 500, "internal_error", fmt.Sprintf("list snapshots: %v", err))
		return
	}
	if snaps == nil {
		snaps = []storage.DatasetSnapshot{}
	}
	writeJSON(w, snaps)
}

func (s *Server) handleDatasetExport(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	format := r.URL.Query().Get("format")
	if format == "" {
		format = "json"
	}

	// Fetch globally so we can distinguish 404 from 403.
	snaps, err := s.store.ListSnapshots(r.Context(), "")
	if err != nil {
		writeError(w, 500, "internal_error", fmt.Sprintf("list snapshots: %v", err))
		return
	}
	var snap *storage.DatasetSnapshot
	for i := range snaps {
		if snaps[i].ID == id {
			snap = &snaps[i]
			break
		}
	}
	if snap == nil {
		writeError(w, 404, "not_found", "snapshot not found")
		return
	}
	tc := auth.GetTeamContext(r.Context())
	if !auth.CanAccess(tc, snap.TeamID) {
		writeError(w, 403, "forbidden", "snapshot belongs to another team")
		return
	}

	switch format {
	case "json":
		w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="snapshot-v%d.json"`, snap.Version))
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, snap.Data)
	case "csv":
		w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="snapshot-v%d.csv"`, snap.Version))
		w.Header().Set("Content-Type", "text/csv")
		cw := csv.NewWriter(w)
		_ = cw.Write([]string{"id", "version", "cluster_count", "entry_count", "pipeline_run_id", "created_at"})
		_ = cw.Write([]string{
			snap.ID, strconv.Itoa(snap.Version),
			strconv.Itoa(snap.ClusterCount), strconv.Itoa(snap.EntryCount),
			snap.PipelineRunID, snap.CreatedAt.Format("2006-01-02T15:04:05Z"),
		})
		cw.Flush()
	default:
		writeError(w, 400, "bad_request", "format must be json or csv")
	}
}

func (s *Server) handleAgentList(w http.ResponseWriter, r *http.Request) {
	tc := auth.GetTeamContext(r.Context())
	agents, err := s.store.ListAgents(r.Context(), tc.ListScopeTeamID())
	if err != nil {
		writeError(w, 500, "internal_error", fmt.Sprintf("list agents: %v", err))
		return
	}
	if agents == nil {
		agents = []storage.Agent{}
	}
	writeJSON(w, agents)
}

func (s *Server) handleAgentGet(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := chi.URLParam(r, "id")

	a, err := s.store.GetAgent(ctx, id)
	if err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			writeError(w, 404, "not_found", "agent not found")
			return
		}
		writeError(w, 500, "internal_error", fmt.Sprintf("get agent: %v", err))
		return
	}
	tc := auth.GetTeamContext(ctx)
	if !auth.CanAccess(tc, a.TeamID) {
		writeError(w, 403, "forbidden", "agent belongs to another team")
		return
	}
	versions, err := s.store.ListAgentVersions(ctx, id)
	if err != nil {
		writeError(w, 500, "internal_error", fmt.Sprintf("list agent versions: %v", err))
		return
	}
	if versions == nil {
		versions = []storage.AgentVersion{}
	}
	writeJSON(w, map[string]any{"agent": a, "versions": versions})
}

func (s *Server) handleAgentPublish(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := chi.URLParam(r, "id")

	a, err := s.store.GetAgent(ctx, id)
	if err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			writeError(w, 404, "not_found", "agent not found")
			return
		}
		writeError(w, 500, "internal_error", fmt.Sprintf("get agent: %v", err))
		return
	}
	tc := auth.GetTeamContext(ctx)
	if !auth.CanAccess(tc, a.TeamID) {
		writeError(w, 403, "forbidden", "agent belongs to another team")
		return
	}
	if err := s.store.PublishAgent(ctx, id); err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			writeError(w, 404, "not_found", "agent not found")
			return
		}
		writeError(w, 500, "internal_error", fmt.Sprintf("publish agent: %v", err))
		return
	}
	writeJSON(w, map[string]any{"ok": true})
}

func (s *Server) handleAgentExport(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := chi.URLParam(r, "id")
	format := r.URL.Query().Get("format")
	if format == "" {
		format = "md"
	}

	a, err := s.store.GetAgent(ctx, id)
	if err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			writeError(w, 404, "not_found", "agent not found")
			return
		}
		writeError(w, 500, "internal_error", fmt.Sprintf("get agent: %v", err))
		return
	}
	tc := auth.GetTeamContext(ctx)
	if !auth.CanAccess(tc, a.TeamID) {
		writeError(w, 403, "forbidden", "agent belongs to another team")
		return
	}

	var contentType, ext string
	switch format {
	case "md":
		contentType, ext = "text/markdown", "md"
	case "txt":
		contentType, ext = "text/plain", "txt"
	case "json":
		contentType, ext = "application/json", "json"
	default:
		writeError(w, 400, "bad_request", "format must be md, txt, or json")
		return
	}
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s-agent.%s"`, a.Domain, ext))
	fmt.Fprint(w, agentpkg.Export(a, format))
}

func (s *Server) handleAgentRefactor(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := chi.URLParam(r, "id")

	var body struct {
		Feedback string `json:"feedback"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || len([]rune(body.Feedback)) < 5 {
		writeError(w, 400, "bad_request", "feedback must be at least 5 characters")
		return
	}

	current, err := s.store.GetAgent(ctx, id)
	if err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			writeError(w, 404, "not_found", "agent not found")
			return
		}
		writeError(w, 500, "internal_error", fmt.Sprintf("get agent: %v", err))
		return
	}

	// Resolve agent LLM per request so saved team settings take effect immediately.
	tc := auth.GetTeamContext(ctx)
	if !auth.CanAccess(tc, current.TeamID) {
		writeError(w, 403, "forbidden", "agent belongs to another team")
		return
	}

	if s.aiSrc == nil {
		writeError(w, 503, "no_llm", "LLM not configured — set ANTHROPIC_API_KEY to enable agent refactoring")
		return
	}
	agentLLM := s.aiSrc.AgentLLM(ctx, tc.TeamID)
	if agentLLM == nil {
		writeError(w, 503, "no_llm", "LLM not configured — set ANTHROPIC_API_KEY to enable agent refactoring")
		return
	}

	// Load relevant knowledge entries for context (same domain, up to 20, scoped to team).
	entries, _ := s.store.ListEntries(ctx, storage.ListFilter{Domain: current.Domain, Limit: 20, TeamID: tc.ListScopeTeamID()})

	revised, err := agentpkg.Refactor(ctx, agentLLM, current, entries, body.Feedback)
	if err != nil {
		writeError(w, 500, "llm_error", fmt.Sprintf("refactor failed: %v", err))
		return
	}

	newID, err := s.store.UpsertAgent(ctx, *revised)
	if err != nil {
		writeError(w, 500, "internal_error", fmt.Sprintf("save agent: %v", err))
		return
	}

	_ = s.store.StoreAgentVersion(ctx, storage.AgentVersion{
		AgentID:      newID,
		Version:      revised.Version,
		SystemPrompt: revised.SystemPrompt,
		Instructions: revised.Instructions,
		AntiPatterns: revised.AntiPatterns,
		Changelog:    fmt.Sprintf("User feedback: %s", body.Feedback),
	})

	result, err := s.store.GetAgent(ctx, newID)
	if err != nil {
		writeError(w, 500, "internal_error", "fetch revised agent failed")
		return
	}
	writeJSON(w, map[string]any{"agent": result})
}

// handleAgentRename renames an agent's domain, cascading the new domain across
// the team's entries, clusters, and agents in a single transaction.
func (s *Server) handleAgentRename(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := chi.URLParam(r, "id")

	var body struct {
		NewDomain string `json:"new_domain"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, 400, "bad_request", "invalid JSON body")
		return
	}
	newDomain := strings.TrimSpace(body.NewDomain)
	if newDomain == "" {
		writeError(w, 400, "bad_request", "new_domain is required")
		return
	}
	if len([]rune(newDomain)) > 100 {
		writeError(w, 400, "bad_request", "new_domain must be 100 characters or fewer")
		return
	}

	a, err := s.store.GetAgent(ctx, id)
	if err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			writeError(w, 404, "not_found", "agent not found")
			return
		}
		writeError(w, 500, "internal_error", fmt.Sprintf("get agent: %v", err))
		return
	}
	if a == nil {
		writeError(w, 404, "not_found", "agent not found")
		return
	}

	tc := auth.GetTeamContext(ctx)
	if !auth.CanAccess(tc, a.TeamID) {
		writeError(w, 403, "forbidden", "agent belongs to another team")
		return
	}

	if newDomain == a.Domain {
		writeError(w, 400, "bad_request", "new_domain must differ from the current domain")
		return
	}

	res, err := s.store.RenameDomain(ctx, a.TeamID, a.Domain, newDomain)
	if err != nil {
		if errors.Is(err, storage.ErrDomainExists) {
			writeError(w, 409, "domain_exists", fmt.Sprintf("an agent already uses the domain %q", newDomain))
			return
		}
		writeError(w, 500, "internal_error", fmt.Sprintf("rename domain: %v", err))
		return
	}

	writeJSON(w, map[string]any{
		"ok":         true,
		"old_domain": a.Domain,
		"new_domain": newDomain,
		"updated":    res,
	})
}

func (s *Server) handleAgentBulkExport(w http.ResponseWriter, r *http.Request) {
	agents, err := s.store.ListAgents(r.Context(), "")
	if err != nil {
		writeError(w, 500, "internal_error", fmt.Sprintf("list agents: %v", err))
		return
	}
	tc := auth.GetTeamContext(r.Context())
	visible := agents[:0]
	for _, a := range agents {
		if auth.CanAccess(tc, a.TeamID) {
			visible = append(visible, a)
		}
	}
	agents = visible

	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", `attachment; filename="agents-export.zip"`)

	zw := zip.NewWriter(w)
	for i := range agents {
		for _, format := range []string{"md", "txt", "json"} {
			f, err := zw.Create(fmt.Sprintf("%s/agent.%s", agents[i].Domain, format))
			if err != nil {
				continue
			}
			fmt.Fprint(f, agentpkg.Export(&agents[i], format))
		}
	}
	_ = zw.Close()
}

func (s *Server) handlePipelineStatus(w http.ResponseWriter, r *http.Request) {
	tc := auth.GetTeamContext(r.Context())
	run, err := s.store.GetLatestPipelineRun(r.Context(), tc.ListScopeTeamID())
	if err != nil {
		writeError(w, 500, "internal_error", fmt.Sprintf("get pipeline run: %v", err))
		return
	}
	if run == nil {
		writeJSON(w, map[string]any{"status": "idle"})
		return
	}
	writeJSON(w, run)
}

func (s *Server) handleListPipelineRuns(w http.ResponseWriter, r *http.Request) {
	tc := auth.GetTeamContext(r.Context())
	limitStr := r.URL.Query().Get("limit")
	limit := 20
	if n, err := strconv.Atoi(limitStr); err == nil && n > 0 {
		limit = n
	}
	runs, err := s.store.ListPipelineRuns(r.Context(), tc.ListScopeTeamID(), limit)
	if err != nil {
		writeError(w, 500, "internal_error", fmt.Sprintf("list pipeline runs: %v", err))
		return
	}
	if runs == nil {
		runs = []storage.PipelineRun{}
	}
	writeJSON(w, runs)
}

func (s *Server) handleKnowledgeStore(w http.ResponseWriter, r *http.Request) {
	tc := auth.GetTeamContext(r.Context())
	var body struct {
		Title       string   `json:"title"`
		Content     string   `json:"content"`
		Type        string   `json:"type"`
		Domain      string   `json:"domain"`
		Description string   `json:"description"`
		Author      string   `json:"author"`
		Tags        []string `json:"tags"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, 400, "bad_request", "invalid JSON body")
		return
	}
	if body.Title == "" || body.Content == "" || body.Type == "" {
		writeError(w, 400, "bad_request", "title, content, and type are required")
		return
	}
	// Resolve the team this entry should land in. A superadmin in see-all mode
	// (empty tc.TeamID, no X-Team-Id) falls back to their home team rather than
	// writing a team-less record.
	home := ""
	if tc.UserID != "" {
		if u, err := s.store.GetUserByID(r.Context(), tc.UserID); err == nil {
			home = u.TeamID
		}
	}
	target := tc.WriteTargetTeamID(home)
	entry := storage.KnowledgeEntry{
		Type:        storage.KnowledgeType(body.Type),
		Title:       body.Title,
		Content:     body.Content,
		Description: body.Description,
		Domain:      body.Domain,
		Author:      body.Author,
		Team:        target,
		TeamID:      target,
		Tags:        tagspkg.Merge(body.Tags, tagspkg.ExtractHashtags(body.Title+" "+body.Content)),
		Status:      "pending",
	}
	id, err := s.store.StoreEntry(r.Context(), entry, nil)
	if err != nil {
		writeError(w, 500, "internal_error", fmt.Sprintf("store entry: %v", err))
		return
	}

	// Fire-and-forget auto-categorization; s.aiSrc is optional.
	if s.aiSrc != nil {
		entry.ID = id
		tagger := &tagspkg.AutoTagger{Store: s.store, LLMFor: s.aiSrc.ImprovementLLM}
		tagger.TagEntryAsync(r.Context(), entry, target)
	}

	// Publish a live event so connected SSE clients see the new entry in real time.
	actorID := tc.UserID
	if actorID == "" {
		actorID = tc.KeyID
	}
	s.publishLive(live.LiveEvent{
		Type:    live.TypeKnowledgeStored,
		TeamID:  target,
		EntryID: id,
		Title:   live.CapFragment(body.Title),
		Actor:   live.ActorRef{ID: actorID, Display: tc.Display},
	})

	writeJSON(w, map[string]string{"id": id})
}

func (s *Server) handleKnowledgeUpdate(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := chi.URLParam(r, "id")
	existing, ok := s.fetchEntryForTeam(w, r, id)
	if !ok {
		return
	}
	var body struct {
		Title       string   `json:"title"`
		Content     string   `json:"content"`
		Description string   `json:"description"`
		Domain      string   `json:"domain"`
		Tags        []string `json:"tags"`
		Author      string   `json:"author"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, 400, "bad_request", "invalid JSON body")
		return
	}
	oldContent := existing.Content
	if body.Title != "" {
		existing.Title = body.Title
	}
	if body.Content != "" {
		existing.Content = body.Content
	}
	if body.Description != "" {
		existing.Description = body.Description
	}
	if body.Domain != "" {
		existing.Domain = body.Domain
	}
	if body.Tags != nil {
		existing.Tags = body.Tags
	}
	// Allow setting the author only when it is currently empty, protecting real
	// authorship. A non-empty existing author is never overwritten. This does
	// not change Content, so it never triggers the re-embed path below.
	if body.Author != "" && existing.Author == "" {
		existing.Author = body.Author
	}
	if err := s.store.UpdateEntry(ctx, *existing); err != nil {
		writeError(w, 500, "internal_error", fmt.Sprintf("update entry: %v", err))
		return
	}

	// Re-embed only when the content actually changed. The entry row is already
	// updated above; refreshing the stored vectors keeps similarity search in
	// sync with the new content (otherwise edits leave a stale embedding behind).
	if existing.Content != oldContent {
		if s.aiSrc == nil {
			slog.Info("knowledge update: re-embed skipped, AI sources not configured", "id", id, "team", existing.TeamID)
			writeJSON(w, map[string]any{"ok": true})
			return
		}
		teamID := existing.TeamID
		embedder := s.aiSrc.Embedder(ctx, teamID)
		if embedder == nil {
			slog.Info("knowledge update: re-embed skipped, embedding not configured", "id", id, "team", teamID)
			writeJSON(w, map[string]any{"ok": true})
			return
		}
		cfg := s.aiSrc.ChunkConfig(ctx, teamID)
		chunks := embedding.Chunk(existing.Content, cfg)
		entryChunks := make([]storage.EntryChunk, 0, len(chunks))
		for _, c := range chunks {
			emb, err := embedder.Embed(ctx, c.Content)
			if err != nil {
				slog.Error("knowledge update: re-embed failed", "id", id, "team", teamID, "chunk", c.Index, "err", err)
				writeError(w, 500, "internal_error", fmt.Sprintf("re-embed content: %v", err))
				return
			}
			entryChunks = append(entryChunks, storage.EntryChunk{
				Index:         c.Index,
				Content:       c.Content,
				TokenEstimate: c.TokenEstimate,
				Embedding:     emb,
			})
		}
		if err := s.store.ReplaceEntryChunks(ctx, id, entryChunks); err != nil {
			if errors.Is(err, storage.ErrNotFound) {
				writeError(w, 404, "not_found", "entry not found")
				return
			}
			slog.Error("knowledge update: replace chunks failed", "id", id, "team", teamID, "err", err)
			writeError(w, 500, "internal_error", fmt.Sprintf("replace entry chunks: %v", err))
			return
		}
	}

	writeJSON(w, map[string]any{"ok": true})
}

func (s *Server) handleKnowledgeDelete(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := chi.URLParam(r, "id")
	if _, ok := s.fetchEntryForTeam(w, r, id); !ok {
		return
	}
	if err := s.store.DeleteEntry(ctx, id); err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			writeError(w, 404, "not_found", "entry not found")
			return
		}
		writeError(w, 500, "internal_error", fmt.Sprintf("delete entry: %v", err))
		return
	}
	writeJSON(w, map[string]any{"ok": true})
}

func (s *Server) handleKnowledgeApprove(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := chi.URLParam(r, "id")
	if _, ok := s.fetchEntryForTeam(w, r, id); !ok {
		return
	}
	if err := s.store.ApproveEntry(ctx, id); err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			writeError(w, 404, "not_found", "entry not found")
			return
		}
		writeError(w, 500, "internal_error", fmt.Sprintf("approve entry: %v", err))
		return
	}
	writeJSON(w, map[string]any{"ok": true})
}

func (s *Server) handleKnowledgeReject(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := chi.URLParam(r, "id")
	if _, ok := s.fetchEntryForTeam(w, r, id); !ok {
		return
	}
	if err := s.store.RejectEntry(ctx, id); err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			writeError(w, 404, "not_found", "entry not found")
			return
		}
		writeError(w, 500, "internal_error", fmt.Sprintf("reject entry: %v", err))
		return
	}
	writeJSON(w, map[string]any{"ok": true})
}

func (s *Server) handleClusterGet(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	// Fetch globally first so we can distinguish 404 from 403.
	clusters, err := s.store.ListClusters(r.Context(), "")
	if err != nil {
		writeError(w, 500, "internal_error", fmt.Sprintf("list clusters: %v", err))
		return
	}
	for _, c := range clusters {
		if c.ID == id {
			tc := auth.GetTeamContext(r.Context())
			if !auth.CanAccess(tc, c.TeamID) {
				writeError(w, 403, "forbidden", "cluster belongs to another team")
				return
			}
			writeJSON(w, c)
			return
		}
	}
	writeError(w, 404, "not_found", "cluster not found")
}

func (s *Server) handleClusterSummary(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	// Fetch globally first so we can distinguish 404 from 403.
	clusters, err := s.store.ListClusters(r.Context(), "")
	if err != nil {
		writeError(w, 500, "internal_error", fmt.Sprintf("list clusters: %v", err))
		return
	}
	for _, c := range clusters {
		if c.ID == id {
			tc := auth.GetTeamContext(r.Context())
			if !auth.CanAccess(tc, c.TeamID) {
				writeError(w, 403, "forbidden", "cluster belongs to another team")
				return
			}
			writeJSON(w, map[string]string{"summary": c.Summary})
			return
		}
	}
	writeError(w, 404, "not_found", "cluster not found")
}

func (s *Server) handleAgentLatestByDomain(w http.ResponseWriter, r *http.Request) {
	domain := chi.URLParam(r, "domain")
	tc := auth.GetTeamContext(r.Context())
	agent, err := s.store.GetAgentByDomain(r.Context(), domain, tc.TeamID)
	if err != nil || agent == nil {
		writeError(w, 404, "not_found", "no agent for domain")
		return
	}
	if !auth.CanAccess(tc, agent.TeamID) {
		writeError(w, 403, "forbidden", "agent belongs to another team")
		return
	}
	writeJSON(w, agent)
}

func (s *Server) handlePipelineTrigger(w http.ResponseWriter, r *http.Request) {
	if s.triggerPipeline != nil {
		select {
		case s.triggerPipeline <- struct{}{}:
			writeJSON(w, map[string]string{"status": "triggered"})
		default:
			writeJSON(w, map[string]string{"status": "already_running"})
		}
		return
	}
	writeError(w, 503, "internal_error", "pipeline not configured")
}
