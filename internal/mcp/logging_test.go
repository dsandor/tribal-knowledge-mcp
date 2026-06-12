package mcp

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"

	mcplib "github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// captureSlog installs a buffer-backed slog handler at Debug level, returning
// the buffer and a restore function. Callers must call restore() (or pass it
// to t.Cleanup) to reinstate the previous default logger.
func captureSlog(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	h := slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})
	prev := slog.Default()
	slog.SetDefault(slog.New(h))
	t.Cleanup(func() { slog.SetDefault(prev) })
	return &buf
}

// nopHandler returns a ToolHandlerFunc that immediately returns the given result and error.
func nopHandler(result *mcplib.CallToolResult, err error) server.ToolHandlerFunc {
	return func(_ context.Context, _ mcplib.CallToolRequest) (*mcplib.CallToolResult, error) {
		return result, err
	}
}

// TestLogToolOK verifies that a successful handler:
//   - returns the result unchanged
//   - logs msg="mcp tool", tool=<name>, status="ok", duration_ms present
func TestLogToolOK(t *testing.T) {
	buf := captureSlog(t)

	want := mcplib.NewToolResultText("hello")
	wrapped := logTool("test_tool", nopHandler(want, nil))
	got, err := wrapped(context.Background(), mcplib.CallToolRequest{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != want {
		t.Errorf("result not passed through: got %v want %v", got, want)
	}

	line := buf.String()
	if !strings.Contains(line, "mcp tool") {
		t.Errorf("log missing msg=mcp tool: %s", line)
	}
	if !strings.Contains(line, "tool=test_tool") {
		t.Errorf("log missing tool field: %s", line)
	}
	if !strings.Contains(line, "status=ok") {
		t.Errorf("log missing status=ok: %s", line)
	}
	if !strings.Contains(line, "duration_ms=") {
		t.Errorf("log missing duration_ms field: %s", line)
	}
	if strings.Contains(line, "err=") {
		t.Errorf("log should not contain err= on success: %s", line)
	}
}

// TestLogToolError verifies that a handler returning IsError=true:
//   - returns the result unchanged
//   - logs status="error"
func TestLogToolError(t *testing.T) {
	buf := captureSlog(t)

	errResult := mcplib.NewToolResultError("something broke")
	wrapped := logTool("test_tool_err", nopHandler(errResult, nil))
	got, err := wrapped(context.Background(), mcplib.CallToolRequest{})
	if err != nil {
		t.Fatalf("unexpected protocol error: %v", err)
	}
	if got != errResult {
		t.Errorf("result not passed through")
	}

	line := buf.String()
	if !strings.Contains(line, "status=error") {
		t.Errorf("log missing status=error: %s", line)
	}
	if !strings.Contains(line, "tool=test_tool_err") {
		t.Errorf("log missing tool field: %s", line)
	}
}

// TestLogToolProtocolError verifies that a handler returning (nil, err):
//   - propagates the error unchanged
//   - logs status="error" with the err field present
func TestLogToolProtocolError(t *testing.T) {
	buf := captureSlog(t)

	boom := errors.New("boom")
	wrapped := logTool("test_tool_proto", nopHandler(nil, boom))
	got, err := wrapped(context.Background(), mcplib.CallToolRequest{})
	if err != boom {
		t.Errorf("expected err=boom, got %v", err)
	}
	if got != nil {
		t.Errorf("expected nil result, got %v", got)
	}

	line := buf.String()
	if !strings.Contains(line, "status=error") {
		t.Errorf("log missing status=error: %s", line)
	}
	if !strings.Contains(line, "err=boom") {
		t.Errorf("log missing err field: %s", line)
	}
	if !strings.Contains(line, "tool=test_tool_proto") {
		t.Errorf("log missing tool field: %s", line)
	}
}
