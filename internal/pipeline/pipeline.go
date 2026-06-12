package pipeline

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/dsandor/memory/internal/agent"
	"github.com/dsandor/memory/internal/live"
	"github.com/dsandor/memory/internal/llm"
	"github.com/dsandor/memory/internal/storage"
	"github.com/dsandor/memory/internal/tags"
	"github.com/google/uuid"
)

// AISource resolves ready-to-use LLM clients per pipeline run. *aiconfig.Sources
// satisfies this interface; tests may supply a lightweight fake.
type AISource interface {
	AnalysisLLM(ctx context.Context, teamID string) llm.Client
	AgentLLM(ctx context.Context, teamID string) llm.Client
	ImprovementLLM(ctx context.Context, teamID string) llm.Client
	// LLMFingerprint identifies the effective LLM provider+model for teamID and
	// touchpoint (e.g. "anthropic|claude-x" or "ollama|http://o|llama3.1"). Used
	// to discriminate provider-dependent cache entries; "" when unresolvable.
	// Use plain string touchpoint constants ("analysis", "agents", etc.) — the
	// pipeline does not import aiconfig to avoid a circular dependency.
	LLMFingerprint(ctx context.Context, teamID, touchpoint string) string
}

// Config controls when and how the pipeline runs.
type Config struct {
	MinEntries       int
	Interval         time.Duration
	ClusterThreshold float64
}

// Pipeline orchestrates the knowledge analysis pipeline.
type Pipeline struct {
	store           storage.AnalysisStore
	agentStore      storage.AgentStore
	src             AISource
	cfg             Config
	trigger         chan struct{}
	mu              sync.Mutex
	stageDone       chan struct{}
	liveBus         live.EventBus // optional; nil disables live publishing
	weakSignal      bool
	autoTagBackfill bool
}

// New creates a new Pipeline. src resolves LLM clients per run so that saved
// team settings take effect immediately. Call WithAgentGeneration to enable
// agent synthesis.
func New(store storage.AnalysisStore, src AISource, cfg Config) *Pipeline {
	return &Pipeline{store: store, src: src, cfg: cfg, trigger: make(chan struct{}, 1)}
}

// TriggerNow requests an immediate pipeline run. Non-blocking; drops the signal
// if a trigger is already pending.
func (p *Pipeline) TriggerNow() {
	select {
	case p.trigger <- struct{}{}:
	default:
	}
}

// WithAgentGeneration configures the pipeline to generate agents from clusters.
func (p *Pipeline) WithAgentGeneration(agentStore storage.AgentStore) *Pipeline {
	p.agentStore = agentStore
	return p
}

// WithWeakSignalImprovement configures the pipeline to draft LLM-rewritten
// improvements for entries that have received poor outcome ratings. Each run
// processes all teams; the teamID is derived from the per-team loop rather
// than a fixed field.
func (p *Pipeline) WithWeakSignalImprovement() *Pipeline {
	p.weakSignal = true
	return p
}

// WithLivePublish attaches an optional live.EventBus to the pipeline so it can
// publish real-time activity events to the dashboard SSE stream. Passing nil is
// safe and disables publishing (identical to not calling this method).
func (p *Pipeline) WithLivePublish(bus live.EventBus) *Pipeline {
	p.liveBus = bus
	return p
}

// WithAutoTagBackfill enables a stage that LLM-tags entries whose auto_tags
// are still empty (covers pre-feature entries and async-tagging failures).
func (p *Pipeline) WithAutoTagBackfill() *Pipeline {
	p.autoTagBackfill = true
	return p
}

// pipelineActor is the ActorRef used for all pipeline-originated events.
var pipelineActor = live.ActorRef{ID: "pipeline", Display: "pipeline"}

// publishPipelineEvent publishes ev to p.liveBus, filling ID/CreatedAt if zero.
// No-ops when p.liveBus is nil.
func (p *Pipeline) publishPipelineEvent(ev live.LiveEvent) {
	if p.liveBus == nil {
		return
	}
	if ev.ID == "" {
		ev.ID = uuid.New().String()
	}
	if ev.CreatedAt.IsZero() {
		ev.CreatedAt = time.Now().UTC()
	}
	p.liveBus.Publish(ev)
}

