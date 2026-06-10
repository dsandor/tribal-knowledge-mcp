package web_test

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"testing/fstest"
	"time"

	"github.com/dsandor/memory/internal/live"
	"github.com/dsandor/memory/internal/storage"
	"github.com/dsandor/memory/internal/web"
)

// streamStore extends mockStore with configurable ListActivity results.
type streamStore struct {
	mockStore
	activities []storage.ActivityEvent
}

func (s *streamStore) ListActivity(_ context.Context, _ string, _, _ int) ([]storage.ActivityEvent, error) {
	return s.activities, nil
}

// newLiveTestServer builds a Server wired with the given hub+presence,
// using a streamStore so ListActivity returns the provided activity events.
func newLiveTestServer(t *testing.T, store *streamStore, hub web.LiveHub, p *live.Presence) *web.Server {
	t.Helper()
	staticFS := fstest.MapFS{
		"index.html": {Data: []byte("<html><body>app</body></html>"), Mode: 0444, ModTime: time.Now()},
	}
	srv := web.NewServer(staticFS, store)
	if hub != nil && p != nil {
		srv = srv.WithLive(hub, p)
	}
	return srv
}

// safeRecorder is a thread-safe replacement for httptest.ResponseRecorder used
// in SSE tests where the handler goroutine writes concurrently with the test
// goroutine reading the accumulated body.
type safeRecorder struct {
	mu      sync.Mutex
	buf     bytes.Buffer
	code    int
	headers http.Header
	wroteHeader bool
}

func newSafeRecorder() *safeRecorder {
	return &safeRecorder{
		code:    200,
		headers: make(http.Header),
	}
}

func (r *safeRecorder) Header() http.Header { return r.headers }

func (r *safeRecorder) WriteHeader(code int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.wroteHeader {
		r.code = code
		r.wroteHeader = true
	}
}

func (r *safeRecorder) Write(b []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.buf.Write(b)
}

// Flush is a no-op — the handler already buffers through Write.
func (r *safeRecorder) Flush() {}

// Body returns a copy of the accumulated body so far, safe to call concurrently.
func (r *safeRecorder) Body() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.buf.String()
}

// Code returns the recorded HTTP status code.
func (r *safeRecorder) Code() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.code
}

// readSSEFrames reads up to maxFrames SSE frames from the recorder body,
// stopping early if ctx is cancelled or the body is exhausted.
// Each frame is returned as a raw string (all lines up to the blank line).
func readSSEFrames(body string, maxFrames int) []string {
	var frames []string
	var current strings.Builder
	scanner := bufio.NewScanner(strings.NewReader(body))
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			if current.Len() > 0 {
				frames = append(frames, current.String())
				current.Reset()
				if len(frames) >= maxFrames {
					break
				}
			}
		} else {
			if current.Len() > 0 {
				current.WriteByte('\n')
			}
			current.WriteString(line)
		}
	}
	return frames
}

// parseSSEFrame splits an SSE frame into its event: and data: components.
func parseSSEFrame(frame string) (event, data string) {
	for _, line := range strings.Split(frame, "\n") {
		if strings.HasPrefix(line, "event: ") {
			event = strings.TrimPrefix(line, "event: ")
		} else if strings.HasPrefix(line, "data: ") {
			data = strings.TrimPrefix(line, "data: ")
		}
	}
	return
}

// TestHandleActivityStream_NilHub ensures the handler returns 503 when no hub is configured.
func TestHandleActivityStream_NilHub(t *testing.T) {
	store := &streamStore{}
	srv := newLiveTestServer(t, store, nil, nil)

	req := authRequest("GET", "/api/activity/stream", "")
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("want 503, got %d: %s", w.Code, w.Body.String())
	}
}

