package clitask

import (
	"strings"
	"testing"
)

// task_entry_test.go —— 入口路由 A1：声明了 spec 的仓库里，功能改动必须先立 change；
// --kind task 是一句话小修的显式出口。

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

func TestTaskStart_RequiresChangeOrKindTask(t *testing.T) {
	dir := entryRepo(t)
	stdout, stderr, code := runForgeStreams(t, dir, "task", "start", "--ref", "feat/entry", "--title", "功能改动")
	if code == 0 {
		t.Fatalf("task start without change must fail in a spec repo, stdout=%s", stdout)
	}
	combined := stdout + stderr
	if !strings.Contains(combined, "先立 change") || !strings.Contains(combined, "--kind task") {
		t.Fatalf("error should point at change new and the --kind task escape: %s", combined)
	}
}

func TestTaskStart_ExplicitKindTaskAllowed(t *testing.T) {
	dir := entryRepo(t)
	stdout, stderr, code := runForgeStreams(t, dir, "task", "start", "--ref", "feat/entry", "--title", "小修", "--kind", "task")
	if code != 0 {
		t.Fatalf("--kind task should pass in a spec repo: %s%s", stdout, stderr)
	}
	runForge(t, dir, "task", "abort", "--ref", "feat/entry")
}

func TestGuardEntryRouting_PhaseMustBeReady(t *testing.T) {
	dir := entryRepo(t)
	if out, _, code := runForge(t, dir, "change", "new", "entry-demo", "--title", "demo"); code != 0 {
		t.Fatalf("change new failed: %s", out)
	}
	// 新 change 的 goal 还是脚手架 → 不能开工。
	err := guardEntryRouting(dir, "", "entry-demo")
	if err == nil || !strings.Contains(err.Error(), "specify") {
		t.Fatalf("goal not filled: want specify error, got %v", err)
	}
	// 非 spec 仓库不受影响。
	plain := t.TempDir()
	runGit(t, plain, "init")
	if err := guardEntryRouting(plain, "", ""); err != nil {
		t.Fatalf("plain repo must not be gated: %v", err)
	}
}