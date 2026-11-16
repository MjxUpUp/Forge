package toolusage

import (
	"testing"
	"time"
)

// TestExploreCountsEmptyDir ensures graceful behavior when toollog is absent.
func TestExploreCountsEmptyDir(t *testing.T) {
	dir := t.TempDir()
	explores, err := ExploreCounts(dir, "any", time.Time{})
	if err != nil {
		t.Fatalf("expected nil error on missing toollog, got %v", err)
	}
	if explores != 0 {
		t.Fatalf("got explores=%d, want 0 on empty", explores)
	}
}

// TestReadEditCountsEmptyDir ensures graceful behavior when toollog is absent.
func TestReadEditCountsEmptyDir(t *testing.T) {
	dir := t.TempDir()
	reads, edits, err := ReadEditCounts(dir, "any", time.Time{})
	if err != nil {
		t.Fatalf("expected nil error on missing toollog, got %v", err)
	}
	if reads != 0 || edits != 0 {
		t.Fatalf("got reads=%d edits=%d, want 0/0 on empty", reads, edits)
	}
}
