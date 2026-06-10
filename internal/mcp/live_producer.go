package mcp

import (
	"context"
	"time"

	"github.com/dsandor/memory/internal/auth"
	"github.com/dsandor/memory/internal/live"
	"github.com/google/uuid"
)

// resolveActorTeam extracts a (teamID, ActorRef) pair from the context.
// When an auth.TeamContext is present (HTTP/bearer MCP) the context values are used.
// When no auth context is present (stdio MCP) teamID is empty and the actor is "stdio".
func resolveActorTeam(ctx context.Context) (teamID string, actor live.ActorRef) {
	tc := auth.GetTeamContext(ctx)

	actorID := tc.UserID
	if actorID == "" {
		actorID = tc.KeyID
	}
	display := tc.Display
	if display == "" {
		display = actorID
	}

	if actorID != "" {
		// Authenticated request.
		return tc.TeamID, live.ActorRef{ID: actorID, Display: display}
	}

	// stdio / no-auth fallback.
	return "", live.ActorRef{ID: "stdio", Display: "stdio"}
}

// publishEvent publishes ev to bus, filling ID and CreatedAt if zero.
// No-ops when bus is nil, so callers need no nil-guard.
func publishEvent(bus live.EventBus, ev live.LiveEvent) {
	if bus == nil {
		return
	}
	if ev.ID == "" {
		ev.ID = uuid.New().String()
	}
	if ev.CreatedAt.IsZero() {
		ev.CreatedAt = time.Now().UTC()
	}
	bus.Publish(ev)
}
