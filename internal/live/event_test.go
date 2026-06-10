package live

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestCapFragment_UnderCap(t *testing.T) {
	s := strings.Repeat("a", MaxFragmentLen)
	got := CapFragment(s)
	if got != s {
		t.Fatalf("expected unchanged string for exactly MaxFragmentLen runes, got length %d", utf8.RuneCountInString(got))
	}
}

func TestCapFragment_OverCap_ASCIITruncated(t *testing.T) {
	// One rune over the cap.
	s := strings.Repeat("a", MaxFragmentLen+1)
	got := CapFragment(s)

	runeLen := utf8.RuneCountInString(got)
	if runeLen > MaxFragmentLen {
		t.Fatalf("result is %d runes, want <= %d", runeLen, MaxFragmentLen)
	}
	if !strings.HasSuffix(got, "…") {
		t.Fatalf("expected trailing ellipsis, got: %q", got[len(got)-4:])
	}
	if runeLen != MaxFragmentLen {
		t.Fatalf("expected exactly %d runes, got %d", MaxFragmentLen, runeLen)
	}
}

func TestCapFragment_ShortString(t *testing.T) {
	s := "hello"
	if got := CapFragment(s); got != s {
		t.Fatalf("expected %q unchanged, got %q", s, got)
	}
}

func TestCapFragment_MultibyteNotSplit(t *testing.T) {
	// Build a string of 300 Japanese rune characters (each is 3 bytes in UTF-8).
	s := strings.Repeat("あ", MaxFragmentLen+20)
	got := CapFragment(s)

	// Must be valid UTF-8 — no split multibyte sequences.
	if !utf8.ValidString(got) {
		t.Fatal("result is not valid UTF-8")
	}

	runeLen := utf8.RuneCountInString(got)
	if runeLen > MaxFragmentLen {
		t.Fatalf("result is %d runes, want <= %d", runeLen, MaxFragmentLen)
	}

	// Must end with ellipsis.
	if !strings.HasSuffix(got, "…") {
		t.Fatalf("expected trailing ellipsis")
	}
}

func TestCapFragment_ExactlyOneLess(t *testing.T) {
	s := strings.Repeat("x", MaxFragmentLen-1)
	got := CapFragment(s)
	if got != s {
		t.Fatalf("string one under cap should be unchanged")
	}
}

func TestCapFragment_Empty(t *testing.T) {
	if got := CapFragment(""); got != "" {
		t.Fatalf("empty string should return empty, got %q", got)
	}
}
