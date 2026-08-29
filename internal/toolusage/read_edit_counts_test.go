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
	reads, edits, _ = ReadEditCounts(dir, taskRef, now.Add(-6*time.Second))
	if reads != 0 || edits != 2 {
		t.Fatalf("got reads=%d edits=%d, want 0/2 (since filter)", reads, edits)
	}

	// since in the future captures nothing.
	reads, edits, _ = ReadEditCounts(dir, taskRef, now.Add(1*time.Hour))
	if reads != 0 || edits != 0 {
		t.Fatalf("got reads=%d edits=%d, want 0/0 (future since)", reads, edits)
	}
}

// TestExploreCounts verifies the exploration signal sourced from toollog: Grep
// and Glob calls since `since`, scoped to the task.
//
// TestExploreCounts 验证取自 toollog 的探索信号：自 since 起、按 task 限定的
// Grep/Glob 调用数。ExploreCounts 只供 work-activity 的「有无真实工作」判定——
// 绝不供 read-before-edit（浏览匹配不等于读过要改的文件；该信号的严格性见
// ReadEditCounts 文档）。钉住 2026-08-23 文档-实现漂移修复：CLAUDE.md 错误表
// 建议门禁间「用 Read/Grep/Glob 探索」，但 Grep/Glob 此前根本进不了 toollog
// （tool-track matcher 不含它们），纯探索段落被计为零工作。
func TestExploreCounts(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 8, 23, 12, 0, 0, 0, time.UTC)
	taskRef := "fix/explore"

	records := []ToolCall{
		{ToolName: "Grep", TaskRef: taskRef, Timestamp: now.Add(-8 * time.Second)},
		{ToolName: "Glob", TaskRef: taskRef, Timestamp: now.Add(-7 * time.Second)},
		{ToolName: "Grep", TaskRef: taskRef, Timestamp: now.Add(-4 * time.Second)},
		// Exploration-adjacent but NOT exploration signals:
		{ToolName: "Read", TaskRef: taskRef, Timestamp: now.Add(-3 * time.Second)},      // read-before-edit's signal, not activity's explore axis
		{ToolName: "Bash", TaskRef: taskRef, Timestamp: now.Add(-2 * time.Second)},      // always excluded (gate commands ride Bash)
		{ToolName: "Grep", TaskRef: "other-task", Timestamp: now.Add(-1 * time.Second)}, // other task
		{ToolName: "Glob", TaskRef: taskRef, Timestamp: now.Add(-20 * time.Second)},     // before `since` below
	}
	for i, r := range records {
		rr := r
		if err := Record(dir, &rr); err != nil {
			t.Fatalf("record %d: %v", i, err)
		}
	}

	// since=-10s: Grep(-8s), Glob(-7s), Grep(-4s) — the Glob(-20s) predates since.
	explores, err := ExploreCounts(dir, taskRef, now.Add(-10*time.Second))
	if err != nil {
		t.Fatalf("ExploreCounts: %v", err)
	}
	if explores != 3 {
		t.Fatalf("got explores=%d, want 3 (2 Grep + 1 Glob; Read/Bash/other-task/old excluded)", explores)
	}

	// Future since captures nothing.
	explores, _ = ExploreCounts(dir, taskRef, now.Add(1*time.Hour))
	if explores != 0 {
		t.Fatalf("got explores=%d, want 0 (future since)", explores)
	}
}

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

// TestReadEditCountsGraceWindow verifies the race-recovery query: a Read fired
// concurrently with `forge task start` lands under the PREVIOUS task's ref, so
// the grace window counts Reads across ALL tasks in [since-window, ∞) — this is
// the second opinion the executor uses before hard-failing read-before-edit.
func TestReadEditCountsGraceWindow(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 6, 13, 12, 0, 0, 0, time.UTC)

	records := []ToolCall{
		// Race: Read logged under a DIFFERENT task ref (active ref hadn't switched
		// yet), timestamp just before the new task's StartedAt (= now).
		{ToolName: "Read", TaskRef: "previous-task", Timestamp: now.Add(-2 * time.Second)},
		// Outside window: too old, must not count.
		{ToolName: "Read", TaskRef: "previous-task", Timestamp: now.Add(-10 * time.Minute)},
		// Edit in window — must NOT count as a read.
		{ToolName: "Edit", TaskRef: "any", Timestamp: now.Add(-1 * time.Second)},
	}
	for i, r := range records {
		rr := r
		if err := Record(dir, &rr); err != nil {
			t.Fatalf("record %d: %v", i, err)
		}
	}

	// since=now (new task's StartedAt), window=60s: captures only the -2s
	// cross-task Read → 1. This is the race scenario ReadEditCounts misses.
	reads, err := ReadEditCountsGraceWindow(dir, now, 60*time.Second)
	if err != nil {
		t.Fatalf("ReadEditCountsGraceWindow: %v", err)
	}
	if reads != 1 {
		t.Fatalf("got grace reads=%d, want 1 (only the in-window cross-task Read)", reads)
	}

	// Empty dir: no error, 0 reads.
	reads, err = ReadEditCountsGraceWindow(t.TempDir(), now, 60*time.Second)
	if err != nil || reads != 0 {
		t.Fatalf("empty dir: got reads=%d err=%v, want 0/nil", reads, err)
	}
}
