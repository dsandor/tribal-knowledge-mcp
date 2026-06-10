package web

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/dsandor/memory/internal/auth"
	"github.com/dsandor/memory/internal/live"
	"github.com/dsandor/memory/internal/storage"
)

// wireEvent is the unified JSON shape sent over the SSE stream and in the
// snapshot payload. It mirrors live.LiveEvent's json tags exactly so the
// frontend sees one consistent shape regardless of whether the event came
// from a live publish or from the historical activity feed.
type wireEvent struct {
	ID        string            `json:"id"`
	Type      string            `json:"type"`
	Actor     live.ActorRef     `json:"actor"`
	Fragment  string            `json:"fragment,omitempty"`
	EntryID   string            `json:"entry_id,omitempty"`
	Title     string            `json:"title,omitempty"`
	Meta      map[string]string `json:"meta,omitempty"`
	CreatedAt time.Time         `json:"created_at"`
}

// snapshotPayload is written as the first SSE frame upon connection.
type snapshotPayload struct {
	Online []live.ActorRef `json:"online"`
	Recent []wireEvent     `json:"recent"`
}

// mapActivityEvent converts a storage.ActivityEvent to the wire shape.
func mapActivityEvent(e storage.ActivityEvent) wireEvent {
	display := e.Metadata["display"]
	if display == "" {
		display = e.ActorID
	}
	return wireEvent{
		ID:        e.ID,
		Type:      e.EventType,
		Actor:     live.ActorRef{ID: e.ActorID, Display: display},
		Fragment:  e.Metadata["fragment"],
		Title:     e.Metadata["title"],
		EntryID:   e.EntryID,
		Meta:      e.Metadata,
		CreatedAt: e.CreatedAt,
	}
}

// mapLiveEvent converts a live.LiveEvent to the wire shape.
func mapLiveEvent(ev live.LiveEvent) wireEvent {
	return wireEvent{
		ID:        ev.ID,
		Type:      ev.Type,
		Actor:     ev.Actor,
		Fragment:  ev.Fragment,
		EntryID:   ev.EntryID,
		Title:     ev.Title,
		Meta:      ev.Meta,
		CreatedAt: ev.CreatedAt,
	}
}

// writeSSEEvent writes a single SSE event to the response writer.
// eventType is the SSE event: field value (e.g. "activity", "snapshot", "presence").
// data is the JSON payload.
func writeSSEEvent(w http.ResponseWriter, eventType string, data []byte) {
	fmt.Fprintf(w, "event: %s\ndata: %s\n\n", eventType, data)
}

// handleActivityStream handles GET /api/activity/stream.
// It delivers an initial snapshot frame followed by a live SSE stream of
// activity and presence events for the authenticated user's team.
func (s *Server) handleActivityStream(w http.ResponseWriter, r *http.Request) {
	// Guard: hub/presence not configured.
	if s.hub == nil || s.presence == nil {
		writeError(w, 503, "live_not_configured", "live stream not available")
		return
	}

	// Guard: subscriber cap.
	if s.hub.SubscriberCount() >= live.MaxSubscribers {
		writeError(w, 503, "subscriber_cap", "stream capacity reached — try again later")
		return
	}

	// Require flusher support (always true for net/http's ResponseWriter).
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, 500, "no_flusher", "streaming not supported by this server")
		return
	}

	tc := auth.GetTeamContext(r.Context())
	superadmin := tc.Role == "superadmin"
	teamID := tc.TeamID

	// Set SSE headers before writing any body.
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	// Build and send the initial snapshot frame.
	online := s.presence.Snapshot(teamID, superadmin)
	if online == nil {
		online = []live.ActorRef{}
	}

	recent := []wireEvent{}
	if events, err := s.store.ListActivity(r.Context(), teamID, 30, 0); err == nil && len(events) > 0 {
		// ListActivity returns newest-first; reverse to get oldest-first (newest-last).
		for i, j := 0, len(events)-1; i < j; i, j = i+1, j-1 {
			events[i], events[j] = events[j], events[i]
		}
		recent = make([]wireEvent, len(events))
		for i, e := range events {
			recent[i] = mapActivityEvent(e)
		}
	}

	snap := snapshotPayload{Online: online, Recent: recent}
	snapJSON, err := json.Marshal(snap)
	if err == nil {
		writeSSEEvent(w, "snapshot", snapJSON)
		flusher.Flush()
	}

	// Subscribe to the live event bus.
	ch, unsub := s.hub.Subscribe(teamID, superadmin)
	defer unsub()

	keepalive := time.NewTicker(20 * time.Second)
	defer keepalive.Stop()

	for {
		select {
		case <-r.Context().Done():
			return

		case ev := <-ch:
			we := mapLiveEvent(ev)
			data, err := json.Marshal(we)
			if err != nil {
				continue
			}
			evType := "activity"
			if ev.Type == live.TypePresence {
				evType = "presence"
			}
			writeSSEEvent(w, evType, data)
			flusher.Flush()

		case <-keepalive.C:
			fmt.Fprint(w, ": ping\n\n")
			flusher.Flush()
		}
	}
}