// Start launches the pipeline as a background goroutine until ctx is cancelled.
func (p *Pipeline) Start(ctx context.Context) {
	go func() {
		ticker := time.NewTicker(p.cfg.Interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				p.mu.Lock()
				sd := p.stageDone
				p.mu.Unlock()
				if sd != nil {
					<-sd
				}
				slog.Info("pipeline stopped")
				return
			case <-p.trigger:
				if err := p.Run(ctx, "manual"); err != nil {
					slog.Error("pipeline run error", "err", err, "trigger", "manual")
				}
			case <-ticker.C:
				if err := p.Run(ctx, "interval"); err != nil {
					slog.Error("pipeline run error", "err", err, "trigger", "interval")
				}
			}
		}
	}()
}

// Run executes a pipeline pass for every known team. When no teams exist it
// falls back to a single unscoped pass (teamID "") for dev/single-tenant use.
// For interval triggers, teams below MinEntries are skipped; manual triggers
// process all teams regardless. A single team's failure is logged and recorded
// in its own run row but does not abort other teams.
// The analysis cache is pruned once after all teams have been processed.
func (p *Pipeline) Run(ctx context.Context, trigger string) error {
	teams, err := p.store.ListTeams(ctx)
	if err != nil {
		slog.Warn("pipeline: list teams failed, using fallback", "err", err)
		teams = nil
	}

	// Signal to Start that a stage is in progress so graceful shutdown can wait.
	sd := make(chan struct{})
	p.mu.Lock()
	p.stageDone = sd
	p.mu.Unlock()
	defer func() {
		close(sd)
		p.mu.Lock()
		p.stageDone = nil
		p.mu.Unlock()
	}()

	// Filter to enabled teams only; disabled teams are skipped entirely.
	var enabledTeams []storage.Team
	for _, t := range teams {
		if t.Enabled {
			enabledTeams = append(enabledTeams, t)
		}
	}

	var anySucceeded bool

	if len(enabledTeams) == 0 {
		// Dev / single-tenant fallback: run unscoped (no teams configured or all disabled).
		if runErr := p.runForTeam(ctx, trigger, ""); runErr != nil {
			slog.Error("pipeline run error", "team", "", "trigger", trigger, "err", runErr)
		} else {
			anySucceeded = true
		}
	} else {
		for _, team := range enabledTeams {
			if runErr := p.runForTeam(ctx, trigger, team.ID); runErr != nil {
				slog.Error("pipeline run error", "team", team.ID, "trigger", trigger, "err", runErr)
			} else {
				anySucceeded = true
			}
		}
	}

	// Prune stale cache entries once after all teams, on the successful path.
	if anySucceeded {
		if n, pruneErr := p.store.PruneAnalysisCache(context.Background(), 90*24*time.Hour); pruneErr != nil {
			slog.Warn("analysis cache prune failed", "err", pruneErr)
		} else if n > 0 {
			slog.Info("analysis cache pruned", "deleted", n)
		}
	}

	return nil
}

