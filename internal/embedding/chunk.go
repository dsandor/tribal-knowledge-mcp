package embedding

import (
	"strings"
	"unicode/utf8"
)

// ChunkConfig controls how content is split into embeddable chunks.
type ChunkConfig struct {
	MaxTokens     int // per-chunk token budget (embedding model context window)
	OverlapTokens int // tokens of trailing context repeated into the next chunk
	MaxChunks     int // 0 = unlimited
}

// ContentChunk is a single coherent slice of content destined to be embedded
// separately. (Named ContentChunk rather than Chunk to avoid colliding with the
// Chunk function, which the public API and tests call.)
type ContentChunk struct {
	Index         int
	Content       string
	TokenEstimate int
}

// safetyFraction shrinks the configured budget to keep headroom: token counts
// here are a coarse runes/4 estimate (no tokenizer matches the embedding
// model's vocab exactly), so we stay comfortably under the model's true window.
const safetyFraction = 0.9

// CountTokens returns an approximate token count for s using a coarse runes/4
// heuristic. Exactness is not required — the chunker applies safetyFraction on
// top of this estimate so chunks stay within the embedding model's window.
func CountTokens(s string) int {
	return len([]rune(s)) / 4
}

// validUTF8 reports whether s contains only valid UTF-8 (no split runes).
func validUTF8(s string) bool {
	return utf8.ValidString(s)
}

// Chunk splits content into one or more chunks that each fit within an
// effective token budget. It never drops content and never splits inside a
// UTF-8 rune.
func Chunk(content string, cfg ChunkConfig) []ContentChunk {
	// No chunking when unconfigured: an unset/invalid MaxTokens means "do not
	// chunk" (matching the tool-filter's maxTokens<=0 guard). Returning a single
	// chunk here avoids pathological over-chunking (one chunk per few runes, each
	// triggering its own embedding call).
	if cfg.MaxTokens <= 0 {
		return []ContentChunk{{Index: 0, Content: content, TokenEstimate: CountTokens(content)}}
	}

	budget := int(float64(cfg.MaxTokens) * safetyFraction)
	if budget < 1 {
		budget = 1
	}

	// Fast path: small content fits in a single chunk, byte-identical.
	if CountTokens(content) <= budget {
		return []ContentChunk{{Index: 0, Content: content, TokenEstimate: CountTokens(content)}}
	}

	atoms := splitAtoms(content, budget)

	var chunks []ContentChunk
	var cur strings.Builder
	curTokens := 0

	flush := func() {
		if cur.Len() == 0 {
			return
		}
		text := cur.String()
		chunks = append(chunks, ContentChunk{
			Index:         len(chunks),
			Content:       text,
			TokenEstimate: CountTokens(text),
		})
		cur.Reset()
		curTokens = 0
	}

	for i := 0; i < len(atoms); i++ {
		atom := atoms[i]

		// MaxChunks cap: if we've filled all but the last allowed chunk and
		// the current builder is non-empty, the next flush would be the last
		// chunk. Dump every remaining atom (including this one) into it so
		// nothing is dropped.
		if cfg.MaxChunks > 0 && len(chunks) == cfg.MaxChunks-1 {
			startNewChunk(&cur, &curTokens, chunks, cfg)
			for ; i < len(atoms); i++ {
				cur.WriteString(atoms[i])
			}
			flush()
			return chunks
		}

		atomTokens := CountTokens(atom)

		if curTokens > 0 && curTokens+atomTokens > budget {
			flush()
		}

		if cur.Len() == 0 {
			startNewChunk(&cur, &curTokens, chunks, cfg)
		}

		cur.WriteString(atom)
		curTokens += atomTokens
	}
	flush()

	return chunks
}

