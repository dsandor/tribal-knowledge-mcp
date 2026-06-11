# Hashtag & Auto-Tag Support Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

> **PROJECT RULE — NO GIT COMMITS.** Do not run `git add` or `git commit` at any point. The team commits manually. Every task ends at "tests pass", not "commit".

**Goal:** User-supplied `#hashtags` are extracted at store time, an async Haiku call auto-categorizes entries into a separate `auto_tags` field, and the Knowledge UI renders the two as visually distinct, clickable pills.

**Architecture:** A parallel `auto_tags` JSON column (SQLite TEXT / Postgres JSONB) keeps provenance structural without breaking any existing reader. A new `internal/tags` package owns hashtag extraction and the `AutoTagger` (shared by the MCP handler, web handler, and a pipeline backfill stage). The UI gets a `TagPill` component with `user`/`auto` variants and a `tag` filter param threaded through `GET /api/knowledge`.

**Tech Stack:** Go 1.x (chi, mcp-go, database/sql), SQLite + sqlite-vec / Postgres + pgvector, React + TypeScript + MUI (dark theme), Vite.

**Spec:** `docs/superpowers/specs/2026-06-10-hashtag-tags-design.md`

**Verification commands:**
- Go: `cd /Users/dsandor/Projects/memory && go test ./...`
- Web: `cd /Users/dsandor/Projects/memory/web && npm run build`

---

### Task 1: `internal/tags` package — ExtractHashtags + Merge

**Files:**
- Create: `internal/tags/tags.go`
- Test: `internal/tags/tags_test.go`

- [ ] **Step 1: Write the failing tests**

```go
package tags

import (
	"reflect"
	"testing"
)

func TestExtractHashtags(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want []string
	}{
		{"simple", "use #earnings for this", []string{"earnings"}},
		{"multiple", "#Q3-Report and #earnings_call", []string{"q3-report", "earnings_call"}},
		{"lowercased", "#Earnings #EARNINGS", []string{"earnings"}},
		{"dedup", "#alpha #alpha #beta", []string{"alpha", "beta"}},
		{"mid-word hash ignored", "foo#bar", nil},
		{"bare hash ignored", "a # b #! c", nil},
		{"leading punctuation stops", "#-nope but #ok-tag yes", []string{"ok-tag"}},
		{"start of string", "#first word", []string{"first"}},
		{"empty input", "", nil},
		{"hash at end", "trailing #", nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := ExtractHashtags(c.in)
			if len(got) == 0 && len(c.want) == 0 {
				return
			}
			if !reflect.DeepEqual(got, c.want) {
				t.Errorf("ExtractHashtags(%q) = %v, want %v", c.in, got, c.want)
			}
		})
	}
}

func TestMerge(t *testing.T) {
	cases := []struct {
		name      string
		explicit  []string
		extracted []string
		want      []string
	}{
		{"both empty", nil, nil, []string{}},
		{"explicit only", []string{"Alpha", "beta"}, nil, []string{"alpha", "beta"}},
		{"extracted only", nil, []string{"gamma"}, []string{"gamma"}},
		{"dedup across sources", []string{"Alpha"}, []string{"alpha", "beta"}, []string{"alpha", "beta"}},
		{"order preserved explicit first", []string{"b", "a"}, []string{"c"}, []string{"b", "a", "c"}},
		{"whitespace dropped", []string{" ", "a"}, []string{""}, []string{"a"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := Merge(c.explicit, c.extracted)
			if !reflect.DeepEqual(got, c.want) {
				t.Errorf("Merge(%v, %v) = %v, want %v", c.explicit, c.extracted, got, c.want)
			}
		})
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd /Users/dsandor/Projects/memory && go test ./internal/tags/ -v`
Expected: FAIL — `undefined: ExtractHashtags`, `undefined: Merge`

- [ ] **Step 3: Write the implementation**

```go
// Package tags provides hashtag extraction and tag merging for knowledge entries.
package tags

import (
	"regexp"
	"strings"
)

// hashtagRe matches #word where word starts alphanumeric and continues with
// alphanumerics, underscores, or hyphens. (?:^|\s) anchors so foo#bar is not a tag.
var hashtagRe = regexp.MustCompile(`(?:^|\s)#([A-Za-z0-9][A-Za-z0-9_-]*)`)

// ExtractHashtags returns lowercase, deduplicated tags for every #hashtag in
// text, in first-seen order. Returns nil when there are none.
func ExtractHashtags(text string) []string {
	matches := hashtagRe.FindAllStringSubmatch(text, -1)
	if len(matches) == 0 {
		return nil
	}
	seen := make(map[string]bool, len(matches))
	var out []string
	for _, m := range matches {
		tag := strings.ToLower(m[1])
		if !seen[tag] {
			seen[tag] = true
			out = append(out, tag)
		}
	}
	return out
}

