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
// ctx. It keys off a stable effective actor identity (user id, else API key id,
// else "local") so the per-user filter works for team tokens, stdio and
// dev/no-auth setups, not only user-scoped tokens. It returns a zero (no-op)
// RuleSet — which hides nothing — when the rules cannot be loaded. Owner
// identities (id/email/name) are included so a user's own entries are never
// hidden.
func callerVisibility(ctx context.Context, store storage.Store) visibility.RuleSet {
	actorID := auth.GetTeamContext(ctx).EffectiveActorID()
	rules, err := store.ListVisibilityRules(ctx, actorID)
	if err != nil {
		slog.Warn("visibility filter failing open: could not load rules",
			"actor_id", actorID, "error", err)
		return visibility.RuleSet{}
	}
	identities := []string{actorID}
	// When the actor is a real user, include their email/name so their own
	// entries are exempt. For key-id / "local" actors GetUserByID fails and we
	// fall back to the actor id alone.
	if us, ok := store.(userByIDStore); ok {
		if u, err := us.GetUserByID(ctx, actorID); err == nil && u != nil {
			identities = append(identities, u.Email, u.Name)
		}
	}
	return visibility.Compile(rules, identities...)
}