// TestHandleActivityStream_SnapshotFrame verifies that the first SSE frame is
// a "snapshot" event containing the online actors and recent activity.
func TestHandleActivityStream_SnapshotFrame(t *testing.T) {
	hub := live.NewHub()
	presence := live.NewPresence(60 * time.Second)

	// Pre-seed two activity events in the store.
	// ListActivity contract: newest-first, so ev2 (newer) comes before ev1 (older).
	// The handler reverses them so the snapshot contains oldest-first (ev1 then ev2).
	activities := []storage.ActivityEvent{
		{ID: "ev2", EventType: "approved", ActorID: "user-b", EntryID: "entry-2",
			Metadata: map[string]string{}, CreatedAt: time.Now().Add(-1 * time.Minute)},
		{ID: "ev1", EventType: "knowledge_stored", ActorID: "user-a", EntryID: "entry-1",
			Metadata: map[string]string{"title": "First Entry"}, CreatedAt: time.Now().Add(-2 * time.Minute)},
	}
	store := &streamStore{activities: activities}
	srv := newLiveTestServer(t, store, hub, presence)

	// Use a cancelable context so the handler exits cleanly after we've read the snapshot.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	req := authRequest("GET", "/api/activity/stream", "").WithContext(ctx)
	w := newSafeRecorder()

	done := make(chan struct{})
	go func() {
		defer close(done)
		srv.ServeHTTP(w, req)
	}()

	// Poll until the snapshot frame appears (or deadline), then cancel.
	// Reading w.Body() is safe because safeRecorder protects access with a mutex.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if frames := readSSEFrames(w.Body(), 1); len(frames) > 0 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	cancel()
	<-done

	body := w.Body()
	frames := readSSEFrames(body, 5)

	if len(frames) == 0 {
		t.Fatalf("expected at least one SSE frame, got none. body: %q", body)
	}

	evType, data := parseSSEFrame(frames[0])
	if evType != "snapshot" {
		t.Fatalf("first frame event = %q, want snapshot", evType)
	}

	var snap struct {
		Online []live.ActorRef `json:"online"`
		Recent []struct {
			ID   string `json:"id"`
			Type string `json:"type"`
		} `json:"recent"`
	}
	if err := json.Unmarshal([]byte(data), &snap); err != nil {
		t.Fatalf("unmarshal snapshot: %v — raw data: %s", err, data)
	}

	// Two activity events should appear in recent (reversed = oldest first).
	if len(snap.Recent) != 2 {
		t.Errorf("snapshot.recent len = %d, want 2", len(snap.Recent))
	}
	if len(snap.Recent) >= 1 && snap.Recent[0].ID != "ev1" {
		t.Errorf("recent[0].ID = %q, want ev1 (oldest first)", snap.Recent[0].ID)
	}
	if len(snap.Recent) >= 2 && snap.Recent[1].ID != "ev2" {
		t.Errorf("recent[1].ID = %q, want ev2", snap.Recent[1].ID)
	}
}

