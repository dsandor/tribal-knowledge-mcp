package live

import (
	"sync"
)

// MaxSubscribers is the advisory global subscriber cap.
//
// The hub does NOT enforce this limit itself. The HTTP layer is expected to
// check SubscriberCount() >= MaxSubscribers and return HTTP 503 before
// calling Subscribe.
const MaxSubscribers = 256

// subscriberChanCap is the per-subscriber channel buffer depth.
const subscriberChanCap = 256

// EventBus is the interface producers and the SSE handler depend on so the
// in-memory Hub can later be swapped for a Redis-backed implementation.
type EventBus interface {
	// Publish fans the event out to all matching subscribers.
	Publish(ev LiveEvent)

	// Subscribe returns a receive-only channel of events visible to the given
	// team (or all teams when superadmin is true) plus an idempotent
	// unsubscribe func that is safe to call exactly once (or more — it's
	// idempotent).
	Subscribe(teamID string, superadmin bool) (<-chan LiveEvent, func())
}

// subscriber holds the per-connection state.
//
// mu protects the channel from concurrent sends and closes.  Publish takes
// RLock before every channel operation; unsub takes Lock before closing.
// This ensures close(ch) is never concurrent with ch <- ev.
type subscriber struct {
	teamID     string
	superadmin bool
	ch         chan LiveEvent

	mu     sync.RWMutex // guards ch from concurrent send/close
	closed bool         // true once unsub has run (protected by mu)
	once   sync.Once    // ensures unsub body runs exactly once
}

// Hub is a goroutine-safe, in-memory pub/sub event bus with team-scoped
// fan-out and a drop-oldest policy for slow consumers.
type Hub struct {
	mu   sync.RWMutex
	subs map[uint64]*subscriber
	next uint64 // monotonically increasing subscriber ID
}

// NewHub constructs a ready-to-use Hub.
func NewHub() *Hub {
	return &Hub{
		subs: make(map[uint64]*subscriber),
	}
}

// Subscribe registers a new subscriber and returns its event channel plus an
// idempotent unsubscribe function.  The hub does not enforce MaxSubscribers;
// callers should check SubscriberCount() before calling Subscribe and reject
// the connection at the HTTP layer when the limit is reached.
func (h *Hub) Subscribe(teamID string, superadmin bool) (<-chan LiveEvent, func()) {
	sub := &subscriber{
		teamID:     teamID,
		superadmin: superadmin,
		ch:         make(chan LiveEvent, subscriberChanCap),
	}

	h.mu.Lock()
	id := h.next
	h.next++
	h.subs[id] = sub
	h.mu.Unlock()

	unsub := func() {
		sub.once.Do(func() {
			// Remove from the hub map first so no new Publish call can
			// snapshot this subscriber.
			h.mu.Lock()
			delete(h.subs, id)
			h.mu.Unlock()

			// Acquire exclusive lock on the subscriber's own mutex.
			// Any in-flight deliverToSubscriber that already holds RLock
			// will finish before we proceed.  After we hold the Lock no
			// further sends can start.
			sub.mu.Lock()
			sub.closed = true
			close(sub.ch)
			sub.mu.Unlock()
		})
	}

	return sub.ch, unsub
}

// Publish delivers ev to every matching subscriber using a drop-oldest policy:
// if a subscriber's channel is full, the oldest queued event is discarded to
// make room for the incoming one. This ensures Publish never blocks and that
// slow consumers always receive the most recent events.
func (h *Hub) Publish(ev LiveEvent) {
	h.mu.RLock()
	// Snapshot matching subscribers under the hub read lock.
	targets := make([]*subscriber, 0, len(h.subs))
	for _, sub := range h.subs {
		if sub.superadmin || sub.teamID == ev.TeamID {
			targets = append(targets, sub)
		}
	}
	h.mu.RUnlock()

	for _, sub := range targets {
		deliverToSubscriber(sub, ev)
	}
}

// deliverToSubscriber sends ev to sub's channel with drop-oldest semantics.
//
// sub.mu (RLock) is held continuously across BOTH the closed check AND every
// channel operation within a single loop iteration.  unsub acquires the full
// write lock before close(sub.ch), so close cannot interleave with a send or
// drain while we hold the read lock.  If unsub has already run, sub.closed is
// true and we return without touching ch.
func deliverToSubscriber(sub *subscriber, ev LiveEvent) {
	for {
		sub.mu.RLock()
		if sub.closed {
			sub.mu.RUnlock()
			return
		}
		select {
		case sub.ch <- ev:
			sub.mu.RUnlock()
			return
		default:
		}
		// Channel full: drop oldest while STILL holding RLock, then retry.
		// unsub takes the write lock before close, so close cannot interleave
		// with this drain while we hold the read lock.
		select {
		case <-sub.ch:
		default:
		}
		sub.mu.RUnlock()
		// Loop to retry the send.
	}
}

// SubscriberCount returns the current number of active subscribers.
func (h *Hub) SubscriberCount() int {
	h.mu.RLock()
	n := len(h.subs)
	h.mu.RUnlock()
	return n
}

// Compile-time assertion that *Hub satisfies EventBus.
var _ EventBus = (*Hub)(nil)
