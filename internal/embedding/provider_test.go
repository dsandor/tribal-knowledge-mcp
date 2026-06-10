package embedding_test

import (
	"sync"
	"testing"

	"github.com/dsandor/memory/internal/embedding"
)

func TestProviderSameKeyReturnsSameInstance(t *testing.T) {
	p := embedding.NewProvider()
	e1 := p.Embedder("http://ollama:11434", "nomic-embed")
	e2 := p.Embedder("http://ollama:11434", "nomic-embed")
	if e1 == nil || e2 == nil {
		t.Fatal("expected non-nil embedders")
	}
	if e1 != e2 {
		t.Error("same (url, model) should return identical Embedder pointer")
	}
}

func TestProviderDifferentModelReturnsDifferentInstance(t *testing.T) {
	p := embedding.NewProvider()
	e1 := p.Embedder("http://ollama:11434", "nomic-embed")
	e2 := p.Embedder("http://ollama:11434", "mxbai-embed-large")
	if e1 == nil || e2 == nil {
		t.Fatal("expected non-nil embedders")
	}
	if e1 == e2 {
		t.Error("different model should produce different Embedder instance")
	}
}

func TestProviderDifferentURLReturnsDifferentInstance(t *testing.T) {
	p := embedding.NewProvider()
	e1 := p.Embedder("http://ollama-a:11434", "nomic-embed")
	e2 := p.Embedder("http://ollama-b:11434", "nomic-embed")
	if e1 == nil || e2 == nil {
		t.Fatal("expected non-nil embedders")
	}
	if e1 == e2 {
		t.Error("different url should produce different Embedder instance")
	}
}

func TestProviderEmptyURLReturnsNil(t *testing.T) {
	p := embedding.NewProvider()
	e := p.Embedder("", "nomic-embed")
	if e != nil {
		t.Error("empty url should return nil")
	}
}

func TestProviderConcurrentAccess(t *testing.T) {
	p := embedding.NewProvider()
	const goroutines = 50
	var wg sync.WaitGroup
	results := make([]embedding.Embedder, goroutines)

	wg.Add(goroutines)
	for i := range goroutines {
		go func(idx int) {
			defer wg.Done()
			results[idx] = p.Embedder("http://ollama:11434", "shared-model")
		}(i)
	}
	wg.Wait()

	first := results[0]
	if first == nil {
		t.Fatal("expected non-nil embedder")
	}
	for i, e := range results {
		if e != first {
			t.Errorf("goroutine %d got different instance (race or caching bug)", i)
		}
	}
}