// TestHandleActivityStream_LiveEvent verifies that a published event appears
// as an "activity" SSE frame after the snapshot.
func TestHandleActivityStream_LiveEvent(t *testing.T) {
	hub := live.NewHub()
	presence := live.NewPresence(60 * time.Second)
	store := &streamStore{}
	srv := newLiveTestServer(t, store, hub, presence)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	req := authRequest("GET", "/api/activity/stream", "").WithContext(ctx)
	w := newSafeRecorder()

	done := make(chan struct{})
	go func() {
		defer close(done)
		srv.ServeHTTP(w, req)
	}()

	// Allow the handler goroutine to subscribe before publishing.
	// Poll until a subscriber appears (up to 500ms).
	deadline := time.Now().Add(500 * time.Millisecond)
	for hub.SubscriberCount() == 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if hub.SubscriberCount() == 0 {
		t.Fatal("handler did not subscribe within 500ms")
	}

	// Publish an activity event for the same team the mock auth uses ("test-team").
	hub.Publish(live.LiveEvent{
		ID:        "live-ev-1",
		Type:      live.TypeKnowledgeStored,
		TeamID:    "test-team",
		EntryID:   "entry-x",
		Title:     "Live Entry",
		Actor:     live.ActorRef{ID: "user-a", Display: "User A"},
		CreatedAt: time.Now().UTC(),
	})

	// Poll until the activity frame appears (snapshot + at least one more), then cancel.
	deadline2 := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline2) {
		if frames := readSSEFrames(w.Body(), 10); len(frames) >= 2 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	cancel()
	<-done

	body := w.Body()
	frames := readSSEFrames(body, 10)

	// We expect at least: snapshot + activity frame.
	if len(frames) < 2 {
		t.Fatalf("expected >=2 frames (snapshot + activity), got %d. body: %q", len(frames), body)
	}

	// First frame must be snapshot.
	if evType, _ := parseSSEFrame(frames[0]); evType != "snapshot" {
		t.Errorf("frames[0] event = %q, want snapshot", evType)
	}

	// Find the activity frame.
	found := false
	for _, frame := range frames[1:] {
		evType, data := parseSSEFrame(frame)
		if evType != "activity" {
			continue
		}
		var we struct {
			ID   string `json:"id"`
			Type string `json:"type"`
		}
		if err := json.Unmarshal([]byte(data), &we); err != nil {
			continue
		}
		if we.ID == "live-ev-1" && we.Type == live.TypeKnowledgeStored {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("did not find expected activity frame with id=live-ev-1 in frames: %v", frames)
	}
}

// TestHandleActivityStream_ContextCancel verifies the handler exits when the
// request context is cancelled (no goroutine leak).
func TestHandleActivityStream_ContextCancel(t *testing.T) {
	hub := live.NewHub()
	presence := live.NewPresence(60 * time.Second)
	store := &streamStore{}
	srv := newLiveTestServer(t, store, hub, presence)

	ctx, cancel := context.WithCancel(context.Background())

	req := authRequest("GET", "/api/activity/stream", "").WithContext(ctx)
	w := httptest.NewRecorder()

	done := make(chan struct{})
	go func() {
		defer close(done)
		srv.ServeHTTP(w, req)
	}()

	// Cancel immediately and verify the handler exits cleanly.
	cancel()

	select {
	case <-done:
		// Handler exited cleanly.
	case <-time.After(2 * time.Second):
		t.Fatal("handler did not exit within 2s after context cancel")
	}

	// The hub should have no subscribers remaining after handler exits.
	if hub.SubscriberCount() != 0 {
		t.Errorf("subscriber count = %d after handler exit, want 0", hub.SubscriberCount())
	}
}

// TestHandleActivityStream_PresenceFrame verifies that a presence-typed event
// is sent as a "presence" SSE frame (not "activity").
func TestHandleActivityStream_PresenceFrame(t *testing.T) {
	hub := live.NewHub()
	presence := live.NewPresence(60 * time.Second)
	store := &streamStore{}
	srv := newLiveTestServer(t, store, hub, presence)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	req := authRequest("GET", "/api/activity/stream", "").WithContext(ctx)
	w := newSafeRecorder()

	done := make(chan struct{})
	go func() {
		defer close(done)
		srv.ServeHTTP(w, req)
	}()

	// Wait for subscription.
	deadline := time.Now().Add(500 * time.Millisecond)
	for hub.SubscriberCount() == 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}

	hub.Publish(live.LiveEvent{
		ID:        "pres-1",
		Type:      live.TypePresence,
		TeamID:    "test-team",
		Meta:      map[string]string{"online_count": "3"},
		CreatedAt: time.Now().UTC(),
	})

	// Poll until the presence frame appears (or deadline), then cancel.
	presDeadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(presDeadline) {
		found := false
		for _, frame := range readSSEFrames(w.Body(), 10) {
			if evType, _ := parseSSEFrame(frame); evType == "presence" {
				found = true
				break
			}
		}
		if found {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	cancel()
	<-done

	body := w.Body()
	frames := readSSEFrames(body, 10)

	found := false
	for _, frame := range frames {
		evType, _ := parseSSEFrame(frame)
		if evType == "presence" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected a presence frame, got frames: %v", frames)
	}
}
