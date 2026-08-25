package forgedata

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// TestKey_RealGitWorktreeSharesMain pins the worktree identity contract against a
// REAL `git worktree add` (the existing worktree tests hand-construct the .git
// file layout): Key() from a linked worktree must equal Key() from the main
// checkout, so runtime state (toollog/checklog/tasks) recorded while working
// inside a worktree lands in the same DataDir the gates read. Regression pin for
// finding fdkrxk4e9u3gg (work-activity reported 0 tool uses inside a worktree):
// the identity layer is where such a split would come from.
//
// TestKey_RealGitWorktreeSharesMain 用真实 `git worktree add` 钉住 worktree 身份
// 契约（既有 worktree 测试是手工构造 .git file 布局）：linked worktree 里的
// Key() 必须等于主 checkout 的 Key()，使 worktree 内工作时记录的 runtime state
// （toollog/checklog/tasks）落进门禁读取的同一 DataDir。finding fdkrxk4e9u3gg
// （worktree 内 work-activity 报 0 tool uses）的回归钉——身份层正是此类分裂的
// 来源处。
func TestKey_RealGitWorktreeSharesMain(t *testing.T) {
	if err := exec.Command("git", "--version").Run(); err != nil {
		t.Fatalf("git unavailable, cannot build real worktree: %v", err)
	}
	main := t.TempDir()
	run := func(dir string, args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run(main, "init")
	run(main, "commit", "--allow-empty", "-m", "init")
	wt := filepath.Join(filepath.Dir(main), "wt-linked")
	run(main, "worktree", "add", "-b", "wt-branch", wt)

	kMain, err := Key(main)
	if err != nil {
		t.Fatalf("Key(main): %v", err)
	}
	kWT, err := Key(wt)
	if err != nil {
		t.Fatalf("Key(worktree): %v", err)
	}
	if kMain != kWT {
		t.Errorf("worktree Key=%s must share main Key=%s (one repo one key)", kWT, kMain)
	}
	// Same contract from a subdirectory inside the worktree (hooks fire with the
	// edited file's dir as cwd, not necessarily the worktree root).
	//
	// worktree 内子目录同契约（hook 以被编辑文件所在目录为 cwd 触发，不一定是
	// worktree 根）。
	sub := filepath.Join(wt, "sub", "dir")
	if err := os.MkdirAll(sub, 0755); err != nil {
		t.Fatal(err)
	}
	kSub, err := Key(sub)
	if err != nil {
		t.Fatalf("Key(worktree subdir): %v", err)
	}
	if kSub != kMain {
		t.Errorf("worktree subdir Key=%s must share main Key=%s", kSub, kMain)
	}
	// The DataDir is the actual write target of toollog/checklog — pin that the
	// worktree and the main checkout resolve to the same one.
	//
	// DataDir 是 toollog/checklog 的实际写入目标——钉住 worktree 与主 checkout
	// 解析到同一个。
	if dMain, dWT := DataDirFor(main), DataDirFor(wt); dMain != dWT {
		t.Errorf("DataDirFor mismatch: main=%s worktree=%s", dMain, dWT)
	}
}
