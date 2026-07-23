package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/dsandor/memory/internal/config"
	"github.com/dsandor/memory/internal/storage"
	"github.com/dsandor/memory/internal/trainexport"
)

func runExportTrain(cfg config.Config, args []string) error {
	fs := flag.NewFlagSet("export-train", flag.ContinueOnError)
	out := fs.String("out", "./train-export", "output directory for JSONL + manifest")
	team := fs.String("team", "", "optional team_id filter")
	format := fs.String("format", "all", "sft | dpo | sharegpt | all")
	minRating := fs.Int("min-rating", 0, "minimum session outcome / entry rating (0 = no filter)")
	trainOnly := fs.Bool("train-eligible-only", true, "only train_eligible sessions not blocked")
	bootstrap := fs.Bool("bootstrap-entries", true, "include SFT rows from approved knowledge entries")
	sinceStr := fs.String("since", "", "RFC3339 lower bound on session started_at")
	untilStr := fs.String("until", "", "RFC3339 upper bound on session started_at")
	if err := fs.Parse(args); err != nil {
		return err
	}

	var since, until *time.Time
	if *sinceStr != "" {
		t, err := time.Parse(time.RFC3339, *sinceStr)
		if err != nil {
			return fmt.Errorf("parse --since: %w", err)
		}
		since = &t
	}
	if *untilStr != "" {
		t, err := time.Parse(time.RFC3339, *untilStr)
		if err != nil {
			return fmt.Errorf("parse --until: %w", err)
		}
		until = &t
	}

	store, closeFn, err := openTrainExportStore(cfg)
	if err != nil {
		return err
	}
	defer closeFn()

	man, err := trainexport.Export(context.Background(), store, trainexport.Options{
		TeamID:                *team,
		MinRating:             *minRating,
		TrainEligibleOnly:     *trainOnly,
		Since:                 since,
		Until:                 until,
		Format:                *format,
		IncludeEntryBootstrap: *bootstrap,
		OutDir:                *out,
	})
	if err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "Train export written to %s\n", *out)
	fmt.Fprintf(os.Stderr, "  sessions=%d sft=%d dpo=%d sharegpt=%d\n",
		man.Counts["sessions"], man.Counts["sft"], man.Counts["dpo"], man.Counts["sharegpt"])
	return nil
}

// trainExportStore combines FT session + entry listing for export.
type trainExportStore interface {
	storage.FTSessionStore
	ListEntries(ctx context.Context, filter storage.ListFilter) ([]storage.KnowledgeEntry, error)
	GetEntry(ctx context.Context, id string) (*storage.KnowledgeEntry, error)
	Close() error
}

func openTrainExportStore(cfg config.Config) (trainExportStore, func(), error) {
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