// runForTeam executes a single pipeline pass scoped to one team. All store
// operations use teamID as a filter; LLM clients are resolved per team.
// For interval triggers, the team is skipped (no run row) when its entry count
// is below Config.MinEntries. Manual triggers bypass this gate.
func (p *Pipeline) runForTeam(ctx context.Context, trigger, teamID string) error {
	runStart := time.Now()

	// Per-team interval gate: skip (no run row) when below threshold.
	if trigger == "interval" {
		count, err := p.store.CountEntries(ctx, teamID)
		if err != nil {
			return fmt.Errorf("count entries: %w", err)
		}
		if count < p.cfg.MinEntries {
			slog.Info("pipeline skipped: below min entries", "team", teamID, "count", count, "min", p.cfg.MinEntries)
			return nil
		}
	}

	// Resolve LLM clients at the start of each run so that saved settings take
	// effect immediately without a server restart.
	analysisLLM := p.src.AnalysisLLM(ctx, teamID)
	if analysisLLM == nil {
		slog.Info("pipeline skipped: no LLM configured", "team", teamID, "trigger", trigger)
		return nil
	}
	agentLLM := p.src.AgentLLM(ctx, teamID)
	improvementLLM := p.src.ImprovementLLM(ctx, teamID)
	// Resolve fingerprints once per team per run to discriminate provider-keyed
	// cache entries. "analysis" fingerprint gates summary cache; "agents" gates
	// agent-generation cache.
	analysisFingerprint := p.src.LLMFingerprint(ctx, teamID, "analysis")
	agentsFingerprint := p.src.LLMFingerprint(ctx, teamID, "agents")
	improvementFingerprint := p.src.LLMFingerprint(ctx, teamID, "improvement")

	prevRun, _ := p.store.GetLatestPipelineRun(ctx, teamID)
	var prevRunID string
	if prevRun != nil {
		prevRunID = prevRun.ID
	}

	runID, err := p.store.StartPipelineRun(ctx, trigger, teamID)
	if err != nil {
		return fmt.Errorf("start run: %w", err)
	}

	finishCtx := context.Background()

	var runErrs []string
	clustersFound := 0

	// logFinish emits the "pipeline run finished" line then calls FinishPipelineRun.
	// It must be called at every exit path after runID is established.
	logFinish := func(status string) error {
		slog.Info("pipeline run finished",
			"team", teamID,
			"status", status,
			"duration_ms", time.Since(runStart).Milliseconds(),
			"clusters", clustersFound,
			"errors", len(runErrs),
		)
		return p.store.FinishPipelineRun(finishCtx, runID, status, len(runErrs), clustersFound, runErrs)
	}

	entries, err := p.store.ListEntries(ctx, storage.ListFilter{TeamID: teamID, Limit: -1})
	if err != nil {
		runErrs = append(runErrs, fmt.Sprintf("list entries: %v", err))
		_ = logFinish("failed")
		return fmt.Errorf("list entries: %w", err)
	}

	slog.Info("pipeline run started",
		"team", teamID,
		"trigger", trigger,
		"entries", len(entries),
		"analysis_llm", analysisFingerprint,
		"agents_llm", agentsFingerprint,
		"improvement_llm", improvementFingerprint,
	)

	embeddings, err := p.store.GetAllEmbeddings(ctx, teamID)
	if err != nil {
		runErrs = append(runErrs, fmt.Sprintf("get embeddings: %v", err))
		_ = logFinish("failed")
		return fmt.Errorf("get embeddings: %w", err)
	}

	entryByID := make(map[string]storage.KnowledgeEntry, len(entries))
	domainByID := make(map[string]string, len(entries))
	domainCounts := make(map[string]int)
	for _, e := range entries {
		entryByID[e.ID] = e
		domainByID[e.ID] = e.Domain
		domainCounts[e.Domain]++
	}

	candidates := Cluster(embeddings, domainByID, p.cfg.ClusterThreshold)

	for _, cand := range candidates {
		clusterEntries := make([]storage.KnowledgeEntry, 0, len(cand.EntryIDs))
		for _, id := range cand.EntryIDs {
			if e, ok := entryByID[id]; ok {
				clusterEntries = append(clusterEntries, e)
			}
		}

		summary, err := p.cachedSummarizeCluster(ctx, analysisLLM, clusterEntries, analysisFingerprint, teamID)
		if err != nil {
			slog.Warn("pipeline stage error", "stage", "summarize", "team", teamID, "err", err)
			runErrs = append(runErrs, fmt.Sprintf("summarize cluster: %v", err))
			continue
		}

		var totalScore float64
		for _, e := range clusterEntries {
			if score, scoreErr := p.cachedScoreEntry(ctx, analysisLLM, e, teamID); scoreErr == nil {
				totalScore += score.Total
			} else {
				slog.Warn("pipeline stage error", "stage", "score", "team", teamID, "err", scoreErr)
			}
		}
		avgScore := 0.0
		if len(clusterEntries) > 0 {
			avgScore = totalScore / float64(len(clusterEntries))
		}

		cluster := storage.Cluster{
			Domain:        cand.Domain,
			Title:         summary.Title,
			Summary:       summary.Summary,
			EntryIDs:      cand.EntryIDs,
			QualityScore:  avgScore,
			PipelineRunID: runID,
			TeamID:        teamID,
		}
		clusterID, err := p.store.StoreCluster(ctx, cluster)
		if err != nil {
			slog.Warn("pipeline stage error", "stage", "cluster", "team", teamID, "err", err)
			runErrs = append(runErrs, fmt.Sprintf("store cluster: %v", err))
			continue
		}
		cluster.ID = clusterID
		clustersFound++

		if p.agentStore != nil && agentLLM != nil {
			if err := p.generateAgent(ctx, agentLLM, cluster, clusterEntries, agentsFingerprint, teamID); err != nil {
				slog.Warn("pipeline stage error", "stage", "agent_gen", "team", teamID, "err", err)
				runErrs = append(runErrs, fmt.Sprintf("generate agent for %s: %v", cand.Domain, err))
			}
		}
	}

	if prevRunID != "" {
		if err := p.store.DeleteClustersByRunID(finishCtx, prevRunID); err != nil {
			runErrs = append(runErrs, fmt.Sprintf("delete old clusters: %v", err))
		}
	}

	gaps, err := DetectGaps(ctx, analysisLLM, domainCounts)
	if err != nil {
		slog.Warn("pipeline stage error", "stage", "gaps", "team", teamID, "err", err)
		runErrs = append(runErrs, fmt.Sprintf("detect gaps: %v", err))
		gaps = nil
	}

	// Weak-signal improvement stage: runs after quality scoring is complete.
	if p.weakSignal && improvementLLM != nil {
		p.runWeakSignalImprovement(ctx, improvementLLM, teamID)
	}

	// Auto-tag backfill stage: tags entries that have no auto tags yet.
	if p.autoTagBackfill && improvementLLM != nil {
		p.runAutoTagBackfill(ctx, improvementLLM, teamID)
	}

	latest, err := p.store.GetLatestSnapshot(ctx, teamID)
	if err != nil {
		runErrs = append(runErrs, fmt.Sprintf("get latest snapshot: %v", err))
		_ = logFinish("failed")
		return fmt.Errorf("get latest snapshot: %w", err)
	}
	version := 1
	if latest != nil {
		version = latest.Version + 1
	}

	type snapshotData struct {
		Gaps []DomainGap `json:"gaps"`
	}
	snapDataJSON, _ := json.Marshal(snapshotData{Gaps: gaps})

	snap := storage.DatasetSnapshot{
		Version:       version,
		ClusterCount:  clustersFound,
		EntryCount:    len(entries),
		Data:          string(snapDataJSON),
		PipelineRunID: runID,
		TeamID:        teamID,
	}
	if _, err := p.store.StoreSnapshot(finishCtx, snap); err != nil {
		runErrs = append(runErrs, fmt.Sprintf("store snapshot: %v", err))
	}

	status := "complete"
	if len(runErrs) > 0 {
		status = "complete_with_errors"
	}
	finishErr := logFinish(status)

	// Publish pipeline_complete live event (best-effort, nil-safe).
	p.publishPipelineEvent(live.LiveEvent{
		Type:   live.TypePipelineComplete,
		TeamID: teamID,
		Actor:  pipelineActor,
		Meta: map[string]string{
			"run_id":   runID,
			"status":   status,
			"entries":  fmt.Sprintf("%d", len(entries)),
			"clusters": fmt.Sprintf("%d", clustersFound),
		},
	})

	return finishErr
}

