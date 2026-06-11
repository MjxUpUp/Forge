package toolusage

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRecordAndLoad(t *testing.T) {
	dir := t.TempDir()

	call := &ToolCall{
		ToolName:  "Read",
		ToolInput: `{"file_path": "/tmp/test.go"}`,
		TaskRef:   "test-task",
	}

	if err := Record(dir, call); err != nil {
		t.Fatalf("Record failed: %v", err)
	}

	calls, err := LoadAll(dir)
	if err != nil {
		t.Fatalf("LoadAll failed: %v", err)
	}

	if len(calls) != 1 {
		t.Fatalf("expected 1 call, got %d", len(calls))
	}
	if calls[0].ToolName != "Read" {
		t.Errorf("expected ToolName=Read, got %s", calls[0].ToolName)
	}
	if calls[0].TaskRef != "test-task" {
		t.Errorf("expected TaskRef=test-task, got %s", calls[0].TaskRef)
	}
}

func TestLoadForTask(t *testing.T) {
	dir := t.TempDir()

	records := []ToolCall{
		{ToolName: "Read", TaskRef: "task-a"},
		{ToolName: "Edit", TaskRef: "task-b"},
		{ToolName: "Bash", TaskRef: "task-a"},
	}

	for _, r := range records {
		if err := Record(dir, &r); err != nil {
			t.Fatalf("Record failed: %v", err)
		}
	}

	calls, err := LoadForTask(dir, "task-a")
	if err != nil {
		t.Fatalf("LoadForTask failed: %v", err)
	}

	if len(calls) != 2 {
		t.Fatalf("expected 2 calls for task-a, got %d", len(calls))
	}
	for _, c := range calls {
		if c.TaskRef != "task-a" {
			t.Errorf("expected TaskRef=task-a, got %s", c.TaskRef)
		}
	}
}

func TestToolCounts(t *testing.T) {
	calls := []ToolCall{
		{ToolName: "Read"},
		{ToolName: "Read"},
		{ToolName: "Edit"},
		{ToolName: "Bash"},
		{ToolName: "Read"},
	}

	counts := ToolCounts(calls)
	if counts["Read"] != 3 {
		t.Errorf("expected Read=3, got %d", counts["Read"])
	}
	if counts["Edit"] != 1 {
		t.Errorf("expected Edit=1, got %d", counts["Edit"])
	}
	if counts["Bash"] != 1 {
		t.Errorf("expected Bash=1, got %d", counts["Bash"])
	}
}

func TestSortedToolCounts(t *testing.T) {
	calls := []ToolCall{
		{ToolName: "Edit"},
		{ToolName: "Read"},
		{ToolName: "Read"},
		{ToolName: "Bash"},
	}

	result := SortedToolCounts(calls)
	if len(result) != 3 {
		t.Fatalf("expected 3 entries, got %d", len(result))
	}
	// Read(2) should be first
	if result[0] != "Read(2)" {
		t.Errorf("expected Read(2) first, got %s", result[0])
	}
}

func TestTruncateInput(t *testing.T) {
	// ASCII
	short := "hello"
	if TruncateInput(short) != short {
		t.Error("short string should not be truncated")
	}

	// Long ASCII
	long := ""
	for i := 0; i < 600; i++ {
		long += "x"
	}
	truncated := TruncateInput(long)
	if len([]rune(truncated)) != maxToolInputLen {
		t.Errorf("expected %d runes, got %d", maxToolInputLen, len([]rune(truncated)))
	}

	// Chinese (rune-safe)
	chinese := ""
	for i := 0; i < 300; i++ {
		chinese += "中文"
	}
	truncatedChinese := TruncateInput(chinese)
	if len([]rune(truncatedChinese)) != maxToolInputLen {
		t.Errorf("expected %d runes for Chinese, got %d", maxToolInputLen, len([]rune(truncatedChinese)))
	}
}

func TestClear(t *testing.T) {
	dir := t.TempDir()

	Record(dir, &ToolCall{ToolName: "Read"})
	Record(dir, &ToolCall{ToolName: "Edit"})

	calls, _ := LoadAll(dir)
	if len(calls) != 2 {
		t.Fatalf("expected 2 calls before clear, got %d", len(calls))
	}

	if err := Clear(dir); err != nil {
		t.Fatalf("Clear failed: %v", err)
	}

	calls, _ = LoadAll(dir)
	if len(calls) != 0 {
		t.Errorf("expected 0 calls after clear, got %d", len(calls))
	}

	// Archived file should exist
	files, _ := filepath.Glob(filepath.Join(dir, ".forge", "toollog-*.jsonl"))
	if len(files) == 0 {
		// Archive might be in the temp dir that got cleaned, check .forge dir
		forgeDir := filepath.Join(dir, ".forge")
		entries, err := os.ReadDir(forgeDir)
		if err == nil {
			found := false
			for _, e := range entries {
				if len(e.Name()) > 12 && e.Name()[:12] == "toollog-20" {
					found = true
					break
				}
			}
			if !found {
				t.Error("expected archived toollog file after Clear")
			}
		}
	}
}

func TestLoadNonexistent(t *testing.T) {
	dir := t.TempDir()
	calls, err := LoadAll(dir)
	if err != nil {
		t.Fatalf("LoadAll on nonexistent dir should not error: %v", err)
	}
	if len(calls) != 0 {
		t.Errorf("expected 0 calls, got %d", len(calls))
	}
}
