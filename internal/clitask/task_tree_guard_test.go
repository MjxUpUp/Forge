package clitask

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MjxUpUp/Forge/internal/checklog"
	"github.com/MjxUpUp/Forge/internal/worktree"
)

// task_tree_guard_test.go —— 错树守护的端到端契约（docs/design/task-tree-guard.md §5/§7）：
// 任务锚在另一棵树时，主树里的源码级门禁必须 BLOCK；`forge worktree use` 必须把锚迁回当前树。
// 用真二进制跑（runForge），因为断言的正是「用户在终端里看到的行为」，而不是函数返回值。

// initTreeGuardRepo 建一个带 main 分支空提交的临时 repo——`task start --worktree`
// 需要一条主线做 base（与 taskpipeline 的 initRepoOnBranch 同形；测试助手不跨包共享）。
func initTreeGuardRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	runGit(t, dir, "init")
	runGit(t, dir, "config", "user.email", "t@t.com")
	runGit(t, dir, "config", "user.name", "T")
	runGit(t, dir, "commit", "--allow-empty", "-m", "main init")
	runGit(t, dir, "branch", "-M", "main")
	// task start 要求仓库是 forge 项目（.forge-project-id）——与真机一致，先 init。
	if out, _, code := runForge(t, dir, "init"); code != 0 {
		t.Fatalf("forge init 失败（code=%d）：\n%s", code, out)
	}
	return dir
}

// TestTaskGate_OffTreeBlocks 钉住「错树时门禁必须拒绝」——主树 cwd、任务 worktree 在别处。
func TestTaskGate_OffTreeBlocks(t *testing.T) {
	dir := initTreeGuardRepo(t)
	if _, _, code := runForge(t, dir, "task", "start", "--ref", "feat/tg", "--title", "错树守护夹具", "--accept", "true :: ok", "--worktree"); code != 0 {
		t.Fatalf("task start --worktree 失败（code=%d）", code)
	}
	wtPath := filepath.Join(filepath.Dir(dir), filepath.Base(dir)+"-wt", "feat-tg")
	if _, err := exec.Command("git", "-C", dir, "worktree", "list").Output(); err != nil {
		t.Fatalf("git worktree list: %v", err)
	}
	if _, err := exec.Command("test", "-d", wtPath).CombinedOutput(); err != nil {
		t.Skipf("worktree 目录布局与预期不同（%s 不存在），跳过路径断言", wtPath)
	}

	out, _, code := runForge(t, dir, "task", "gate", "task-implement")
	if code == 0 {
		t.Fatalf("主检出里对锚在别处的任务过门禁必须失败，实际 exit=0\n%s", out)
	}
	if !strings.Contains(out, "错树") || !strings.Contains(out, "forge worktree use") {
		t.Fatalf("错树拒绝信息必须指出错树与出路（forge worktree use），实际：\n%s", out)
	}
	// 三条出路必须都在文案里：只给「forge worktree use」会把「回锚树」这条最便宜的路径藏起来。
	for _, want := range []string{"cd ", "FORGE_TASK_TREE_GUARD=0"} {
		if !strings.Contains(out, want) {
			t.Fatalf("错树拒绝信息缺出路 %q，实际：\n%s", want, out)
		}
	}
	// 出路必须可执行：`cd` 后面得是那棵锚树的路径（只写「cd」等于没给答案）。
	if !strings.Contains(out, "feat-tg") {
		t.Fatalf("错树拒绝信息必须给出锚树路径（worktree 名 feat-tg），实际：\n%s", out)
	}

	// 逃生舱：显式关闭后不再以「错树」为由阻断（可能因其他门禁原因失败，故只断言文案消失）。
	cmd := exec.Command(forgeExe, "task", "gate", "task-implement")
	cmd.Dir = dir
	cmd.Env = append(cmd.Environ(), "FORGE_TASK_TREE_GUARD=0")
	escaped, _ := cmd.CombinedOutput()
	if strings.Contains(string(escaped), "错树") {
		t.Fatalf("FORGE_TASK_TREE_GUARD=0 必须关闭错树阻断，实际仍报：\n%s", escaped)
	}
}

