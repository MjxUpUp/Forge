package cli

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MjxUpUp/Forge/internal/forgedata"
	"github.com/MjxUpUp/Forge/internal/worktree"
)

func wtGitOut(t *testing.T, dir string, args ...string) string {
	t.Helper()
	out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).Output()
	if err != nil {
		t.Fatalf("git %v: %v", args, err)
	}
	return string(out)
}

// TestTaskStart_WorktreeE2E is the L4 happy path (multi-task-concurrency §7): one command
// atomically produces worktree + branch + task + binding; the binding anchors the NEW
// path so a fresh window there resolves the task (T4's contract, now created by T5).
//
// TestTaskStart_WorktreeE2E 是 L4 的 happy path（multi-task-concurrency §7）：一条命令
// 原子地产出 worktree + 分支 + 任务 + 绑定；绑定锚定【新】路径，使那边的新窗口解析
// 到任务（T4 的契约，由 T5 创建）。
func TestTaskStart_WorktreeE2E(t *testing.T) {
	t.Setenv("CLAUDE_CODE_SESSION_ID", "")
	t.Setenv("FORGE_DATA_HOME", t.TempDir())
	tmpDir := t.TempDir()
	runGit(t, tmpDir, "init", "-b", "main")
	runGit(t, tmpDir, "config", "user.email", "t@t.t")
	runGit(t, tmpDir, "config", "user.name", "t")
	if stdout, _, code := runForge(t, tmpDir, "init", "--mode", "medium"); code != 0 {
		t.Fatalf("forge init failed: %s", stdout)
	}
	os.WriteFile(filepath.Join(tmpDir, "main.go"), []byte("package main\n"), 0644)
	runGit(t, tmpDir, "add", ".")
	runGit(t, tmpDir, "commit", "-m", "init")

	stdout, stderr, code := runForge(t, tmpDir, "task", "start", "--worktree", "--ref", "feat/wt-probe", "--title", "wt probe")
	if code != 0 {
		t.Fatalf("task start --worktree failed: %s %s", stdout, stderr)
	}

	wtPath := filepath.Join(filepath.Dir(tmpDir), filepath.Base(tmpDir)+"-wt", "feat-wt-probe")
	if _, err := os.Stat(wtPath); err != nil {
		t.Fatalf("worktree 未创建于 %s: %v", wtPath, err)
	}
	if _, err := os.Stat(taskStatePath(tmpDir, "feat/wt-probe")); err != nil {
		t.Fatalf("任务状态未落共享 DataDir: %v", err)
	}
	b := worktree.Load(wtPath)
	if b == nil || b.TaskRef != "feat/wt-probe" {
		t.Fatalf("worktree 绑定未锚定新路径: %+v", b)
	}
	if got := strings.TrimSpace(wtGitOut(t, wtPath, "rev-parse", "--abbrev-ref", "HEAD")); got != "feat/wt-probe" {
		t.Fatalf("worktree 分支应为 feat/wt-probe, got %q", got)
	}

	// finish 拒绝未完成任务（门禁未过），且不动 worktree（宁留勿删）。
	stdout, stderr, code = runForge(t, tmpDir, "task", "finish")
	if code == 0 {
		t.Fatalf("未完成任务 finish 应非零退出: %s", stdout)
	}
	if !strings.Contains(stdout+stderr, "尚未 complete") {
		t.Errorf("finish 拒绝文案应说明门禁未过, got: %s %s", stdout, stderr)
	}
	if _, err := os.Stat(wtPath); err != nil {
		t.Fatalf("拒绝路径不得删除 worktree: %v", err)
	}
}

// TestWorktreeJanitor_DeadAnchor: bindings whose path vanished are dropped; live ones stay.
//
// TestWorktreeJanitor_DeadAnchor：路径消失的绑定被清除；存活的保留。
func TestWorktreeJanitor_DeadAnchor(t *testing.T) {
	t.Setenv("FORGE_DATA_HOME", t.TempDir())
	tmpDir := t.TempDir()
	runGit(t, tmpDir, "init", "-b", "main")
	runGit(t, tmpDir, "config", "user.email", "t@t.t")
	runGit(t, tmpDir, "config", "user.name", "t")
	runGit(t, tmpDir, "commit", "--allow-empty", "-m", "init")
	if stdout, _, code := runForge(t, tmpDir, "init", "--mode", "medium"); code != 0 {
		t.Fatalf("forge init failed: %s", stdout)
	}

	ghost := filepath.Join(t.TempDir(), "vanished-wt")
	if err := worktree.BindTask(ghost, "feat/ghost", "feat/ghost", "s"); err != nil {
		t.Fatal(err)
	}
	if err := worktree.BindTask(tmpDir, "feat/live", "feat/live", "s"); err != nil {
		t.Fatal(err)
	}

	if stdout, _, code := runForge(t, tmpDir, "worktree", "janitor"); code != 0 {
		t.Fatalf("janitor failed: %s", stdout)
	}
	ghostBinding := filepath.Join(forgedata.DataDirFor(tmpDir), "workspaces", worktree.ID(ghost)+".json")
	if _, err := os.Stat(ghostBinding); !os.IsNotExist(err) {
		t.Error("死锚绑定应被清除")
	}
	if worktree.Load(tmpDir) == nil {
		t.Error("存活绑定不得被清除")
	}
}
