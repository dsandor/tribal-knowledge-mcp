package auth_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/dsandor/memory/internal/auth"
	"github.com/dsandor/memory/internal/storage"
)

type mockAuthStore struct {
	key  *storage.APIKey
	sess *storage.Session
	user *storage.User // returned by GetUserByID when non-nil and ID matches
}

func (m *mockAuthStore) GetAPIKeyByHash(_ context.Context, hash string) (*storage.APIKey, error) {
	if m.key != nil && m.key.KeyHash == hash {
		return m.key, nil
	}
	return nil, storage.ErrNotFound
}

func (m *mockAuthStore) GetSession(_ context.Context, tokenHash string) (*storage.Session, error) {
	if m.sess != nil && m.sess.TokenHash == tokenHash {
		return m.sess, nil
	}
	return nil, storage.ErrNotFound
}

func (m *mockAuthStore) GetUserByID(_ context.Context, id string) (*storage.User, error) {
	if m.user != nil && m.user.ID == id {
		return m.user, nil
	}
	return nil, storage.ErrNotFound
}

func (m *mockAuthStore) TouchAPIKey(_ context.Context, id string) error { return nil }

func TestRequireAuth_MissingCredentials(t *testing.T) {
	mw := auth.RequireAuth(&mockAuthStore{})
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	}))
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, httptest.NewRequest("GET", "/", nil))
	if rr.Code != 401 {
		t.Errorf("expected 401, got %d", rr.Code)
	}
}

func TestRequireAuth_ValidBearerToken(t *testing.T) {
	rawKey := "my-raw-key"
	store := &mockAuthStore{key: &storage.APIKey{
		ID:      "k1",
		TeamID:  "team-abc",
		KeyHash: auth.HashSHA256(rawKey),
		Role:    "member",
	}}
	mw := auth.RequireAuth(store)
	var gotCtx auth.TeamContext
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotCtx = auth.GetTeamContext(r.Context())
		w.WriteHeader(200)
	}))
	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("Authorization", "Bearer "+rawKey)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != 200 {
		t.Errorf("expected 200, got %d", rr.Code)
	}
	if gotCtx.TeamID != "team-abc" {
		t.Errorf("TeamID = %q", gotCtx.TeamID)
	}
	if gotCtx.Role != "member" {
		t.Errorf("Role = %q", gotCtx.Role)
	}
}

func TestRequireAuth_ValidSessionCookie(t *testing.T) {
	rawToken := "session-token-value"
	store := &mockAuthStore{sess: &storage.Session{
		ID:        "s1",
		UserID:    "user-xyz",
		TokenHash: auth.HashSHA256(rawToken),
		ExpiresAt: time.Now().Add(time.Hour),
	}}
	mw := auth.RequireAuth(store)
	var gotCtx auth.TeamContext
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotCtx = auth.GetTeamContext(r.Context())
		w.WriteHeader(200)
	}))
	req := httptest.NewRequest("GET", "/", nil)
	req.AddCookie(&http.Cookie{Name: "session", Value: rawToken})
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != 200 {
		t.Errorf("expected 200, got %d", rr.Code)
	}
	if gotCtx.UserID != "user-xyz" {
		t.Errorf("UserID = %q", gotCtx.UserID)
	}
}

func TestRequireAuth_UserScopedKey_SetsUserIDAndKeyType(t *testing.T) {
	rawKey := "user-scoped-key"
	store := &mockAuthStore{key: &storage.APIKey{
		ID:      "k-user",
		TeamID:  "team-u",
		UserID:  "user-123",
		KeyType: "user",
		KeyHash: auth.HashSHA256(rawKey),
		Role:    "member",
	}}
	mw := auth.RequireAuth(store)
	var gotCtx auth.TeamContext
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotCtx = auth.GetTeamContext(r.Context())
		w.WriteHeader(200)
	}))
	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("Authorization", "Bearer "+rawKey)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != 200 {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	if gotCtx.UserID != "user-123" {
		t.Errorf("UserID = %q, want user-123", gotCtx.UserID)
	}
	if gotCtx.KeyType != "user" {
		t.Errorf("KeyType = %q, want user", gotCtx.KeyType)
	}
}

func TestRequireAuth_TeamScopedKey_EmptyUserIDAndTeamKeyType(t *testing.T) {
	rawKey := "team-scoped-key"
	store := &mockAuthStore{key: &storage.APIKey{
		ID:      "k-team",
		TeamID:  "team-t",
		UserID:  "",
		KeyType: "team",
		KeyHash: auth.HashSHA256(rawKey),
		Role:    "member",
	}}
	mw := auth.RequireAuth(store)
	var gotCtx auth.TeamContext
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotCtx = auth.GetTeamContext(r.Context())
		w.WriteHeader(200)
	}))
	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("Authorization", "Bearer "+rawKey)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != 200 {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	if gotCtx.UserID != "" {
		t.Errorf("UserID = %q, want empty", gotCtx.UserID)
	}
	if gotCtx.KeyType != "team" {
		t.Errorf("KeyType = %q, want team", gotCtx.KeyType)
	}
}

