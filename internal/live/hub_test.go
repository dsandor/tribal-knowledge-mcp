package live

import (
	"fmt"
	"sync"
	"testing"
	"time"
)

// makeEvent is a small helper to build a LiveEvent for a given team.
func makeEvent(teamID, typ, fragment string) LiveEvent {
	return LiveEvent{
		ID:        fmt.Sprintf("ev-%s-%s", teamID, typ),
		Type:      typ,
		TeamID:    teamID,
		Actor:     ActorRef{ID: "u1", Display: "User One"},
		Fragment:  fragment,
		CreatedAt: time.Now(),
	}
}

// drain reads up to n events from ch with a short timeout and returns them.
func drain(ch <-chan LiveEvent, n int, timeout time.Duration) []LiveEvent {
	var out []LiveEvent
	deadline := time.After(timeout)
	for i := 0; i < n; i++ {
		select {
		case ev, ok := <-ch:
			if !ok {
				return out
			}
			out = append(out, ev)
		case <-deadline:
			return out
		}
	}
	return out
}

// TestHub_ReceivesOwnTeam verifies a subscriber gets events for its team.
func TestHub_ReceivesOwnTeam(t *testing.T) {
	h := NewHub()
	ch, unsub := h.Subscribe("teamA", false)
	defer unsub()

	ev := makeEvent("teamA", TypeKnowledgeStored, "note")
	h.Publish(ev)

	got := drain(ch, 1, time.Second)
	if len(got) != 1 {
		t.Fatalf("expected 1 event, got %d", len(got))
	}
	if got[0].ID != ev.ID {
		t.Fatalf("wrong event ID: got %q want %q", got[0].ID, ev.ID)
	}
}

// TestHub_DoesNotReceiveOtherTeam verifies cross-team isolation.
func TestHub_DoesNotReceiveOtherTeam(t *testing.T) {
	h := NewHub()
	ch, unsub := h.Subscribe("teamA", false)
	defer unsub()

	h.Publish(makeEvent("teamB", TypeKnowledgeStored, ""))

	got := drain(ch, 1, 100*time.Millisecond)
	if len(got) != 0 {
		t.Fatalf("expected no events, got %d", len(got))
	}
}

// TestHub_SuperadminReceivesAll verifies superadmin fans out across all teams.
func TestHub_SuperadminReceivesAll(t *testing.T) {
	h := NewHub()
	ch, unsub := h.Subscribe("teamA", true) // superadmin
	defer unsub()

	h.Publish(makeEvent("teamA", TypeSignin, ""))
	h.Publish(makeEvent("teamB", TypeSignin, ""))
	h.Publish(makeEvent("teamC", TypeSignin, ""))

	got := drain(ch, 3, time.Second)
	if len(got) != 3 {
		t.Fatalf("superadmin expected 3 events, got %d", len(got))
	}
}

// TestHub_DropOldestKeepsNewest validates the drop-oldest policy.
// We fill the channel beyond capacity and verify the most recent events
// are retained.
func TestHub_DropOldestKeepsNewest(t *testing.T) {
	h := NewHub()
	ch, unsub := h.Subscribe("teamA", false)
	defer unsub()

	total := subscriberChanCap + 50 // 50 more than capacity

	for i := 0; i < total; i++ {
		ev := LiveEvent{
			ID:     fmt.Sprintf("ev-%04d", i),
			Type:   TypeKnowledgeStored,
			TeamID: "teamA",
		}
		h.Publish(ev)
	}

	// Drain all available events.
	got := drain(ch, total, 500*time.Millisecond)

	if len(got) > subscriberChanCap {
		t.Fatalf("channel held more events than its capacity: %d", len(got))
	}

	// The newest event should be the last one published (id "ev-NNNN").
	last := fmt.Sprintf("ev-%04d", total-1)
	found := false
	for _, ev := range got {
		if ev.ID == last {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("most-recently published event %q not found in drained events", last)
	}

	// The very first event should have been dropped.
	first := "ev-0000"
	for _, ev := range got {
		if ev.ID == first {
			t.Fatalf("oldest event %q should have been dropped but was received", first)
		}
	}
}

// TestHub_UnsubscribeClosesChannel verifies the channel is closed and no
// further events are delivered after Unsubscribe is called.
func TestHub_UnsubscribeClosesChannel(t *testing.T) {
	h := NewHub()
	ch, unsub := h.Subscribe("teamA", false)

	unsub()

	// Channel should be closed.
	select {
	case _, ok := <-ch:
		if ok {
			t.Fatal("expected channel to be closed")
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatal("channel not closed after unsubscribe")
	}

	// Publishing after unsubscribe must not panic or deadlock.
	h.Publish(makeEvent("teamA", TypeSignin, ""))
}

// TestHub_UnsubscribeIdempotent verifies calling unsub multiple times is safe.
func TestHub_UnsubscribeIdempotent(t *testing.T) {
	h := NewHub()
	_, unsub := h.Subscribe("teamA", false)
	unsub()
	unsub() // second call must not panic
}

// TestHub_SubscriberCount is accurate before and after subscribe/unsubscribe.
func TestHub_SubscriberCount(t *testing.T) {
	h := NewHub()
	if n := h.SubscriberCount(); n != 0 {
		t.Fatalf("expected 0 subscribers at start, got %d", n)
	}

	_, u1 := h.Subscribe("teamA", false)
	_, u2 := h.Subscribe("teamB", false)

	if n := h.SubscriberCount(); n != 2 {
		t.Fatalf("expected 2 subscribers, got %d", n)
	}

	u1()
	if n := h.SubscriberCount(); n != 1 {
		t.Fatalf("expected 1 subscriber after first unsub, got %d", n)
	}

	u2()
	if n := h.SubscriberCount(); n != 0 {
		t.Fatalf("expected 0 subscribers after second unsub, got %d", n)
	}
}

// TestHub_ConcurrentPublishSubscribe exercises the hub under concurrent load
// to surface data races (use -race flag).
func TestHub_ConcurrentPublishSubscribe(t *testing.T) {
	h := NewHub()

	const goroutines = 20
	const eventsPerGoroutine = 50

	var wg sync.WaitGroup

	// Subscribers.
	channels := make([]<-chan LiveEvent, goroutines)
	unsubs := make([]func(), goroutines)
	for i := 0; i < goroutines; i++ {
		ch, unsub := h.Subscribe("teamA", false)
		channels[i] = ch
		unsubs[i] = unsub
	}

	// Publishers.
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < eventsPerGoroutine; j++ {
				h.Publish(makeEvent("teamA", TypeKnowledgeStored, fmt.Sprintf("g%d-e%d", id, j)))
			}
		}(i)
	}

	// Unsub goroutines that unsub while publishing is in flight.
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			// Small sleep to let some publishes happen first.
			time.Sleep(time.Millisecond)
			unsubs[idx]()
			// Drain any pending events to avoid blocking.
			for range channels[idx] {
			}
		}(i)
	}

	wg.Wait()
}