// TestWorktreeUse_Rebinds 钉住「正当出口」：worktree use 后任务锚落在当前树，门禁不再以错树拒绝。
func TestWorktreeUse_Rebinds(t *testing.T) {
	dir := initTreeGuardRepo(t)
	if _, _, code := runForge(t, dir, "task", "start", "--ref", "feat/tu", "--title", "重锚夹具", "--accept", "true :: ok", "--worktree"); code != 0 {
		t.Fatalf("task start --worktree 失败（code=%d）", code)
	}

	out, _, code := runForge(t, dir, "worktree", "use", "--ref", "feat/tu")
	if code != 0 {
		t.Fatalf("worktree use 失败（code=%d）：\n%s", code, out)
	}
	if !strings.Contains(out, "已锚定到当前树") {
		t.Fatalf("worktree use 必须回报锚定结果，实际：\n%s", out)
	}

	absDir, err := filepath.Abs(dir)
	if err != nil {
		t.Fatal(err)
	}
	anchors := worktree.Anchors(dir, "feat/tu")
	if len(anchors) != 1 {
		t.Fatalf("重锚后应只剩 1 个锚，实际 %d：%+v", len(anchors), anchors)
	}
	if anchors[0].Path != absDir {
		t.Fatalf("锚必须指向当前树 %s，实际 %s", absDir, anchors[0].Path)
	}

	gateOut, _, _ := runForge(t, dir, "task", "gate", "task-implement")
	if strings.Contains(gateOut, "错树") {
		t.Fatalf("重锚后门禁不得再报错树：\n%s", gateOut)
	}
}

// TestTaskGate_OffTreeRecordsChecklog 钉住设计 §5 的留痕承诺：错树 BLOCK 必须落一条
// deterministic tree-guard 行（带锚与 resolve_path）；关守护时落一条 escape-hatch 行——
// 没有这两条，「错树守护被拦了几次」「多少人靠关守护绕过去」都没法回答。
func TestTaskGate_OffTreeRecordsChecklog(t *testing.T) {
	dir := initTreeGuardRepo(t)
	if _, _, code := runForge(t, dir, "task", "start", "--ref", "feat/tgr", "--title", "留痕夹具", "--accept", "true :: ok", "--worktree"); code != 0 {
		t.Fatalf("task start --worktree 失败（code=%d）", code)
	}
	if _, _, code := runForge(t, dir, "task", "gate", "task-implement"); code == 0 {
		t.Fatal("前提：主树对锚在别处的任务过门禁必须失败")
	}

	rowsFor := func(check checklog.CheckName) []checklog.Entry {
		t.Helper()
		all, err := checklog.LoadForTask(dir, "feat/tgr")
		if err != nil {
			t.Fatalf("读 checklog: %v", err)
		}
		var rows []checklog.Entry
		for _, e := range all {
			if e.Check == check {
				rows = append(rows, e)
			}
		}
		return rows
	}

	blocks := rowsFor(checklog.CheckTreeGuard)
	if len(blocks) == 0 {
		t.Fatal("错树 BLOCK 必须落 tree-guard 行，实际一条都没有")
	}
	for _, e := range blocks {
		if e.Passed {
			t.Fatalf("错树拦截行必须 Passed=false：%+v", e)
		}
		if e.Meta["tree_guard"] != "off-tree" || e.Meta["resolve_path"] != "active-file-offtree" || e.Meta["anchor"] == "" {
			t.Fatalf("tree-guard 行缺树形载荷：%v", e.Meta)
		}
	}

	cmd := exec.Command(forgeExe, "task", "gate", "task-implement")
	cmd.Dir = dir
	cmd.Env = append(cmd.Environ(), "FORGE_TASK_TREE_GUARD=0")
	_, _ = cmd.CombinedOutput()

	escapes := rowsFor(checklog.CheckEscapeHatch)
	if len(escapes) == 0 {
		t.Fatal("关守护必须落 escape-hatch 行（否则评分 capped 是假的）")
	}
	if escapes[0].Meta["escape.gate"] != "task-tree-guard" || escapes[0].Meta["escape.reason"] != checklog.EscapeReasonEnv {
		t.Fatalf("escape-hatch 行必须点名 task-tree-guard：%v", escapes[0].Meta)
	}
}
