package storage

import (
	"context"
	"path/filepath"
	"testing"
)

func newFTSessionTestStore(t *testing.T) *SQLiteStore {
	t.Helper()
	path := filepath.Join(t.TempDir(), "ft-test.db")
	s, err := NewSQLiteStore(path, 768)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestFTSessionLifecycle(t *testing.T) {
	s := newFTSessionTestStore(t)
	ctx := context.Background()

	id, err := s.CreateFTSession(ctx, FTSession{
		TeamID:        "team1",
		UserID:        "user1",
		Client:        "opencode",
		Project:       "memory",
		TaskSummary:   "implement sessions",
		Domain:        "engineering",
		TrainEligible: true,
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if id == "" {
		t.Fatal("empty id")
	}

	got, err := s.GetFTSession(ctx, id)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.TaskSummary != "implement sessions" || got.Status != FTSessionOpen {
		t.Fatalf("unexpected session: %+v", got)
	}
	if !got.TrainEligible {
		t.Fatal("expected train eligible")
	}

	turnID, err := s.AddFTTurn(ctx, FTTurn{
		SessionID: id,
		Seq:       -1,
		Role:      FTRoleUser,
		Kind:      FTKindMessage,
		Content:   "please implement ft sessions",
		EntryIDs:  []string{"e1"},
	})
	if err != nil {
		t.Fatalf("add turn: %v", err)
	}
	if turnID == "" {
		t.Fatal("empty turn id")
	}

	turnID2, err := s.AddFTTurn(ctx, FTTurn{
		SessionID: id,
		Seq:       -1,
		Role:      FTRoleAssistant,
		Kind:      FTKindMessage,
		Content:   "done",
	})
	if err != nil {
		t.Fatalf("add turn2: %v", err)
	}
	if turnID2 == turnID {
		t.Fatal("turn ids should differ")
	}

	turns, err := s.ListFTTurns(ctx, id)
	if err != nil {
		t.Fatalf("list turns: %v", err)
	}
	if len(turns) != 2 {
		t.Fatalf("want 2 turns, got %d", len(turns))
	}
	if turns[0].Seq != 0 || turns[1].Seq != 1 {
		t.Fatalf("seq want 0,1 got %d,%d", turns[0].Seq, turns[1].Seq)
	}
	if turns[0].ContentHash == "" {
		t.Fatal("expected content hash")
	}

	// entry_ids on turn should auto-link knowledge as retrieved
	know, err := s.ListFTSessionKnowledge(ctx, id)
	if err != nil {
		t.Fatalf("list know: %v", err)
	}
	if len(know) != 1 || know[0].EntryID != "e1" || know[0].Role != FTKnowRetrieved {
		t.Fatalf("unexpected knowledge links: %+v", know)
	}

	if err := s.LinkFTSessionKnowledge(ctx, id, "e2", FTKnowStored); err != nil {
		t.Fatalf("link: %v", err)
	}
	// idempotent
	if err := s.LinkFTSessionKnowledge(ctx, id, "e2", FTKnowStored); err != nil {
		t.Fatalf("link2: %v", err)
	}

	rating := 5
	prefID, err := s.AddFTPreference(ctx, FTPreference{
		SessionID:    id,
		PromptTurnID: turnID,
		ChosenText:   "final good answer",
		RejectedText: "draft bad answer",
		Source:       FTPrefUserEdit,
		Rating:       &rating,
		UserID:       "user1",
	})
	if err != nil {
		t.Fatalf("prefer: %v", err)
	}
	if prefID == "" {
		t.Fatal("empty pref id")
	}

	outcome := 5
	if err := s.CompleteFTSession(ctx, id, &outcome, "shipped", FTSessionCompleted); err != nil {
		t.Fatalf("complete: %v", err)
	}
	got, err = s.GetFTSession(ctx, id)
	if err != nil {
		t.Fatalf("get after complete: %v", err)
	}
	if got.Status != FTSessionCompleted || got.OutcomeRating == nil || *got.OutcomeRating != 5 {
		t.Fatalf("unexpected completed session: %+v", got)
	}
	if got.CompletedAt == nil {
		t.Fatal("expected completed_at")
	}

	list, err := s.ListFTSessions(ctx, FTSessionFilter{
		TeamID:            "team1",
		TrainEligibleOnly: true,
		MinOutcomeRating:  4,
		Limit:             10,
	})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("want 1 session, got %d", len(list))
	}

	prefs, err := s.ListFTPreferencesExport(ctx, FTSessionFilter{
		TeamID:            "team1",
		TrainEligibleOnly: true,
		MinOutcomeRating:  4,
	})
	if err != nil {
		t.Fatalf("prefs export: %v", err)
	}
	if len(prefs) != 1 || prefs[0].ChosenText != "final good answer" {
		t.Fatalf("unexpected prefs: %+v", prefs)
	}
}

func TestFTSessionNotFound(t *testing.T) {
	s := newFTSessionTestStore(t)
	ctx := context.Background()
	_, err := s.GetFTSession(ctx, "missing")
	if err != ErrNotFound {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
}
