package live

import (
	"strings"
	"testing"
)

func TestScrub_APIKey(t *testing.T) {
	cases := []struct {
		input string
		want  string
	}{
		{
			input: "my key is sk-abc123XYZabc123XYZabc",
			want:  "my key is [redacted]",
		},
		{
			input: "sk-1234567890abcdef",
			want:  "[redacted]",
		},
		// Too short (7 chars after sk-) — should NOT be redacted.
		{
			input: "sk-short",
			want:  "sk-short",
		},
	}
	for _, c := range cases {
		got := Scrub(c.input)
		if got != c.want {
			t.Errorf("Scrub(%q) = %q, want %q", c.input, got, c.want)
		}
	}
}

func TestScrub_AWSKey(t *testing.T) {
	cases := []struct {
		input string
		want  string
	}{
		{
			input: "access key: AKIAIOSFODNN7EXAMPLE",
			want:  "access key: [redacted]",
		},
		{
			input: "AKIAIOSFODNN7EXAMPLE is bad",
			want:  "[redacted] is bad",
		},
	}
	for _, c := range cases {
		got := Scrub(c.input)
		if got != c.want {
			t.Errorf("Scrub(%q) = %q, want %q", c.input, got, c.want)
		}
	}
}

func TestScrub_BearerToken(t *testing.T) {
	cases := []struct {
		input    string
		wantPre  string // must start with this
		wantKeep string // must contain this word
	}{
		{
			input:    "Authorization: Bearer eyJhbGciOiJSUzI1NiJ9.payload.signature",
			wantPre:  "Authorization: Bearer [redacted]",
			wantKeep: "Bearer",
		},
		{
			input:    "bearer sometoken123",
			wantPre:  "bearer [redacted]",
			wantKeep: "bearer",
		},
	}
	for _, c := range cases {
		got := Scrub(c.input)
		if got != c.wantPre {
			t.Errorf("Scrub(%q) = %q, want %q", c.input, got, c.wantPre)
		}
		if !strings.Contains(got, c.wantKeep) {
			t.Errorf("Scrub(%q) = %q: expected %q to be preserved", c.input, got, c.wantKeep)
		}
		if strings.Contains(got, "eyJ") {
			t.Errorf("Scrub(%q) = %q: JWT payload still present", c.input, got)
		}
	}
}

func TestScrub_Email(t *testing.T) {
	cases := []struct {
		input string
		want  string
	}{
		{
			input: "contact me at user@example.com please",
			want:  "contact me at [redacted] please",
		},
		{
			input: "send to alice@company.org and bob@other.co.uk",
			want:  "send to [redacted] and [redacted]",
		},
	}
	for _, c := range cases {
		got := Scrub(c.input)
		if got != c.want {
			t.Errorf("Scrub(%q) = %q, want %q", c.input, got, c.want)
		}
	}
}

func TestScrub_CleanText(t *testing.T) {
	clean := "The quick brown fox jumps over the lazy dog."
	got := Scrub(clean)
	if got != clean {
		t.Errorf("clean text was modified: got %q, want %q", got, clean)
	}
}

func TestScrub_MultiplePatterns(t *testing.T) {
	input := "key=sk-abcdefghijklmnop and AKIAIOSFODNN7EXAMPLE and user@host.com"
	got := Scrub(input)

	if strings.Contains(got, "sk-") {
		t.Errorf("API key not redacted: %q", got)
	}
	if strings.Contains(got, "AKIA") {
		t.Errorf("AWS key not redacted: %q", got)
	}
	if strings.Contains(got, "@host.com") {
		t.Errorf("email not redacted: %q", got)
	}
}
