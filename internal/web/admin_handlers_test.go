package web_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"testing/fstest"
	"time"

	"github.com/dsandor/memory/internal/storage"
	"github.com/dsandor/memory/internal/web"
)

// superadminMockStore wraps mockStore and returns Role:"superadmin" from GetAPIKeyByHash,
// allowing requests to pass the RequireSuperadmin middleware on admin routes.
type superadminMockStore struct {
	mockStore
}

func (s *superadminMockStore) GetAPIKeyByHash(_ context.Context, hash string) (*storage.APIKey, error) {
	return &storage.APIKey{ID: "sa-key", TeamID: "sa-team", Role: "superadmin", KeyHash: hash}, nil
}

// deleteTeamTrackingStore records calls to DeleteTeam and DeleteTeamMigrate
// and lets tests configure their return values.
type deleteTeamTrackingStore struct {
	superadminMockStore

	mu sync.Mutex

	// TeamDataCounts configuration
	countsResult storage.TeamDataCounts
	countsErr    error

	// DeleteTeam configuration
	deleteTeamErr    error
	deleteTeamCalled bool
	deleteTeamID     string

	// DeleteTeamMigrate configuration
	migrateResult   storage.TeamMigrationSummary
	migrateErr      error
	migrateCalled   bool
	migrateSourceID string
	migrateTargetID string
}

func (d *deleteTeamTrackingStore) TeamDataCounts(_ context.Context, id string) (storage.TeamDataCounts, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.countsResult, d.countsErr
}

func (d *deleteTeamTrackingStore) DeleteTeam(_ context.Context, id string) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.deleteTeamCalled = true
	d.deleteTeamID = id
	return d.deleteTeamErr
}

func (d *deleteTeamTrackingStore) DeleteTeamMigrate(_ context.Context, id, targetID string) (storage.TeamMigrationSummary, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.migrateCalled = true
	d.migrateSourceID = id
	d.migrateTargetID = targetID
	return d.migrateResult, d.migrateErr
}

func newAdminTestServer(t *testing.T, store web.AllStore) *web.Server {
	t.Helper()
	staticFS := fstest.MapFS{
		"index.html": {Data: []byte("<html><body>app</body></html>"), Mode: 0444, ModTime: time.Now()},
	}
	return web.NewServer(staticFS, store)
}

// saRequest creates a superadmin-authenticated request.
func saRequest(method, target string) *http.Request {
	req := httptest.NewRequest(method, target, nil)
	req.Header.Set("Authorization", "Bearer superadmin-token")
	return req
}

// TestDeleteTeamEmptyOK: empty team → 200 {ok:true}, DeleteTeam called.
func TestDeleteTeamEmptyOK(t *testing.T) {
	store := &deleteTeamTrackingStore{
		countsResult: storage.TeamDataCounts{}, // all zeros
	}
	srv := newAdminTestServer(t, store)

	req := saRequest("DELETE", "/api/admin/teams/team-x")
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp["ok"] != true {
		t.Errorf("want ok:true, got %v", resp)
	}
	store.mu.Lock()
	called := store.deleteTeamCalled
	id := store.deleteTeamID
	store.mu.Unlock()
	if !called {
		t.Error("DeleteTeam must be called for an empty team")
	}
	if id != "team-x" {
		t.Errorf("DeleteTeam called with %q, want %q", id, "team-x")
	}
}

// TestDeleteTeamWithDataReturns409Counts: team with 1 user + 2 entries → 409 with counts body;
// DeleteTeam must NOT be called.
func TestDeleteTeamWithDataReturns409Counts(t *testing.T) {
	store := &deleteTeamTrackingStore{
		countsResult: storage.TeamDataCounts{Users: 1, Entries: 2},
	}
	srv := newAdminTestServer(t, store)

	req := saRequest("DELETE", "/api/admin/teams/team-y")
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusConflict {
		t.Fatalf("want 409, got %d: %s", w.Code, w.Body.String())
	}

	var body struct {
		Error  string                 `json:"error"`
		Counts storage.TeamDataCounts `json:"counts"`
	}
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Error != "team_not_empty" {
		t.Errorf("error = %q, want %q", body.Error, "team_not_empty")
	}
	if body.Counts.Users != 1 {
		t.Errorf("counts.users = %d, want 1", body.Counts.Users)
	}
	if body.Counts.Entries != 2 {
		t.Errorf("counts.entries = %d, want 2", body.Counts.Entries)
	}

	store.mu.Lock()
	called := store.deleteTeamCalled
	store.mu.Unlock()
	if called {
		t.Error("DeleteTeam must NOT be called when team has data")
	}
}

