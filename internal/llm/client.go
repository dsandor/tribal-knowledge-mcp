package llm

import "context"

// Client is the interface for LLM text completion.
type Client interface {
	Complete(ctx context.Context, prompt string) (string, error)
}
