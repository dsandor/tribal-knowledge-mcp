# Evaluation: Should we use Mantic.sh to improve memory search in the tribal-knowledge MCP?

**Date:** 2026-07-01
**Author:** Analysis prepared by a multi-expert review (information retrieval, licensing/compliance, codebase architecture, solutions architecture)
**Subject:** [Mantic.sh](https://github.com/marcoaapfortes/Mantic.sh) — "a structural code search engine for AI agents"

---

## TL;DR — Verdict

**No. Do not adopt, integrate, or license Mantic.sh for memory search.** It is the wrong category of tool, it would be a downgrade over what we already run, and its license carries real risk for a proprietary product.

Three independent findings all point the same way:

1. **Fit:** Mantic ranks **code files** in a git repo using file paths, filenames, and code symbols. Our memories are free-text DB records with *none* of those signals. Its core algorithm is inert on our data.
2. **We're already ahead of it.** Our MCP already does proper **semantic vector search** (embeddings + cosine kNN via sqlite-vec/pgvector) plus an optional **hybrid lexical+dense** path. Mantic's only content feature is an optional `transformers.js` reranker over *code* — adopting it would be a step **backward**.
3. **License:** Mantic is **AGPL-3.0**. Importing it as a library would force us to open-source our entire proprietary server (AGPL §13 network clause). The "$500/yr commercial" terms aren't even public.

**What to do instead:** invest that effort in our own retrieval pipeline — specifically, feed the rating/usage/outcome signals we *already capture* into ranking. That is the highest-leverage, lowest-cost win, and it's the thing Mantic fundamentally cannot do. A concrete roadmap is in §6.

---

## 1. What Mantic.sh actually is

| Attribute | Detail |
|---|---|
| Purpose | Rank **code files** in a git repository by relevance to a query, for AI coding agents |
| Core algorithm | **Heuristic/structural scoring over file metadata** — not content retrieval |
| Key signals | Exact filename match (**+10,000**), path/directory relevance, filename semantics (`.service.ts` ranked above `.test.ts`), penalties for `index.ts`/`page.tsx`, camelCase splitting, "+150 for previously-viewed files," multi-term path-sequence matching |
| Content semantics | **Optional** `--semantic` flag: local `transformers.js` embeddings to rerank "conceptually relevant" *code*. Model/dimensionality unspecified; not a standalone module |
| Index | None — enumerates files live via `git ls-files`; "zero config" |
| Language / packaging | TypeScript/JS; npm package + MCP server + editor installers |
| Maturity | ~551★, 47 commits, single maintainer, v1.0.25 (Jan 2026) |
| License | **AGPL-3.0**; a non-public "$500/yr+" commercial option is advertised in the README only |
| Explicit non-goal | *"Does not index plain-text knowledge bases or arbitrary document collections."* |

The last line is the whole story: **Mantic itself says it isn't built for our use case.**

---

## 2. What our MCP already does for search

Mapped directly from the codebase (Go 1.25.5, `mark3labs/mcp-go`):

**Storage:** SQLite (embedded, `sqlite-vec`) + PostgreSQL (production, `pgvector`). Entries carry `title, content, description, domain, tags, auto_tags, author, team, team_id, created_at, rating, usage_count, status`. Large entries are chunked and each chunk embedded.

**Current `enrich_context` retrieval pipeline:**

```
query → embed (Ollama/OpenAI) → cosine kNN over chunks (sqlite-vec MATCH / pgvector <=>)
      → dedupe chunks→entries by min distance → relevance = 1 − distance
      → access-control filter (team + per-user visibility)
      → preference filter (deny/allow domains&tags, MinRelevance, pin-first, cap)
      → optional LLM prompt rewrite (Anthropic) → return
```

**Also present (Postgres only):** a hybrid mode blending full-text (`tsvector`) 50/50 with cosine — though note it is **not currently wired into `enrich_context`** (that path is semantic-only today).

**Feedback already captured but not yet used in ranking:** `entries.rating` (1–5), `usage_events`, `outcome_ratings` (1–5), and a "trending signal score" used only for dashboards.

**Bottom line:** we already have the semantic + hybrid architecture that is *correct* for free-text retrieval. Mantic offers nothing above it — and less.

---

## 3. Does it add value over what we currently use?

**No — it would be a regression.** Signal-by-signal, almost none of Mantic transfers:

| Mantic signal | Applies to our free-text memories? |
|---|---|
| Exact filename match (+10,000) | ❌ no filenames |
| Path / directory relevance | ❌ no paths |
| Filename-extension semantics (`.service.ts`…) | ❌ no file types |
| `index.ts`/`page.tsx` penalties, code business-logic heuristics | ❌ nonsensical for docs |
| camelCase normalization | ⚠️ harmless but adds nothing (our text is already natural language) |
| "+150 previously-viewed" boost | ⚠️ we already have richer usage/recency metadata |
| Intent Analyzer (auth/payment/UI code categories) | ❌ code-domain categories, not analyst topics |
| **transformers.js semantic rerank** | ⚠️ commodity embedding search — **we already do this, better** |

Crucially, Mantic has **no TF-IDF/term-frequency content ranking**, so its non-semantic path can't even do lexical retrieval over our bodies of text — it's *weaker* than our existing Postgres full-text search. And it cannot incorporate ratings/usage/recency/team, which are exactly the signals that make a tribal-knowledge base improve over time.

Its one overlapping capability (embedding-based semantic search) is standard sentence-embedding retrieval that we already implement with a document-appropriate, pinned model and a real vector index. Using Mantic for it would mean pulling a code-search CLI to reach a single internal feature that embeds *code*, not documents, with an unspecified model.

---

## 4. Can we integrate it into the codebase?

**Technically awkward, and gated by license.** Mantic is a TypeScript/npm package + MCP server; our backend is Go. There is no natural in-process integration — you would either shell out to its CLI or run it as a separate MCP service. But integration mechanics are moot because:

- It doesn't index our data model (DB text records, not repo files), so there is nothing meaningful for it to search without us re-inventing an adapter — at which point we've built the retrieval ourselves anyway.
- The license makes the only "clean" integration (importing as a library) unacceptable (see §5).

---

## 5. Licensing & cost risk (AGPL-3.0)

The repo's `LICENSE` file is **unmodified AGPL-3.0**. The dual-license/"$500/yr" language exists **only in the README marketing** — no commercial terms are public. Absent a signed commercial agreement, **AGPL-3.0 is the only grant** and governs everything.

| Integration scenario | Risk | Why |
|---|---|---|
| **Import as a library into our server** | 🔴 **HIGH** | AGPL §13 network clause forces disclosure of our **entire proprietary server's source** to all users. Disqualifying. |
| Shell out to unmodified CLI (internal only) | 🟡 Low–Med | Separate-program boundary is favorable but legally untested; Mantic's README pushes a broad "all derivatives" reading |
| Separate networked MCP service (unmodified) | 🟡 Low–Med | Our client stays clean; we'd only owe Mantic's own (public) source to that service |
| Clean-room reimplement the *idea* | 🟢 Essentially none | Algorithms/methods aren't copyrightable (17 U.S.C. §102(b); *Baker v. Selden*; *Google v. Oracle*) |

Cost unknowns if we ever wanted the commercial route: the "$500/yr" is a **floor, based on usage**, with undefined per-seat/per-org scope, undefined redistribution rights, and single-maintainer continuity risk. Transitive deps are fine (`transformers.js` Apache-2.0, `tree-sitter` MIT), but note `transformers.js` downloads ML **models** whose *individual* licenses would need vetting.

*(This is a technical compliance summary, not legal advice.)*

---

## 6. What we should do instead (the useful part)

Our real search weaknesses have nothing to do with Mantic. The `enrich_context` path is semantic-only and **distance-ranked**, ignoring the human-judgment signals we already collect. Recommended, prioritized:

### P0 — Feedback-aware re-ranking *(highest leverage, lowest cost — S–M)*
Replace the distance-only sort in `internal/enrich/enrich.go` `Select` with a composite score. All inputs already exist on the entry or one cheap aggregate away (reuse the trending SQL, scoped to candidate IDs):

```
score = 0.55 * relevance      (1 − distance, existing)
      + 0.20 * outcomeNorm    (avg outcome_ratings / 5)
      + 0.10 * ratingNorm     (entries.rating / 5)
      + 0.10 * usageNorm      (ln(1+usage30d) / ln(1+poolMaxUsage))
      + 0.05 * recencyNorm    (exp(−ageDays / 180))
```
Keep relevance ≥ 0.5 of the blend and gate feedback behind `MinRelevance` so it re-ranks *within* a semantically valid pool and never surfaces off-topic-but-popular entries. Zero-usage entries simply don't get the boost (no cold-start penalty). Since only a handful of entries are injected, promoting the *proven-useful* ones is exactly what moves precision@k. **This is the win Mantic categorically cannot provide.**

### P1 — Make weights tunable + log score breakdown *(S)*; wire hybrid into `enrich_context` *(M)*
Bring lexical search to the SQLite path via **FTS5** (title/content/tags virtual table + sync triggers), replace the fixed 50/50 Postgres blend with **Reciprocal Rank Fusion** (scale-free, no normalization), factor a shared `rank` package, and point `enrich_context` at the fused path so LLM injection finally gets lexical recall (exact tokens: tickers, acronyms, error codes).

### P2 — As measured need dictates
- **LLM rerank of top ~8** using the already-wired Anthropic model (cross-encoder-style; reuse `internal/llm/json.go`; degrade gracefully).
- **Query expansion / multi-query** (small acronym glossary for the lexical leg; ≤3 LLM paraphrases for recall).
- **MMR diversity** to stop near-duplicate entries eating scarce injection slots (`content_hash` already handles exact dupes).

### Avoid over-engineering at this scale
No external vector DB (sqlite-vec/pgvector is exact kNN in ms for a team-sized corpus), no dedicated cross-encoder/GPU serving, no offline NDCG/LTR pipeline yet. Tune by observation and analyst feedback. Land P0 first — it is the dominant win for the effort.

---

## 7. Answers to the three questions asked

1. **Does it add value over what we currently use?** No — it's a code-file ranker; our semantic+hybrid pipeline is already the right and more capable approach for free-text memories.
2. **Can we integrate it into the codebase?** Only as a separate process (Go↔TS), and the library path is blocked by AGPL. But there's nothing worth integrating — it doesn't fit our data model.
3. **Is it worth using?** No. Spend the effort on P0 feedback-aware re-ranking, which is higher value, lower cost, license-free, and uniquely leverages the signals this product is built to capture.

---

### Sources
- [Mantic.sh repository](https://github.com/marcoaapfortes/Mantic.sh) · [README](https://raw.githubusercontent.com/marcoaapfortes/Mantic.sh/main/README.md) · [LICENSE (AGPL-3.0)](https://github.com/marcoaapfortes/Mantic.sh/blob/main/LICENSE) · [npm](https://www.npmjs.com/package/mantic.sh) · [Show HN](https://news.ycombinator.com/item?id=46512182)
- Internal codebase review of `internal/storage`, `internal/enrich`, `internal/mcp/enrich_context.go`, `internal/embedding`, `postgres_search.go`, `postgres_usage.go` (July 2026)
