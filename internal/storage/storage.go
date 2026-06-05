package storage

import (
	"context"
	"errors"
	"time"
)

type KnowledgeType string

const (
	KTPrompt      KnowledgeType = "prompt"
	KTPattern     KnowledgeType = "pattern"
	KTWorkflow    KnowledgeType = "workflow"
	KTDomainFact  KnowledgeType = "domain_fact"
	KTAntiPattern KnowledgeType = "anti_pattern"
)

// ErrNotFound is returned when a requested entry does not exist.
var ErrNotFound = errors.New("entry not found")

type KnowledgeEntry struct {
	ID          string
	Type        KnowledgeType
	Title       string
	Content     string
	Description string
	Domain      string
	Tags        []string
	Author      string
	Team        string
	CreatedAt   time.Time
	UpdatedAt   time.Time
	Version     int
}

type SearchResult struct {
	Entry    KnowledgeEntry
	Score    float64
}

type ListFilter struct {
	Domain string
	Type   KnowledgeType
	// Limit is the maximum number of entries to return. Zero means use implementation default (typically 50).
	Limit  int
}

type Store interface {
	// StoreEntry always creates a new entry, assigning a fresh UUID as ID.
	// The ID field on the passed entry is ignored. Returns the assigned ID.
	StoreEntry(ctx context.Context, entry KnowledgeEntry, embedding []float32) (string, error)
	GetEntry(ctx context.Context, id string) (*KnowledgeEntry, error)
	ListEntries(ctx context.Context, filter ListFilter) ([]KnowledgeEntry, error)
	DeleteEntry(ctx context.Context, id string) error
	SearchSimilar(ctx context.Context, embedding []float32, topK int) ([]SearchResult, error)
	Close() error
}
