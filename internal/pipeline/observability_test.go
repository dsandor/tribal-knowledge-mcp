package pipeline

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/dsandor/memory/internal/storage"
)

// newTestLogger installs a buffer-backed slog handler at Debug level as the
// default logger and returns the buffer plus a cleanup func that restores the
// previous default. Callers should defer the returned cleanup.
func newTestLogger(t *testing.T) *bytes.Buffer {
	t.Helper()
	prev := slog.Default()
	var buf bytes.Buffer
	h := slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})
	slog.SetDefault(slog.New(h))
	t.Cleanup(func() { slog.SetDefault(prev) })
	return &buf
}

// minimalStore returns a store+src pair that satisfies Run() end-to-end with
// one team and one cluster (two similar entries) — minimal but real enough to
// exercise lifecycle logs.
func minimalLifecycleFixture() (*mockAnalysisStore, *mockAISource) {
	store := &mockAnalysisStore{
		entries: []storage.KnowledgeEntry{
			{ID: "x1", Title: "Finance A", Content: "Finance pattern A", Domain: "finance"},
			{ID: "x2", Title: "Finance B", Content: "Finance pattern B", Domain: "finance"},
		},
		embeddings: map[string][]float32{
			"x1": {1, 0, 0, 0},
			"x2": {0.99, 0.14, 0, 0},
		},
	}
	llmMock := &mockLLM{
		response: `{"title":"Finance","summary":"Finance entries.","coherence":0.8,"specificity":0.7,"gaps":[]}`,
	}
	src := &mockAISource{
		analysisClient:    llmMock,
		improvementClient: llmMock,
		fingerprint:       "testprovider|testmodel",
	}
	return store, src
}

// TestRunLogsLifecycle verifies that a successful single-team run emits:
//   - "pipeline run started" at Info with team, analysis_llm, agents_llm, and
//     improvement_llm fields present.
//   - "pipeline run finished" at Info with status, duration_ms, and clusters fields.
func TestRunLogsLifecycle(t *testing.T) {
	buf := newTestLogger(t)

	store, src := minimalLifecycleFixture()
	p := New(store, src, Config{
		MinEntries:       1,
		Interval:         time.Hour,
		ClusterThreshold: 0.9,
	})

	if err := p.Run(context.Background(), "manual"); err != nil {
		t.Fatalf("Run error: %v", err)
	}

	out := buf.String()

	// "pipeline run started" must be present with the three LLM fingerprint fields.
	if !strings.Contains(out, "pipeline run started") {
		t.Errorf("expected 'pipeline run started' in log output; got:\n%s", out)
	}
	if !strings.Contains(out, "analysis_llm=") {
		t.Errorf("expected 'analysis_llm=' field in log output; got:\n%s", out)
	}
	if !strings.Contains(out, "agents_llm=") {
		t.Errorf("expected 'agents_llm=' field in log output; got:\n%s", out)
	}
	if !strings.Contains(out, "improvement_llm=") {
		t.Errorf("expected 'improvement_llm=' field in log output; got:\n%s", out)
	}

	// "pipeline run finished" must be present with status, duration_ms, clusters.
	if !strings.Contains(out, "pipeline run finished") {
		t.Errorf("expected 'pipeline run finished' in log output; got:\n%s", out)
	}
	if !strings.Contains(out, "status=") {
		t.Errorf("expected 'status=' field in finished log; got:\n%s", out)
	}
	if !strings.Contains(out, "duration_ms=") {
		t.Errorf("expected 'duration_ms=' field in finished log; got:\n%s", out)
	}
	if !strings.Contains(out, "clusters=") {
		t.Errorf("expected 'clusters=' field in finished log; got:\n%s", out)
	}
}

// scoringErrorStore wraps mockAnalysisStore but causes GetAnalysisCache to
// always return a miss so the scoring LLM is always called.
type scoringErrorStore struct {
	mockAnalysisStore
}

// GetAnalysisCache always returns a miss so the LLM is invoked.
func (s *scoringErrorStore) GetAnalysisCache(_ context.Context, _, _ string) (string, bool, error) {
	return "", false, nil
}

// TestStageFailureLogged verifies that when the scoring LLM errors (which
// causes summarize/score failures inside a cluster loop), a WARN log line is
// emitted containing "stage" and "team" fields.
func TestStageFailureLogged(t *testing.T) {
	buf := newTestLogger(t)

	store := &scoringErrorStore{
		mockAnalysisStore: mockAnalysisStore{
			entries: []storage.KnowledgeEntry{
				{ID: "y1", Title: "Finance A", Content: "Finance pattern A", Domain: "finance", TeamID: "teamX"},
				{ID: "y2", Title: "Finance B", Content: "Finance pattern B", Domain: "finance", TeamID: "teamX"},
			},
			embeddings: map[string][]float32{
				"y1": {1, 0, 0, 0},
				"y2": {0.99, 0.14, 0, 0},
			},
		},
	}

	// summarize will fail because of the error response
	failLLM := &mockLLM{err: errors.New("llm unavailable")}
	src := &mockAISource{
		analysisClient:    failLLM,
		improvementClient: failLLM,
	}

	p := New(store, src, Config{
		MinEntries:       1,
		Interval:         time.Hour,
		ClusterThreshold: 0.9,
	})

	_ = p.Run(context.Background(), "manual")

	out := buf.String()

	// Expect at least one WARN line with stage and team fields.
	if !strings.Contains(out, "level=WARN") {
		t.Errorf("expected at least one WARN line; got:\n%s", out)
	}
	if !strings.Contains(out, "stage=") {
		t.Errorf("expected 'stage=' field in WARN log; got:\n%s", out)
	}
	if !strings.Contains(out, "team=") {
		t.Errorf("expected 'team=' field in WARN log; got:\n%s", out)
	}
}
