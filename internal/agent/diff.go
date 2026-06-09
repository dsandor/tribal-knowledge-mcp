package agent

import (
	"fmt"
	"strings"

	"github.com/dsandor/memory/internal/storage"
)

// Diff produces a human-readable changelog string comparing two agent versions.
// old may be nil (indicates first generation). updated must be non-nil.
func Diff(old, updated *storage.Agent) string {
	if old == nil {
		return "initial generation"
	}
	var changes []string
	if old.SystemPrompt != updated.SystemPrompt {
		changes = append(changes, "system_prompt updated")
	}
	if old.Instructions != updated.Instructions {
		changes = append(changes, "instructions updated")
	}
	if old.AntiPatterns != updated.AntiPatterns {
		changes = append(changes, "anti_patterns updated")
	}
	if len(changes) == 0 {
		return "no changes"
	}
	return fmt.Sprintf("changed: %s", strings.Join(changes, ", "))
}
