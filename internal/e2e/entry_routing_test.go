package e2e

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// entry_routing_test.go —— 入口路由 A1/A2 的真实二进制链路（2026-11-15 修订）：
// 任务是一等入口（不带 change 也能开工）；change 是记录单元，可后补；显式
// --change 时才要求 ready。

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

func TestEntryRoutingTaskFirstThenChange(t *testing.T) {
	t.Setenv("FORGE_DATA_HOME", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	dir := entryRepo(t)

	// 脏树 + 声明 spec：next 建议立 change，但同时明确 task 可以先开。
	out := forge(t, dir, "next", "--json")
	if !strings.Contains(out, "forge change new") {
		t.Fatalf("dirty spec repo: next should mention change new, got %s", out)
	}
	if !strings.Contains(out, "task start") {
		t.Fatalf("next should keep task start reachable, got %s", out)
	}

	// 任务是一等入口：不带 --change 直接开工，不被拒。
	forge(t, dir, "task", "start", "--ref", "feat/demo", "--title", "demo")
	forge(t, dir, "task", "abort", "--ref", "feat/demo")

	// 补一个 change 作为记录：显式链接时要求 ready。
	forge(t, dir, "change", "new", "demo", "--title", "demo")
	out = forge(t, dir, "next", "--json")
	if !strings.Contains(out, "forge change phase demo") {
		t.Fatalf("with an unready open change: next should point at phase, got %s", out)
	}
	if out, err := forgeErr(t, dir, "task", "start", "--ref", "feat/demo2", "--title", "demo", "--change", "demo"); err == nil {
		t.Fatalf("task start --change should fail before the change is ready: %s", out)
	} else if !strings.Contains(out, "specify") {
		t.Fatalf("error should name the phase (specify): %s", out)
	}

	// 补齐：goal → clarify → change/acceptance。
	writeFile(t, dir, "spec/changes/demo/goal.md", entryGoal("demo"))
	forge(t, dir, "change", "clarify", "demo", "--by-human", "--note", "只做入口路由，不做组装。")
	writeFile(t, dir, "spec/changes/demo/change.md", "# 改动\n\n实现入口路由。\n\n## 影响\n\n无。\n\n## 验证\n\nacceptance #1。\n")
	writeFile(t, dir, "spec/changes/demo/acceptance.md", "## 判据\n\n```bash\ntrue\n```\n")

	out = forge(t, dir, "change", "phase", "demo")
	if !strings.Contains(out, "ready") {
		t.Fatalf("filled change should be ready, got %s", out)
	}
	// ready 之后 next 必须前进到开工，而不是停在 change phase。
	out = forge(t, dir, "next", "--json")
	if !strings.Contains(out, "--change demo") {
		t.Fatalf("ready change: next should advance to task start --change, got %s", out)
	}
	forge(t, dir, "task", "start", "--ref", "feat/demo3", "--title", "demo", "--change", "demo", "--accept", "true ::")
	forge(t, dir, "task", "abort", "--ref", "feat/demo3")
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