// runWeakSignalImprovement fetches poorly-rated entries, rewrites them via LLM,
// and stores draft improved copies pending curator review.
func (p *Pipeline) runWeakSignalImprovement(ctx context.Context, improvementLLM llm.Client, teamID string) {
	weak, err := p.store.GetWeakSignalEntries(ctx, teamID, 3, 2.5)
	if err != nil {
		slog.Warn("pipeline stage error", "stage", "weak_signal", "team", teamID, "err", err)
		return
	}
	if len(weak) == 0 {
		return
	}

	// Cap at 5 per run to limit LLM cost.
	if len(weak) > 5 {
		weak = weak[:5]
	}

	for _, entry := range weak {
		if err := p.improveEntry(ctx, improvementLLM, entry); err != nil {
			slog.Warn("pipeline stage error", "stage", "weak_signal", "team", teamID, "err", err, "id", entry.ID)
			// continue to next entry on failure
		}
	}
}

// autoTagBackfillCap bounds LLM cost per pipeline run.
const autoTagBackfillCap = 20

// runAutoTagBackfill tags entries that have no auto tags yet. Idempotent:
// already-tagged entries are skipped, so repeated runs converge.
func (p *Pipeline) runAutoTagBackfill(ctx context.Context, improvementLLM llm.Client, teamID string) {
	entries, err := p.store.ListEntries(ctx, storage.ListFilter{TeamID: teamID, Limit: 500})
	if err != nil {
		slog.Warn("pipeline stage error", "stage", "autotag", "team", teamID, "err", err)
		return
	}
	tagger := &tags.AutoTagger{
		Store:  p.store,
		LLMFor: func(context.Context, string) llm.Client { return improvementLLM },
	}
	tagged := 0
	for _, e := range entries {
		if ctx.Err() != nil {
			return // shutdown in progress — don't start more LLM calls
		}
		if len(e.AutoTags) > 0 {
			continue
		}
		tagger.TagEntry(ctx, e, teamID)
		tagged++
		if tagged >= autoTagBackfillCap {
			slog.Info("autotag backfill: cap reached", "cap", autoTagBackfillCap)
			break
		}
	}
	if tagged > 0 {
		slog.Info("autotag backfill complete", "tagged", tagged)
	}
}

