package web

import (
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/dsandor/memory/internal/backup"
	"github.com/dsandor/memory/internal/storage"
)

// handleBackupDownload streams a .tar.gz logical backup of the database to the
// caller. Superadmin only (gated by the route group).
func (s *Server) handleBackupDownload(w http.ResponseWriter, r *http.Request) {
	bs, ok := s.store.(storage.BackupStore)
	if !ok {
		writeError(w, http.StatusInternalServerError, "backup_unsupported", "storage engine does not support backup")
		return
	}
	now := time.Now()
	name := fmt.Sprintf("tribal-backup-%s.tar.gz", now.Format("20060102-150405"))
	w.Header().Set("Content-Type", "application/gzip")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", name))
	if _, err := backup.Export(r.Context(), bs, w, s.appVersion, s.embeddingDim, now); err != nil {
		// Headers (and likely some body) are already sent, so we cannot change
		// the status code; log for diagnosis.
		slog.Error("backup export failed", "err", err)
	}
}

// handleRestoreUpload restores a .tar.gz archive uploaded as multipart field
// "archive". ?force=true overwrites a non-empty target. Superadmin only.
func (s *Server) handleRestoreUpload(w http.ResponseWriter, r *http.Request) {
	bs, ok := s.store.(storage.BackupStore)
	if !ok {
		writeError(w, http.StatusInternalServerError, "backup_unsupported", "storage engine does not support restore")
		return
	}
	force := r.URL.Query().Get("force") == "true"
	file, _, err := r.FormFile("archive")
	if err != nil {
		writeError(w, http.StatusBadRequest, "missing_archive", "expected multipart field 'archive'")
		return
	}
	defer file.Close()

	rep, err := backup.Import(r.Context(), bs, file, backup.ImportOptions{Force: force}, s.embeddingDim)
	if err != nil {
		writeError(w, http.StatusBadRequest, "restore_failed", err.Error())
		return
	}
	writeJSON(w, map[string]any{
		"tables_restored":     rep.TablesRestored,
		"embeddings_restored": rep.EmbeddingsRestored,
	})
}
