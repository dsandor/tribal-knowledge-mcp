package live

import "time"

// ActorRef identifies who performed an action, with a resolved display name.
type ActorRef struct {
	ID      string `json:"id"`
	Display string `json:"display"` // resolved email/name; callers fall back to id
}

// Event type constants — one string per activity kind.
const (
	TypeEnrichContext       = "enrich_context"
	TypeKnowledgeStored     = "knowledge_stored"
	TypeKnowledgeUsed       = "knowledge_used"
	TypeKnowledgeRated      = "knowledge_rated"
	TypeApproved            = "approved"
	TypeRejected            = "rejected"
	TypePipelineComplete    = "pipeline_complete"
	TypeAgentGenerated      = "agent_generated"
	TypeImprovementDrafted  = "improvement_drafted"
	TypeSignin              = "signin"
	TypePresence            = "presence"
)

// LiveEvent is one item in the activity stream.
//
// TeamID is used only for server-side fan-out filtering and MUST NOT be
// serialised to clients (json:"-").
type LiveEvent struct {
	ID        string            `json:"id"`
	Type      string            `json:"type"`
	TeamID    string            `json:"-"`
	Actor     ActorRef          `json:"actor"`
	Fragment  string            `json:"fragment,omitempty"`
	EntryID   string            `json:"entry_id,omitempty"`
	Title     string            `json:"title,omitempty"`
	Meta      map[string]string `json:"meta,omitempty"`
	CreatedAt time.Time         `json:"created_at"`
}

// MaxFragmentLen is the hard cap (in runes) on stored/served fragment text.
const MaxFragmentLen = 280

// CapFragment truncates s to MaxFragmentLen runes, appending a Unicode
// ellipsis character "…" when truncation is necessary so the result
// (including the ellipsis) is always ≤ MaxFragmentLen runes.
// The function is rune-safe: multibyte characters are never split.
// Returns s unchanged when it is already within the cap.
func CapFragment(s string) string {
	runes := []rune(s)
	if len(runes) <= MaxFragmentLen {
		return s
	}
	// Truncate to MaxFragmentLen-1 runes, then append the ellipsis so the
	// total is exactly MaxFragmentLen runes.
	truncated := runes[:MaxFragmentLen-1]
	return string(truncated) + "…"
}
