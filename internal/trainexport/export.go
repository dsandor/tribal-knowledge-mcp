// Package trainexport builds fine-tuning datasets from FT sessions and knowledge entries.
package trainexport

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/dsandor/memory/internal/storage"
	"github.com/google/uuid"
)

const SchemaVersion = "tribal-ft-v1"

// Options control what is exported.
type Options struct {
	TeamID            string
	MinRating         int  // session outcome and/or entry rating floor
	TrainEligibleOnly bool // default true for CLI
	Since             *time.Time
	Until             *time.Time
	// Formats: "sft", "dpo", "sharegpt", or "all"
	Format string
	// IncludeEntryBootstrap adds SFT rows from approved knowledge entries.
	IncludeEntryBootstrap bool
	OutDir                string
}

// Manifest is written alongside JSONL files.
type Manifest struct {
	FormatVersion string         `json:"format_version"`
	Schema        string         `json:"schema"`
	ExportedAt    time.Time      `json:"exported_at"`
	TeamID        string         `json:"team_id"`
	Filters       map[string]any `json:"filters"`
	Counts        map[string]int `json:"counts"`
}

// SFTRow is one instruction-tuning example.
type SFTRow struct {
	ID        string   `json:"id"`
	SessionID string   `json:"session_id,omitempty"`
	Domain    string   `json:"domain,omitempty"`
	Source    string   `json:"source"`
	System    string   `json:"system,omitempty"`
	Instruction string `json:"instruction"`
	Input     string   `json:"input,omitempty"`
	Output    string   `json:"output"`
	EntryIDs  []string `json:"entry_ids,omitempty"`
	Rating    float64  `json:"rating,omitempty"`
	Weight    float64  `json:"weight"`
}

// DPORow is one preference pair.
type DPORow struct {
	ID             string  `json:"id"`
	SessionID      string  `json:"session_id,omitempty"`
	Prompt         string  `json:"prompt"`
	Chosen         string  `json:"chosen"`
	Rejected       string  `json:"rejected"`
	Source         string  `json:"source"`
	RatingChosen   int     `json:"rating_chosen,omitempty"`
	RatingRejected int     `json:"rating_rejected,omitempty"`
}

// ShareGPTRow is a multi-turn conversation.
type ShareGPTRow struct {
	ID            string              `json:"id"`
	SessionID     string              `json:"session_id,omitempty"`
	Conversations []ShareGPTMessage   `json:"conversations"`
	Meta          map[string]any      `json:"meta,omitempty"`
}

// ShareGPTMessage is one conversation turn.
type ShareGPTMessage struct {
	From  string `json:"from"` // system | human | gpt
	Value string `json:"value"`
}

// Store is the storage surface required for export.
type Store interface {
	storage.FTSessionStore
	ListEntries(ctx context.Context, filter storage.ListFilter) ([]storage.KnowledgeEntry, error)
	GetEntry(ctx context.Context, id string) (*storage.KnowledgeEntry, error)
}

// Export writes train JSONL files into opts.OutDir and returns the manifest.
func Export(ctx context.Context, store Store, opts Options) (*Manifest, error) {
	if opts.OutDir == "" {
		return nil, fmt.Errorf("out dir required")
	}
	if err := os.MkdirAll(opts.OutDir, 0o755); err != nil {
		return nil, fmt.Errorf("mkdir: %w", err)
	}
	format := strings.ToLower(opts.Format)
	if format == "" {
		format = "all"
	}
	wantSFT := format == "all" || format == "sft"
	wantDPO := format == "all" || format == "dpo"
	wantSG := format == "all" || format == "sharegpt"

	filter := storage.FTSessionFilter{
		TeamID:            opts.TeamID,
		TrainEligibleOnly: opts.TrainEligibleOnly,
		MinOutcomeRating:  opts.MinRating,
		Since:             opts.Since,
		Until:             opts.Until,
		Limit:             100000,
	}

	sessions, err := store.ListFTSessions(ctx, filter)
	if err != nil {
		return nil, fmt.Errorf("list sessions: %w", err)
	}

	var sft []SFTRow
	var dpo []DPORow
	var sg []ShareGPTRow

	for _, sess := range sessions {
		turns, err := store.ListFTTurns(ctx, sess.ID)
		if err != nil {
			return nil, fmt.Errorf("list turns %s: %w", sess.ID, err)
		}
		prefs, err := store.ListFTPreferences(ctx, sess.ID)
		if err != nil {
			return nil, fmt.Errorf("list prefs %s: %w", sess.ID, err)
		}
		know, err := store.ListFTSessionKnowledge(ctx, sess.ID)
		if err != nil {
			return nil, fmt.Errorf("list knowledge %s: %w", sess.ID, err)
		}

		if wantSFT {
			sft = append(sft, buildSessionSFT(sess, turns, know, store, ctx)...)
		}
		if wantDPO {
			dpo = append(dpo, buildSessionDPO(sess, turns, prefs)...)
		}
		if wantSG {
			if row := buildShareGPT(sess, turns); row != nil {
				sg = append(sg, *row)
			}
		}
	}

	if wantSFT && opts.IncludeEntryBootstrap {
		boot, err := bootstrapFromEntries(ctx, store, opts)
		if err != nil {
			return nil, err
		}
		sft = append(sft, boot...)
	}

	counts := map[string]int{
		"sessions": len(sessions),
		"sft":      len(sft),
		"dpo":      len(dpo),
		"sharegpt": len(sg),
	}

	if wantSFT {
		if err := writeJSONL(filepath.Join(opts.OutDir, "sft.jsonl"), sft); err != nil {
			return nil, err
		}
	}
	if wantDPO {
		if err := writeJSONL(filepath.Join(opts.OutDir, "dpo.jsonl"), dpo); err != nil {
			return nil, err
		}
	}
	if wantSG {
		if err := writeJSONL(filepath.Join(opts.OutDir, "sharegpt.jsonl"), sg); err != nil {
			return nil, err
		}
	}

	man := &Manifest{
		FormatVersion: "1",
		Schema:        SchemaVersion,
		ExportedAt:    time.Now().UTC(),
		TeamID:        opts.TeamID,
		Filters: map[string]any{
			"min_rating":          opts.MinRating,
			"train_eligible_only": opts.TrainEligibleOnly,
			"format":              format,
			"entry_bootstrap":     opts.IncludeEntryBootstrap,
		},
		Counts: counts,
	}
	mb, err := json.MarshalIndent(man, "", "  ")
	if err != nil {
		return nil, err
	}
	if err := os.WriteFile(filepath.Join(opts.OutDir, "manifest.json"), mb, 0o644); err != nil {
		return nil, err
	}
	return man, nil
}

