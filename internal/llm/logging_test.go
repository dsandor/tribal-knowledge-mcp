package llm

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
)

type fakeInnerClient struct {
	out string
	err error
}

func (f *fakeInnerClient) Complete(ctx context.Context, prompt string) (string, error) {
	return f.out, f.err
}

func newTestLogger(buf *bytes.Buffer) *slog.Logger {
	return slog.New(slog.NewTextHandler(buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
}

// TestLoggingClientPassthrough: inner returns ("hi", nil) → wrapper returns identical;
// captured log contains level=DEBUG msg="llm call" and the attrs given.
func TestLoggingClientPassthrough(t *testing.T) {
	var buf bytes.Buffer
	logger := newTestLogger(&buf)
	prev := slog.Default()
	slog.SetDefault(logger)
	t.Cleanup(func() { slog.SetDefault(prev) })

	inner := &fakeInnerClient{out: "hi", err: nil}
	c := &LoggingClient{
		Inner: inner,
		Attrs: []any{"provider", "anthropic", "model", "claude-x", "touchpoint", "analysis", "team", "t1"},
	}

	out, err := c.Complete(context.Background(), "test prompt")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out != "hi" {
		t.Fatalf("got %q, want %q", out, "hi")
	}

	log := buf.String()
	if !strings.Contains(log, "DEBUG") {
		t.Errorf("expected DEBUG log, got: %s", log)
	}
	if !strings.Contains(log, "llm call") {
		t.Errorf("expected msg='llm call', got: %s", log)
	}
	if !strings.Contains(log, "provider=anthropic") {
		t.Errorf("expected provider=anthropic, got: %s", log)
	}
	if !strings.Contains(log, "duration_ms=") {
		t.Errorf("expected duration_ms, got: %s", log)
	}
}

// TestLoggingClientErrorWarns: inner errors → identical error returned; captured log
// contains level=WARN msg="llm call failed" with err and duration_ms.
func TestLoggingClientErrorWarns(t *testing.T) {
	var buf bytes.Buffer
	logger := newTestLogger(&buf)
	prev := slog.Default()
	slog.SetDefault(logger)
	t.Cleanup(func() { slog.SetDefault(prev) })

	wantErr := errors.New("connection refused")
	inner := &fakeInnerClient{out: "", err: wantErr}
	c := &LoggingClient{
		Inner: inner,
		Attrs: []any{"provider", "ollama", "model", "llama3", "touchpoint", "agents", "team", "t2"},
	}

	out, err := c.Complete(context.Background(), "prompt")
	if !errors.Is(err, wantErr) {
		t.Fatalf("got err=%v, want %v", err, wantErr)
	}
	if out != "" {
		t.Fatalf("got out=%q, want empty", out)
	}

	log := buf.String()
	if !strings.Contains(log, "WARN") {
		t.Errorf("expected WARN log, got: %s", log)
	}
	if !strings.Contains(log, "llm call failed") {
		t.Errorf("expected msg='llm call failed', got: %s", log)
	}
	if !strings.Contains(log, "err=") {
		t.Errorf("expected err field, got: %s", log)
	}
	if !strings.Contains(log, "duration_ms=") {
		t.Errorf("expected duration_ms, got: %s", log)
	}
}