// improveEntry rewrites a single weak-signal entry using exemplars from the same domain.
func (p *Pipeline) improveEntry(ctx context.Context, improvementLLM llm.Client, entry storage.KnowledgeEntry) error {
	// Fetch exemplars from the same domain and team (up to 10 so we can pick
	// top 2 by quality score). Team scoping keeps cross-team content out of
	// the improvement prompt; legacy team-less entries stay unscoped.
	domainEntries, err := p.store.ListEntries(ctx, storage.ListFilter{
		Domain: entry.Domain,
		TeamID: entry.TeamID,
		Limit:  10,
	})
	if err != nil {
		return fmt.Errorf("list domain entries: %w", err)
	}

	// Pick top 2 by Rating as a proxy for quality score (exclude the entry being improved).
	exemplars := make([]storage.KnowledgeEntry, 0, 2)
	for _, e := range domainEntries {
		if e.ID == entry.ID {
			continue
		}
		exemplars = append(exemplars, e)
		if len(exemplars) == 2 {
			break
		}
	}

	// Build the prompt.
	avgRating := entry.Rating
	ex1Title, ex1Content := "(none)", "(none)"
	ex2Title, ex2Content := "(none)", "(none)"
	if len(exemplars) > 0 {
		ex1Title = exemplars[0].Title
		ex1Content = exemplars[0].Content
	}
	if len(exemplars) > 1 {
		ex2Title = exemplars[1].Title
		ex2Content = exemplars[1].Content
	}

	tagsJSON, _ := json.Marshal(entry.Tags)

	prompt := fmt.Sprintf(`You are improving a knowledge entry that users rated poorly (avg rating: %.1f/5).

High-quality entries in the same domain for reference:
--- EXEMPLAR 1 ---
Title: %s
Content: %s
---
EXEMPLAR 2 ---
Title: %s
Content: %s
---

Entry to improve:
Title: %s
Content: %s
Tags: %s

Rewrite the entry to be more actionable, specific, and useful.
Return JSON: {"title": "...", "content": "...", "tags": ["..."]}`,
		avgRating,
		ex1Title, ex1Content,
		ex2Title, ex2Content,
		entry.Title, entry.Content, string(tagsJSON),
	)

	rawResponse, err := improvementLLM.Complete(ctx, prompt)
	if err != nil {
		return fmt.Errorf("llm complete: %w", err)
	}

	// Parse the JSON response.
	var result struct {
		Title   string   `json:"title"`
		Content string   `json:"content"`
		Tags    []string `json:"tags"`
	}
	if err := json.Unmarshal([]byte(rawResponse), &result); err != nil {
		return fmt.Errorf("parse llm response: %w (raw: %.200s)", err, rawResponse)
	}
	if result.Title == "" || result.Content == "" {
		return fmt.Errorf("llm returned empty title or content")
	}

	// Store the improved entry as a draft for curator review.
	improved := storage.KnowledgeEntry{
		Type:    entry.Type,
		Title:   "[Improved] " + result.Title,
		Content: result.Content,
		Domain:  entry.Domain,
		Tags:    result.Tags,
		Status:  "draft",
		TeamID:  entry.TeamID,
	}

	newID, err := p.store.StoreEntry(ctx, improved, nil)
	if err != nil {
		return fmt.Errorf("store improved entry: %w", err)
	}

	// Record the activity event.
	_ = p.store.RecordActivity(ctx, storage.ActivityEvent{
		EventType: live.TypeImprovementDrafted,
		EntryID:   newID,
		Metadata: map[string]string{
			"original_id":         entry.ID,
			"original_avg_rating": fmt.Sprintf("%.1f", avgRating),
		},
	})

	slog.Info("drafted improvement for entry", "id", entry.ID, "improved_id", newID)
	return nil
}

