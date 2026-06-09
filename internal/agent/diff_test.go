package agent

import (
	"strings"
	"testing"

	"github.com/dsandor/memory/internal/storage"
)

func TestDiff_NoChange(t *testing.T) {
	a := &storage.Agent{SystemPrompt: "SP", Instructions: "I", AntiPatterns: "AP"}
	result := Diff(a, a)
	if result != "no changes" {
		t.Errorf("Diff(identical) = %q, want %q", result, "no changes")
	}
}

func TestDiff_SystemPromptChanged(t *testing.T) {
	old := &storage.Agent{SystemPrompt: "old prompt", Instructions: "I", AntiPatterns: "AP"}
	newA := &storage.Agent{SystemPrompt: "new prompt", Instructions: "I", AntiPatterns: "AP"}
	result := Diff(old, newA)
	if !strings.Contains(result, "system_prompt") {
		t.Errorf("Diff should mention system_prompt, got: %q", result)
	}
}

func TestDiff_MultipleFieldsChanged(t *testing.T) {
	old := &storage.Agent{SystemPrompt: "SP1", Instructions: "I1", AntiPatterns: "AP1"}
	newA := &storage.Agent{SystemPrompt: "SP2", Instructions: "I2", AntiPatterns: "AP1"}
	result := Diff(old, newA)
	if !strings.Contains(result, "system_prompt") {
		t.Errorf("Diff should mention system_prompt")
	}
	if !strings.Contains(result, "instructions") {
		t.Errorf("Diff should mention instructions")
	}
}

func TestDiff_NilOld(t *testing.T) {
	newA := &storage.Agent{SystemPrompt: "SP", Instructions: "I", AntiPatterns: "AP"}
	result := Diff(nil, newA)
	if !strings.Contains(result, "initial generation") {
		t.Errorf("Diff(nil, new) should say initial generation, got: %q", result)
	}
}
