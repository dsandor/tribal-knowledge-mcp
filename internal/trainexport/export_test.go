package trainexport

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/dsandor/memory/internal/storage"
)

func TestExportBootstrapAndSession(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "t.db")
	store, err := storage.NewSQLiteStore(dbPath, 768)
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	defer store.Close()
	ctx := context.Background()

	// Bootstrap knowledge
	eid, err := store.StoreEntry(ctx, storage.KnowledgeEntry{
		Type:        storage.KTPrompt,
		Title:       "Good review opener",
		Content:     "Start with what you used it for...",
		Description: "When writing a product review intro",
		Domain:      "reviews",
		Status:      "approved",
		Rating:      5,
		TeamID:      "t1",
	}, nil)
	if err != nil {
		t.Fatalf("store entry: %v", err)
	}
	_ = store.ApproveEntry(ctx, eid)

	// Session with turns + preference
	sid, err := store.CreateFTSession(ctx, storage.FTSession{
		TeamID:        "t1",
		UserID:        "u1",
		TaskSummary:   "write a review",
		Domain:        "reviews",
		TrainEligible: true,
	})
	if err != nil {
		t.Fatalf("session: %v", err)
	}
	uTurn, err := store.AddFTTurn(ctx, storage.FTTurn{
		SessionID: sid, Seq: -1, Role: storage.FTRoleUser, Kind: storage.FTKindMessage,
		Content: "Write a review for this blender", EntryIDs: []string{eid},
	})
	if err != nil {
		t.Fatalf("turn: %v", err)
	}
	_, err = store.AddFTTurn(ctx, storage.FTTurn{
		SessionID: sid, Seq: -1, Role: storage.FTRoleAssistant, Kind: storage.FTKindMessage,
		Content: "I used this blender every morning...",
	})
	if err != nil {
		t.Fatalf("turn2: %v", err)
	}
	_, err = store.AddFTPreference(ctx, storage.FTPreference{
		SessionID: sid, PromptTurnID: uTurn,
		ChosenText: "I used this blender every morning for smoothies.",
		RejectedText: "This amazing blender is a game-changer!!!",
		Source: storage.FTPrefUserEdit,
	})
	if err != nil {
		t.Fatalf("pref: %v", err)
	}
	r := 5
	if err := store.CompleteFTSession(ctx, sid, &r, "", storage.FTSessionCompleted); err != nil {
		t.Fatalf("complete: %v", err)
	}

	out := t.TempDir()
	man, err := Export(ctx, store, Options{
		TeamID:                "t1",
		MinRating:             0,
		TrainEligibleOnly:     true,
		Format:                "all",
		IncludeEntryBootstrap: true,
		OutDir:                out,
	})
	if err != nil {
		t.Fatalf("export: %v", err)
	}
	if man.Counts["sessions"] != 1 {
		t.Fatalf("sessions=%d", man.Counts["sessions"])
	}
	if man.Counts["sft"] < 2 { // session final + bootstrap entry
		t.Fatalf("sft=%d want >=2", man.Counts["sft"])
	}
	if man.Counts["dpo"] != 1 {
		t.Fatalf("dpo=%d", man.Counts["dpo"])
	}
	if man.Counts["sharegpt"] != 1 {
		t.Fatalf("sharegpt=%d", man.Counts["sharegpt"])
	}

	// Validate JSONL parses
	for _, name := range []string{"sft.jsonl", "dpo.jsonl", "sharegpt.jsonl", "manifest.json"} {
		p := filepath.Join(out, name)
		b, err := os.ReadFile(p)
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		if len(b) == 0 {
			t.Fatalf("empty %s", name)
		}
		if name == "manifest.json" {
			var m Manifest
			if err := json.Unmarshal(b, &m); err != nil {
				t.Fatalf("manifest: %v", err)
			}
		}
	}
}
