package embedding

import (
	"crypto/sha256"
	"encoding/hex"
	"sync"
)

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

// OpenAIEmbedder returns a cached OpenAIEmbedder for the given
// (baseURL, apiKey, model). Caching lets callers share an HTTP connection
// pool. The map key never contains the raw apiKey: a short SHA-256 prefix of
// the key is used so that rotating the key yields a fresh embedder without
// persisting the plaintext secret in memory as a map key.
func (p *Provider) OpenAIEmbedder(baseURL, apiKey, model string) Embedder {
	keyHash := sha256.Sum256([]byte(apiKey))
	cacheKey := "openai|" + baseURL + "|" + model + "|" + hex.EncodeToString(keyHash[:8])

	p.mu.Lock()
	defer p.mu.Unlock()

	if e, ok := p.cache[cacheKey]; ok {
		return e
	}
	e := NewOpenAIEmbedder(baseURL, apiKey, model)
	p.cache[cacheKey] = e
	return e
}
