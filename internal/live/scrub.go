package live

import "regexp"

// Regexes are compiled once at package initialisation to avoid repeated
// allocation.  They are intentionally conservative (few false-positives) while
// covering the most common secret/PII patterns seen in LLM prompts and
// knowledge fragments.
var (
	// reAPIKey matches OpenAI-style secret keys: sk- followed by ≥8
	// alphanumeric/dash/underscore characters.
	reAPIKey = regexp.MustCompile(`sk-[A-Za-z0-9_\-]{8,}`)

	// reAWSKey matches AWS IAM access key IDs (always start with AKIA and
	// are exactly 20 characters long).
	reAWSKey = regexp.MustCompile(`AKIA[0-9A-Z]{16}`)

	// reBearer matches "Bearer <token>" in Authorization headers or fragments.
	// We keep the word "Bearer" visible so the reader knows what was redacted.
	reBearer = regexp.MustCompile(`(?i)(Bearer\s+)[A-Za-z0-9\-._~+/]+=*`)

	// reEmail matches typical email addresses.  We use a simple pattern that
	// avoids catastrophic backtracking: local-part @ domain.tld.
	reEmail = regexp.MustCompile(`[a-zA-Z0-9._%+\-]+@[a-zA-Z0-9.\-]+\.[a-zA-Z]{2,}`)
)

const redacted = "[redacted]"

// Scrub redacts common secret/PII patterns in s, replacing each match with
// "[redacted]".  Patterns covered:
//   - OpenAI-style API keys: sk-<8+ alphanumeric/dash/underscore>
//   - AWS IAM access key IDs: AKIA<16 uppercase alphanumeric>
//   - HTTP Bearer tokens: "Bearer <token>" — "Bearer" is preserved
//   - Email addresses
//
// Returns s unchanged when no patterns match.
// This function is safe to call from multiple goroutines concurrently.
func Scrub(s string) string {
	// reBearer capture group 1 is "Bearer\s+" — back-reference ${1} keeps it
	// visible while redacting only the token in a single pass.
	s = reBearer.ReplaceAllString(s, "${1}[redacted]")

	s = reAPIKey.ReplaceAllString(s, redacted)
	s = reAWSKey.ReplaceAllString(s, redacted)
	s = reEmail.ReplaceAllString(s, redacted)

	return s
}