func TestRequireCurator_RejectsMember(t *testing.T) {
	rawKey := "member-key"
	store := &mockAuthStore{key: &storage.APIKey{
		ID:      "k2",
		TeamID:  "t",
		KeyHash: auth.HashSHA256(rawKey),
		Role:    "member",
	}}
	chain := auth.RequireAuth(store)(auth.RequireCurator()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	})))
	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("Authorization", "Bearer "+rawKey)
	rr := httptest.NewRecorder()
	chain.ServeHTTP(rr, req)
	if rr.Code != 403 {
		t.Errorf("expected 403, got %d", rr.Code)
	}
}

func TestRequireCurator_AllowsCurator(t *testing.T) {
	rawKey := "curator-key"
	store := &mockAuthStore{key: &storage.APIKey{
		ID:      "k3",
		TeamID:  "t",
		KeyHash: auth.HashSHA256(rawKey),
		Role:    "curator",
	}}
	chain := auth.RequireAuth(store)(auth.RequireCurator()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	})))
	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("Authorization", "Bearer "+rawKey)
	rr := httptest.NewRecorder()
	chain.ServeHTTP(rr, req)
	if rr.Code != 200 {
		t.Errorf("expected 200, got %d", rr.Code)
	}
}

func TestRequireSuperadmin_AllowsSuperadmin(t *testing.T) {
	rawKey := "super-key"
	store := &mockAuthStore{key: &storage.APIKey{
		ID:      "k4",
		KeyHash: auth.HashSHA256(rawKey),
		Role:    "superadmin",
	}}
	chain := auth.RequireAuth(store)(auth.RequireSuperadmin()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	})))
	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("Authorization", "Bearer "+rawKey)
	rr := httptest.NewRecorder()
	chain.ServeHTTP(rr, req)
	if rr.Code != 200 {
		t.Errorf("expected 200, got %d", rr.Code)
	}
}

func TestRequireAdmin_RejectsAdmin_WhenSuperadminRequired(t *testing.T) {
	rawKey := "admin-key"
	store := &mockAuthStore{key: &storage.APIKey{
		ID:      "k5",
		KeyHash: auth.HashSHA256(rawKey),
		Role:    "admin",
	}}
	chain := auth.RequireAuth(store)(auth.RequireSuperadmin()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	})))
	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("Authorization", "Bearer "+rawKey)
	rr := httptest.NewRecorder()
	chain.ServeHTTP(rr, req)
	if rr.Code != 403 {
		t.Errorf("expected 403, got %d", rr.Code)
	}
}

// --- Presence hook and Display tests ---

// mockPresenceToucher records calls to Touch for assertion in tests.
type mockPresenceToucher struct {
	calls []presenceCall
}

type presenceCall struct {
	teamID  string
	actorID string
	display string
}

func (m *mockPresenceToucher) Touch(teamID, actorID, display string) {
	m.calls = append(m.calls, presenceCall{teamID, actorID, display})
}

func TestRequireAuth_BearerToken_InvokesPresenceHook(t *testing.T) {
	rawKey := "hook-test-key"
	store := &mockAuthStore{key: &storage.APIKey{
		ID:      "key-id-1",
		TeamID:  "team-hook",
		KeyHash: auth.HashSHA256(rawKey),
		Name:    "my-key-name",
		Role:    "member",
	}}
	toucher := &mockPresenceToucher{}
	var gotCtx auth.TeamContext
	mw := auth.RequireAuth(store, toucher)
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotCtx = auth.GetTeamContext(r.Context())
		w.WriteHeader(200)
	}))
	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("Authorization", "Bearer "+rawKey)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != 200 {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	if len(toucher.calls) != 1 {
		t.Fatalf("expected 1 presence touch, got %d", len(toucher.calls))
	}
	call := toucher.calls[0]
	if call.teamID != "team-hook" {
		t.Errorf("Touch teamID = %q, want team-hook", call.teamID)
	}
	if call.actorID != "key-id-1" {
		t.Errorf("Touch actorID = %q, want key-id-1", call.actorID)
	}
	if call.display != "my-key-name" {
		t.Errorf("Touch display = %q, want my-key-name", call.display)
	}
	// Verify TeamContext.Display is also set.
	if gotCtx.Display != "my-key-name" {
		t.Errorf("TeamContext.Display = %q, want my-key-name", gotCtx.Display)
	}
}