// Merge combines explicit tags and extracted hashtags, lowercasing, trimming,
// and deduplicating while preserving first-seen order (explicit tags first).
// Always returns a non-nil slice so callers can store it directly.
func Merge(explicit, extracted []string) []string {
	out := make([]string, 0, len(explicit)+len(extracted))
	seen := make(map[string]bool, len(explicit)+len(extracted))
	for _, src := range [][]string{explicit, extracted} {
		for _, t := range src {
			tag := strings.ToLower(strings.TrimSpace(t))
			if tag == "" || seen[tag] {
				continue
			}
			seen[tag] = true
			out = append(out, tag)
		}
	}
	return out
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd /Users/dsandor/Projects/memory && go test ./internal/tags/ -v`
Expected: PASS (all subtests)

---

### Task 2: Storage — `AutoTags` field, migrations, scans, `UpdateAutoTags`, `ListFilter.Tag`

**Files:**
- Modify: `internal/storage/storage.go` (struct at :24, `ListFilter` at :48, `Store` interface at :149)
- Modify: `internal/storage/sqlite.go` (migrate :39, StoreEntry :393, GetEntry :450, ListEntries :466, UpdateEntry :567, SearchSimilar inline scan ~:668-697, scanEntry :725, GetEntryByContentHash ~:1242, plus every other SELECT feeding scanEntry — find with grep below)
- Modify: `internal/storage/postgres.go` (migrate ~:47, StoreEntry :115, GetEntry :165, ListEntries :181, scanEntryPG :402, GetEntryByContentHash :434, SearchSimilar :302, plus other SELECTs)
- Test: `internal/storage/sqlite_autotags_test.go` (new file; follow helper patterns from existing tests in this package)

**Critical sweep:** every SQL statement that SELECTs the entry column list must gain `auto_tags` immediately after `tags`, and every scan must read it. Find them all with:

```bash
grep -n "usage_count, team_id, status" internal/storage/*.go
```

Update **every** hit in `sqlite.go`, `postgres.go`, and any other file the grep surfaces (e.g. search/hybrid query files). Do the same check for `INSERT INTO entries` statements (StoreEntry, BulkImport).

- [ ] **Step 1: Write the failing tests**

Create `internal/storage/sqlite_autotags_test.go`. Look at the top of the existing `internal/storage/sqlite_test.go` first — if a store-constructor helper already exists, reuse it instead of `newAutoTagTestStore`.

```go
package storage

import (
	"context"
	"testing"
)

func newAutoTagTestStore(t *testing.T) *SQLiteStore {
	t.Helper()
	s, err := NewSQLiteStore(":memory:", 4)
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestAutoTagsRoundTrip(t *testing.T) {
	s := newAutoTagTestStore(t)
	ctx := context.Background()

	id, err := s.StoreEntry(ctx, KnowledgeEntry{
		Type: KTPattern, Title: "t", Content: "c",
		Tags: []string{"user-tag"},
	}, nil)
	if err != nil {
		t.Fatalf("store: %v", err)
	}

	// Fresh entries have empty (non-nil after scan fallback) auto tags.
	e, err := s.GetEntry(ctx, id)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if len(e.AutoTags) != 0 {
		t.Fatalf("expected no auto tags, got %v", e.AutoTags)
	}

	if err := s.UpdateAutoTags(ctx, id, []string{"valuation", "banking"}); err != nil {
		t.Fatalf("update auto tags: %v", err)
	}
	e, err = s.GetEntry(ctx, id)
	if err != nil {
		t.Fatalf("get after update: %v", err)
	}
	if len(e.AutoTags) != 2 || e.AutoTags[0] != "valuation" || e.AutoTags[1] != "banking" {
		t.Fatalf("auto tags = %v, want [valuation banking]", e.AutoTags)
	}
	// User tags untouched.
	if len(e.Tags) != 1 || e.Tags[0] != "user-tag" {
		t.Fatalf("tags = %v, want [user-tag]", e.Tags)
	}
}

func TestUpdateAutoTagsNotFound(t *testing.T) {
	s := newAutoTagTestStore(t)
	err := s.UpdateAutoTags(context.Background(), "no-such-id", []string{"x"})
	if err == nil {
		t.Fatal("expected error for missing entry")
	}
}

func TestListEntriesTagFilter(t *testing.T) {
	s := newAutoTagTestStore(t)
	ctx := context.Background()

	idUser, _ := s.StoreEntry(ctx, KnowledgeEntry{Type: KTPattern, Title: "a", Content: "c", Tags: []string{"earnings"}}, nil)
	idAuto, _ := s.StoreEntry(ctx, KnowledgeEntry{Type: KTPattern, Title: "b", Content: "c"}, nil)
	_, _ = s.StoreEntry(ctx, KnowledgeEntry{Type: KTPattern, Title: "d", Content: "c", Tags: []string{"other"}}, nil)
	if err := s.UpdateAutoTags(ctx, idAuto, []string{"earnings"}); err != nil {
		t.Fatalf("update auto tags: %v", err)
	}

	got, err := s.ListEntries(ctx, ListFilter{Tag: "earnings"})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d entries, want 2 (user-tagged %s and auto-tagged %s)", len(got), idUser, idAuto)
	}

	got, err = s.ListEntries(ctx, ListFilter{Tag: "nope"})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("got %d entries for unknown tag, want 0", len(got))
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd /Users/dsandor/Projects/memory && go test ./internal/storage/ -run 'AutoTags|TagFilter' -v`
Expected: FAIL — `e.AutoTags undefined`, `s.UpdateAutoTags undefined`, `unknown field Tag`

- [ ] **Step 3: Update the shared types in `storage.go`**

In `KnowledgeEntry` add after `Tags []string`:

```go
	AutoTags    []string // LLM-assigned category tags; never user-edited
```

In `ListFilter` add after `TeamID`:

```go
	Tag    string // exact-match against any user tag or auto tag; empty = no filter
```

In the `Store` interface, after `UpdateEntry`:

```go
	// UpdateAutoTags replaces the auto-generated tags for an entry without
	// touching user tags or bumping version. Returns ErrNotFound if missing.
	UpdateAutoTags(ctx context.Context, id string, tags []string) error
```

- [ ] **Step 4: SQLite — migration, insert, scans, update, filter**

In `sqlite.go` `migrate()`, append to the first idempotent ALTER block (the one containing `content_hash`, lines 81-85):

```go
		"ALTER TABLE entries ADD COLUMN auto_tags TEXT NOT NULL DEFAULT '[]'",
```

`StoreEntry` (:393): marshal and insert auto_tags —

```go
	autoTagsJSON, err := json.Marshal(entry.AutoTags)
	if err != nil {
		return "", fmt.Errorf("marshal auto tags: %w", err)
	}
```

and change the INSERT to:

```go
	res, err := tx.ExecContext(ctx, `
		INSERT INTO entries (id, type, title, content, description, domain, tags, auto_tags, author, team, content_hash)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, entry.ID, string(entry.Type), entry.Title, entry.Content,
		entry.Description, entry.Domain, string(tagsJSON), string(autoTagsJSON), entry.Author, entry.Team, contentHash)
```

`scanEntry` (:725): add a second JSON var and scan position directly after `tags`:

```go
func scanEntry(row rowScanner) (*KnowledgeEntry, error) {
	var e KnowledgeEntry
	var tagsJSON, autoTagsJSON string
	var createdAt, updatedAt string

	err := row.Scan(
		&e.ID, &e.Type, &e.Title, &e.Content, &e.Description,
		&e.Domain, &tagsJSON, &autoTagsJSON, &e.Author, &e.Team,
		&createdAt, &updatedAt, &e.Version,
		&e.Rating, &e.UsageCount, &e.TeamID, &e.Status,
	)
	if err != nil {
		return nil, err
	}

	if err := json.Unmarshal([]byte(tagsJSON), &e.Tags); err != nil {
		e.Tags = []string{}
	}
	if err := json.Unmarshal([]byte(autoTagsJSON), &e.AutoTags); err != nil {
		e.AutoTags = []string{}
	}

	e.CreatedAt = parseTimestamp(createdAt)
	e.UpdatedAt = parseTimestamp(updatedAt)
	return &e, nil
}
```

Every SELECT feeding `scanEntry` changes its column list from
`..., domain, tags, author, team, ...` to `..., domain, tags, auto_tags, author, team, ...`.
Known sites: GetEntry :451, ListEntries :472, GetEntryByContentHash ~:1240, the scan loop at :890's query, and the **inline** scan inside SearchSimilar (:668-697) which scans manually — there, add `var autoTagsJSON string` alongside `tagsJSON`, scan it after `&tagsJSON`, and unmarshal into `e.AutoTags` with the same fallback. Use the grep from the task header to confirm none are missed; also check `sqlite` search/hybrid code (`SearchHybrid`) and `GetWeakSignalEntries` for entry-column SELECTs.

`UpdateAutoTags` — add after `UpdateEntry` (:580):

```go
func (s *SQLiteStore) UpdateAutoTags(ctx context.Context, id string, tags []string) error {
	autoTagsJSON, err := json.Marshal(tags)
	if err != nil {
		return fmt.Errorf("marshal auto tags: %w", err)
	}
	res, err := s.db.ExecContext(ctx,
		`UPDATE entries SET auto_tags=?, updated_at=CURRENT_TIMESTAMP WHERE id=?`,
		string(autoTagsJSON), id,
	)
	if err != nil {
		return fmt.Errorf("update auto tags: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("entry %q: %w", id, ErrNotFound)
	}
	return nil
}
```

`ListEntries` (:466) — add after the `TeamID` filter block (:497-500):

```go
	if filter.Tag != "" {
		query += ` AND (EXISTS (SELECT 1 FROM json_each(entries.tags) WHERE json_each.value = ?)
		            OR EXISTS (SELECT 1 FROM json_each(entries.auto_tags) WHERE json_each.value = ?))`
		args = append(args, filter.Tag, filter.Tag)
	}
```

(go-sqlite3 ships with JSON1 enabled; `json_each` is available.)

- [ ] **Step 5: Run the new storage tests**

Run: `cd /Users/dsandor/Projects/memory && go test ./internal/storage/ -run 'AutoTags|TagFilter' -v`
Expected: PASS

- [ ] **Step 6: Postgres — mirror every change**

In `postgres.go` `migrate()`, with the existing `ALTER TABLE ... ADD COLUMN IF NOT EXISTS` statements:

```go
	if _, err := s.db.ExecContext(ctx, `ALTER TABLE entries ADD COLUMN IF NOT EXISTS auto_tags JSONB NOT NULL DEFAULT '[]'`); err != nil {
		return fmt.Errorf("alter entries add auto_tags: %w", err)
	}
```

(Match the surrounding migrate code's exact style/signature — it may not take ctx.)

`StoreEntry` (:115): marshal `entry.AutoTags` as `autoTagsJSON` (same error wrap as SQLite) and extend the INSERT:

```go
	_, err = tx.ExecContext(ctx, `
		INSERT INTO entries
			(id, type, title, content, description, domain, tags, auto_tags, author, team, team_id, status, content_hash)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)
	`,
		entry.ID, string(entry.Type), entry.Title, entry.Content,
		entry.Description, entry.Domain, string(tagsJSON), string(autoTagsJSON),
		entry.Author, entry.Team, entry.TeamID, statusOrDefault(entry.Status), contentHash,
	)
```

`scanEntryPG` (:402): add `var autoTagsRaw []byte`, scan after `&tagsRaw`, unmarshal into `e.AutoTags` with `[]string{}` fallback.

All SELECT column lists feeding `scanEntryPG` gain `auto_tags` after `tags` (GetEntry :167, ListEntries :196, SearchSimilar :304, GetEntryByContentHash :436, plus grep hits in `postgres_search.go` / other postgres files).

`UpdateAutoTags`:

```go
func (s *PostgresStore) UpdateAutoTags(ctx context.Context, id string, tags []string) error {
	autoTagsJSON, err := json.Marshal(tags)
	if err != nil {
		return fmt.Errorf("marshal auto tags: %w", err)
	}
	res, err := s.db.ExecContext(ctx,
		`UPDATE entries SET auto_tags = $1, updated_at = NOW() WHERE id = $2`,
		string(autoTagsJSON), id,
	)
	if err != nil {
		return fmt.Errorf("update auto tags: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("entry %q: %w", id, ErrNotFound)
	}
	return nil
}
```

`ListEntries` (:181) — after the TeamID block, using the `nextArg` helper:

```go
	if filter.Tag != "" {
		p1 := nextArg(filter.Tag)
		p2 := nextArg(filter.Tag)
		query += fmt.Sprintf(" AND (jsonb_exists(tags, %s) OR jsonb_exists(auto_tags, %s))", p1, p2)
	}
```

Also check `BulkImport` (in whichever file it lives — grep `func.*BulkImport`) in **both** adapters: its INSERT must include `auto_tags` if it lists entry columns explicitly.

- [ ] **Step 7: Fix any fake stores broken by the interface change**

Adding `UpdateAutoTags` to the `Store` interface breaks compilation of every fake/mock store in test files across packages (`internal/web`, `internal/pipeline`, `internal/mcp`, ...). Find them:

```bash
go build ./... 2>&1 | head -30
```

For each broken fake, add a no-op method matching its style, e.g.:

```go
func (f *fakeStore) UpdateAutoTags(ctx context.Context, id string, tags []string) error { return nil }
```

- [ ] **Step 8: Run the full Go suite**

Run: `cd /Users/dsandor/Projects/memory && go test ./...`
Expected: PASS — all 9+ packages. (Postgres methods compile-checked; Postgres tests follow existing conventions in this repo, which skip without a live DB.)

---

### Task 3: `AutoTagger` — async LLM categorization

**Files:**
- Create: `internal/tags/autotag.go`
- Test: `internal/tags/autotag_test.go`

The `llm.Client` interface (`internal/llm/client.go:6`) is `Complete(ctx context.Context, prompt string) (string, error)`. The resolver function matches `(*aiconfig.Sources).ImprovementLLM` (`internal/aiconfig/sources.go:71`), which returns nil when no API key is configured.

- [ ] **Step 1: Write the failing tests**

```go
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
	gotID   string
	gotTags []string
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
	// "earnings" deduped against user tags (case-insensitive); rest lowercased.
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
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd /Users/dsandor/Projects/memory && go test ./internal/tags/ -run AutoTagger -v`
Expected: FAIL — `undefined: AutoTagger`

- [ ] **Step 3: Write the implementation**

```go
package tags

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/dsandor/memory/internal/llm"
	"github.com/dsandor/memory/internal/storage"
)

// AutoTagger assigns LLM-generated category tags to stored entries.
// Failures are logged and dropped — callers never block on or observe them.
type AutoTagger struct {
	Store storage.Store
	// LLMFor resolves a client per call so saved team settings apply
	// immediately. Wire to (*aiconfig.Sources).ImprovementLLM.
	LLMFor func(ctx context.Context, teamID string) llm.Client
}

const autoTagTimeout = 30 * time.Second

// TagEntry asks the LLM for 3-5 category tags, dedupes them against the
// entry's user tags (case-insensitive), and persists them via UpdateAutoTags.
func (a *AutoTagger) TagEntry(ctx context.Context, entry storage.KnowledgeEntry, teamID string) {
	client := a.LLMFor(ctx, teamID)
	if client == nil {
		return // no API key configured — skip silently
	}

	ctx, cancel := context.WithTimeout(ctx, autoTagTimeout)
	defer cancel()

	prompt := fmt.Sprintf(`Categorize this knowledge entry with 3-5 short lowercase topic tags (single words or hyphenated phrases). Tags should describe WHAT the entry is about, useful for browsing a team knowledge base.

Title: %s
Type: %s
Domain: %s
Content: %s

Return ONLY valid JSON: {"tags": ["...", "..."]}`,
		entry.Title, entry.Type, entry.Domain, entry.Content)

	raw, err := client.Complete(ctx, prompt)
	if err != nil {
		slog.Warn("autotag: llm complete", "entry", entry.ID, "err", err)
		return
	}

	var result struct {
		Tags []string `json:"tags"`
	}
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		slog.Warn("autotag: parse llm response", "entry", entry.ID, "err", err)
		return
	}

	userTags := make(map[string]bool, len(entry.Tags))
	for _, t := range entry.Tags {
		userTags[strings.ToLower(strings.TrimSpace(t))] = true
	}
	seen := make(map[string]bool, len(result.Tags))
	autoTags := make([]string, 0, len(result.Tags))
	for _, t := range result.Tags {
		tag := strings.ToLower(strings.TrimSpace(t))
		if tag == "" || userTags[tag] || seen[tag] {
			continue
		}
		seen[tag] = true
		autoTags = append(autoTags, tag)
	}
	if len(autoTags) == 0 {
		return
	}

	if err := a.Store.UpdateAutoTags(ctx, entry.ID, autoTags); err != nil {
		slog.Warn("autotag: update auto tags", "entry", entry.ID, "err", err)
	}
}

// TagEntryAsync runs TagEntry in a goroutine on a context detached from the
// request, so the caller's response is never delayed and request cancellation
// does not abort tagging.
func (a *AutoTagger) TagEntryAsync(ctx context.Context, entry storage.KnowledgeEntry, teamID string) {
	detached := context.WithoutCancel(ctx)
	go a.TagEntry(detached, entry, teamID)
}
```

(If `context.WithoutCancel` is unavailable on this Go version, use `context.Background()` instead — check `go.mod`.)

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd /Users/dsandor/Projects/memory && go test ./internal/tags/ -v`
Expected: PASS

---

### Task 4: Wire extraction + auto-tagging into MCP `knowledge_store`

**Files:**
- Modify: `internal/mcp/tools.go:23-116` (`HandleKnowledgeStore`)
- Modify: `internal/mcp/server.go:39,47` (tool description text)
- Test: extend the existing MCP tools test file (grep `HandleKnowledgeStore` under `internal/mcp/*_test.go`; follow its request-building helpers)

- [ ] **Step 1: Write the failing test**

Add to the existing MCP test file (adapt the construction of `mcplib.CallToolRequest` to match how neighboring tests in that file build requests — there will be an existing helper or literal pattern; reuse it):

```go
func TestKnowledgeStoreExtractsHashtags(t *testing.T) {
	// Arrange a store + handler exactly like the existing knowledge_store tests
	// in this file (same fake/real store and Sources setup), then call the
	// handler with:
	//   title:   "Earnings workflow"
	//   content: "Always check #earnings and #q3-report first"
	//   type:    "workflow"
	//   tags:    ["manual-tag", "earnings"]
	// Assert: the stored entry's Tags == ["manual-tag", "earnings", "q3-report"]
	// (explicit first, extracted appended, "earnings" deduped).
}
```

Fill in the body using the file's existing setup — the assertion above is the contract. If the existing tests use a real `SQLiteStore`, fetch the entry by the returned ID and assert on `entry.Tags`.

- [ ] **Step 2: Run to verify it fails**

Run: `cd /Users/dsandor/Projects/memory && go test ./internal/mcp/ -run Hashtag -v`
Expected: FAIL — tags are stored as-given, no `q3-report`

- [ ] **Step 3: Implement in `HandleKnowledgeStore`**

Import `"github.com/dsandor/memory/internal/tags"` in `tools.go`. Replace line 54:

```go
		tags := tagsFromArgs(req.GetArguments(), "tags")
```

with:

```go
		explicitTags := tagsFromArgs(req.GetArguments(), "tags")
		entryTags := tags.Merge(explicitTags, tags.ExtractHashtags(title+" "+content))
```

and use `entryTags` in both the dry-run preview map (`"tags": entryTags`) and the `storage.KnowledgeEntry{... Tags: entryTags}` literal.

After the successful `StoreEntry` call (after :101, alongside the live event), fire async auto-tagging — `teamID` is already resolved at :86:

```go
		entry.ID = id
		autoTagger := &tags.AutoTagger{Store: store, LLMFor: src.ImprovementLLM}
		autoTagger.TagEntryAsync(ctx, entry, teamID)
```

(Naming note: the package import is `tags` and the local variable on the old line 54 was also `tags` — the rename to `explicitTags`/`entryTags` above resolves the collision. Verify no other local `tags` identifier shadows the import in this function.)

- [ ] **Step 4: Update the tool description in `server.go`**

Line 39, append to the `knowledge_store` description: ` Inline #hashtags in the title or content are automatically extracted as tags.` Line 47, change the tags description to `mcplib.Description("Additional tags (inline #hashtags in content are also extracted automatically)")`.

- [ ] **Step 5: Run the MCP package tests**

Run: `cd /Users/dsandor/Projects/memory && go test ./internal/mcp/ -v`
Expected: PASS, including the new test

---

### Task 5: Wire extraction + auto-tagging into web store and import handlers

**Files:**
- Modify: `internal/web/handlers.go:425-476` (`handleKnowledgeStore`)
- Modify: `internal/web/import_handlers.go` (JSON + CSV entry construction, ~:74-97)
- Test: extend existing web handler tests (grep `handleKnowledgeStore\|knowledge` in `internal/web/*_test.go`; follow their server/store setup)

- [ ] **Step 1: Write the failing test**

In the existing web handlers test file, add a test that POSTs to `/api/knowledge` (using the file's established test-server helper and auth setup):

```go
// POST body:
// {"title":"T","content":"check #alpha now","type":"pattern","tags":["beta"]}
// Then GET the entry by returned id and assert Tags == ["beta","alpha"].
```

Use the same request/auth helpers as the neighboring tests in that file.

- [ ] **Step 2: Run to verify it fails**

Run: `cd /Users/dsandor/Projects/memory && go test ./internal/web/ -run Hashtag -v`
Expected: FAIL — stored tags are `["beta"]`

- [ ] **Step 3: Implement in `handleKnowledgeStore`**

Import `tagspkg "github.com/dsandor/memory/internal/tags"` (aliased — `tags` is a common local in this package). Replace the entry literal's `Tags: body.Tags` (:453) with:

```go
		Tags: tagspkg.Merge(body.Tags, tagspkg.ExtractHashtags(body.Title+" "+body.Content)),
```

After the successful store (:456-460), before `writeJSON`:

```go
	if s.aiSrc != nil {
		entry.ID = id
		tagger := &tagspkg.AutoTagger{Store: s.store, LLMFor: s.aiSrc.ImprovementLLM}
		tagger.TagEntryAsync(r.Context(), entry, tc.TeamID)
	}
```

(`s.aiSrc` is the optional `*aiconfig.Sources` field at `internal/web/server.go:43` — nil-guard required.)

- [ ] **Step 4: Implement in import handlers**

In `import_handlers.go`, where each imported entry is constructed (both JSON array and CSV paths, ~:74-97 and the CSV row loop), merge extracted hashtags the same way:

```go
	entry.Tags = tagspkg.Merge(entry.Tags, tagspkg.ExtractHashtags(entry.Title+" "+entry.Content))
```

Do **not** auto-tag imports synchronously or per-row async (bulk imports may be thousands of rows) — the pipeline backfill stage (Task 6) covers them.

- [ ] **Step 5: Run the web package tests**

Run: `cd /Users/dsandor/Projects/memory && go test ./internal/web/ -v`
Expected: PASS

---

### Task 6: Pipeline backfill stage

**Files:**
- Modify: `internal/pipeline/pipeline.go` (Pipeline struct ~:34, options ~:62-73, `Run` — insert after the weak-signal stage call at ~:262-264)
- Test: `internal/pipeline/autotag_backfill_test.go` (follow existing pipeline test fakes)

- [ ] **Step 1: Write the failing test**

Look at the existing pipeline tests for the fake `AnalysisStore`/`AISource` patterns and reuse them. The contract:

```go
// TestAutoTagBackfill: given a store containing
//   - entry A with AutoTags == [] (untagged)
//   - entry B with AutoTags == ["done"] (already tagged)
// and a fake LLM returning {"tags":["x","y"]},
// runAutoTagBackfill must call UpdateAutoTags for A only.
//
// TestAutoTagBackfillCap: with 25 untagged entries, at most 20
// UpdateAutoTags calls are made per run.
```

Write both tests against `p.runAutoTagBackfill(ctx, client)` using the package's existing fakes (the weak-signal tests in this package show the structure to copy).

- [ ] **Step 2: Run to verify it fails**

Run: `cd /Users/dsandor/Projects/memory && go test ./internal/pipeline/ -run AutoTagBackfill -v`
Expected: FAIL — `undefined: runAutoTagBackfill`

- [ ] **Step 3: Implement the stage**

Add to `Pipeline` struct: `autoTagBackfill bool`. Add option (next to `WithWeakSignalImprovement` :69-72):

```go
// WithAutoTagBackfill enables a stage that LLM-tags entries whose auto_tags
// are still empty (covers pre-feature entries and async-tagging failures).
func (p *Pipeline) WithAutoTagBackfill() *Pipeline {
	p.autoTagBackfill = true
	return p
}
```

Implement (import `"github.com/dsandor/memory/internal/tags"`):

```go
// autoTagBackfillCap bounds LLM cost per pipeline run.
const autoTagBackfillCap = 20

// runAutoTagBackfill tags entries that have no auto tags yet. Idempotent:
// already-tagged entries are skipped, so repeated runs converge.
func (p *Pipeline) runAutoTagBackfill(ctx context.Context, improvementLLM llm.Client) {
	entries, err := p.store.ListEntries(ctx, storage.ListFilter{TeamID: p.teamID, Limit: 500})
	if err != nil {
		slog.Error("autotag backfill: list entries", "err", err)
		return
	}
	tagger := &tags.AutoTagger{
		Store:  p.store,
		LLMFor: func(context.Context, string) llm.Client { return improvementLLM },
	}
	tagged := 0
	for _, e := range entries {
		if len(e.AutoTags) > 0 {
			continue
		}
		tagger.TagEntry(ctx, e, p.teamID)
		tagged++
		if tagged >= autoTagBackfillCap {
			slog.Info("autotag backfill: cap reached", "cap", autoTagBackfillCap)
			break
		}
	}
	if tagged > 0 {
		slog.Info("autotag backfill complete", "tagged", tagged)
	}
}
```

In `Run`, directly after the weak-signal stage block (~:262-264), following the same guard style (the weak-signal block shows how the improvement LLM client is resolved — reuse that resolved client variable):

```go
	if p.autoTagBackfill && improvementLLM != nil {
		p.runAutoTagBackfill(ctx, improvementLLM)
	}
```

(Match the actual local variable name for the improvement client used at ~:262 — read the surrounding code first. If the weak-signal stage resolves it only inside its own guard, hoist the resolution or resolve again via `p.src.ImprovementLLM(ctx, p.teamID)` with a nil check.)

In `cmd/server/main.go` (~:200-206), chain the new option onto the pipeline construction:

```go
		WithAutoTagBackfill().
```

- [ ] **Step 4: Run the pipeline tests**

Run: `cd /Users/dsandor/Projects/memory && go test ./internal/pipeline/ -v`
Expected: PASS

---

### Task 7: REST API — `tag` filter param + export updates

**Files:**
- Modify: `internal/web/handlers.go:90-115` (`handleKnowledgeList`)
- Modify: `internal/web/export_handlers.go:14-95`
- Test: extend existing web handler tests

- [ ] **Step 1: Write the failing test**

In the web handlers test file: store two entries (one with `Tags: ["alpha"]`, one without), then GET `/api/knowledge?tag=alpha` and assert exactly one entry returns. Use the file's existing helpers.

- [ ] **Step 2: Run to verify it fails**

Run: `cd /Users/dsandor/Projects/memory && go test ./internal/web/ -run Tag -v`
Expected: FAIL — both entries returned (param ignored)

- [ ] **Step 3: Implement**

`handleKnowledgeList` — add to the `storage.ListFilter` literal (:98-105):

```go
		Tag:    q.Get("tag"),
```

`handleKnowledgeExport` — replace the post-hoc filter block (:46-59) by adding `Tag: q.Get("tag")` to the `ListFilter` literal (:33-38) and deleting the loop. In the CSV branch, add `"auto_tags"` to the header row after `"tags"` and write `csvSafeCell(strings.Join(e.AutoTags, "|"))` after the tags cell.

- [ ] **Step 4: Run the web tests**

Run: `cd /Users/dsandor/Projects/memory && go test ./internal/web/ -v`
Expected: PASS

- [ ] **Step 5: Full Go suite + build**

Run: `cd /Users/dsandor/Projects/memory && go build ./... && go test ./...`
Expected: clean build, all packages PASS

---

### Task 8: Frontend — `TagPill` component + API client

**Files:**
- Create: `web/src/components/ui/tag-pill.tsx`
- Modify: `web/src/lib/api.ts` (KnowledgeEntry :36-51, knowledge.list :167-178)

- [ ] **Step 1: Add `AutoTags` and the `tag` param to `api.ts`**

In `KnowledgeEntry`, after `Tags`:

```ts
  AutoTags: string[] | null | undefined
```

In `api.knowledge.list`, extend the params type with `tag?: string` and add:

```ts
      if (params.tag) q.set('tag', params.tag)
```

- [ ] **Step 2: Create the TagPill component**

```tsx
import Chip from '@mui/material/Chip'
import Tooltip from '@mui/material/Tooltip'
import AutoAwesome from '@mui/icons-material/AutoAwesome'

interface TagPillProps {
  label: string
  variant: 'user' | 'auto'
  onClick?: () => void
}

// User tags: solid emerald-tinted pill with # prefix.
// Auto tags: outlined indigo pill with a sparkle icon and tooltip.
export default function TagPill({ label, variant, onClick }: TagPillProps) {
  if (variant === 'user') {
    return (
      <Chip
        label={`#${label}`}
        size="small"
        onClick={onClick}
        sx={{
          bgcolor: 'rgba(16, 185, 129, 0.18)',
          color: '#34d399',
          fontWeight: 500,
          '&:hover': onClick ? { bgcolor: 'rgba(16, 185, 129, 0.30)' } : undefined,
        }}
      />
    )
  }
  return (
    <Tooltip title="Auto-categorized" arrow>
      <Chip
        label={label}
        size="small"
        variant="outlined"
        onClick={onClick}
        icon={<AutoAwesome sx={{ fontSize: 12, color: '#818cf8 !important' }} />}
        sx={{
          borderColor: 'rgba(99, 102, 241, 0.5)',
          color: '#a5b4fc',
          '&:hover': onClick ? { borderColor: '#6366f1', bgcolor: 'rgba(99, 102, 241, 0.12)' } : undefined,
        }}
      />
    </Tooltip>
  )
}
```

Note: check whether `@mui/icons-material` is in `web/package.json` dependencies. If it is NOT installed, do not add the dependency — use the already-installed `lucide-react` instead: `import { Sparkles } from 'lucide-react'` and pass `icon={<Sparkles style={{ width: 12, height: 12, color: '#818cf8' }} />}`.

- [ ] **Step 3: Verify it compiles**

Run: `cd /Users/dsandor/Projects/memory/web && npm run build`
Expected: clean Vite production build (component is not yet imported anywhere; build confirms syntax + types)

---

### Task 9: Frontend — Knowledge Browser pills + click-to-filter

**Files:**
- Modify: `web/src/pages/KnowledgeBrowser.tsx`

- [ ] **Step 1: Add tag filter state and wire it into the list call**

Add state after `searchMode` (:97):

```tsx
  const [tagFilter, setTagFilter] = useState('')
```

Extend the effect (:99-108): add `tag: tagFilter` to the `api.knowledge.list({...})` params object and `tagFilter` to the dependency array.

- [ ] **Step 2: Render the active-filter chip in the toolbar**

Imports: add `import TagPill from '@/components/ui/tag-pill'` and `Tag` to the lucide-react import (:4). Inside the mode-toggle row `<Box>` (:159-177), after the `ToggleButtonGroup`, add:

```tsx
          {tagFilter && (
            <Chip
              icon={<Tag style={{ width: 12, height: 12 }} />}
              label={`tag: ${tagFilter}`}
              size="small"
              onDelete={() => { setTagFilter(''); setPage(0) }}
              sx={{ bgcolor: 'rgba(16, 185, 129, 0.18)', color: '#34d399' }}
            />
          )}
```

- [ ] **Step 3: Render pills on each card**

Inside the card's left `<Box>` (:213-228), after the domain/author caption line (:220-222), add a capped pill row. Clicks must not navigate (the whole Card is a `Link`), so stop propagation:

```tsx
                    {(() => {
                      const user = e.Tags ?? []
                      const auto = e.AutoTags ?? []
                      const MAX = 5
                      const pills = [
                        ...user.map(t => ({ t, v: 'user' as const })),
                        ...auto.map(t => ({ t, v: 'auto' as const })),
                      ]
                      if (pills.length === 0) return null
                      const overflow = pills.length - MAX
                      return (
                        <Box sx={{ display: 'flex', gap: 0.5, flexWrap: 'wrap', mt: 0.75 }}
                             onClick={ev => { ev.preventDefault(); ev.stopPropagation() }}>
                          {pills.slice(0, MAX).map(({ t, v }) => (
                            <TagPill key={`${v}-${t}`} label={t} variant={v}
                              onClick={() => { setTagFilter(t); setPage(0) }} />
                          ))}
                          {overflow > 0 && (
                            <Chip label={`+${overflow}`} size="small" variant="outlined"
                              sx={{ height: 20, fontSize: 10 }} />
                          )}
                        </Box>
                      )
                    })()}
```

- [ ] **Step 4: Build**

Run: `cd /Users/dsandor/Projects/memory/web && npm run build`
Expected: clean build

---

### Task 10: Frontend — Knowledge Detail pills

**Files:**
- Modify: `web/src/pages/KnowledgeDetail.tsx`

- [ ] **Step 1: Replace view-mode tag chips with TagPills**

Import `TagPill from '@/components/ui/tag-pill'`. In the metadata row (:287-303), replace:

```tsx
        {entry.Tags?.map(t => (
          <Chip key={t} label={`#${t}`} size="small" variant="outlined" />
        ))}
```

with:

```tsx
        {entry.Tags?.map(t => (
          <TagPill key={t} label={t} variant="user" />
        ))}
        {entry.AutoTags?.map(t => (
          <TagPill key={`auto-${t}`} label={t} variant="auto" />
        ))}
```

- [ ] **Step 2: Show read-only auto tags in edit mode**

In the edit form, directly after the Tags `TextField` (:212-218), add:

```tsx
              {entry.AutoTags && entry.AutoTags.length > 0 && (
                <Box>
                  <Typography variant="caption" color="text.secondary" sx={{ display: 'block', mb: 0.5 }}>
                    Auto-categorized (read-only)
                  </Typography>
                  <Box sx={{ display: 'flex', gap: 0.5, flexWrap: 'wrap' }}>
                    {entry.AutoTags.map(t => (
                      <TagPill key={`auto-${t}`} label={t} variant="auto" />
                    ))}
                  </Box>
                </Box>
              )}
```

(`entry` remains in scope during edit mode; `editFields` only mirrors editable fields, which correctly excludes `AutoTags`.)

- [ ] **Step 3: Build**

Run: `cd /Users/dsandor/Projects/memory/web && npm run build`
Expected: clean build

---

### Task 11: Final verification

- [ ] **Step 1: Full Go suite**

Run: `cd /Users/dsandor/Projects/memory && go build ./... && go test ./...`
Expected: clean build, all packages PASS

- [ ] **Step 2: Frontend production build**

Run: `cd /Users/dsandor/Projects/memory/web && npm run build`
Expected: clean Vite build, no TypeScript errors

- [ ] **Step 3: Migration smoke test against the existing dev DB schema**

Run (uses a throwaway copy so the real `knowledge.db` is never touched):

```bash
cd /Users/dsandor/Projects/memory && cp knowledge.db /tmp/knowledge-migration-test.db && cat > /tmp/migrate_check.go <<'EOF'
package main

import (
	"context"
	"fmt"
	"os"

	"github.com/dsandor/memory/internal/storage"
)

func main() {
	s, err := storage.NewSQLiteStore("/tmp/knowledge-migration-test.db", 768)
	if err != nil {
		fmt.Println("MIGRATE FAIL:", err)
		os.Exit(1)
	}
	defer s.Close()
	entries, err := s.ListEntries(context.Background(), storage.ListFilter{Limit: 3})
	if err != nil {
		fmt.Println("LIST FAIL:", err)
		os.Exit(1)
	}
	fmt.Printf("OK: %d entries listed, auto_tags column migrated\n", len(entries))
}
EOF
go run /tmp/migrate_check.go && rm /tmp/migrate_check.go /tmp/knowledge-migration-test.db*
```

Expected: `OK: N entries listed, auto_tags column migrated`
(If the embedding dim differs from 768, read it from how `cmd/server/main.go` configures it and adjust. NEVER point this at `knowledge.db` directly.)

- [ ] **Step 4: Report**

Summarize: tasks completed, test counts, build results. **Do not commit.**
