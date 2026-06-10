package pipeline

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/dsandor/memory/internal/live"
	"github.com/dsandor/memory/internal/storage"
)

// pipelineFakeBus captures events for assertions inside the pipeline package tests.
type pipelineFakeBus struct {
	mu     sync.Mutex
	events []live.LiveEvent
}

func (f *pipelineFakeBus) Publish(ev live.LiveEvent) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.events = append(f.events, ev)
}

func (f *pipelineFakeBus) Subscribe(_ string, _ bool) (<-chan live.LiveEvent, func()) {
	ch := make(chan live.LiveEvent)
	return ch, func() { close(ch) }
}

func (f *pipelineFakeBus) snapshot() []live.LiveEvent {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]live.LiveEvent, len(f.events))
	copy(out, f.events)
	return out
}

// eventsOfType filters a snapshot by event type.
func eventsOfType(evs []live.LiveEvent, t string) []live.LiveEvent {
	var out []live.LiveEvent
	for _, ev := range evs {
		if ev.Type == t {
			out = append(out, ev)
		}
	}
	return out
}

// compile-time check
var _ live.EventBus = (*pipelineFakeBus)(nil)

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

func TestPipeline_WithLivePublish_PublishesPipelineComplete(t *testing.T) {
	store := &mockAnalysisStore{
		entries: []storage.KnowledgeEntry{
			{ID: "a", Title: "Entry A", Content: "Finance pattern", Domain: "finance"},
		},
		embeddings: map[string][]float32{
			"a": {1, 0, 0, 0},
		},
	}
	llmMock := &mockLLM{response: `{"gaps":[]}`}
	bus := &pipelineFakeBus{}

	p := New(store, newSrc(llmMock), Config{
		MinEntries:       1,
		Interval:         time.Hour,
		ClusterThreshold: 0.9,
	}).WithLivePublish(bus)

	if err := p.Run(context.Background(), "test"); err != nil {
		t.Fatalf("pipeline run: %v", err)
	}

	evs := eventsOfType(bus.snapshot(), live.TypePipelineComplete)
	if len(evs) != 1 {
		t.Fatalf("expected 1 pipeline_complete event, got %d", len(evs))
	}
	ev := evs[0]
	if ev.Actor.ID != "pipeline" {
		t.Errorf("actor ID = %q, want pipeline", ev.Actor.ID)
	}
	if ev.ID == "" {
		t.Error("event ID should be filled")
	}
	if ev.CreatedAt.IsZero() {
		t.Error("CreatedAt should be filled")
	}
	if ev.Meta["status"] == "" {
		t.Error("meta.status should be set")
	}
}

func TestPipeline_WithLivePublish_NilBus_NoPanic(t *testing.T) {
	store := &mockAnalysisStore{
		entries: []storage.KnowledgeEntry{
			{ID: "a", Title: "Entry A", Content: "Finance pattern", Domain: "finance"},
		},
		embeddings: map[string][]float32{
			"a": {1, 0, 0, 0},
		},
	}
	llmMock := &mockLLM{response: `{"gaps":[]}`}

	// Intentionally do NOT call WithLivePublish — liveBus remains nil.
	p := New(store, newSrc(llmMock), Config{
		MinEntries:       1,
		Interval:         time.Hour,
		ClusterThreshold: 0.9,
	})

	if err := p.Run(context.Background(), "test"); err != nil {
		t.Fatalf("pipeline run with nil bus: %v", err)
	}
}

func TestPipeline_WithLivePublish_PublishesAgentGenerated(t *testing.T) {
	baseStore := &mockAnalysisStore{
		entries: []storage.KnowledgeEntry{
			{ID: "a", Title: "Entry A", Content: "Finance pattern", Domain: "finance"},
			{ID: "b", Title: "Entry B", Content: "Finance workflow", Domain: "finance"},
		},
		embeddings: map[string][]float32{
			"a": {1, 0, 0, 0},
			"b": {0.99, 0.14, 0, 0},
		},
	}
	agentStore := &mockAgentStore{}
	llmMock := &mockLLM{response: `{"title":"Finance","summary":"Finance entries.","coherence":0.8,"specificity":0.7,"gaps":[]}`}
	agentLLMMock := &mockLLM{response: `{"system_prompt":"You are a finance agent.","instructions":"Use DCF.","anti_patterns":"No guessing."}`}
	bus := &pipelineFakeBus{}

	p := New(baseStore, newSrcWithAgent(llmMock, agentLLMMock, llmMock), Config{
		MinEntries:       2,
		Interval:         time.Hour,
		ClusterThreshold: 0.9,
	}).
		WithAgentGeneration(agentStore).
		WithLivePublish(bus)

	if err := p.Run(context.Background(), "test"); err != nil {
		t.Fatalf("pipeline run: %v", err)
	}

	agentEvs := eventsOfType(bus.snapshot(), live.TypeAgentGenerated)
	if len(agentEvs) != 1 {
		t.Fatalf("expected 1 agent_generated event, got %d", len(agentEvs))
	}
	ev := agentEvs[0]
	if ev.Actor.ID != "pipeline" {
		t.Errorf("actor ID = %q, want pipeline", ev.Actor.ID)
	}
	if ev.Title == "" {
		t.Error("Title (domain) should be set on agent_generated event")
	}
	if ev.Meta["domain"] != "finance" {
		t.Errorf("meta.domain = %q, want finance", ev.Meta["domain"])
	}
}
