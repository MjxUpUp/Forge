package e2e

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// entry_routing_test.go —— 入口路由 A1/A2 的真实二进制链路：
// 脏树 → next 让先立 change → change new → next 让看 phase → 补齐 → task start 放行。

func entryRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	git(t, dir, "init", "-q")
	git(t, dir, "config", "user.email", "t@t.com")
	git(t, dir, "config", "user.name", "T")
	git(t, dir, "commit", "--allow-empty", "-q", "-m", "init")
	if err := os.WriteFile(filepath.Join(dir, "wip.txt"), []byte("work in progress\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	forge(t, dir, "init")
	return dir
}

func TestEntryRoutingChangeFirst(t *testing.T) {
	t.Setenv("FORGE_DATA_HOME", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	dir := entryRepo(t)

	out := forge(t, dir, "next", "--json")
	if !strings.Contains(out, "forge change new") || !strings.Contains(out, "--kind task") {
		t.Fatalf("dirty spec repo: next should ask for a change first, got %s", out)
	}

	forge(t, dir, "change", "new", "demo", "--title", "demo")
	out = forge(t, dir, "next", "--json")
	if !strings.Contains(out, "forge change phase demo") {
		t.Fatalf("with an open change: next should point at its stage, got %s", out)
	}

	// goal 还是脚手架 → task start --change 必须被拒。
	msg := readFile(t, dir, "spec/changes/demo/goal.md")
	_ = msg
	if out, err := forgeErr(t, dir, "task", "start", "--ref", "feat/demo", "--title", "demo", "--change", "demo"); err == nil {
		t.Fatalf("task start --change should fail before the change is ready: %s", out)
	} else if !strings.Contains(out, "specify") {
		t.Fatalf("error should name the phase (specify): %s", out)
	}

	// 补齐：goal → clarify → design → tasks。
	writeFile(t, dir, "spec/changes/demo/goal.md", entryGoal("demo"))
	forge(t, dir, "change", "clarify", "demo", "--by-human", "--note", "只做入口路由，不做组装。")
	writeFile(t, dir, "spec/changes/demo/design.md", "# 设计\n\n实现入口路由。\n")
	writeFile(t, dir, "spec/changes/demo/tasks.md", "- [ ] 1. 做 A（判据：acceptance #1）\n")
	writeFile(t, dir, "spec/changes/demo/acceptance.md", "# 验收\n\n```bash\ntrue\n```\n")

	out = forge(t, dir, "change", "phase", "demo")
	if !strings.Contains(out, "ready") {
		t.Fatalf("filled change should be ready, got %s", out)
	}
	out = forge(t, dir, "next", "--json")
	if !strings.Contains(out, "forge change phase demo") {
		t.Fatalf("next should keep pointing at the ready change, got %s", out)
	}
	forge(t, dir, "task", "start", "--ref", "feat/demo", "--title", "demo", "--change", "demo")
	forge(t, dir, "task", "abort", "--ref", "feat/demo")
}

func TestChangeStageBlocksEarlyTask(t *testing.T) {
	t.Setenv("FORGE_DATA_HOME", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	dir := entryRepo(t)
	forge(t, dir, "change", "new", "early", "--title", "early")

	out, err := forgeErr(t, dir, "task", "start", "--ref", "feat/early", "--title", "early", "--change", "early")
	if err == nil {
		t.Fatalf("task start --change must fail while goal is a scaffold: %s", out)
	}
	if !strings.Contains(out, "specify") || !strings.Contains(out, "goal.md") {
		t.Fatalf("error should explain the specify phase: %s", out)
	}
}

func entryGoal(id string) string {
	return "# 目标：" + id + "\n\nchange: `" + id + "`\n\n" +
		"## Objective\n\n把入口路由变成状态。\n\n" +
		"## Why\n\n脏树直接开工会让 change 旁路。\n\n" +
		"## Constraints\n\n只对声明 spec 的仓库生效。\n\n" +
		"## Out of Scope\n\n不做 fragment 组装。\n\n" +
		"## Check\n\n- forge change phase 报阶段\n"
}