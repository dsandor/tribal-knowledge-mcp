package mcp_test

import (
	"encoding/json"
	"math"
	"testing"

	"github.com/dsandor/memory/internal/enrich"
	internalmcp "github.com/dsandor/memory/internal/mcp"
	"github.com/dsandor/memory/internal/storage"
)

// enrichContextResp mirrors the JSON shape of enrich_context's response for the
// fields this test asserts on.
type enrichContextResp struct {
	RelevantKnowledge []struct {
		ID    string  `json:"id"`
		Title string  `json:"title"`
		Score float64 `json:"score"`
	} `json:"relevant_knowledge"`
}

// TestHandleEnrichContext_SelectFiltersByRelevance verifies that the handler
// runs SearchSimilar results through enrich.Select: with a MinRelevance prefs
// threshold, a far entry (low relevance) is dropped while a close entry is kept,
// and the reported Score is the normalized relevance (1 - distance), not the raw
// vector distance returned by SearchSimilar.
func TestHandleEnrichContext_SelectFiltersByRelevance(t *testing.T) {
	const actorID = "test-user-t1"

	store := &mockStore{
		entries: []storage.KnowledgeEntry{
			{ID: "near", Title: "Close Entry", TeamID: "t1"},
			{ID: "far", Title: "Far Entry", TeamID: "t1"},
		},
		// Raw vector distances: near -> relevance 0.9, far -> relevance 0.2.
		searchScores: map[string]float64{
			"near": 0.1,
			"far":  0.8,
		},
		enrichPrefs: map[string]*storage.EnrichmentPrefs{
			actorID: {
				MinRelevance:    0.5,
				MinRelevanceSet: true,
				MaxMemories:     5,
				MaxMemoriesSet:  true,
			},
		},
	}

	embedder := &mockEmbedder{embedding: []float32{0.1, 0.2, 0.3}}
	// No LLM client so the rule-enhanced prompt is returned verbatim and the
	// test stays focused on selection behavior.
	src := newTestSources(embedder, nil)

	handler := internalmcp.HandleEnrichContext(store, src, nil, enrich.EnrichDefaults{
		MinRelevance: 0.0,
		MaxMemories:  5,
	})

	ctx := ctxWithTeam("t1")
	req := callReq("prompt", "what is the best approach for financial analysis?")

	result, err := handler(ctx, req)
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if result.IsError {
		t.Fatalf("tool returned error: %s", textContent(result))
	}

	var resp enrichContextResp
	if err := json.Unmarshal([]byte(textContent(result)), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}

	if len(resp.RelevantKnowledge) != 1 {
		t.Fatalf("expected exactly 1 relevant entry (far entry excluded by threshold), got %d: %+v",
			len(resp.RelevantKnowledge), resp.RelevantKnowledge)
	}
	got := resp.RelevantKnowledge[0]
	if got.ID != "near" {
		t.Errorf("kept entry ID = %q, want %q", got.ID, "near")
	}
	// Score must be the normalized relevance (1 - distance = 1 - 0.1 = 0.9),
	// NOT the raw distance (0.1).
	if math.Abs(got.Score-0.9) > 1e-9 {
		t.Errorf("Score = %v, want normalized relevance 0.9", got.Score)
	}
}

// TestHandleEnrichContext_AppliesDefaultsWhenPrefsUnset verifies that when the
// caller has no saved prefs, the deployment EnrichDefaults threshold is applied:
// a far entry below the default MinRelevance is excluded.
func TestHandleEnrichContext_AppliesDefaultsWhenPrefsUnset(t *testing.T) {
	store := &mockStore{
		entries: []storage.KnowledgeEntry{
			{ID: "near", Title: "Close Entry", TeamID: "t1"},
			{ID: "far", Title: "Far Entry", TeamID: "t1"},
		},
		searchScores: map[string]float64{
			"near": 0.1, // relevance 0.9
			"far":  0.8, // relevance 0.2
		},
		// enrichPrefs nil -> GetEnrichmentPrefs returns nil -> full defaults.
	}

	embedder := &mockEmbedder{embedding: []float32{0.1, 0.2, 0.3}}
	src := newTestSources(embedder, nil)

	handler := internalmcp.HandleEnrichContext(store, src, nil, enrich.EnrichDefaults{
		MinRelevance: 0.5,
		MaxMemories:  5,
	})

	ctx := ctxWithTeam("t1")
	req := callReq("prompt", "what is the best approach for financial analysis?")

	result, err := handler(ctx, req)
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if result.IsError {
		t.Fatalf("tool returned error: %s", textContent(result))
	}

	var resp enrichContextResp
	if err := json.Unmarshal([]byte(textContent(result)), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}

	if len(resp.RelevantKnowledge) != 1 || resp.RelevantKnowledge[0].ID != "near" {
		t.Fatalf("expected only the near entry after applying default threshold, got %+v",
			resp.RelevantKnowledge)
	}
}
