package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/dsandor/memory/internal/backup"
	"github.com/dsandor/memory/internal/config"
	"github.com/dsandor/memory/internal/storage"
)

// openBackupStore builds the same store the server uses, returning it as a
// BackupStore. Caller must call the returned close func.
func openBackupStore(cfg config.Config) (storage.BackupStore, func(), error) {
	if cfg.DatabaseURL != "" {
		s, err := storage.NewPostgresStore(cfg.DatabaseURL, cfg.EmbeddingDim)
		if err != nil {
			return nil, nil, err
		}
		return s, func() { s.Close() }, nil
	}
	s, err := storage.NewSQLiteStore(cfg.DBPath, cfg.EmbeddingDim)
	if err != nil {
		return nil, nil, err
	}
	return s, func() { s.Close() }, nil
}

func runExport(cfg config.Config, args []string) error {
	fs := flag.NewFlagSet("export", flag.ContinueOnError)
	out := fs.String("out", "", "output archive path (default: backup-<timestamp>.tar.gz)")
	toStdout := fs.Bool("stdout", false, "write the archive to stdout instead of a file")
	if err := fs.Parse(args); err != nil {
		return err
	}

	store, closeFn, err := openBackupStore(cfg)
	if err != nil {
		return err
	}
	defer closeFn()

	now := time.Now()
	var w *os.File
	target := *out
	if *toStdout {
		w = os.Stdout
	} else {
		if target == "" {
			target = fmt.Sprintf("backup-%s.tar.gz", now.Format("20060102-150405"))
		}
		// 0600: the archive contains secrets (API key hashes, auth config).
		w, err = os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
		if err != nil {
			return err
		}
		defer w.Close()
	}

	man, err := backup.Export(context.Background(), store, w, version(), cfg.EmbeddingDim, now)
	if err != nil {
		return err
	}
	if !*toStdout {
		fmt.Fprintf(os.Stderr, "WARNING: %s contains secrets (API keys, auth config, password hashes). Protect it like a credential.\n", target)
		fmt.Fprintf(os.Stderr, "Backup written to %s (engine=%s, embeddings=%d)\n", target, man.SourceEngine, man.Embeddings)
	}
	return nil
}

func runImport(cfg config.Config, args []string) error {
	fs := flag.NewFlagSet("import", flag.ContinueOnError)
	in := fs.String("in", "", "input archive path (required)")
	force := fs.Bool("force", false, "overwrite a non-empty target database")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *in == "" {
		return fmt.Errorf("--in is required")
	}

	store, closeFn, err := openBackupStore(cfg)
	if err != nil {
		return err
	}
	defer closeFn()

	f, err := os.Open(*in)
	if err != nil {
		return err
	}
	defer f.Close()

	rep, err := backup.Import(context.Background(), store, f, backup.ImportOptions{Force: *force}, cfg.EmbeddingDim)
	if err != nil {
		return err
	}
	total := 0
	for _, n := range rep.TablesRestored {
		total += n
	}
	fmt.Fprintf(os.Stderr, "Restore complete: %d rows across %d tables, %d embeddings.\n", total, len(rep.TablesRestored), rep.EmbeddingsRestored)
	return nil
}

// version returns a build version string. If a version var already exists in
// the package (e.g. set via -ldflags), reuse it instead of this fallback.
func version() string {
	if v := os.Getenv("APP_VERSION"); v != "" {
		return v
	}
	return "dev"
}
