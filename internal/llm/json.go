package llm

import (
	"regexp"
	"strings"
)

// jsonFenceRe matches a Markdown code fence (optionally tagged ```json) and
// captures its body. Models frequently wrap JSON responses in fences despite
// being asked for raw JSON, which breaks json.Unmarshal with errors like
// "invalid character '`' looking for beginning of value".
var jsonFenceRe = regexp.MustCompile("(?s)```(?:json)?\\s*(.*?)```")

// ExtractJSON returns the JSON body of an LLM response, stripping a surrounding
// Markdown code fence if present. When no fence is found the input is returned
// trimmed, so callers can pass any completion straight to json.Unmarshal.
func ExtractJSON(s string) string {
	if m := jsonFenceRe.FindStringSubmatch(s); len(m) > 1 {
		return strings.TrimSpace(m[1])
	}
	return strings.TrimSpace(s)
}
