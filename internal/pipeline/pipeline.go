package pipeline

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/dsandor/memory/internal/agent"
	"github.com/dsandor/memory/internal/llm"
	"github.com/dsandor/memory/internal/storage"
)

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
	llm             llm.Client
	agentLLM        llm.Client
	improvementLLM  llm.Client
	teamID          string
	cfg             Config
	trigger         chan struct{}
	mu              sync.Mutex
	stageDone       chan struct{}
}

// New creates a new Pipeline. Call WithAgentGeneration to enable agent synthesis.
func New(store storage.AnalysisStore, llmClient llm.Client, cfg Config) *Pipeline {
	return &Pipeline{store: store, llm: llmClient, cfg: cfg, trigger: make(chan struct{}, 1)}
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
func (p *Pipeline) WithAgentGeneration(agentStore storage.AgentStore, agentLLM llm.Client) *Pipeline {
	p.agentStore = agentStore
	p.agentLLM = agentLLM
	return p
}

// WithWeakSignalImprovement configures the pipeline to draft LLM-rewritten improvements
// for entries that have received poor outcome ratings. teamID scopes the query.
func (p *Pipeline) WithWeakSignalImprovement(improvementLLM llm.Client, teamID string) *Pipeline {
	p.improvementLLM = improvementLLM
	p.teamID = teamID
	return p
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
				count, err := p.store.CountEntries(ctx)
				if err != nil {
					slog.Error("pipeline count entries error", "err", err)
					continue
				}
				if count < p.cfg.MinEntries {
					continue
				}
				if err := p.Run(ctx, "interval"); err != nil {
					slog.Error("pipeline run error", "err", err, "trigger", "interval")
				}
			}
		}
	}()
}

// Run executes a single pipeline pass: cluster → score → summarize → detect gaps → snapshot → generate agents.
func (p *Pipeline) Run(ctx context.Context, trigger string) error {
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

	prevRun, _ := p.store.GetLatestPipelineRun(ctx)
	var prevRunID string
	if prevRun != nil {
		prevRunID = prevRun.ID
	}

	runID, err := p.store.StartPipelineRun(ctx, trigger)
	if err != nil {
		return fmt.Errorf("start run: %w", err)
	}

	finishCtx := context.Background()

	var runErrs []string
	clustersFound := 0

	entries, err := p.store.ListEntries(ctx, storage.ListFilter{Limit: -1})
	if err != nil {
		runErrs = append(runErrs, fmt.Sprintf("list entries: %v", err))
		return p.store.FinishPipelineRun(finishCtx, runID, "failed", 0, 0, runErrs)
	}

	embeddings, err := p.store.GetAllEmbeddings(ctx)
	if err != nil {
		runErrs = append(runErrs, fmt.Sprintf("get embeddings: %v", err))
		return p.store.FinishPipelineRun(finishCtx, runID, "failed", len(entries), 0, runErrs)
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

		summary, err := SummarizeCluster(ctx, p.llm, clusterEntries)
		if err != nil {
			runErrs = append(runErrs, fmt.Sprintf("summarize cluster: %v", err))
			continue
		}

		var totalScore float64
		for _, e := range clusterEntries {
			if score, err := ScoreEntry(ctx, p.llm, e); err == nil {
				totalScore += score.Total
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
		}
		clusterID, err := p.store.StoreCluster(ctx, cluster)
		if err != nil {
			runErrs = append(runErrs, fmt.Sprintf("store cluster: %v", err))
			continue
		}
		cluster.ID = clusterID
		clustersFound++

		if p.agentStore != nil && p.agentLLM != nil {
			if err := p.generateAgent(ctx, cluster, clusterEntries); err != nil {
				runErrs = append(runErrs, fmt.Sprintf("generate agent for %s: %v", cand.Domain, err))
			}
		}
	}

	if prevRunID != "" {
		if err := p.store.DeleteClustersByRunID(finishCtx, prevRunID); err != nil {
			runErrs = append(runErrs, fmt.Sprintf("delete old clusters: %v", err))
		}
	}

	gaps, err := DetectGaps(ctx, p.llm, domainCounts)
	if err != nil {
		runErrs = append(runErrs, fmt.Sprintf("detect gaps: %v", err))
		gaps = nil
	}

	// Weak-signal improvement stage: runs after quality scoring is complete.
	if p.improvementLLM != nil {
		p.runWeakSignalImprovement(ctx)
	}

	latest, err := p.store.GetLatestSnapshot(ctx)
	if err != nil {
		runErrs = append(runErrs, fmt.Sprintf("get latest snapshot: %v", err))
		return p.store.FinishPipelineRun(finishCtx, runID, "failed", len(entries), clustersFound, runErrs)
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
	}
	if _, err := p.store.StoreSnapshot(finishCtx, snap); err != nil {
		runErrs = append(runErrs, fmt.Sprintf("store snapshot: %v", err))
	}

	status := "complete"
	if len(runErrs) > 0 {
		status = "complete_with_errors"
	}
	return p.store.FinishPipelineRun(finishCtx, runID, status, len(entries), clustersFound, runErrs)
}

// runWeakSignalImprovement fetches poorly-rated entries, rewrites them via LLM,
// and stores draft improved copies pending curator review.
func (p *Pipeline) runWeakSignalImprovement(ctx context.Context) {
	weak, err := p.store.GetWeakSignalEntries(ctx, p.teamID, 3, 2.5)
	if err != nil {
		slog.Error("weak signal improvement: get entries", "err", err)
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
		if err := p.improveEntry(ctx, entry); err != nil {
			slog.Error("weak signal improvement: improve entry", "id", entry.ID, "err", err)
			// continue to next entry on failure
		}
	}
}

// improveEntry rewrites a single weak-signal entry using exemplars from the same domain.
func (p *Pipeline) improveEntry(ctx context.Context, entry storage.KnowledgeEntry) error {
	// Fetch exemplars from the same domain (up to 10 so we can pick top 2 by quality score).
	domainEntries, err := p.store.ListEntries(ctx, storage.ListFilter{
		Domain: entry.Domain,
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

	rawResponse, err := p.improvementLLM.Complete(ctx, prompt)
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
		EventType: "improvement_drafted",
		EntryID:   newID,
		Metadata: map[string]string{
			"original_id":         entry.ID,
			"original_avg_rating": fmt.Sprintf("%.1f", avgRating),
		},
	})

	slog.Info("drafted improvement for entry", "id", entry.ID, "improved_id", newID)
	return nil
}

func (p *Pipeline) generateAgent(ctx context.Context, cluster storage.Cluster, entries []storage.KnowledgeEntry) error {
	newAgent, err := agent.Generate(ctx, p.agentLLM, cluster, entries)
	if err != nil {
		return fmt.Errorf("generate: %w", err)
	}

	existing, err := p.agentStore.GetAgentByDomain(ctx, cluster.Domain)
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

	return p.agentStore.StoreAgentVersion(ctx, storage.AgentVersion{
		AgentID:      id,
		Version:      newAgent.Version,
		SystemPrompt: newAgent.SystemPrompt,
		Instructions: newAgent.Instructions,
		AntiPatterns: newAgent.AntiPatterns,
		Changelog:    changelog,
	})
}
