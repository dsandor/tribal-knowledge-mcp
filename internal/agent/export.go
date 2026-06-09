package agent

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/dsandor/memory/internal/storage"
)

// Export serializes an agent to the requested format.
// Supported formats: "md" (YAML frontmatter + markdown body), "txt" (plain text), "json" (full config).
// Returns an error message string for unknown formats.
func Export(a *storage.Agent, format string) string {
	switch format {
	case "md":
		return exportMD(a)
	case "txt":
		return exportTXT(a)
	case "json":
		return exportJSON(a)
	default:
		return fmt.Sprintf("unsupported format %q: use md, txt, or json", format)
	}
}

func exportMD(a *storage.Agent) string {
	var sb strings.Builder
	sb.WriteString("---\n")
	fmt.Fprintf(&sb, "name: %s-agent\n", a.Domain)
	fmt.Fprintf(&sb, "domain: %s\n", a.Domain)
	fmt.Fprintf(&sb, "version: %d\n", a.Version)
	fmt.Fprintf(&sb, "status: %s\n", string(a.Status))
	sb.WriteString("---\n\n")
	sb.WriteString(a.SystemPrompt)
	if a.Instructions != "" {
		sb.WriteString("\n\n## Instructions\n\n")
		sb.WriteString(a.Instructions)
	}
	if a.AntiPatterns != "" {
		sb.WriteString("\n\n## Anti-Patterns\n\n")
		sb.WriteString(a.AntiPatterns)
	}
	return sb.String()
}

func exportTXT(a *storage.Agent) string {
	var sb strings.Builder
	sb.WriteString(a.SystemPrompt)
	if a.Instructions != "" {
		sb.WriteString("\n\nInstructions:\n")
		sb.WriteString(a.Instructions)
	}
	if a.AntiPatterns != "" {
		sb.WriteString("\n\nAvoid:\n")
		sb.WriteString(a.AntiPatterns)
	}
	return sb.String()
}

func exportJSON(a *storage.Agent) string {
	type exportShape struct {
		ID           string   `json:"id"`
		Domain       string   `json:"domain"`
		Version      int      `json:"version"`
		Status       string   `json:"status"`
		SystemPrompt string   `json:"system_prompt"`
		Instructions string   `json:"instructions"`
		AntiPatterns string   `json:"anti_patterns"`
		SourceRefs   []string `json:"source_refs"`
		ClusterID    string   `json:"cluster_id"`
	}
	refs := a.SourceRefs
	if refs == nil {
		refs = []string{}
	}
	data, _ := json.MarshalIndent(exportShape{
		ID:           a.ID,
		Domain:       a.Domain,
		Version:      a.Version,
		Status:       string(a.Status),
		SystemPrompt: a.SystemPrompt,
		Instructions: a.Instructions,
		AntiPatterns: a.AntiPatterns,
		SourceRefs:   refs,
		ClusterID:    a.ClusterID,
	}, "", "  ")
	return string(data)
}
