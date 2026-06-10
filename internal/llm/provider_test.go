package llm_test

import (
	"sync"
	"testing"

	"github.com/dsandor/memory/internal/llm"
)

func TestProviderSameKeyReturnsSameInstance(t *testing.T) {
	p := llm.NewProvider()
	c1 := p.Client("key-a", "model-x")
	c2 := p.Client("key-a", "model-x")
	if c1 == nil || c2 == nil {
		t.Fatal("expected non-nil clients")
	}
	if c1 != c2 {
		t.Error("same (apiKey, model) should return identical Client pointer")
	}
}

func TestProviderDifferentModelReturnsDifferentInstance(t *testing.T) {
	p := llm.NewProvider()
	c1 := p.Client("key-a", "model-x")
	c2 := p.Client("key-a", "model-y")
	if c1 == nil || c2 == nil {
		t.Fatal("expected non-nil clients")
	}
	if c1 == c2 {
		t.Error("different model should produce different Client instance")
	}
}

func TestProviderDifferentKeyReturnsDifferentInstance(t *testing.T) {
	p := llm.NewProvider()
	c1 := p.Client("key-a", "model-x")
	c2 := p.Client("key-b", "model-x")
	if c1 == nil || c2 == nil {
		t.Fatal("expected non-nil clients")
	}
	if c1 == c2 {
		t.Error("different apiKey should produce different Client instance")
	}
}

func TestProviderEmptyKeyReturnsNil(t *testing.T) {
	p := llm.NewProvider()
	c := p.Client("", "model-x")
	if c != nil {
		t.Error("empty apiKey should return nil")
	}
}

func TestProviderConcurrentAccess(t *testing.T) {
	p := llm.NewProvider()
	const goroutines = 50
	var wg sync.WaitGroup
	results := make([]llm.Client, goroutines)

	wg.Add(goroutines)
	for i := range goroutines {
		go func(idx int) {
			defer wg.Done()
			results[idx] = p.Client("shared-key", "shared-model")
		}(i)
	}
	wg.Wait()

	first := results[0]
	if first == nil {
		t.Fatal("expected non-nil client")
	}
	for i, c := range results {
		if c != first {
			t.Errorf("goroutine %d got different instance (race or caching bug)", i)
		}
	}
}