// startNewChunk seeds a fresh chunk builder with overlap from the previous
// chunk's trailing text, when overlap is enabled and a previous chunk exists.
func startNewChunk(cur *strings.Builder, curTokens *int, chunks []ContentChunk, cfg ChunkConfig) {
	if cfg.OverlapTokens <= 0 || len(chunks) == 0 {
		return
	}
	prev := chunks[len(chunks)-1].Content
	overlapRunes := cfg.OverlapTokens * 4
	r := []rune(prev)
	if len(r) > overlapRunes {
		r = r[len(r)-overlapRunes:]
	}
	tail := string(r)
	if tail == "" {
		return
	}
	cur.WriteString(tail)
	*curTokens += CountTokens(tail)
}

// splitAtoms breaks content into the smallest reusable units following a
// boundary hierarchy: markdown headings, blank-line paragraphs, sentences,
// then a hard rune-window fallback for any atom that alone exceeds budget.
func splitAtoms(content string, budget int) []string {
	var atoms []string
	for _, para := range splitParagraphs(content) {
		if para == "" {
			continue
		}
		if CountTokens(para) <= budget {
			atoms = append(atoms, para)
			continue
		}
		for _, sent := range splitSentences(para) {
			if sent == "" {
				continue
			}
			if CountTokens(sent) <= budget {
				atoms = append(atoms, sent)
				continue
			}
			atoms = append(atoms, hardSplit(sent, budget)...)
		}
	}
	return atoms
}

// splitParagraphs splits on markdown heading lines and blank-line boundaries,
// preserving the trailing whitespace/separators so joins reproduce the text.
func splitParagraphs(content string) []string {
	lines := strings.SplitAfter(content, "\n")
	var atoms []string
	var cur strings.Builder

	flush := func() {
		if cur.Len() > 0 {
			atoms = append(atoms, cur.String())
			cur.Reset()
		}
	}

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		isHeading := strings.HasPrefix(trimmed, "#")
		isBlank := trimmed == ""

		if isHeading {
			// Heading starts a new atom.
			flush()
			cur.WriteString(line)
			continue
		}
		if isBlank {
			// Blank line terminates the current paragraph (keep the blank
			// line attached to it so no whitespace is lost).
			cur.WriteString(line)
			flush()
			continue
		}
		cur.WriteString(line)
	}
	flush()
	return atoms
}

// splitSentences splits text on sentence-terminating punctuation, keeping the
// terminator and following whitespace attached to each sentence.
func splitSentences(text string) []string {
	var atoms []string
	var cur strings.Builder
	runes := []rune(text)

	for i := 0; i < len(runes); i++ {
		c := runes[i]
		cur.WriteRune(c)
		if c == '.' || c == '!' || c == '?' || c == '。' || c == '！' || c == '？' {
			// Consume trailing whitespace into this sentence.
			for i+1 < len(runes) && isSpace(runes[i+1]) {
				cur.WriteRune(runes[i+1])
				i++
			}
			atoms = append(atoms, cur.String())
			cur.Reset()
		}
	}
	if cur.Len() > 0 {
		atoms = append(atoms, cur.String())
	}
	return atoms
}

func isSpace(r rune) bool {
	return r == ' ' || r == '\t' || r == '\n' || r == '\r'
}

// hardSplit cuts s into rune-window slices that each fit the budget, never
// splitting inside a rune. Window size is estimated from the rune/token ratio.
func hardSplit(s string, budget int) []string {
	runes := []rune(s)
	// CountTokens ~ runes/4, so budget tokens ≈ budget*4 runes. Use a slightly
	// smaller window and verify each slice against the real budget.
	window := budget * 3
	if window < 1 {
		window = 1
	}

	var atoms []string
	for start := 0; start < len(runes); {
		end := start + window
		if end > len(runes) {
			end = len(runes)
		}
		// Shrink the window until the slice actually fits the budget.
		for end > start+1 && CountTokens(string(runes[start:end])) > budget {
			end--
		}
		atoms = append(atoms, string(runes[start:end]))
		start = end
	}
	return atoms
}