func (p *Pipeline) generateAgent(ctx context.Context, agentLLM llm.Client, cluster storage.Cluster, entries []storage.KnowledgeEntry, llmFingerprint string, teamID string) error {
	genResult, err := p.cachedAgentGen(ctx, agentLLM, cluster, entries, llmFingerprint, teamID)
	if err != nil {
		return fmt.Errorf("generate: %w", err)
	}
	newAgent := &storage.Agent{
		Domain:       cluster.Domain,
		Version:      1,
		Status:       storage.AgentStatusDraft,
		SystemPrompt: genResult.SystemPrompt,
		Instructions: genResult.Instructions,
		AntiPatterns: genResult.AntiPatterns,
		SourceRefs:   []string{cluster.ID},
		ClusterID:    cluster.ID,
	}
	newAgent.TeamID = teamID

	existing, err := p.agentStore.GetAgentByDomain(ctx, cluster.Domain, teamID)
	if err != nil {
		return fmt.Errorf("get existing agent: %w", err)
	}

	changelog := agent.Diff(existing, newAgent)

	if existing != nil {
		newAgent.ID = existing.ID
		newAgent.Version = existing.Version + 1
		newAgent.Status = existing.Status // preserve published/draft state across pipeline runs
	}

	id, err := p.agentStore.UpsertAgent(ctx, *newAgent)
	if err != nil {
		return fmt.Errorf("upsert agent: %w", err)
	}

	if err := p.agentStore.StoreAgentVersion(ctx, storage.AgentVersion{
		AgentID:      id,
		Version:      newAgent.Version,
		SystemPrompt: newAgent.SystemPrompt,
		Instructions: newAgent.Instructions,
		AntiPatterns: newAgent.AntiPatterns,
		Changelog:    changelog,
	}); err != nil {
		return err
	}

	// Publish agent_generated live event (best-effort, nil-safe).
	p.publishPipelineEvent(live.LiveEvent{
		Type:   live.TypeAgentGenerated,
		TeamID: teamID,
		Actor:  pipelineActor,
		Title:  live.CapFragment(cluster.Domain),
		Meta: map[string]string{
			"domain":     cluster.Domain,
			"cluster_id": cluster.ID,
			"agent_id":   id,
		},
	})

	return nil
}
