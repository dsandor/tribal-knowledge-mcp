package backup

import (
	"context"

	"github.com/dsandor/memory/internal/storage"
)

type fakeStore struct {
	tables     map[string][]map[string]any
	embeddings []storage.EmbeddingItem
	empty      bool
}

func newFakeStore() *fakeStore {
	return &fakeStore{tables: map[string][]map[string]any{}, empty: true}
}

func (f *fakeStore) EngineName() string { return "fake" }

func (f *fakeStore) DumpTable(_ context.Context, t string, fn func(map[string]any) error) error {
	for _, r := range f.tables[t] {
		if err := fn(r); err != nil {
			return err
		}
	}
	return nil
}

func (f *fakeStore) LoadTable(_ context.Context, t string, rows []map[string]any) error {
	f.tables[t] = append(f.tables[t], rows...)
	f.empty = false
	return nil
}

func (f *fakeStore) DumpEmbeddings(_ context.Context, fn func(storage.EmbeddingItem) error) error {
	for _, e := range f.embeddings {
		if err := fn(e); err != nil {
			return err
		}
	}
	return nil
}

func (f *fakeStore) LoadEmbeddings(_ context.Context, items []storage.EmbeddingItem) error {
	f.embeddings = append(f.embeddings, items...)
	return nil
}

func (f *fakeStore) IsEmpty(context.Context) (bool, error) { return f.empty, nil }

func (f *fakeStore) TruncateAll(context.Context, []string) error {
	f.tables = map[string][]map[string]any{}
	f.embeddings = nil
	f.empty = true
	return nil
}
