package toolusage

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// TestRecord_WorktreeSharesMainDataDir pins the store-layer half of finding
// fdkrxk4e9u3gg: a ToolCall recorded with the worktree root as `root` must be
// visible to LoadForTask called with the MAIN checkout root — both resolve to
// the same user-level DataDir (one repo one key), so tool activity performed
// inside a linked worktree is attributed to the same project the gates read.
//
// TestRecord_WorktreeSharesMainDataDir 钉住 finding fdkrxk4e9u3gg 的 store 层
// 半边：以 worktree 根为 `root` 记录的 ToolCall，必须能被以主 checkout 根调用
// 的 LoadForTask 读到——两者解析到同一个用户级 DataDir（一 repo 一 key），
// 使 linked worktree 内的工具活动归属到门禁读取的同一项目。
func TestRecord_WorktreeSharesMainDataDir(t *testing.T) {
	if err := exec.Command("git", "--version").Run(); err != nil {
		t.Fatalf("git unavailable, cannot build real worktree: %v", err)
	}
	t.Setenv("FORGE_DATA_HOME", t.TempDir())
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

	if err := Record(wt, &ToolCall{ToolName: "Read", TaskRef: "WT"}); err != nil {
		t.Fatalf("Record(worktree root): %v", err)
	}
	calls, err := LoadForTask(main, "WT")
	if err != nil {
		t.Fatalf("LoadForTask(main root): %v", err)
	}
	if len(calls) != 1 || calls[0].ToolName != "Read" {
		t.Fatalf("LoadForTask(main) = %+v, want the 1 Read recorded from the worktree", calls)
	}
	// Symmetric direction: main-root record visible from the worktree root.
	//
	// 对称方向：主根记录从 worktree 根可见。
	if err := Record(main, &ToolCall{ToolName: "Edit", TaskRef: "WT"}); err != nil {
		t.Fatalf("Record(main root): %v", err)
	}
	calls, err = LoadForTask(wt, "WT")
	if err != nil {
		t.Fatalf("LoadForTask(worktree root): %v", err)
	}
	if len(calls) != 2 {
		t.Fatalf("LoadForTask(worktree) = %d calls, want 2 (Read from worktree + Edit from main)", len(calls))
	}
}
