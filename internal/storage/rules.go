package storage

import (
	"context"
	"time"
)

type RuleScope string

const (
	RuleScopeTeam     RuleScope = "team"
	RuleScopeCategory RuleScope = "category"
	RuleScopeUser     RuleScope = "user"
)

type Rule struct {
	ID         string
	Title      string
	Content    string
	Scope      RuleScope
	ScopeValue string    // team ID, domain/category name, or user/author identifier
	Priority   int       // higher = applied first within the same scope
	Enabled    bool
	Author     string
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

type RuleFilter struct {
	Scope      RuleScope
	ScopeValue string
	// Limit: 0 = default (50), negative = unlimited
	Limit int
}

type RuleStore interface {
	StoreRule(ctx context.Context, rule Rule) (string, error)
	GetRule(ctx context.Context, id string) (*Rule, error)
	ListRules(ctx context.Context, filter RuleFilter) ([]Rule, error)
	UpdateRule(ctx context.Context, rule Rule) error
	DeleteRule(ctx context.Context, id string) error
	GetApplicableRules(ctx context.Context, team, category, user string) ([]Rule, error)
}
