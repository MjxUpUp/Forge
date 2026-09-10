package taskpipeline

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/MjxUpUp/Forge/internal/checklog"
	"github.com/MjxUpUp/Forge/internal/toolusage"
	"github.com/MjxUpUp/Forge/internal/worktree"
)

// Attribution freeze (docs/design/harness-fixes-a-g-2026-09.md E): completing a task clears
// every session pointer and workspace binding that names it; audit rows backfill the session
// and carry the resolve_path / post_seal probe; efficiency uses the toollog active span
// instead of the wall clock.
//
// 归因冻结（docs/design/harness-fixes-a-g-2026-09.md E）：完成时清掉所有指向该任务的
// 会话指针 + workspace 绑定；审计行回填 session 并打 resolve_path / post_seal 探针；
// efficiency 用 toollog 活跃跨度替代挂钟。

func sealedState(ref string, t0 time.Time) *TaskState {
	s := &TaskState{TaskRef: ref, Branch: ref, StartedAt: t0, SessionID: "sess-owner"}
	for i, g := range []string{GateImplement, GateVerify, GateComplete} {
		s.RecordGateResult(g, true, "")
		s.History[i].CompletedAt = t0.Add(time.Duration(i+1) * 10 * time.Minute)
	}
	return s
}

func TestClearActiveTaskRefsForTask_ClearsAllPointersAndBinding(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, ".forge", "tasks"), 0755)
	saveIncompleteStateFor(t, dir, "feat/a")
	saveIncompleteStateFor(t, dir, "feat/b")
	for _, sid := range []string{"sess-1", "sess-2", ""} {
		if err := SetActiveTaskRef(dir, sid, "feat/a"); err != nil {
			t.Fatal(err)
		}
	}
	if err := SetActiveTaskRef(dir, "sess-3", "feat/b"); err != nil {
		t.Fatal(err)
	}
	if err := worktree.BindTask(dir, "feat/a", "feat/a", "sess-1"); err != nil {
		t.Fatal(err)
	}

	if err := ClearActiveTaskRefsForTask(dir, "feat/a"); err != nil {
		t.Fatalf("ClearActiveTaskRefsForTask: %v", err)
	}
	for _, sid := range []string{"sess-1", "sess-2", ""} {
		if got := ReadActiveTaskRef(dir, sid); got != "" {
			t.Errorf("pointer for session %q still = %q after clear", sid, got)
		}
	}
	if got := ReadActiveTaskRef(dir, "sess-3"); got != "feat/b" {
		t.Errorf("other task's pointer must survive, got %q", got)
	}
	if b := worktree.Load(dir); b != nil && b.TaskRef == "feat/a" {
		t.Errorf("workspace binding to feat/a must be cleared, got %+v", b)
	}
}

func TestActiveTaskStateWithPath_LabelsResolutionPath(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, ".forge", "tasks"), 0755)
	saveIncompleteStateFor(t, dir, "feat/a")

	if _, path, _ := ActiveTaskStateWithPath(dir, "sess-none"); path != "" {
		t.Fatalf("no pointer: path = %q, want empty", path)
	}
	if err := SetActiveTaskRef(dir, "sess-A", "feat/a"); err != nil {
		t.Fatal(err)
	}
	st, path, err := ActiveTaskStateWithPath(dir, "sess-A")
	if err != nil || st == nil || st.TaskRef != "feat/a" {
		t.Fatalf("session pointer resolve failed: %v %v", st, err)
	}
	if path != ResolvePathActiveFile {
		t.Fatalf("path = %q, want %q", path, ResolvePathActiveFile)
	}
	// workspace 绑定路径：无本会话指针、有目录绑定
	if err := worktree.BindTask(dir, "feat/a", "feat/a", "sess-A"); err != nil {
		t.Fatal(err)
	}
	_, path, _ = ActiveTaskStateWithPath(dir, "sess-B")
	if path != ResolvePathWorkspace {
		t.Fatalf("workspace path = %q, want %q", path, ResolvePathWorkspace)
	}
}

func TestRecordAudit_BackfillsSessionAndMarksPostSeal(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, ".forge", "tasks"), 0755)
	t0 := time.Now().Add(-2 * time.Hour)

	open := &TaskState{TaskRef: "feat/open", Branch: "feat/open", StartedAt: t0, SessionID: "sess-open"}
	if err := SaveTaskState(dir, open); err != nil {
		t.Fatal(err)
	}
	recordAudit(dir, &checklog.Entry{Check: checklog.CheckTaskVerify, Passed: true, Checked: true, TaskRef: "feat/open", Detail: "verify"})
	rows, err := checklog.LoadForTask(dir, "feat/open")
	if err != nil || len(rows) != 1 {
		t.Fatalf("load: %v n=%d", err, len(rows))
	}
	if rows[0].SessionID != "sess-open" {
		t.Errorf("SessionID not backfilled from task state: %q", rows[0].SessionID)
	}
	if _, ok := rows[0].Meta[checklog.MetaKeyPostSeal]; ok {
		t.Errorf("open task must not carry post_seal meta: %v", rows[0].Meta)
	}

	sealed := sealedState("feat/sealed", t0)
	if err := SaveTaskState(dir, sealed); err != nil {
		t.Fatal(err)
	}
	recordAudit(dir, &checklog.Entry{Check: checklog.CheckTaskVerify, Passed: true, Checked: true, TaskRef: "feat/sealed", SessionID: "sess-other", Detail: "re-verify after seal"})
	rows, _ = checklog.LoadForTask(dir, "feat/sealed")
	if len(rows) != 1 {
		t.Fatalf("sealed task row count = %d, want 1 (rows are kept for trace, flagged not dropped)", len(rows))
	}
	if rows[0].SessionID != "sess-other" {
		t.Errorf("explicit SessionID must be preserved, got %q", rows[0].SessionID)
	}
	if rows[0].Meta[checklog.MetaKeyPostSeal] != "true" {
		t.Errorf("post-seal row must be flagged, meta = %v", rows[0].Meta)
	}
}

func TestActiveSpanFromCalls(t *testing.T) {
	t0 := time.Date(2026, 9, 7, 22, 50, 0, 0, time.UTC)
	calls := []toolusage.ToolCall{
		{ToolName: "Read", Timestamp: t0.Add(-5 * time.Minute)}, // task start 之前：不计
		{ToolName: "Edit", Timestamp: t0.Add(2 * time.Minute)},
		{ToolName: "Bash", Timestamp: t0.Add(40 * time.Minute)},
		{ToolName: "Bash", Timestamp: t0.Add(30 * time.Hour)}, // 封印之后：不计
	}
	if got := activeSpanFromCalls(calls, t0, t0.Add(time.Hour)); got != 38*time.Minute {
		t.Fatalf("active span = %v, want 38m", got)
	}
	if got := activeSpanFromCalls(calls[:1], t0, t0.Add(time.Hour)); got != 0 {
		t.Fatalf("fewer than 2 in-window calls must yield 0 (fallback to wall clock), got %v", got)
	}
	if got := activeSpanFromCalls(calls, t0, time.Time{}); got != 30*time.Hour-2*time.Minute {
		t.Fatalf("zero until = unbounded upper, got %v", got)
	}
}
