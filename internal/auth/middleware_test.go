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
