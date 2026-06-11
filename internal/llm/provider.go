package llm

import "sync"

// Provider returns cached Client instances keyed by (apiKey, model).
// The same (apiKey, model) pair always yields the identical Client pointer,
// which avoids unnecessary allocation and allows callers to share HTTP
// connection pools across requests.
//
// All methods are goroutine-safe.
type Provider struct {
	mu    sync.Mutex
	cache map[string]Client
}

// NewProvider creates an empty Provider.
func NewProvider() *Provider {
	return &Provider{cache: make(map[string]Client)}
}

// Client returns a cached AnthropicClient for the given (apiKey, model) pair.
// Returns nil when apiKey is empty (no credentials available).
func (p *Provider) Client(apiKey, model string) Client {
	if apiKey == "" {
		return nil
	}
	key := apiKey + "|" + model

	p.mu.Lock()
	defer p.mu.Unlock()

	if c, ok := p.cache[key]; ok {
		return c
	}
	c := NewAnthropicClient(apiKey, model)
	p.cache[key] = c
	return c
}

// Ollama returns a cached OllamaClient for the given (url, model) pair.
// Returns nil when url or model is empty (Ollama LLM not configured).
func (p *Provider) Ollama(url, model string) Client {
	if url == "" || model == "" {
		return nil
	}
	key := "ollama|" + url + "|" + model

	p.mu.Lock()
	defer p.mu.Unlock()

	if c, ok := p.cache[key]; ok {
		return c
	}
	c := NewOllamaClient(url, model)
	p.cache[key] = c
	return c
}
