package clitask

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// task_entry_test.go —— 入口路由（A1，2026-11-15 修订）：任务是一等入口，
// change 可以后补；只有显式 `--change` 时才要求那个 change 已 ready。

func entryRepo(t *testing.T) string {
	t.Helper()
	t.Setenv("FORGE_DATA_HOME", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	dir := t.TempDir()
	runGit(t, dir, "init")
	runGit(t, dir, "config", "user.email", "t@t.com")
	runGit(t, dir, "config", "user.name", "T")
	runGit(t, dir, "commit", "--allow-empty", "-m", "init")
	runGit(t, dir, "checkout", "-b", "feat/entry")
	if out, _, code := runForge(t, dir, "init"); code != 0 {
		t.Fatalf("forge init failed: %s", out)
	}
	return dir
}

// TestTaskStart_NoChangeAllowedInSpecRepo：声明 spec 的仓库也允许直接开工——
// 「先立 change」是建议，不是开工许可；任务是一等入口。
func TestTaskStart_NoChangeAllowedInSpecRepo(t *testing.T) {
	dir := entryRepo(t)
	stdout, stderr, code := runForgeStreams(t, dir, "task", "start", "--ref", "feat/entry", "--title", "功能改动")
	if code != 0 {
		t.Fatalf("task start without change must pass (task-first): %s%s", stdout, stderr)
	}
	runForge(t, dir, "task", "abort", "--ref", "feat/entry")
}

// TestTaskStart_ChangeMustBeReady：显式链接 change 时，它必须已经 ready；
// 否则钉进去的 goal 契约与阶段不符。
func TestTaskStart_ChangeMustBeReady(t *testing.T) {
	dir := entryRepo(t)
	if out, _, code := runForge(t, dir, "change", "new", "entry-demo", "--title", "demo"); code != 0 {
		t.Fatalf("change new failed: %s", out)
	}
	stdout, stderr, code := runForgeStreams(t, dir, "task", "start", "--ref", "feat/entry", "--title", "功能改动", "--change", "entry-demo")
	if code == 0 {
		t.Fatalf("linking an unready change must fail, stdout=%s", stdout)
	}
	combined := stdout + stderr
	if !strings.Contains(combined, "specify") || !strings.Contains(combined, "change phase") {
		t.Fatalf("error should name the phase and the phase command: %s", combined)
	}
	// --kind task 不能绕过显式链接检查（否则「小修」成了万能逃生口）。
	if out, _, code := runForge(t, dir, "task", "start", "--ref", "feat/entry2", "--title", "小修", "--kind", "task", "--change", "entry-demo"); code == 0 {
		t.Fatalf("--kind task must not bypass the ready check for an explicit --change: %s", out)
	}
}

// TestGuardEntryRouting_AllowsWithoutChange：没有 --change 一律放行（spec 与普通仓库同）。
func TestGuardEntryRouting_AllowsWithoutChange(t *testing.T) {
	spec := entryRepo(t)
	if err := guardEntryRouting(spec, ""); err != nil {
		t.Fatalf("spec repo without --change must pass: %v", err)
	}
	plain := t.TempDir()
	runGit(t, plain, "init")
	if err := guardEntryRouting(plain, ""); err != nil {
		t.Fatalf("plain repo must pass: %v", err)
	}
}

// --kind 的帮助面必须列出 task：它是「小修标记」的唯一显式入口说明。
func TestTaskStart_KindHelpListsTask(t *testing.T) {
	flag := taskStartCmd.Flags().Lookup("kind")
	if flag == nil {
		t.Fatal("task start 应有 --kind")
	}
	if !strings.Contains(flag.Usage, "task（小修标记") {
		t.Errorf("--kind 帮助未列出 task：%s", flag.Usage)
	}
}

// TestGuardEntryRouting_RequiresClarify：ready 只说明产物齐了；clarify 是 verify/gate
// 的硬前置，入口同一口径先拦——否则会先放行、后面 verify 才说缺 clarify（阶段分叉）。
func TestGuardEntryRouting_RequiresClarify(t *testing.T) {
	dir := entryRepo(t)
	if out, _, code := runForge(t, dir, "change", "new", "clarify-demo", "--title", "demo"); code != 0 {
		t.Fatalf("change new failed: %s", out)
	}
	goal := "# 目标：clarify-demo\n\nchange: `clarify-demo`\n\n## Objective\n\n验证入口对 clarify 的口径。\n\n## Out of Scope\n\n不改 verify。\n\n## Check\n\n- 缺 clarify 时 start 拒绝\n"
	if err := os.WriteFile(filepath.Join(dir, "spec", "changes", "clarify-demo", "goal.md"), []byte(goal), 0o644); err != nil {
		t.Fatal(err)
	}
	makeChangeFiles(t, dir, "clarify-demo")

	if err := guardEntryRouting(dir, "clarify-demo"); err != nil {
		t.Fatalf("产物齐 + clarify 齐应放行: %v", err)
	}
	clarify := filepath.Join(dir, "spec", "changes", "clarify-demo", "clarify.md")
	if err := os.Remove(clarify); err != nil {
		t.Fatal(err)
	}
	err := guardEntryRouting(dir, "clarify-demo")
	if err == nil || !strings.Contains(err.Error(), "clarify") {
		t.Fatalf("缺 clarify 时入口必须拒绝并点出 clarify（阶段停在 clarify，与 verify/gate 同口径）, got %v", err)
	}
}
