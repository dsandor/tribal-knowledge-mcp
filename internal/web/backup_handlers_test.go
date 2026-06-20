package web_test

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"
	"testing/fstest"
	"time"

	"github.com/dsandor/memory/internal/storage"
	"github.com/dsandor/memory/internal/web"
)

// newBackupTestServer builds a Server backed by a real SQLite store with auth
// bypassed (every request runs as superadmin) and backup config wired.
func newBackupTestServer(t *testing.T) *web.Server {
	t.Helper()
	store, err := storage.NewSQLiteStore(t.TempDir()+"/web.db", 4)
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	staticFS := fstest.MapFS{
		"index.html": {Data: []byte("<html><body>app</body></html>"), Mode: 0444, ModTime: time.Now()},
	}
	return web.NewServer(staticFS, store).WithDevBypass(true).WithBackupConfig(4, "test")
}

func TestBackupDownload(t *testing.T) {
	srv := newBackupTestServer(t)

	req := httptest.NewRequest(http.MethodGet, "/api/admin/backup", nil)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status: got %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/gzip" {
		t.Errorf("Content-Type: got %q, want application/gzip", ct)
	}
	if cd := rec.Header().Get("Content-Disposition"); cd == "" {
		t.Errorf("Content-Disposition: expected attachment header, got empty")
	}
	if rec.Body.Len() == 0 {
		t.Errorf("body: expected non-empty archive, got 0 bytes")
	}
}

func TestRestoreRoundTrip(t *testing.T) {
	srv := newBackupTestServer(t)

	// 1. Download a backup archive.
	getReq := httptest.NewRequest(http.MethodGet, "/api/admin/backup", nil)
	getRec := httptest.NewRecorder()
	srv.ServeHTTP(getRec, getReq)
	if getRec.Code != http.StatusOK {
		t.Fatalf("backup status: got %d, want 200; body=%s", getRec.Code, getRec.Body.String())
	}
	archive := getRec.Body.Bytes()
	if len(archive) == 0 {
		t.Fatalf("backup body: expected non-empty archive")
	}

	// 2. Build a multipart body with the archive in field "archive".
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	part, err := mw.CreateFormFile("archive", "backup.tar.gz")
	if err != nil {
		t.Fatalf("CreateFormFile: %v", err)
	}
	if _, err := part.Write(archive); err != nil {
		t.Fatalf("write archive: %v", err)
	}
	if err := mw.Close(); err != nil {
		t.Fatalf("close multipart: %v", err)
	}

	// 3. POST the archive to restore with force=true.
	postReq := httptest.NewRequest(http.MethodPost, "/api/admin/restore?force=true", &body)
	postReq.Header.Set("Content-Type", mw.FormDataContentType())
	postRec := httptest.NewRecorder()
	srv.ServeHTTP(postRec, postReq)

	if postRec.Code != http.StatusOK {
		t.Fatalf("restore status: got %d, want 200; body=%s", postRec.Code, postRec.Body.String())
	}

	var resp map[string]any
	if err := json.Unmarshal(postRec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode restore response: %v; body=%s", err, postRec.Body.String())
	}
	if _, ok := resp["tables_restored"]; !ok {
		t.Errorf("response missing tables_restored: %s", postRec.Body.String())
	}
	if _, ok := resp["embeddings_restored"]; !ok {
		t.Errorf("response missing embeddings_restored: %s", postRec.Body.String())
	}
}
