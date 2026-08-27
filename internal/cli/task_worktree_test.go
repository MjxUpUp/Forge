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

// TestTaskFinish_MergeTargetGuard pins the B2 fix (review BLOCKER): finish refuses when
// the main checkout is NOT on the merge target (bare `git merge` would silently merge
// the task branch into the wrong branch while the output claims the target), and a
// main-checkout binding never goes through the merge/remove path.
//
// TestTaskFinish_MergeTargetGuard 钉住 B2 修正（review BLOCKER）：主检出不在合并
// 目标上时 finish 拒绝（裸 git merge 会把任务分支默默合进错误分支、输出却声称目标
// 分支）；主检出绑定绝不走 merge/remove 路径。
func TestTaskFinish_MergeTargetGuard(t *testing.T) {
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

	// 建 worktree 任务并手工 complete（任务已完成但门禁走不了 verify——直接标记）。
	stdout, stderr, code := runForge(t, tmpDir, "task", "start", "--worktree", "--ref", "feat/finish-probe", "--title", "fp")
	if code != 0 {
		t.Fatalf("start --worktree: %s %s", stdout, stderr)
	}
	wtPath := filepath.Join(filepath.Dir(tmpDir), filepath.Base(tmpDir)+"-wt", "feat-finish-probe")
	// 在 worktree 提交一个变更并手工 complete 任务（门禁层由单元测试覆盖，这里测 finish 的合并守卫）。
	os.WriteFile(filepath.Join(wtPath, "wip.go"), []byte("package main\n"), 0644)
	runGit(t, wtPath, "add", ".")
	runGit(t, wtPath, "commit", "-m", "wip")
	if _, _, code := runForge(t, tmpDir, "task", "abort", "--ref", "feat/finish-probe"); code != 0 {
		// abort 会解绑——需要重新绑定来测 finish。改用直接操作 state 太绕，用 complete 失败预期：
		// finish 要求 CompletedAt 非空。用 generic 任务规避门禁。
		t.Fatalf("setup abort failed")
	}

	// 重开：generic 任务（complete 不评分、无门禁）+ worktree + complete。
	stdout, stderr, code = runForge(t, tmpDir, "task", "start", "--worktree", "--ref", "feat/finish-g", "--title", "g", "--kind", "generic")
	if code != 0 {
		t.Fatalf("start generic --worktree: %s %s", stdout, stderr)
	}
	wtPath = filepath.Join(filepath.Dir(tmpDir), filepath.Base(tmpDir)+"-wt", "feat-finish-g")
	os.WriteFile(filepath.Join(wtPath, "g.go"), []byte("package main\n"), 0644)
	runGit(t, wtPath, "add", ".")
	runGit(t, wtPath, "commit", "-m", "g")
	if stdout, _, code := runForge(t, wtPath, "task", "complete"); code != 0 {
		t.Fatalf("generic complete: %s", stdout)
	}

	// 主检出切到别的分支 → finish 必须拒绝（不合并）。
	runGit(t, tmpDir, "checkout", "-b", "feat/elsewhere")
	stdout, stderr, code = runForge(t, wtPath, "task", "finish")
	if code == 0 {
		t.Fatalf("B2 回归：主检出不在目标分支上 finish 应拒绝: %s", stdout)
	}
	if !strings.Contains(stdout+stderr, "不在合并目标") {
		t.Errorf("拒绝文案应点出分支错配, got: %s %s", stdout, stderr)
	}
	// 任务分支不得被合并进错误分支（HEAD 应仍是初始化提交）。
	log := wtGitOut(t, tmpDir, "log", "--oneline", "-1", "feat/elsewhere")
	if !strings.Contains(log, "init") {
		t.Errorf("任务分支被误合并: %s", log)
	}

	// 切回主线后 finish 成功：合并 + 清理 + 解绑。
	runGit(t, tmpDir, "checkout", "main")
	if stdout, _, code := runForge(t, wtPath, "task", "finish"); code != 0 {
		t.Fatalf("主线 finish: %s", stdout)
	}
	if _, err := os.Stat(wtPath); !os.IsNotExist(err) {
		t.Errorf("finish 后 worktree 应已清理: %v", err)
	}
	if b := worktree.Load(wtPath); b != nil {
		t.Errorf("finish 后绑定应解绑: %+v", b)
	}
}
