package pipeline

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"log/slog"
	"sort"
	"strings"

	"github.com/dsandor/memory/internal/agent"
	"github.com/dsandor/memory/internal/llm"
	"github.com/dsandor/memory/internal/storage"
)

// Cache kinds for memoized LLM results.
const (
	cacheKindScore   = "score"
	cacheKindSummary = "summary"
	cacheKindAgent   = "agent"
)

// entryHash returns the content-derived cache key for a single entry,
// mirroring the storage content-hash convention (sha256 of title+content).
func entryHash(e storage.KnowledgeEntry) string {
	h := sha256.Sum256([]byte(e.Title + e.Content))
	return hex.EncodeToString(h[:])
}

// clusterHash returns an order-independent cache key for a set of entries:
// sha256 over the sorted member entry hashes.
func clusterHash(entries []storage.KnowledgeEntry) string {
	hashes := make([]string, len(entries))
	for i, e := range entries {
		hashes[i] = entryHash(e)
	}
	sort.Strings(hashes)
	h := sha256.Sum256([]byte(strings.Join(hashes, "|")))
	return hex.EncodeToString(h[:])
}

// Note: analysis_cache.team_id is informational only — rows are addressed by
// content+provider key and may be shared or overwritten across teams that
// produce identical content with the same LLM provider.

// cachedScoreEntry returns the quality score for entry, consulting the
// analysis cache first. LLM failures are returned uncached so the item
// retries on the next run; cache-write failures only log. teamID scopes
// the cache write to the owning team.
func (p *Pipeline) cachedScoreEntry(ctx context.Context, client llm.Client, entry storage.KnowledgeEntry, teamID string) (QualityScore, error) {
	key := entryHash(entry)
	raw, ok, err := p.store.GetAnalysisCache(ctx, cacheKindScore, key)
	slog.Debug("analysis cache", "kind", cacheKindScore, "hit", ok && err == nil, "team", teamID)
	if err == nil && ok {
		var score QualityScore
		if json.Unmarshal([]byte(raw), &score) == nil {
			score.Total = score.Coherence + score.Specificity
			return score, nil
		}
	}
	score, err := ScoreEntry(ctx, client, entry)
	if err != nil {
		return score, err
	}
	if raw, merr := json.Marshal(score); merr == nil {
		if perr := p.store.PutAnalysisCache(ctx, cacheKindScore, key, string(raw), teamID); perr != nil {
			slog.Warn("analysis cache: put score", "err", perr)
		}
	}
	return score, nil
}

// providerKey hashes a provider fingerprint to a fixed-length hex string so that
// cache keys remain uniformly sized regardless of fingerprint length. An empty
// fingerprint hashes cleanly to its own deterministic value; no special-casing
// is needed at call sites.
func providerKey(fingerprint string) string {
	h := sha256.Sum256([]byte(fingerprint))
	return hex.EncodeToString(h[:])
}

// cachedSummarizeCluster mirrors cachedScoreEntry for cluster summaries but
// incorporates the LLM provider+model fingerprint into the cache key so that
// switching providers (e.g. anthropic→ollama) or models forces a fresh
// generation. Score entries are NOT fingerprinted because numeric quality
// scores are content-derived and provider-independent by design.
// teamID scopes the cache write to the owning team.
func (p *Pipeline) cachedSummarizeCluster(ctx context.Context, client llm.Client, entries []storage.KnowledgeEntry, llmFingerprint string, teamID string) (SummarizeResult, error) {
	key := providerKey(llmFingerprint) + "|" + clusterHash(entries)
	raw, ok, err := p.store.GetAnalysisCache(ctx, cacheKindSummary, key)
	slog.Debug("analysis cache", "kind", cacheKindSummary, "hit", ok && err == nil, "team", teamID)
	if err == nil && ok {
		var result SummarizeResult
		if json.Unmarshal([]byte(raw), &result) == nil {
			return result, nil
		}
	}
	result, err := SummarizeCluster(ctx, client, entries)
	if err != nil {
		return result, err
	}
	if raw, merr := json.Marshal(result); merr == nil {
		if perr := p.store.PutAnalysisCache(ctx, cacheKindSummary, key, string(raw), teamID); perr != nil {
			slog.Warn("analysis cache: put summary", "err", perr)
		}
	}
	return result, nil
}

// agentGenResult is the LLM-derived subset of a generated agent that we cache.
// Storage effects (UpsertAgent, StoreAgentVersion) happen outside the cached seam.
type agentGenResult struct {
	SystemPrompt string `json:"system_prompt"`
	Instructions string `json:"instructions"`
	AntiPatterns string `json:"anti_patterns"`
}

// cachedAgentGen returns the LLM-derived generation result for clusterEntries,
// consulting the analysis cache first. The cache key incorporates the LLM
// provider+model fingerprint so that a provider change forces re-generation;
// prose-heavy agent outputs (system prompt, instructions, anti-patterns) are
// provider-dependent and must not be served across provider boundaries.
// The caller is responsible for all storage effects (UpsertAgent, StoreAgentVersion).
// teamID scopes the cache write to the owning team.
func (p *Pipeline) cachedAgentGen(ctx context.Context, client llm.Client, cluster storage.Cluster, clusterEntries []storage.KnowledgeEntry, llmFingerprint string, teamID string) (agentGenResult, error) {
	key := providerKey(llmFingerprint) + "|" + clusterHash(clusterEntries)
	raw, ok, err := p.store.GetAnalysisCache(ctx, cacheKindAgent, key)
	slog.Debug("analysis cache", "kind", cacheKindAgent, "hit", ok && err == nil, "team", teamID)
	if err == nil && ok {
		var result agentGenResult
		if json.Unmarshal([]byte(raw), &result) == nil {
			return result, nil
		}
	}

	generatedAgent, err := agent.Generate(ctx, client, cluster, clusterEntries)
	if err != nil {
		return agentGenResult{}, err
	}
	result := agentGenResult{
		SystemPrompt: generatedAgent.SystemPrompt,
		Instructions: generatedAgent.Instructions,
		AntiPatterns: generatedAgent.AntiPatterns,
	}
	if raw, merr := json.Marshal(result); merr == nil {
		if perr := p.store.PutAnalysisCache(ctx, cacheKindAgent, key, string(raw), teamID); perr != nil {
			slog.Warn("analysis cache: put agent", "err", perr)
		}
	}
	return result, nil
}
