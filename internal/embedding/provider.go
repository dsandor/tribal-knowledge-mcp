package embedding

import "sync"

// Provider returns cached Embedder instances keyed by (url, model).
// The same (url, model) pair always yields the identical Embedder pointer,
// allowing callers to share HTTP connection pools across requests.
//
// All methods are goroutine-safe.
type Provider struct {
	mu    sync.Mutex
	cache map[string]Embedder
}

// NewProvider creates an empty Provider.
func NewProvider() *Provider {
	return &Provider{cache: make(map[string]Embedder)}
}

// Embedder returns a cached OllamaEmbedder for the given (url, model) pair.
// Returns nil when url is empty (no Ollama endpoint configured).
func (p *Provider) Embedder(url, model string) Embedder {
	if url == "" {
		return nil
	}
	key := url + "|" + model

	p.mu.Lock()
	defer p.mu.Unlock()

	if e, ok := p.cache[key]; ok {
		return e
	}
	e := NewOllamaEmbedder(url, model)
	p.cache[key] = e
	return e
}
