package hookdispatch

import (
	"testing"

	"github.com/MjxUpUp/Forge/internal/checklog"
	"github.com/MjxUpUp/Forge/internal/taskpipeline"
)

// hook_tree_guard_record_test.go —— §5 留痕契约：错树拦截与「关守护」都必须落 checklog
// 行，且各自按会话+任务节流（guard 每次 Write|Edit 都跑，不节流就是日志洪水）。
//
// The audit contract of docs/design/task-tree-guard.md §5: an off-tree block and a
// guard-disabled escape both leave a deterministic row, throttled per session+task.

func countRows(t *testing.T, root string, want checklog.CheckName) int {
	t.Helper()
	all, err := checklog.LoadAll(root)
	if err != nil {
		t.Fatalf("LoadAll: %v", err)
	}
	n := 0
	for _, e := range all {
		if e.Check == want {
			n++
		}
	}
	return n
}

func TestRecordTreeGuardEscapesAreThrottled(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("FORGE_DATA_HOME", t.TempDir())

	recordTreeGuardEscape(dir, "feat/x", "sess-1")
	recordTreeGuardEscape(dir, "feat/x", "sess-1")
	if got := countRows(t, dir, checklog.CheckEscapeHatch); got != 1 {
		t.Fatalf("escape-hatch 行必须节流成 1 条，实际 %d", got)
	}

	all, err := checklog.LoadAll(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range all {
		if e.Check != checklog.CheckEscapeHatch {
			continue
		}
		if e.Meta["escape.gate"] != "task-tree-guard" || e.Meta["escape.reason"] != checklog.EscapeReasonEnv {
			t.Fatalf("逃生舱行必须自带 gate/reason，实际 Meta=%v", e.Meta)
		}
		if e.Level != checklog.LevelWarn || !e.Passed || !e.Checked {
			t.Fatalf("逃生舱行形态不对：Passed=%v Checked=%v Level=%v", e.Passed, e.Checked, e.Level)
		}
		if e.SessionID != "sess-1" || e.TaskRef != "feat/x" {
			t.Fatalf("逃生舱行必须带会话与任务：%+v", e)
		}
	}

	// 换一个会话 → 允许再记一条（节流键含 sid）。
	recordTreeGuardEscape(dir, "feat/x", "sess-2")
	if got := countRows(t, dir, checklog.CheckEscapeHatch); got != 2 {
		t.Fatalf("不同会话应各记一条，实际 %d", got)
	}
}

func TestRecordTreeGuardBlockThrottled(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("FORGE_DATA_HOME", t.TempDir())

	recordTreeGuardBlock(dir, "feat/y", "sess-1", "/tmp/other-tree")
	recordTreeGuardBlock(dir, "feat/y", "sess-1", "/tmp/other-tree")
	if got := countRows(t, dir, checklog.CheckTreeGuard); got != 1 {
		t.Fatalf("tree-guard 行必须节流成 1 条，实际 %d", got)
	}

	all, err := checklog.LoadAll(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range all {
		if e.Check != checklog.CheckTreeGuard {
			continue
		}
		if e.Passed {
			t.Fatal("错树拦截行必须 Passed=false")
		}
		if e.Meta["tree_guard"] != "off-tree" || e.Meta["anchor"] != "/tmp/other-tree" {
			t.Fatalf("tree-guard 行缺锚/类型：%v", e.Meta)
		}
		if e.Meta["layer"] != "task-guard" || e.Meta["resolve_path"] != "active-file-offtree" {
			t.Fatalf("tree-guard 行缺层/resolve_path：%v", e.Meta)
		}
		if e.Meta["wtid"] == "" {
			t.Fatalf("tree-guard 行必须带当前树 wtid（与锚路径一起才能定位错树）：%v", e.Meta)
		}
	}
}

// TestTreeGuardApplies pins the scope of the task-tree verdict: only calls that carry a
// file path need it. Without this, every Read/Bash/PostToolUse hook call would re-print
// the dead-anchor notice and re-touch the throttle markers for one edit.
func TestTreeGuardApplies(t *testing.T) {
	state := &taskpipeline.TaskState{TaskRef: "feat/x"}
	cases := []struct {
		name     string
		state    *taskpipeline.TaskState
		root     string
		filePath string
		want     bool
	}{
		{"无活跃任务", nil, "/repo", "internal/a.go", false},
		{"无仓库根", state, "", "internal/a.go", false},
		{"Read/Bash 等无文件路径的调用", state, "/repo", "", false},
		{"带文件路径的编辑调用", state, "/repo", "internal/a.go", true},
	}
	for _, c := range cases {
		if got := treeGuardApplies(c.state, c.root, c.filePath); got != c.want {
			t.Errorf("%s: treeGuardApplies=%v want %v", c.name, got, c.want)
		}
	}
}