// TestDeleteTeamMigrateHappyPath: ?migrate_to=t2 → 200 with summary JSON;
// store records the DeleteTeamMigrate call.
func TestDeleteTeamMigrateHappyPath(t *testing.T) {
	summary := storage.TeamMigrationSummary{
		Users:         3,
		Entries:       35,
		Agents:        1,
		AgentsSkipped: 0,
		Rules:         2,
	}
	store := &deleteTeamTrackingStore{
		migrateResult: summary,
	}
	srv := newAdminTestServer(t, store)

	req := saRequest("DELETE", "/api/admin/teams/src-team?migrate_to=dst-team")
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", w.Code, w.Body.String())
	}

	var got storage.TeamMigrationSummary
	if err := json.NewDecoder(w.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Users != 3 {
		t.Errorf("summary.users = %d, want 3", got.Users)
	}
	if got.Entries != 35 {
		t.Errorf("summary.entries = %d, want 35", got.Entries)
	}

	store.mu.Lock()
	migrateCalled := store.migrateCalled
	srcID := store.migrateSourceID
	tgtID := store.migrateTargetID
	store.mu.Unlock()

	if !migrateCalled {
		t.Error("DeleteTeamMigrate must be called for migrate path")
	}
	if srcID != "src-team" {
		t.Errorf("migrate sourceID = %q, want %q", srcID, "src-team")
	}
	if tgtID != "dst-team" {
		t.Errorf("migrate targetID = %q, want %q", tgtID, "dst-team")
	}
}

// TestDeleteTeamMigrateSelfTarget400: migrate_to == id → 400.
func TestDeleteTeamMigrateSelfTarget400(t *testing.T) {
	store := &deleteTeamTrackingStore{}
	srv := newAdminTestServer(t, store)

	req := saRequest("DELETE", "/api/admin/teams/team-a?migrate_to=team-a")
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d: %s", w.Code, w.Body.String())
	}
}

// TestDeleteTeamMigrateUnknownTarget400: unknown migrate_to target → 400.
func TestDeleteTeamMigrateUnknownTarget400(t *testing.T) {
	store := &deleteTeamTrackingStore{
		migrateErr: storage.ErrBadTarget,
	}
	srv := newAdminTestServer(t, store)

	req := saRequest("DELETE", "/api/admin/teams/team-a?migrate_to=no-such-team")
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d: %s", w.Code, w.Body.String())
	}
}

// TestDeleteTeamMigrateUnknownSource404: unknown source team → 404.
func TestDeleteTeamMigrateUnknownSource404(t *testing.T) {
	store := &deleteTeamTrackingStore{
		countsErr: storage.ErrNotFound,
	}
	srv := newAdminTestServer(t, store)

	req := saRequest("DELETE", "/api/admin/teams/no-such-team")
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("want 404, got %d: %s", w.Code, w.Body.String())
	}
}

// setRoleMockStore lets a test control the acting user's role (via GetAPIKeyByHash)
// and the set of users on the team (via ListUsers) so the IDOR guard in
// handleSetUserRole resolves the target user.
type setRoleMockStore struct {
	mockStore
	actorRole string
	teamUsers []storage.User
}

func (s *setRoleMockStore) GetAPIKeyByHash(_ context.Context, hash string) (*storage.APIKey, error) {
	return &storage.APIKey{ID: "actor-key", TeamID: "team-1", Role: s.actorRole, KeyHash: hash}, nil
}

func (s *setRoleMockStore) ListUsers(_ context.Context, _ string) ([]storage.User, error) {
	return s.teamUsers, nil
}

// TestSetUserRole_SuperadminGrant: only a superadmin may grant the superadmin role.
func TestSetUserRole_SuperadminGrant(t *testing.T) {
	cases := []struct {
		name      string
		actorRole string
		wantCode  int
	}{
		{name: "superadmin grants superadmin -> success", actorRole: "superadmin", wantCode: http.StatusOK},
		{name: "admin grants superadmin -> forbidden", actorRole: "admin", wantCode: http.StatusForbidden},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			store := &setRoleMockStore{
				actorRole: tc.actorRole,
				// teamUsers (ListUsers) is empty so the target is NOT in the
				// caller's active team. The superadmin path must resolve the
				// target via GetUserByID, not ListUsers; an admin would 404.
				teamUsers: nil,
			}
			// The target lives in a different team than the actor (team-1).
			store.users = map[string]storage.User{
				"target-user": {ID: "target-user", TeamID: "other-team", Role: "member"},
			}
			srv := newAdminTestServer(t, store)

			req := httptest.NewRequest("PUT", "/api/users/target-user/role",
				strings.NewReader(`{"role":"superadmin"}`))
			req.Header.Set("Authorization", "Bearer actor-token")
			w := httptest.NewRecorder()
			srv.ServeHTTP(w, req)

			if w.Code != tc.wantCode {
				t.Fatalf("want %d, got %d: %s", tc.wantCode, w.Code, w.Body.String())
			}
		})
	}
}
