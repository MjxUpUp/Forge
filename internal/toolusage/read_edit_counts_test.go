package toolusage

import (
	"testing"
	"time"
)

// TestReadEditCounts verifies the toollog-sourced read/edit separation used by
// the activity ratio gate. Grep/Glob/Bash must be excluded; only Read vs
// Edit+Write matter for the read-before-edit signal. Other-task records and
// records before `since` must be filtered out.
func TestReadEditCounts(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 6, 13, 12, 0, 0, 0, time.UTC)
	taskRef := "feat/test"

	records := []ToolCall{
		{ToolName: "Read", TaskRef: taskRef, Timestamp: now.Add(-10 * time.Second)},
		{ToolName: "Read", TaskRef: taskRef, Timestamp: now.Add(-9 * time.Second)},
		{ToolName: "Edit", TaskRef: taskRef, Timestamp: now.Add(-5 * time.Second)},
		{ToolName: "Write", TaskRef: taskRef, Timestamp: now.Add(-3 * time.Second)},
		{ToolName: "Grep", TaskRef: taskRef, Timestamp: now.Add(-2 * time.Second)}, // excluded (not read/edit)
		{ToolName: "Read", TaskRef: "other-task", Timestamp: now},                  // excluded (other task)
	}
	for i, r := range records {
		rr := r
		if err := Record(dir, &rr); err != nil {
			t.Fatalf("record %d: %v", i, err)
		}
	}

	// since=-15s captures the four read/edit records: 2 reads, 2 edits.
	reads, edits, err := ReadEditCounts(dir, taskRef, now.Add(-15*time.Second))
	if err != nil {
		t.Fatalf("ReadEditCounts: %v", err)
	}
	if reads != 2 || edits != 2 {
		t.Fatalf("got reads=%d edits=%d, want 2/2", reads, edits)
	}

	// since=-6s captures only Edit(-5s) and Write(-3s): 0 reads, 2 edits.
	reads, edits, _ = ReadEditCounts(dir, taskRef, now.Add(-6 * time.Second))
	if reads != 0 || edits != 2 {
		t.Fatalf("got reads=%d edits=%d, want 0/2 (since filter)", reads, edits)
	}

	// since in the future captures nothing.
	reads, edits, _ = ReadEditCounts(dir, taskRef, now.Add(1*time.Hour))
	if reads != 0 || edits != 0 {
		t.Fatalf("got reads=%d edits=%d, want 0/0 (future since)", reads, edits)
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
