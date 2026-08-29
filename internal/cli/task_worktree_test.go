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

// wtSetupProject stands up the shared worktree-test project: isolated env
// (claude session cleared), fresh FORGE_DATA_HOME, git repo on branch main
// with an initial commit ("init"), forge init, and a tracked main.go.
//
// wtSetupProject 搭建 worktree 测试共享的项目：隔离 env（清 claude session）、
// 全新 FORGE_DATA_HOME、main 分支的 git 仓 + 初始提交（"init"）、forge init
// 与被跟踪的 main.go。
func wtSetupProject(t *testing.T) string {
	t.Helper()
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
	return tmpDir
}

// TestTaskStart_WorktreeE2E is the L4 happy path (multi-task-concurrency §7): one command
// atomically produces worktree + branch + task + binding; the binding anchors the NEW
// path so a fresh window there resolves the task (T4's contract, now created by T5).
//
// TestTaskStart_WorktreeE2E 是 L4 的 happy path（multi-task-concurrency §7）：一条命令
// 原子地产出 worktree + 分支 + 任务 + 绑定；绑定锚定【新】路径，使那边的新窗口解析
// 到任务（T4 的契约，由 T5 创建）。
func TestTaskStart_WorktreeE2E(t *testing.T) {
	tmpDir := wtSetupProject(t)

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
	tmpDir := wtSetupProject(t)

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
	// 绑定残留断言（#4 二次修订）：必须扫 workspaces 目录而非 Load(已消失路径)
	//——消失路径的 DataDirFor 身份推导漂移，Load 恒 nil，旧断言空转掩盖了真残留
	//（2026-08-27 dogfood 实录：CI 绿但本机残留）。
	wsDir := filepath.Join(forgedata.DataDirFor(tmpDir), "workspaces")
	if entries, err := os.ReadDir(wsDir); err == nil && len(entries) > 0 {
		for _, e := range entries {
			data, _ := os.ReadFile(filepath.Join(wsDir, e.Name()))
			t.Errorf("finish 后绑定文件残留: %s → %s", e.Name(), strings.TrimSpace(string(data)))
		}
	}
}

// TestTaskStart_BranchDerivation pins dogfood finding #6 (2026-08-28, conventions-
// profile session audit): `task start --branch` must share the --worktree ref→branch
// derivation — a non-conventional ref (conventions-profile) derives feat/<dashed>
// instead of being refused.
//
// TestTaskStart_BranchDerivation 钉住 dogfood 发现 #6（2026-08-28，
// conventions-profile 会话审计）：`task start --branch` 须与 --worktree 共享
// ref→分支派生——非惯例 ref（conventions-profile）派生 feat/<连字> 而非被拒。
func TestTaskStart_BranchDerivation(t *testing.T) {
	tmpDir := wtSetupProject(t)

	// 非惯例前缀 ref + --branch：派生 feat/conventions-profile，不再被前缀校验拒绝。
	stdout, _, code := runForge(t, tmpDir, "task", "start", "--ref", "conventions-profile", "--branch", "--title", "cp")
	if code != 0 {
		t.Fatalf("非惯例 ref + --branch 应派生成功（#6 回归）: %s", stdout)
	}
	if got := strings.TrimSpace(wtGitOut(t, tmpDir, "rev-parse", "--abbrev-ref", "HEAD")); got != "feat/conventions-profile" {
		t.Fatalf("派生分支应为 feat/conventions-profile, got %q", got)
	}
}

