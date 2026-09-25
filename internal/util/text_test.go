package util

import "testing"

func TestTruncateRunes(t *testing.T) {
	// Short string passes through untouched.
	if got := TruncateRunes("hello", 5); got != "hello" {
		t.Errorf("short string: got %q", got)
	}
	// Over-length string is truncated with an ellipsis marker.
	if got := TruncateRunes("hello", 4); got != "hell…" {
		t.Errorf("truncated ASCII: got %q, want %q", got, "hell…")
	}
	// Rune-safe: Chinese chars are multi-byte; truncation must not split a character.
	got := TruncateRunes("中文中文", 3)
	if got != "中文中…" {
		t.Errorf("truncated CJK: got %q, want %q", got, "中文中…")
	}
	if n := len([]rune(got)); n != 4 {
		t.Errorf("rune count after truncation: got %d, want 4 (3 runes + ellipsis)", n)
	}
	// Empty input.
	if got := TruncateRunes("", 3); got != "" {
		t.Errorf("empty: got %q", got)
	}
}
