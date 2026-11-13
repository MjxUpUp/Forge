package worktree

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/MjxUpUp/Forge/internal/forgedata"
)

// runGit 跑 git 并失败即止（worktree 包首个测试的内联助手）。
func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %s: %v", args, string(out), err)
	}
}

// realWorktrees 造一个 main 检出 + 两个 linked worktree，并钉住「linked worktree
// 与主检出共享同一 DataDir」这一前提——错树锚能跨树可见的全部依据（forgedata
// 的 Key 经 git common dir 归一）。前提不成立就 Fatal，避免测试静默退化成单树。
func realWorktrees(t *testing.T) (main, wt1, wt2 string) {
	t.Helper()
	base := t.TempDir()
	t.Setenv("FORGE_DATA_HOME", filepath.Join(base, "data"))
	main = filepath.Join(base, "repo")
	if err := os.MkdirAll(main, 0o755); err != nil {
		t.Fatal(err)
	}
	runGit(t, main, "init")
	runGit(t, main, "config", "user.email", "t@t.com")
	runGit(t, main, "config", "user.name", "T")
	if err := os.WriteFile(filepath.Join(main, "a.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, main, "add", ".")
	runGit(t, main, "commit", "-m", "init")
	wt1 = filepath.Join(base, "wt1")
	wt2 = filepath.Join(base, "wt2")
	runGit(t, main, "worktree", "add", "-b", "feat/x", wt1)
	runGit(t, main, "worktree", "add", "-b", "feat/other", wt2)

	if forgedata.DataDirFor(main) != forgedata.DataDirFor(wt1) {
		t.Fatalf("前提不成立：linked worktree 未共享 DataDir（%s vs %s）",
			forgedata.DataDirFor(main), forgedata.DataDirFor(wt1))
	}
	return main, wt1, wt2
}

// TestAnchors_ListsOnlyMatchingTask is the task-tree guard's data source
// (docs/design/task-tree-guard.md §3): anchors are per-wtid binding files in the
// project's shared DataDir, so the query must return exactly the bindings pointing at
// the task and ignore everything else.
//
// TestAnchors_ListsOnlyMatchingTask 是错树守护的数据源（docs/design/task-tree-guard.md
// §3）：锚是共享 DataDir 里按 wtid 分的绑定文件，查询必须只返回指向该任务的绑定、
// 忽略其余全部。
func TestAnchors_ListsOnlyMatchingTask(t *testing.T) {
	main, wt1, wt2 := realWorktrees(t)

	if err := BindTask(main, "feat/x", "feat/x", "sess-1"); err != nil {
		t.Fatal(err)
	}
	if err := BindTask(wt1, "feat/x", "feat/x", "sess-2"); err != nil {
		t.Fatal(err)
	}
	if err := BindTask(wt2, "feat/other", "feat/other", "sess-3"); err != nil {
		t.Fatal(err)
	}

	anchors := Anchors(main, "feat/x")
	if len(anchors) != 2 {
		t.Fatalf("期望 2 个锚（主检出 + wt1），实际 %d：%+v", len(anchors), anchors)
	}
	want := map[string]bool{}
	for _, p := range []string{main, wt1} {
		abs, _ := filepath.Abs(p)
		want[forgedata.CanonicalCase(abs)] = true
	}
	for _, a := range anchors {
		delete(want, a.Path)
	}
	if len(want) != 0 {
		t.Fatalf("锚集合缺 %v（实际 %+v）", want, anchors)
	}
	if got := Anchors(main, ""); got != nil {
		t.Errorf("空 taskRef 必须返回 nil（不猜任务），got %+v", got)
	}
	if got := Anchors(main, "feat/nobody"); got != nil {
		t.Errorf("无人绑定的任务必须返回 nil，got %+v", got)
	}
}

// TestAnchors_SkipsCorruptBinding pins the degradation direction: one unreadable binding
// file must not blind the guard for every other tree (skip, not fail).
//
// TestAnchors_SkipsCorruptBinding 钉住降级方向：一个读不出来的绑定文件不能把守护对其
// 他树的眼睛蒙住（跳过，而不是整体失败）。
func TestAnchors_SkipsCorruptBinding(t *testing.T) {
	main, wt1, _ := realWorktrees(t)
	if err := BindTask(main, "feat/x", "feat/x", "sess-1"); err != nil {
		t.Fatal(err)
	}
	if err := BindTask(wt1, "feat/x", "feat/x", "sess-2"); err != nil {
		t.Fatal(err)
	}
	dir := bindingDir(main)
	if err := os.WriteFile(filepath.Join(dir, "corrupt000000.json"), []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := Anchors(main, "feat/x"); len(got) != 2 {
		t.Fatalf("坏 JSON 必须被跳过而其余锚保留（期望 2），got %+v", got)
	}
}
