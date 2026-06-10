package live

import (
	"fmt"
	"sync"
	"testing"
	"time"
)

// fixedClock returns a func() time.Time that always returns the pointed-to value.
// Tests advance *t to simulate time passing.
func fixedClock(t *time.Time) func() time.Time {
	return func() time.Time { return *t }
}

func TestPresence_TouchAndSnapshot(t *testing.T) {
	now := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)
	p := NewPresence(60 * time.Second)
	p.WithClock(fixedClock(&now))

	alice := ActorRef{ID: "alice", Display: "Alice"}
	p.Touch("teamA", alice)

	actors := p.Snapshot("teamA", false)
	if len(actors) != 1 {
		t.Fatalf("expected 1 actor, got %d", len(actors))
	}
	if actors[0].ID != "alice" {
		t.Fatalf("expected alice, got %q", actors[0].ID)
	}
}

func TestPresence_ExpiredActorNotInSnapshot(t *testing.T) {
	now := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)
	p := NewPresence(60 * time.Second)
	p.WithClock(fixedClock(&now))

	p.Touch("teamA", ActorRef{ID: "alice", Display: "Alice"})

	// Advance clock beyond the window.
	now = now.Add(61 * time.Second)

	actors := p.Snapshot("teamA", false)
	if len(actors) != 0 {
		t.Fatalf("expected no actors after expiry, got %d", len(actors))
	}
}

func TestPresence_SweepJoinedAndLeft(t *testing.T) {
	now := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)
	p := NewPresence(60 * time.Second)
	p.WithClock(fixedClock(&now))

	alice := ActorRef{ID: "alice", Display: "Alice"}
	p.Touch("teamA", alice)

	// First Sweep: alice should appear as Joined.
	deltas := p.Sweep()
	d, ok := deltas["teamA"]
	if !ok {
		t.Fatal("expected delta for teamA")
	}
	if len(d.Joined) != 1 || d.Joined[0].ID != "alice" {
		t.Fatalf("expected alice in Joined, got %v", d.Joined)
	}
	if len(d.Left) != 0 {
		t.Fatalf("expected no Left, got %v", d.Left)
	}

	// Second Sweep without time change: no change.
	deltas2 := p.Sweep()
	if d2, ok2 := deltas2["teamA"]; ok2 {
		t.Fatalf("expected no delta on second sweep, got %+v", d2)
	}

	// Advance beyond window so alice expires, then Sweep.
	now = now.Add(61 * time.Second)
	deltas3 := p.Sweep()
	d3, ok3 := deltas3["teamA"]
	if !ok3 {
		t.Fatal("expected delta for teamA after expiry")
	}
	if len(d3.Left) != 1 || d3.Left[0].ID != "alice" {
		t.Fatalf("expected alice in Left, got %v", d3.Left)
	}
	if len(d3.Joined) != 0 {
		t.Fatalf("expected no Joined after expiry, got %v", d3.Joined)
	}
}

func TestPresence_SuperadminSnapshotUnionsTeams(t *testing.T) {
	now := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)
	p := NewPresence(60 * time.Second)
	p.WithClock(fixedClock(&now))

	p.Touch("teamA", ActorRef{ID: "alice", Display: "Alice"})
	p.Touch("teamB", ActorRef{ID: "bob", Display: "Bob"})

	actors := p.Snapshot("teamA", true) // superadmin
	if len(actors) != 2 {
		t.Fatalf("superadmin expected 2 actors, got %d: %v", len(actors), actors)
	}
}

func TestPresence_SuperadminSnapshotDeduplicates(t *testing.T) {
	now := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)
	p := NewPresence(60 * time.Second)
	p.WithClock(fixedClock(&now))

	// Same actor in two teams.
	p.Touch("teamA", ActorRef{ID: "alice", Display: "Alice"})

	now = now.Add(1 * time.Second)
	p.Touch("teamB", ActorRef{ID: "alice", Display: "Alice TeamB"})

	actors := p.Snapshot("", true) // superadmin
	if len(actors) != 1 {
		t.Fatalf("expected dedup to 1 actor, got %d: %v", len(actors), actors)
	}
	// The more recent entry (teamB) should win.
	if actors[0].Display != "Alice TeamB" {
		t.Fatalf("expected most-recent display name, got %q", actors[0].Display)
	}
}

