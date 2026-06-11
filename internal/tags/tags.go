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