func TestRequireAuth_BearerToken_FallsBackToKeyIDWhenNameEmpty(t *testing.T) {
	rawKey := "hook-test-key-noname"
	store := &mockAuthStore{key: &storage.APIKey{
		ID:      "key-id-noname",
		TeamID:  "team-nn",
		KeyHash: auth.HashSHA256(rawKey),
		Name:    "", // empty name: should fall back to ID
		Role:    "member",
	}}
	toucher := &mockPresenceToucher{}
	mw := auth.RequireAuth(store, toucher)
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) }))
	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("Authorization", "Bearer "+rawKey)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if len(toucher.calls) != 1 {
		t.Fatalf("expected 1 presence touch, got %d", len(toucher.calls))
	}
	if toucher.calls[0].display != "key-id-noname" {
		t.Errorf("display fallback = %q, want key-id-noname", toucher.calls[0].display)
	}
}

func TestRequireAuth_SessionCookie_InvokesPresenceHook(t *testing.T) {
	rawToken := "hook-session-token"
	store := &mockAuthStore{
		sess: &storage.Session{
			ID:        "s-hook",
			UserID:    "user-hook-id",
			TokenHash: auth.HashSHA256(rawToken),
			ExpiresAt: time.Now().Add(time.Hour),
		},
		// Provide the user record so the session path resolves TeamID/Role/Display.
		user: &storage.User{
			ID:     "user-hook-id",
			TeamID: "team-session",
			Name:   "Hook User",
			Role:   "member",
		},
	}
	toucher := &mockPresenceToucher{}
	var gotCtx auth.TeamContext
	mw := auth.RequireAuth(store, toucher)
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotCtx = auth.GetTeamContext(r.Context())
		w.WriteHeader(200)
	}))
	req := httptest.NewRequest("GET", "/", nil)
	req.AddCookie(&http.Cookie{Name: "session", Value: rawToken})
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != 200 {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	if len(toucher.calls) != 1 {
		t.Fatalf("expected 1 presence touch, got %d", len(toucher.calls))
	}
	call := toucher.calls[0]
	if call.teamID != "team-session" {
		t.Errorf("Touch teamID = %q, want team-session", call.teamID)
	}
	if call.actorID != "user-hook-id" {
		t.Errorf("Touch actorID = %q, want user-hook-id", call.actorID)
	}
	if call.display != "Hook User" {
		t.Errorf("Touch display = %q, want Hook User", call.display)
	}
	// TeamContext should be fully populated.
	if gotCtx.TeamID != "team-session" {
		t.Errorf("TeamContext.TeamID = %q, want team-session", gotCtx.TeamID)
	}
	if gotCtx.Role != "member" {
		t.Errorf("TeamContext.Role = %q, want member", gotCtx.Role)
	}
	if gotCtx.Display != "Hook User" {
		t.Errorf("TeamContext.Display = %q, want Hook User", gotCtx.Display)
	}
}

// TestRequireAuth_Session_FallsBackWhenUserLookupFails verifies that if
// GetUserByID fails the session path still succeeds with minimal context
// (UserID only) and does NOT call presence (teamID would be empty).
func TestRequireAuth_Session_FallsBackWhenUserLookupFails(t *testing.T) {
	rawToken := "fallback-session-token"
	store := &mockAuthStore{
		sess: &storage.Session{
			ID:        "s-fallback",
			UserID:    "user-fallback",
			TokenHash: auth.HashSHA256(rawToken),
			ExpiresAt: time.Now().Add(time.Hour),
		},
		// user is nil → GetUserByID returns ErrNotFound → minimal context
	}
	toucher := &mockPresenceToucher{}
	var gotCtx auth.TeamContext
	mw := auth.RequireAuth(store, toucher)
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotCtx = auth.GetTeamContext(r.Context())
		w.WriteHeader(200)
	}))
	req := httptest.NewRequest("GET", "/", nil)
	req.AddCookie(&http.Cookie{Name: "session", Value: rawToken})
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != 200 {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	// With empty teamID, presence must NOT be touched.
	if len(toucher.calls) != 0 {
		t.Errorf("expected 0 presence touches (no team), got %d", len(toucher.calls))
	}
	if gotCtx.UserID != "user-fallback" {
		t.Errorf("UserID = %q, want user-fallback", gotCtx.UserID)
	}
	if gotCtx.TeamID != "" {
		t.Errorf("TeamID = %q, want empty (fallback path)", gotCtx.TeamID)
	}
}

func TestRequireAuth_NilPresenceHook_DoesNotPanic(t *testing.T) {
	rawKey := "nil-hook-key"
	store := &mockAuthStore{key: &storage.APIKey{
		ID:      "k-nil",
		TeamID:  "t-nil",
		KeyHash: auth.HashSHA256(rawKey),
		Role:    "member",
	}}
	// Passing nil hook explicitly — must not panic.
	mw := auth.RequireAuth(store, nil)
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) }))
	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("Authorization", "Bearer "+rawKey)
	rr := httptest.NewRecorder()

	defer func() {
		if r := recover(); r != nil {
			t.Errorf("handler panicked: %v", r)
		}
	}()
	handler.ServeHTTP(rr, req)
	if rr.Code != 200 {
		t.Errorf("expected 200, got %d", rr.Code)
	}
}