// TestDeriveBranchName pins the SINGLE ref→branch derivation now shared by the
// --worktree entry (createTaskWorktree delegates to it since the inline copy
// was deleted, fix/cleanup-batch 2026-08-29 — dogfood #6 class: two copies of
// the rule drift) and the --branch entry: a conventionally-prefixed ref maps
// to itself; anything else derives feat/<slashes→dashes>. The only failing
// input class is a ref whose derived name validateBranchRef rejects (e.g. a
// bare "feat/" — prefix present but nothing after it).
//
// TestDeriveBranchName 钉住 --worktree 入口（createTaskWorktree 自内联副本删除后
// 委托给它，fix/cleanup-batch 2026-08-29——dogfood #6 类：规则两份副本必漂移）与
// --branch 入口共享的【单一】ref→分支派生：带惯例前缀的 ref 同名；其余派生
// feat/<斜杠转连字>。唯一失败类是派生名被 validateBranchRef 拒绝的 ref（如裸
// "feat/"——前缀在场但后面为空）。
func TestDeriveBranchName(t *testing.T) {
	cases := []struct {
		ref   string
		want  string
		fails bool
	}{
		{`feat/login`, `feat/login`, false},                    // 已带惯例前缀 → 同名
		{`fix/crash`, `fix/crash`, false},                      // 同上（表内其他前缀）
		{`login/modal/split`, `feat/login-modal-split`, false}, // 无前缀 → feat/ + 斜杠转连字
		{`PROJ-123`, `feat/PROJ-123`, false},                   // 裸 ticket ref → 派生
		{`feat/`, ``, true},                                    // 前缀后为空 → validateBranchRef 拒绝
	}
	for _, c := range cases {
		got, err := deriveBranchName(c.ref)
		if c.fails {
			if err == nil {
				t.Errorf("deriveBranchName(%q) = %q, want error", c.ref, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("deriveBranchName(%q) unexpected error: %v", c.ref, err)
			continue
		}
		if got != c.want {
			t.Errorf("deriveBranchName(%q) = %q, want %q", c.ref, got, c.want)
		}
	}
}

// TestCreateTaskWorktreeDelegatesDerivation pins the delegation without needing
// a git repo: a ref whose derived name is unvalidatable must fail with
// deriveBranchName's error BEFORE any git interaction (worktreeBase etc.) —
// proving the inline derivation copy is gone and the shared derivation runs
// first (dogfood #6's root cause, closed at the second entry).
//
// TestCreateTaskWorktreeDelegatesDerivation 无需 git 仓即钉住委托：派生名不可校验的
// ref 必须先以 deriveBranchName 的错误失败、早于任何 git 交互（worktreeBase 等）
// ——证明内联派生副本已删、共享派生先行（dogfood #6 的根因在第二入口被关死）。
func TestCreateTaskWorktreeDelegatesDerivation(t *testing.T) {
	root := t.TempDir()
	_, err := createTaskWorktree(root, `feat/`, ``, "")
	if err == nil {
		t.Fatal("createTaskWorktree with bare feat/ ref should fail (empty tail)")
	}
	if want := `invalid derived branch`; !strings.Contains(err.Error(), want) {
		t.Errorf("err = %v, want it to carry deriveBranchName's %q context (delegation proof)", err, want)
	}
}

// TestCopyWorktreeIncludesWarnsOnFailure pins the failure-accountability fix
// (fix/cleanup-batch, 2026-08-29): per-file copy failures no longer vanish —
// they are accumulated and surfaced as ONE stderr warning listing each failed
// include, while successful copies still land and the walk never fails the
// start. The failure is induced by pre-creating the destination as a read-only
// file (Windows FILE_ATTRIBUTE_READONLY / Unix 0444 — OpenFile for write is
// refused either way).
//
// TestCopyWorktreeIncludesWarnsOnFailure 钉住失败可问责修复（fix/cleanup-batch，
// 2026-08-29）：逐文件复制失败不再静默蒸发——累积后以一条 stderr 警告列出每个
// 失败的 include，成功的照常落盘，遍历绝不中断 start。失败以「目标预置为只读
// 文件」诱发（Windows FILE_ATTRIBUTE_READONLY / Unix 0444——两种系统都拒写打开）。
func TestCopyWorktreeIncludesWarnsOnFailure(t *testing.T) {
	src := t.TempDir()
	dst := t.TempDir()

	if err := os.WriteFile(filepath.Join(src, "forge.worktreeinclude"), []byte(".env\nlocked.env\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, ".env"), []byte("OK=1\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "locked.env"), []byte("SECRET=1\n"), 0644); err != nil {
		t.Fatal(err)
	}
	// Pre-create the destination as a READ-ONLY file: the copy's OpenFile for
	// write is refused → the include fails and must be reported.
	//
	// 目标预置为只读文件：复制的 OpenFile 写打开被拒 → 该 include 失败且必须上报。
	lockedDst := filepath.Join(dst, "locked.env")
	if err := os.WriteFile(lockedDst, []byte("placeholder\n"), 0444); err != nil {
		// Some sandboxes cannot create read-only files; skip rather than fail.
		//
		// 部分沙箱无法创建只读文件；跳过而非失败。
		t.Skipf("cannot create read-only destination: %v", err)
	}
	defer os.Chmod(lockedDst, 0644)

	warn := captureStderr(t, func() { copyWorktreeIncludes(src, dst) })

	// The good include still copied.
	//
	// 好的 include 照常复制。
	if data, err := os.ReadFile(filepath.Join(dst, ".env")); err != nil || string(data) != "OK=1\n" {
		t.Errorf("non-failing include should copy, got %q err=%v", string(data), err)
	}
	// The failing include is named in the warning — the user can fix the cause
	// BEFORE the first session inside the worktree trips over the missing file.
	//
	// 失败的 include 在警告中点名——用户能在首个会话踩坑前修复根因。
	if !strings.Contains(warn, "locked.env") {
		t.Errorf("warning should name the failed include, got: %q", warn)
	}
	if !strings.Contains(warn, "1 个") {
		t.Errorf("warning should report the failure count, got: %q", warn)
	}
}
