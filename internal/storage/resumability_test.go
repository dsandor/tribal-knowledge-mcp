package storage

import (
	"context"
	"testing"
	"time"
)

func TestMarkInterruptedRuns(t *testing.T) {
	s := newTestStoreInternal(t)
	ctx := context.Background()

	runID, err := s.StartPipelineRun(ctx, "interval", "t1")
	if err != nil {
		t.Fatalf("start run: %v", err)
	}
	doneID, err := s.StartPipelineRun(ctx, "manual", "t1")
	if err != nil {
		t.Fatalf("start done run: %v", err)
	}
	if err := s.FinishPipelineRun(ctx, doneID, "completed", 5, 2, nil); err != nil {
		t.Fatalf("finish run: %v", err)
	}

	n, err := s.MarkInterruptedRuns(ctx)
	if err != nil {
		t.Fatalf("mark interrupted: %v", err)
	}
	if n != 1 {
		t.Fatalf("marked %d runs, want 1", n)
	}

	runs, err := s.ListPipelineRuns(ctx, "t1", 10)
	if err != nil {
		t.Fatalf("list runs: %v", err)
	}
	for _, r := range runs {
		switch r.ID {
		case runID:
			if r.Status != "failed" {
				t.Errorf("interrupted run status = %q, want failed", r.Status)
			}
			if len(r.Errors) == 0 || r.Errors[0] != "interrupted by restart" {
				t.Errorf("interrupted run errors = %v, want [interrupted by restart]", r.Errors)
			}
			if r.CompletedAt == nil {
				t.Error("interrupted run has nil CompletedAt")
			}
		case doneID:
			if r.Status != "completed" {
				t.Errorf("completed run mutated to %q", r.Status)
			}
		}
	}

	n2, err := s.MarkInterruptedRuns(ctx)
	if err != nil || n2 != 0 {
		t.Fatalf("second mark = %d err=%v, want 0 nil", n2, err)
	}
}

func TestAnalysisCacheRoundTrip(t *testing.T) {
	s := newTestStoreInternal(t)
	ctx := context.Background()

	if _, ok, err := s.GetAnalysisCache(ctx, "score", "k1"); err != nil || ok {
		t.Fatalf("expected miss, got ok=%v err=%v", ok, err)
	}
	if err := s.PutAnalysisCache(ctx, "score", "k1", `{"coherence":0.8}`, "t1"); err != nil {
		t.Fatalf("put: %v", err)
	}
	v, ok, err := s.GetAnalysisCache(ctx, "score", "k1")
	if err != nil || !ok || v != `{"coherence":0.8}` {
		t.Fatalf("get = %q ok=%v err=%v", v, ok, err)
	}
	if err := s.PutAnalysisCache(ctx, "score", "k1", `{"coherence":0.9}`, "t1"); err != nil {
		t.Fatalf("overwrite: %v", err)
	}
	v, _, _ = s.GetAnalysisCache(ctx, "score", "k1")
	if v != `{"coherence":0.9}` {
		t.Fatalf("after overwrite got %q", v)
	}
	if _, ok, _ := s.GetAnalysisCache(ctx, "summary", "k1"); ok {
		t.Fatal("kind namespacing broken")
	}
	time.Sleep(1100 * time.Millisecond) // created_at is second-precision; ensure it's in the past
	deleted, err := s.PruneAnalysisCache(ctx, 0)
	if err != nil || deleted != 1 {
		t.Fatalf("prune = %d err=%v, want 1", deleted, err)
	}
	if _, ok, _ := s.GetAnalysisCache(ctx, "score", "k1"); ok {
		t.Fatal("entry survived prune")
	}
}
