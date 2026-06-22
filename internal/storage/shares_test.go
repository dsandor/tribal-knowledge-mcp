package storage_test

import (
	"context"
	"errors"
	"testing"

	"github.com/dsandor/memory/internal/storage"
)

func TestCreateShare_ReturnsUnguessableID(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	sh, err := s.CreateShare(ctx, "entry-1", "team-src", "user-creator")
	if err != nil {
		t.Fatalf("CreateShare: %v", err)
	}
	if sh.ID == "" {
		t.Fatalf("CreateShare returned empty ID")
	}
	// A 32-byte base64url token is ~43 chars; assert it's clearly unguessable
	// (not a short/sequential value).
	if len(sh.ID) < 32 {
		t.Errorf("share ID too short to be unguessable: %q (len %d)", sh.ID, len(sh.ID))
	}
	if sh.EntryID != "entry-1" {
		t.Errorf("EntryID = %q, want %q", sh.EntryID, "entry-1")
	}
	if sh.SourceTeamID != "team-src" {
		t.Errorf("SourceTeamID = %q, want %q", sh.SourceTeamID, "team-src")
	}
	if sh.CreatedBy != "user-creator" {
		t.Errorf("CreatedBy = %q, want %q", sh.CreatedBy, "user-creator")
	}
	if sh.CreatedAt.IsZero() {
		t.Errorf("CreatedAt is zero")
	}

	// Two shares must not collide.
	sh2, err := s.CreateShare(ctx, "entry-1", "team-src", "user-creator")
	if err != nil {
		t.Fatalf("CreateShare second: %v", err)
	}
	if sh2.ID == sh.ID {
		t.Errorf("two shares produced the same ID %q", sh.ID)
	}
}

func TestGetShare_RoundTrips(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	created, err := s.CreateShare(ctx, "entry-42", "team-a", "alice")
	if err != nil {
		t.Fatalf("CreateShare: %v", err)
	}

	got, err := s.GetShare(ctx, created.ID)
	if err != nil {
		t.Fatalf("GetShare: %v", err)
	}
	if got.ID != created.ID {
		t.Errorf("ID = %q, want %q", got.ID, created.ID)
	}
	if got.EntryID != "entry-42" {
		t.Errorf("EntryID = %q, want %q", got.EntryID, "entry-42")
	}
	if got.SourceTeamID != "team-a" {
		t.Errorf("SourceTeamID = %q, want %q", got.SourceTeamID, "team-a")
	}
	if got.CreatedBy != "alice" {
		t.Errorf("CreatedBy = %q, want %q", got.CreatedBy, "alice")
	}
	if got.UsedAt != nil {
		t.Errorf("UsedAt should be nil for a fresh share, got %v", got.UsedAt)
	}
	if got.RevokedAt != nil {
		t.Errorf("RevokedAt should be nil for a fresh share, got %v", got.RevokedAt)
	}
	if got.CreatedAt.IsZero() {
		t.Errorf("CreatedAt is zero")
	}
}

func TestGetShare_Missing(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	_, err := s.GetShare(ctx, "does-not-exist")
	if !errors.Is(err, storage.ErrNotFound) {
		t.Fatalf("GetShare(missing) error = %v, want ErrNotFound", err)
	}
}

func TestMarkShareUsed_SingleUse(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	created, err := s.CreateShare(ctx, "entry-9", "team-x", "bob")
	if err != nil {
		t.Fatalf("CreateShare: %v", err)
	}

	// First use succeeds.
	if err := s.MarkShareUsed(ctx, created.ID, "carol", "imported-entry-1"); err != nil {
		t.Fatalf("MarkShareUsed (first): %v", err)
	}

	got, err := s.GetShare(ctx, created.ID)
	if err != nil {
		t.Fatalf("GetShare after use: %v", err)
	}
	if got.UsedAt == nil {
		t.Errorf("UsedAt should be set after MarkShareUsed")
	}
	if got.UsedBy != "carol" {
		t.Errorf("UsedBy = %q, want %q", got.UsedBy, "carol")
	}
	if got.ImportedEntryID != "imported-entry-1" {
		t.Errorf("ImportedEntryID = %q, want %q", got.ImportedEntryID, "imported-entry-1")
	}

	// Second use must fail (single-use).
	if err := s.MarkShareUsed(ctx, created.ID, "dave", "imported-entry-2"); err == nil {
		t.Fatalf("MarkShareUsed (second) should have failed, got nil")
	}
}

func TestMarkShareUsed_OnRevoked(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	created, err := s.CreateShare(ctx, "entry-11", "team-y", "erin")
	if err != nil {
		t.Fatalf("CreateShare: %v", err)
	}

	if err := s.RevokeShare(ctx, created.ID); err != nil {
		t.Fatalf("RevokeShare: %v", err)
	}

	got, err := s.GetShare(ctx, created.ID)
	if err != nil {
		t.Fatalf("GetShare after revoke: %v", err)
	}
	if got.RevokedAt == nil {
		t.Errorf("RevokedAt should be set after RevokeShare")
	}

	// Using a revoked share must fail.
	if err := s.MarkShareUsed(ctx, created.ID, "frank", "imported-entry-3"); err == nil {
		t.Fatalf("MarkShareUsed on revoked share should have failed, got nil")
	}
}