func buildSessionSFT(sess storage.FTSession, turns []storage.FTTurn, know []storage.FTSessionKnowledge, store Store, ctx context.Context) []SFTRow {
	var rows []SFTRow
	// Prefer last user message + last accepted assistant (or preference chosen).
	var lastUser, lastAsst string
	var entryIDs []string
	for _, t := range turns {
		switch {
		case t.Role == storage.FTRoleUser && (t.Kind == storage.FTKindMessage || t.Kind == storage.FTKindEnrich):
			lastUser = t.Content
		case t.Role == storage.FTRoleAssistant && t.Kind == storage.FTKindMessage:
			lastAsst = t.Content
		}
		entryIDs = append(entryIDs, t.EntryIDs...)
	}
	for _, k := range know {
		entryIDs = append(entryIDs, k.EntryID)
	}
	entryIDs = uniqueStrings(entryIDs)

	inputCtx := buildKnowledgeInput(ctx, store, entryIDs)
	rating := 0.0
	if sess.OutcomeRating != nil {
		rating = float64(*sess.OutcomeRating)
	}
	weight := 1.0
	if rating >= 4 {
		weight = 1.2
	}

	if lastUser != "" && lastAsst != "" {
		rows = append(rows, SFTRow{
			ID:          "sft_" + uuid.NewString(),
			SessionID:   sess.ID,
			Domain:      sess.Domain,
			Source:      "session_final",
			Instruction: lastUser,
			Input:       inputCtx,
			Output:      lastAsst,
			EntryIDs:    entryIDs,
			Rating:      rating,
			Weight:      weight,
		})
	}
	return rows
}

func buildSessionDPO(sess storage.FTSession, turns []storage.FTTurn, prefs []storage.FTPreference) []DPORow {
	var rows []DPORow
	// Map turn id -> content for prompt reconstruction.
	byID := map[string]string{}
	var lastUser string
	for _, t := range turns {
		byID[t.ID] = t.Content
		if t.Role == storage.FTRoleUser && (t.Kind == storage.FTKindMessage || t.Kind == storage.FTKindEnrich) {
			lastUser = t.Content
		}
	}
	for _, p := range prefs {
		if strings.TrimSpace(p.ChosenText) == "" || strings.TrimSpace(p.RejectedText) == "" {
			continue
		}
		if p.ChosenText == p.RejectedText {
			continue
		}
		prompt := lastUser
		if p.PromptTurnID != "" {
			if c, ok := byID[p.PromptTurnID]; ok && c != "" {
				prompt = c
			}
		}
		if prompt == "" {
			prompt = sess.TaskSummary
		}
		row := DPORow{
			ID:        "dpo_" + uuid.NewString(),
			SessionID: sess.ID,
			Prompt:    prompt,
			Chosen:    p.ChosenText,
			Rejected:  p.RejectedText,
			Source:    p.Source,
		}
		if p.Rating != nil {
			row.RatingChosen = *p.Rating
		}
		rows = append(rows, row)
	}
	return rows
}

