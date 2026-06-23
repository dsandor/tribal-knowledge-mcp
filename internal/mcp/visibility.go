package mcp

import (
	"context"
	"log/slog"

	"github.com/dsandor/memory/internal/auth"
	"github.com/dsandor/memory/internal/storage"
	"github.com/dsandor/memory/internal/visibility"
)

// userByIDStore is the optional capability a Store may provide to resolve a
// user's email/name for the own-entry exemption. The base storage.Store does
// not require it, so the helper type-asserts and degrades gracefully (the user
// id alone still serves as an owner identity).
type userByIDStore interface {
	GetUserByID(ctx context.Context, id string) (*storage.User, error)
}

// callerVisibility builds the calling user's compiled suppression RuleSet from
// ctx. It returns a zero (no-op) RuleSet — which hides nothing — when the caller
// is not user-scoped (tc.UserID == "", i.e. team tokens / stdio) or when the
// rules cannot be loaded. Owner identities (id/email/name) are included so a
// user's own entries are never hidden.
func callerVisibility(ctx context.Context, store storage.Store) visibility.RuleSet {
	tc := auth.GetTeamContext(ctx)
	if tc.UserID == "" {
		return visibility.RuleSet{}
	}
	rules, err := store.ListVisibilityRules(ctx, tc.UserID)
	if err != nil {
		slog.Warn("visibility filter failing open: could not load rules",
			"user_id", tc.UserID, "error", err)
		return visibility.RuleSet{}
	}
	identities := []string{tc.UserID}
	if us, ok := store.(userByIDStore); ok {
		if u, err := us.GetUserByID(ctx, tc.UserID); err == nil && u != nil {
			identities = append(identities, u.Email, u.Name)
		}
	}
	return visibility.Compile(rules, identities...)
}
