package tags

import (
	"context"
	"errors"
	"testing"

	"github.com/dsandor/memory/internal/llm"
	"github.com/dsandor/memory/internal/storage"
)

type fakeLLM struct {
	resp string
	err  error
}

func (f *fakeLLM) Complete(ctx context.Context, prompt string) (string, error) {
	return f.resp, f.err
}

type fakeAutoTagStore struct {
	storage.Store // embed nil; only UpdateAutoTags is called
	gotID         string
	gotTags       []string
}

func (f *fakeAutoTagStore) UpdateAutoTags(ctx context.Context, id string, tags []string) error {
	f.gotID = id
	f.gotTags = tags
	return nil
}

func tagger(client llm.Client, store storage.Store) *AutoTagger {
	return &AutoTagger{
		Store:  store,
		LLMFor: func(ctx context.Context, teamID string) llm.Client { return client },
	}
}

func TestAutoTaggerSuccess(t *testing.T) {
	st := &fakeAutoTagStore{}
	a := tagger(&fakeLLM{resp: `{"tags": ["Valuation", "banking", "earnings"]}`}, st)
	entry := storage.KnowledgeEntry{ID: "e1", Title: "t", Content: "c", Tags: []string{"earnings"}}

	a.TagEntry(context.Background(), entry, "team1")

	if st.gotID != "e1" {
		t.Fatalf("updated id = %q, want e1", st.gotID)
	}
	want := []string{"valuation", "banking"}
	if len(st.gotTags) != 2 || st.gotTags[0] != want[0] || st.gotTags[1] != want[1] {
		t.Fatalf("auto tags = %v, want %v", st.gotTags, want)
	}
}

func TestAutoTaggerMalformedJSON(t *testing.T) {
	st := &fakeAutoTagStore{}
	a := tagger(&fakeLLM{resp: `not json`}, st)
	a.TagEntry(context.Background(), storage.KnowledgeEntry{ID: "e1"}, "")
	if st.gotID != "" {
		t.Fatal("store must not be called on parse failure")
	}
}

func TestAutoTaggerLLMError(t *testing.T) {
	st := &fakeAutoTagStore{}
	a := tagger(&fakeLLM{err: errors.New("boom")}, st)
	a.TagEntry(context.Background(), storage.KnowledgeEntry{ID: "e1"}, "")
	if st.gotID != "" {
		t.Fatal("store must not be called on LLM failure")
	}
}

func TestAutoTaggerNilClient(t *testing.T) {
	st := &fakeAutoTagStore{}
	a := &AutoTagger{Store: st, LLMFor: func(ctx context.Context, teamID string) llm.Client { return nil }}
	a.TagEntry(context.Background(), storage.KnowledgeEntry{ID: "e1"}, "")
	if st.gotID != "" {
		t.Fatal("store must not be called when no LLM is configured")
	}
}
