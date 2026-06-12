package llm

import (
	"context"
	"log/slog"
	"time"
)

// LoggingClient wraps a Client with structured logging: failures at Warn with
// full provider context, successes at Debug with duration.
type LoggingClient struct {
	Inner Client
	Attrs []any // alternating key/value pairs: provider, model, touchpoint, team
}

func (c *LoggingClient) Complete(ctx context.Context, prompt string) (string, error) {
	start := time.Now()
	out, err := c.Inner.Complete(ctx, prompt)
	d := time.Since(start).Milliseconds()
	if err != nil {
		args := append(append([]any{}, c.Attrs...), "duration_ms", d, "err", err)
		slog.Warn("llm call failed", args...)
		return out, err
	}
	args := append(append([]any{}, c.Attrs...), "duration_ms", d)
	slog.Debug("llm call", args...)
	return out, nil
}
