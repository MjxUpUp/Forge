package checklog

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// writeTestEntries writes entries directly to checklog file with preserved RecordedAt.
// Record() overwrites RecordedAt, so we bypass it for dedup testing.
func writeTestEntries(t *testing.T, dir string, entries []*Entry) {
	t.Helper()
	os.MkdirAll(filepath.Join(dir, ".forge"), 0755)
	f, err := os.OpenFile(filepath.Join(dir, ".forge", "checklog.jsonl"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		t.Fatalf("open checklog: %v", err)
	}
	defer f.Close()
	for _, e := range entries {
		data, _ := json.Marshal(e)
		f.Write(data)
		f.Write([]byte("\n"))
	}
}

func TestWorkActivityCountsDistinct(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()

	// Simulate: one Write triggers 4 hooks within 500ms
	entries := []*Entry{}
	for i, check := range []CheckName{"task-guard", "assertion-check", "experience-check", "auto-compile"} {
		entries = append(entries, &Entry{
			Check:      check,
			Passed:     true,
			Checked:    true,
			ToolName:   "Write",
			TaskRef:    "test-task",
			RecordedAt: now.Add(time.Duration(i*10) * time.Millisecond),
		})
	}
	// One Read via tool-track (200ms later)
	entries = append(entries, &Entry{
		Check:      "tool-track",
		Passed:     true,
		Checked:    true,
		ToolName:   "Read",
		TaskRef:    "test-task",
		RecordedAt: now.Add(200 * time.Millisecond),
	})
	writeTestEntries(t, dir, entries)

	since := now.Add(-1 * time.Second)
	count, err := WorkActivity(dir, "test-task", since)
	if err != nil {
		t.Fatalf("WorkActivity failed: %v", err)
	}

	// 4 Write records within 500ms deduplicate to 1, plus 1 Read = 2
	if count != 2 {
		t.Errorf("WorkActivity count = %d, want 2 (1 Write deduped from 4, plus 1 Read)", count)
	}
}

func TestWorkActivitySeparatesDifferentInvocations(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()

	// First Write at t=0
	// Second Write at t=1s (beyond 500ms window — should count separately)
	writeTestEntries(t, dir, []*Entry{
		{
			Check:      "auto-compile",
			Passed:     true,
			Checked:    true,
			ToolName:   "Write",
			TaskRef:    "test-task",
			RecordedAt: now,
		},
		{
			Check:      "auto-compile",
			Passed:     true,
			Checked:    true,
			ToolName:   "Write",
			TaskRef:    "test-task",
			RecordedAt: now.Add(1 * time.Second),
		},
	})

	since := now.Add(-1 * time.Second)
	count, err := WorkActivity(dir, "test-task", since)
	if err != nil {
		t.Fatalf("WorkActivity failed: %v", err)
	}

	if count != 2 {
		t.Errorf("WorkActivity count = %d, want 2 (two distinct Write invocations >500ms apart)", count)
	}
}

func TestWorkActivityExcludesBash(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()

	writeTestEntries(t, dir, []*Entry{
		{
			Check:      "tool-track",
			Passed:     true,
			Checked:    true,
			ToolName:   "Bash",
			TaskRef:    "test-task",
			RecordedAt: now,
		},
		{
			Check:      "tool-track",
			Passed:     true,
			Checked:    true,
			ToolName:   "Read",
			TaskRef:    "test-task",
			RecordedAt: now.Add(100 * time.Millisecond),
		},
	})

	since := now.Add(-1 * time.Second)
	count, err := WorkActivity(dir, "test-task", since)
	if err != nil {
		t.Fatalf("WorkActivity failed: %v", err)
	}

	if count != 1 {
		t.Errorf("WorkActivity count = %d, want 1 (Bash excluded, only Read counted)", count)
	}
}

func TestWorkActivityExcludesBlockedOperations(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()

	// A blocked Write (task-guard blocked) has empty tool_name
	writeTestEntries(t, dir, []*Entry{
		{
			Check:      "task-guard",
			Passed:     false,
			Checked:    true,
			ToolName:   "", // blocked — tool_name cleared by hook.go
			TaskRef:    "test-task",
			RecordedAt: now,
		},
		{
			Check:      "tool-track",
			Passed:     true,
			Checked:    true,
			ToolName:   "Read",
			TaskRef:    "test-task",
			RecordedAt: now.Add(100 * time.Millisecond),
		},
	})

	since := now.Add(-1 * time.Second)
	count, err := WorkActivity(dir, "test-task", since)
	if err != nil {
		t.Fatalf("WorkActivity failed: %v", err)
	}

	if count != 1 {
		t.Errorf("WorkActivity count = %d, want 1 (blocked Write with empty tool_name excluded)", count)
	}
}

func TestWorkActivityFiltersByTask(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()

	writeTestEntries(t, dir, []*Entry{
		{
			Check:      "tool-track",
			Passed:     true,
			Checked:    true,
			ToolName:   "Read",
			TaskRef:    "other-task",
			RecordedAt: now,
		},
		{
			Check:      "tool-track",
			Passed:     true,
			Checked:    true,
			ToolName:   "Read",
			TaskRef:    "target-task",
			RecordedAt: now.Add(100 * time.Millisecond),
		},
	})

	since := now.Add(-1 * time.Second)
	count, err := WorkActivity(dir, "target-task", since)
	if err != nil {
		t.Fatalf("WorkActivity failed: %v", err)
	}

	if count != 1 {
		t.Errorf("WorkActivity count = %d, want 1 (only target-task activity)", count)
	}
}

func TestWorkActivityCountsLegacyEmptyTaskRef(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()

	writeTestEntries(t, dir, []*Entry{
		{
			Check:      "tool-track",
			Passed:     true,
			Checked:    true,
			ToolName:   "Read",
			TaskRef:    "", // legacy entry without task_ref
			RecordedAt: now,
		},
		{
			Check:      "tool-track",
			Passed:     true,
			Checked:    true,
			ToolName:   "Grep",
			TaskRef:    "", // legacy entry without task_ref
			RecordedAt: now.Add(100 * time.Millisecond),
		},
	})

	since := now.Add(-1 * time.Second)
	count, err := WorkActivity(dir, "some-task", since)
	if err != nil {
		t.Fatalf("WorkActivity failed: %v", err)
	}

	if count != 2 {
		t.Errorf("WorkActivity count = %d, want 2 (legacy entries with empty task_ref should be counted)", count)
	}
}

func TestWorkActivityZeroWhenNoActivity(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()

	writeTestEntries(t, dir, []*Entry{
		{
			Check:      "tool-track",
			Passed:     true,
			Checked:    true,
			ToolName:   "Bash",
			TaskRef:    "test-task",
			RecordedAt: now,
		},
	})

	since := now.Add(-1 * time.Second)
	count, err := WorkActivity(dir, "test-task", since)
	if err != nil {
		t.Fatalf("WorkActivity failed: %v", err)
	}

	if count != 0 {
		t.Errorf("WorkActivity count = %d, want 0 (only Bash, no work tools)", count)
	}
}
