package live

import (
	"sort"
	"sync"
	"time"
)

// Delta reports actors who joined (appeared) or left (expired) during a Sweep.
type Delta struct {
	Joined []ActorRef
	Left   []ActorRef
}

// presenceEntry holds the last-seen timestamp and display name for one actor
// within one team.
type presenceEntry struct {
	actor    ActorRef
	lastSeen time.Time
}

// Presence tracks which actors are currently "online" (seen within a
// configurable time window). It is goroutine-safe.
//
// No background goroutine is started here; the caller is responsible for
// periodically invoking Sweep() to evict stale entries.
type Presence struct {
	mu sync.RWMutex

	window time.Duration

	// entries is keyed by teamID -> actorID -> presenceEntry.
	entries map[string]map[string]presenceEntry

	// known tracks which actor IDs were present at the last Sweep, used to
	// compute Joined/Left deltas.  Keyed by teamID -> set of actorIDs.
	known map[string]map[string]struct{}

	// now is injectable so tests can advance time without sleeping.
	now func() time.Time
}

// NewPresence creates a Presence tracker with the given online window.
// window is typically 60 seconds in production.
func NewPresence(window time.Duration) *Presence {
	return &Presence{
		window:  window,
		entries: make(map[string]map[string]presenceEntry),
		known:   make(map[string]map[string]struct{}),
		now:     time.Now,
	}
}

// WithClock replaces the clock used for "now". Intended for testing only.
func (p *Presence) WithClock(fn func() time.Time) {
	p.mu.Lock()
	p.now = fn
	p.mu.Unlock()
}

// Touch records or updates the actor's presence in teamID.
// If the actor already exists, lastSeen is advanced and the display name is
// updated if it has changed.
func (p *Presence) Touch(teamID string, actor ActorRef) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.entries[teamID] == nil {
		p.entries[teamID] = make(map[string]presenceEntry)
	}
	p.entries[teamID][actor.ID] = presenceEntry{
		actor:    actor,
		lastSeen: p.now(),
	}
}

// Snapshot returns the list of actors currently online (lastSeen within the
// window) for teamID.  When superadmin is true the union across all teams is
// returned (deduped by actor ID, preferring the most recent entry).
// The result is sorted by Display then ID for determinism.
func (p *Presence) Snapshot(teamID string, superadmin bool) []ActorRef {
	p.mu.RLock()
	defer p.mu.RUnlock()

	now := p.now()
	// Collect by actor ID to deduplicate when superadmin spans multiple teams.
	seen := make(map[string]presenceEntry)

	if superadmin {
		for _, teamEntries := range p.entries {
			for id, e := range teamEntries {
				if now.Sub(e.lastSeen) <= p.window {
					if existing, ok := seen[id]; !ok || e.lastSeen.After(existing.lastSeen) {
						seen[id] = e
					}
				}
			}
		}
	} else {
		for id, e := range p.entries[teamID] {
			if now.Sub(e.lastSeen) <= p.window {
				seen[id] = e
			}
		}
	}

	result := make([]ActorRef, 0, len(seen))
	for _, e := range seen {
		result = append(result, e.actor)
	}
	sortActors(result)
	return result
}

// OnlineCount returns the number of currently-online actors for teamID
// (or all teams when superadmin is true).
func (p *Presence) OnlineCount(teamID string, superadmin bool) int {
	return len(p.Snapshot(teamID, superadmin))
}

// Sweep evicts entries older than the window and returns per-team deltas
// describing which actors joined (appeared for the first time since the last
// Sweep) and which left (expired since the last Sweep).
//
// Callers should invoke Sweep periodically (e.g. every 10 s).
func (p *Presence) Sweep() map[string]Delta {
	p.mu.Lock()
	defer p.mu.Unlock()

	now := p.now()
	deltas := make(map[string]Delta)

	// Build the current "alive" set, evicting expired entries.
	// We hold the full lock throughout, so there is no concurrent modification.
	// Collect keys to delete in a separate slice to make map mutation explicit
	// and unambiguous (ranging over a map while deleting from it is legal Go
	// but harder to reason about).
	currentByTeam := make(map[string]map[string]struct{})

	// leftActors stores the display name of evicted actors keyed by
	// teamID -> actorID, captured before deletion so the Left delta carries
	// the correct Display field.
	leftActors := make(map[string]map[string]ActorRef)

	for teamID, teamEntries := range p.entries {
		// First pass: collect expired IDs.
		var toDelete []string
		for id, e := range teamEntries {
			if now.Sub(e.lastSeen) > p.window {
				toDelete = append(toDelete, id)
			}
		}
		// Second pass: capture display names, then delete.
		if len(toDelete) > 0 {
			leftActors[teamID] = make(map[string]ActorRef, len(toDelete))
			for _, id := range toDelete {
				leftActors[teamID][id] = teamEntries[id].actor
				delete(teamEntries, id)
			}
		}

		if len(teamEntries) == 0 {
			delete(p.entries, teamID)
			continue
		}

		alive := make(map[string]struct{}, len(teamEntries))
		for id := range teamEntries {
			alive[id] = struct{}{}
		}
		currentByTeam[teamID] = alive
	}

	// Compute deltas by diffing known (previous Sweep) against current.
	// Collect all teams that appear in either set.
	allTeams := make(map[string]struct{})
	for t := range currentByTeam {
		allTeams[t] = struct{}{}
	}
	for t := range p.known {
		allTeams[t] = struct{}{}
	}

	for teamID := range allTeams {
		prev := p.known[teamID]
		curr := currentByTeam[teamID]

		var joined, left []ActorRef

		// Joined: in curr but not in prev.
		for id := range curr {
			if _, wasThere := prev[id]; !wasThere {
				e := p.entries[teamID][id]
				joined = append(joined, e.actor)
			}
		}

		// Left: in prev but not in curr.
		// The entry was already evicted above; use the display name captured
		// in leftActors before deletion.
		for id := range prev {
			if _, stillThere := curr[id]; !stillThere {
				actor := leftActors[teamID][id]
				left = append(left, actor)
			}
		}

		if len(joined) > 0 || len(left) > 0 {
			sortActors(joined)
			sortActors(left)
			deltas[teamID] = Delta{Joined: joined, Left: left}
		}
	}

	// Update known to the current alive set.
	p.known = currentByTeam

	return deltas
}

// sortActors sorts a slice of ActorRef by Display then ID for determinism.
func sortActors(actors []ActorRef) {
	sort.Slice(actors, func(i, j int) bool {
		if actors[i].Display != actors[j].Display {
			return actors[i].Display < actors[j].Display
		}
		return actors[i].ID < actors[j].ID
	})
}