func buildShareGPT(sess storage.FTSession, turns []storage.FTTurn) *ShareGPTRow {
	if len(turns) == 0 {
		return nil
	}
	var conv []ShareGPTMessage
	if sess.TaskSummary != "" {
		conv = append(conv, ShareGPTMessage{From: "system", Value: "Task: " + sess.TaskSummary})
	}
	for _, t := range turns {
		// Fold tool turns into a simple narrative for cleaner multi-turn SFT.
		switch t.Role {
		case storage.FTRoleUser, storage.FTRoleSystemInject:
			from := "human"
			if t.Role == storage.FTRoleSystemInject {
				from = "system"
			}
			conv = append(conv, ShareGPTMessage{From: from, Value: t.Content})
		case storage.FTRoleAssistant:
			conv = append(conv, ShareGPTMessage{From: "gpt", Value: t.Content})
		case storage.FTRoleTool:
			label := t.ToolName
			if label == "" {
				label = "tool"
			}
			conv = append(conv, ShareGPTMessage{From: "gpt", Value: fmt.Sprintf("[%s %s]\n%s", t.Kind, label, t.Content)})
		}
	}
	// Need at least one human and one gpt.
	hasH, hasG := false, false
	for _, m := range conv {
		if m.From == "human" {
			hasH = true
		}
		if m.From == "gpt" {
			hasG = true
		}
	}
	if !hasH || !hasG {
		return nil
	}
	meta := map[string]any{"domain": sess.Domain}
	if sess.OutcomeRating != nil {
		meta["rating"] = *sess.OutcomeRating
	}
	return &ShareGPTRow{
		ID:            "sg_" + uuid.NewString(),
		SessionID:     sess.ID,
		Conversations: conv,
		Meta:          meta,
	}
}

func bootstrapFromEntries(ctx context.Context, store Store, opts Options) ([]SFTRow, error) {
	entries, err := store.ListEntries(ctx, storage.ListFilter{
		TeamID: opts.TeamID,
		Status: "approved",
		Limit:  10000,
	})
	if err != nil {
		return nil, fmt.Errorf("list entries: %w", err)
	}
	// Also include entries with empty status filter miss — list pending if needed.
	// Some installs may have status approved only.

	var rows []SFTRow
	for _, e := range entries {
		if opts.MinRating > 0 && e.Rating > 0 && e.Rating < float64(opts.MinRating) {
			continue
		}
		row := entryToSFT(e)
		if row != nil {
			rows = append(rows, *row)
		}
	}

	// If team filter returned nothing and team is set, also try unfiltered approved
	// is not done — team isolation stays.
	if len(entries) == 0 && opts.TeamID == "" {
		// try without status filter for bootstrap of older data
		all, err := store.ListEntries(ctx, storage.ListFilter{Limit: 10000})
		if err != nil {
			return nil, err
		}
		for _, e := range all {
			if e.Status == "rejected" {
				continue
			}
			if opts.MinRating > 0 && e.Rating > 0 && e.Rating < float64(opts.MinRating) {
				continue
			}
			row := entryToSFT(e)
			if row != nil {
				rows = append(rows, *row)
			}
		}
	}
	return rows, nil
}

func entryToSFT(e storage.KnowledgeEntry) *SFTRow {
	if strings.TrimSpace(e.Content) == "" {
		return nil
	}
	weight := 0.8
	instruction := e.Description
	if instruction == "" {
		instruction = e.Title
	}
	source := "entry_" + string(e.Type)
	switch e.Type {
	case storage.KTPrompt:
		weight = 1.0
		if e.Description == "" {
			instruction = "Use this prompt template: " + e.Title
		}
	case storage.KTPattern, storage.KTWorkflow:
		instruction = fmt.Sprintf("Apply this %s: %s", e.Type, e.Title)
		if e.Description != "" {
			instruction += "\n" + e.Description
		}
		weight = 0.8
	case storage.KTAntiPattern:
		instruction = fmt.Sprintf("What to avoid (%s): %s", e.Title, e.Description)
		if e.Description == "" {
			instruction = "What to avoid: " + e.Title
		}
		weight = 0.7
	case storage.KTDomainFact:
		instruction = "Domain fact: " + e.Title
		weight = 0.6
	default:
		if instruction == "" {
			return nil
		}
	}
	if e.Rating >= 4 {
		weight += 0.2
	}
	return &SFTRow{
		ID:          "sft_entry_" + e.ID,
		Domain:      e.Domain,
		Source:      source,
		Instruction: instruction,
		Output:      e.Content,
		EntryIDs:    []string{e.ID},
		Rating:      e.Rating,
		Weight:      weight,
	}
}

func buildKnowledgeInput(ctx context.Context, store Store, entryIDs []string) string {
	if len(entryIDs) == 0 {
		return ""
	}
	var b strings.Builder
	for _, id := range entryIDs {
		e, err := store.GetEntry(ctx, id)
		if err != nil || e == nil {
			continue
		}
		b.WriteString("### ")
		b.WriteString(e.Title)
		b.WriteString("\n")
		b.WriteString(e.Content)
		b.WriteString("\n\n")
	}
	return strings.TrimSpace(b.String())
}

func uniqueStrings(in []string) []string {
	seen := map[string]struct{}{}
	var out []string
	for _, s := range in {
		if s == "" {
			continue
		}
		if _, ok := seen[s]; ok {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	return out
}

func writeJSONL[T any](path string, rows []T) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	enc := json.NewEncoder(f)
	for _, row := range rows {
		if err := enc.Encode(row); err != nil {
			return err
		}
	}
	return nil
}