func TestPresence_DeterministicSort(t *testing.T) {
	now := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)
	p := NewPresence(60 * time.Second)
	p.WithClock(fixedClock(&now))

	// Insert in reverse alphabetical order.
	p.Touch("teamA", ActorRef{ID: "charlie", Display: "Charlie"})
	p.Touch("teamA", ActorRef{ID: "bob", Display: "Bob"})
	p.Touch("teamA", ActorRef{ID: "alice", Display: "Alice"})

	actors := p.Snapshot("teamA", false)
	if len(actors) != 3 {
		t.Fatalf("expected 3, got %d", len(actors))
	}
	order := []string{actors[0].Display, actors[1].Display, actors[2].Display}
	expected := []string{"Alice", "Bob", "Charlie"}
	for i := range expected {
		if order[i] != expected[i] {
			t.Fatalf("sort order wrong at %d: got %q want %q", i, order[i], expected[i])
		}
	}
}

func TestPresence_TouchUpdatesDisplayName(t *testing.T) {
	now := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)
	p := NewPresence(60 * time.Second)
	p.WithClock(fixedClock(&now))

	p.Touch("teamA", ActorRef{ID: "alice", Display: "Alice"})
	p.Touch("teamA", ActorRef{ID: "alice", Display: "Alice Updated"})

	actors := p.Snapshot("teamA", false)
	if len(actors) != 1 {
		t.Fatalf("expected 1, got %d", len(actors))
	}
	if actors[0].Display != "Alice Updated" {
		t.Fatalf("expected updated display name, got %q", actors[0].Display)
	}
}

// TestPresence_ConcurrentTouchSweepSnapshot is a goroutine-storm race test.
// N goroutines call Touch on several teams while a sweeper goroutine
// repeatedly calls Sweep and Snapshot.  No time.Sleep is used; all loops are
// count-bounded.  The test asserts no panic/race and that the final Snapshot
// returns only valid actor IDs.
func TestPresence_ConcurrentTouchSweepSnapshot(t *testing.T) {
	const (
		touchGoroutines  = 8
		touchIterations  = 200
		sweepIterations  = 100
		teams            = 3
	)

	p := NewPresence(60 * time.Second)

	var wg sync.WaitGroup

	// Touchers: each goroutine repeatedly touches actors across multiple teams.
	for g := 0; g < touchGoroutines; g++ {
		wg.Add(1)
		go func(gid int) {
			defer wg.Done()
			for i := 0; i < touchIterations; i++ {
				teamID := fmt.Sprintf("team-%d", i%teams)
				actor := ActorRef{
					ID:      fmt.Sprintf("actor-%d", gid),
					Display: fmt.Sprintf("Actor %d", gid),
				}
				p.Touch(teamID, actor)
			}
		}(g)
	}

	// Sweeper: repeatedly sweeps and snapshots while touchers are running.
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < sweepIterations; i++ {
			p.Sweep()
			for ti := 0; ti < teams; ti++ {
				actors := p.Snapshot(fmt.Sprintf("team-%d", ti), false)
				for _, a := range actors {
					if a.ID == "" {
						t.Errorf("Snapshot returned actor with empty ID")
					}
				}
			}
			// Also exercise superadmin path.
			p.Snapshot("", true)
		}
	}()

	wg.Wait()

	// Final state: OnlineCount must be non-negative and not panic.
	total := p.OnlineCount("", true)
	if total < 0 {
		t.Fatalf("OnlineCount returned negative value: %d", total)
	}
}

func TestPresence_OnlineCount(t *testing.T) {
	now := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)
	p := NewPresence(60 * time.Second)
	p.WithClock(fixedClock(&now))

	p.Touch("teamA", ActorRef{ID: "alice", Display: "Alice"})
	p.Touch("teamA", ActorRef{ID: "bob", Display: "Bob"})

	if n := p.OnlineCount("teamA", false); n != 2 {
		t.Fatalf("expected 2, got %d", n)
	}

	now = now.Add(61 * time.Second)
	if n := p.OnlineCount("teamA", false); n != 0 {
		t.Fatalf("expected 0 after expiry, got %d", n)
	}
}
