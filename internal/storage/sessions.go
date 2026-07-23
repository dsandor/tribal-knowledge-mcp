package storage

import (
	"context"
	"time"
)

// FT session status values.
const (
	FTSessionOpen       = "open"
	FTSessionCompleted  = "completed"
	FTSessionAbandoned  = "abandoned"
)

// FT turn roles.
const (
	FTRoleUser         = "user"
	FTRoleAssistant    = "assistant"
	FTRoleTool         = "tool"
	FTRoleSystemInject = "system_inject"
)

// FT turn kinds.
const (
	FTKindMessage    = "message"
	FTKindToolCall   = "tool_call"
	FTKindToolResult = "tool_result"
	FTKindEnrich     = "enrich"
	FTKindRevision   = "revision"
)

// FT preference sources.
const (
	FTPrefUserEdit       = "user_edit"
	FTPrefRateUp         = "rate_up"
	FTPrefRateDown       = "rate_down"
	FTPrefABPick         = "a_b_pick"
	FTPrefKnowledgeStore = "knowledge_store"
)

// FT session-knowledge link roles.
const (
	FTKnowRetrieved    = "retrieved"
	FTKnowStored       = "stored"
	FTKnowRated        = "rated"
	FTKnowAgentSource  = "agent_source"
)

// FTSession is one LLM task / conversation thread used for fine-tune capture.
type FTSession struct {
	ID               string
	TeamID           string
	UserID           string
	Client           string // opencode | claude-code | cursor | api
	Project          string
	TaskSummary      string
	Domain           string
	Status           string // open | completed | abandoned
	OutcomeRating    *int   // 1-5 overall; nil if unset
	OutcomeNote      string
	TrainEligible    bool
	RedactionStatus  string // raw | scrubbed | blocked
	MetadataJSON     string
	StartedAt        time.Time
	CompletedAt      *time.Time
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

// FTTurn is one ordered step inside a session.
type FTTurn struct {
	ID            string
	SessionID     string
	Seq           int
	Role          string // user | assistant | tool | system_inject
	Kind          string // message | tool_call | tool_result | enrich | revision
	Content       string
	ContentHash   string
	Model         string
	TokenEstimate int
	EntryIDs      []string
	RuleIDs       []string
	AgentID       string
	ToolName      string
	ParentTurnID  string
	CreatedAt     time.Time
}

// FTPreference is a chosen/rejected pair for DPO-style training.
type FTPreference struct {
	ID           string
	SessionID    string
	TurnID       string // assistant turn judged (optional)
	PromptTurnID string // input turn
	ChosenText   string
	RejectedText string
	Source       string // user_edit | rate_up | rate_down | a_b_pick | knowledge_store
	Rating       *int
	EntryID      string
	UserID       string
	CreatedAt    time.Time
}

// FTSessionKnowledge links a session to a knowledge entry.
type FTSessionKnowledge struct {
	SessionID string
	EntryID   string
	Role      string // retrieved | stored | rated | agent_source
}

// FTSessionFilter selects sessions for listing/export. Zero values mean no filter.
type FTSessionFilter struct {
	TeamID            string
	UserID            string
	Status            string
	Domain            string
	TrainEligibleOnly bool
	MinOutcomeRating  int // 0 = no filter; otherwise outcome_rating >= N
	Since             *time.Time
	Until             *time.Time
	Limit             int
	Offset            int
}

// FTSessionStore captures fine-tune session traces and preference pairs.
type FTSessionStore interface {
	CreateFTSession(ctx context.Context, s FTSession) (string, error)
	GetFTSession(ctx context.Context, id string) (*FTSession, error)
	ListFTSessions(ctx context.Context, filter FTSessionFilter) ([]FTSession, error)
	UpdateFTSession(ctx context.Context, s FTSession) error
	CompleteFTSession(ctx context.Context, id string, outcomeRating *int, outcomeNote string, status string) error

	AddFTTurn(ctx context.Context, t FTTurn) (string, error)
	ListFTTurns(ctx context.Context, sessionID string) ([]FTTurn, error)
	NextFTTurnSeq(ctx context.Context, sessionID string) (int, error)

	AddFTPreference(ctx context.Context, p FTPreference) (string, error)
	ListFTPreferences(ctx context.Context, sessionID string) ([]FTPreference, error)
	// ListFTPreferencesByFilter returns preferences for sessions matching the filter
	// (used by train export). Empty SessionID on the filter means all matching sessions.
	ListFTPreferencesExport(ctx context.Context, filter FTSessionFilter) ([]FTPreference, error)

	LinkFTSessionKnowledge(ctx context.Context, sessionID, entryID, role string) error
	ListFTSessionKnowledge(ctx context.Context, sessionID string) ([]FTSessionKnowledge, error)
}

// ValidFTSessionStatus reports whether s is an allowed session status.
func ValidFTSessionStatus(s string) bool {
	switch s {
	case FTSessionOpen, FTSessionCompleted, FTSessionAbandoned:
		return true
	}
	return false
}

// ValidFTRole reports whether r is an allowed turn role.
func ValidFTRole(r string) bool {
	switch r {
	case FTRoleUser, FTRoleAssistant, FTRoleTool, FTRoleSystemInject:
		return true
	}
	return false
}

// ValidFTKind reports whether k is an allowed turn kind.
func ValidFTKind(k string) bool {
	switch k {
	case FTKindMessage, FTKindToolCall, FTKindToolResult, FTKindEnrich, FTKindRevision:
		return true
	}
	return false
}

// ValidFTPrefSource reports whether s is an allowed preference source.
func ValidFTPrefSource(s string) bool {
	switch s {
	case FTPrefUserEdit, FTPrefRateUp, FTPrefRateDown, FTPrefABPick, FTPrefKnowledgeStore:
		return true
	}
	return false
}

// ValidFTKnowRole reports whether r is an allowed session-knowledge role.
func ValidFTKnowRole(r string) bool {
	switch r {
	case FTKnowRetrieved, FTKnowStored, FTKnowRated, FTKnowAgentSource:
		return true
	}
	return false
}
