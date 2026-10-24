package taskpipeline

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/MjxUpUp/Forge/internal/forgedata"
	"github.com/MjxUpUp/Forge/internal/worktree"
)

// treeRepos 造 main 检出 + 两个 linked worktree，并钉住「linked worktree 与主检出
// 共享 DataDir」这一前提——错树锚能跨树可见的依据。
func treeRepos(t *testing.T) (main, wt1, wt2 string) {
	t.Helper()
	base := t.TempDir()
	t.Setenv("FORGE_DATA_HOME", filepath.Join(base, "data"))
	main = filepath.Join(base, "repo")
	if err := os.MkdirAll(main, 0o755); err != nil {
		t.Fatal(err)
	}
	git := func(dir string, args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %s: %v", args, string(out), err)
		}
	}
	git(main, "init")
	git(main, "config", "user.email", "t@t.com")
	git(main, "config", "user.name", "T")
	if err := os.WriteFile(filepath.Join(main, "a.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git(main, "add", ".")
	git(main, "commit", "-m", "init")
	wt1 = filepath.Join(base, "wt1")
	wt2 = filepath.Join(base, "wt2")
	git(main, "worktree", "add", "-b", "feat/tg", wt1)
	git(main, "worktree", "add", "-b", "feat/other", wt2)
	if forgedata.DataDirFor(main) != forgedata.DataDirFor(wt1) {
		t.Fatalf("前提不成立：linked worktree 未共享 DataDir")
	}
	return main, wt1, wt2
}

// TestTaskTreeStatus_States 钉住错树判定四态（docs/design/task-tree-guard.md §2）：
// ok（无任务/无锚）、ok-anchored（锚就在本树）、off-tree（锚在别处→阻断）、
// dead-anchor（锚目录已删→放行）。判据只看 binding 锚，不比分支名。
func TestTaskTreeStatus_States(t *testing.T) {
	main, wt1, wt2 := treeRepos(t)

	if got := TaskTreeStatus(main, ""); got.State != TreeOK {
		t.Fatalf("无任务必须 ok，got %+v", got)
	}

	if err := worktree.BindTask(main, "feat/tg", "feat/tg", "sess-1"); err != nil {
		t.Fatal(err)
	}
	if got := TaskTreeStatus(main, "feat/tg"); got.State != TreeOKAnchored {
		t.Fatalf("锚在本树必须 ok-anchored，got %+v", got)
	}

	// 多锚：同一任务在两棵树都有活锚时，任一命中即放行（不在场的树不能阻断在场的树）。
	if err := worktree.BindTask(wt1, "feat/tg", "feat/tg", "sess-1b"); err != nil {
		t.Fatal(err)
	}
	if got := TaskTreeStatus(wt1, "feat/tg"); got.State != TreeOKAnchored {
		t.Fatalf("多锚时任一本树命中必须 ok-anchored，got %+v", got)
	}

	absMain, _ := filepath.Abs(main)
	if got := TaskTreeStatus(wt2, "feat/tg"); got.State != TreeOffTree {
		t.Fatalf("锚在他树必须 off-tree，got %+v", got)
	} else if !samePath(got.AnchorPath, absMain) {
		t.Fatalf("off-tree 必须指出锚路径 %s，got %s", absMain, got.AnchorPath)
	}

	// 锚所在树消失 → dead-anchor（放行 + advisory），不能因一个已删 worktree 永久阻断。
	if err := worktree.BindTask(wt1, "feat/dead", "feat/dead", "sess-2"); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(wt1); err != nil {
		t.Fatal(err)
	}
	if got := TaskTreeStatus(main, "feat/dead"); got.State != TreeDeadAnchor {
		t.Fatalf("锚目录已删必须 dead-anchor，got %+v", got)
	}
}

// samePath 比较两路径是否指向同一位置（大小写/短路径归一）。
func samePath(a, b string) bool {
	pa, errA := filepath.Abs(a)
	pb, errB := filepath.Abs(b)
	if errA != nil || errB != nil {
		return a == b
	}
	return forgedata.CanonicalCase(pa) == forgedata.CanonicalCase(pb)
}

// TestTreeGuardDisabled_Env 钉住逃生舱是显式环境变量而非隐式行为。
func TestTreeGuardDisabled_Env(t *testing.T) {
	t.Setenv("FORGE_TASK_TREE_GUARD", "")
	if TreeGuardDisabled() {
		t.Fatal("未设置时必须启用守护")
	}
	t.Setenv("FORGE_TASK_TREE_GUARD", "1")
	if TreeGuardDisabled() {
		t.Fatal("只有 0 才关闭守护")
	}
	t.Setenv("FORGE_TASK_TREE_GUARD", "0")
	if !TreeGuardDisabled() {
		t.Fatal("FORGE_TASK_TREE_GUARD=0 必须关闭守护")
	}
}
