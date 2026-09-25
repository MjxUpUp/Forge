package hookdispatch

// hook_task_drift_test.go —— task-drift（escape-hatch-hardening P0-B,
// docs/plans/escape-hatch-hardening-2026-09.md）的守卫:git 边界动词
// （commit/merge/branch/checkout -b/switch -c）发生在任务分支之外 →
// advisory（stdout 经 EmitAdvisoryRouted）+ checklog warn 行,阶梯节流
// （1/2/10/20/…）;任务分支上的 commit、无任务会话、非边界动词、判定前提
// 缺失 → 静默。永不阻断（P0 契约）。

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MjxUpUp/Forge/internal/checklog"
	"github.com/MjxUpUp/Forge/internal/forgedata"
	"github.com/MjxUpUp/Forge/internal/util"
)

// driftGitProject 隔离数据 home 并返回一个真实 git 仓库（分支 feat/x,
// 一个空提交）——drift 判定读真实 git 状态,非 mock。
func driftGitProject(t *testing.T) string {
	t.Helper()
	root := trackTestProject(t)
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", root}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "-q", "-b", "feat/x")
	run("-c", "user.email=t@t", "-c", "user.name=t", "commit", "-q", "--allow-empty", "-m", "init")
	return root
}

// resetDriftMarker 清除会话阶梯/阻断计数文件（跨运行隔离,同 resetNudgeState）。
func resetDriftMarker(t *testing.T, root, sessionID string) {
	t.Helper()
	dir := filepath.Join(forgedata.DataDirFor(root), "markers")
	ladder := filepath.Join(dir, "forge-taskdrift-"+util.SanitizeSessionID(sessionID))
	blocks := filepath.Join(dir, "forge-taskdrift-blocks-"+util.SanitizeSessionID(sessionID))
	escape := filepath.Join(dir, "forge-taskdrift-escape-"+util.SanitizeSessionID(sessionID))
	for _, f := range []string{ladder, blocks, escape} {
		_ = os.Remove(f)
	}
	t.Cleanup(func() {
		for _, f := range []string{ladder, blocks, escape} {
			_ = os.Remove(f)
		}
	})
}

// runDriftHook 模拟一次经 runTaskDriftHook 的 PreToolUse Bash 事件,返回捕获
// 的 stdout（advisory 发射面）。
func runDriftHook(t *testing.T, root, sessionID, command string) string {
	t.Helper()
	raw, _ := json.Marshal(map[string]string{"command": command})
	in := HookInput{
		HookEventName: "PreToolUse",
		SessionID:     sessionID,
		ToolName:      "Bash",
		ToolInput:     raw,
	}
	return captureStdout(t, func() {
		if err := runTaskDriftHook(in, root, "test", ""); err != nil {
			t.Fatalf("task-drift must never error: %v", err)
		}
	})
}

func findDriftEntries(t *testing.T, root string) []checklog.Entry {
	t.Helper()
	return findTrackEntries(t, root, checklog.CheckTaskDrift)
}

// TestRunTaskDriftHook_AdvisoryOnBranchDrift 钉住核心契约（M2）:任务分支外的
// git 边界命令首次即有 advisory + warn 行,文案含双分支与三条出口。
func TestRunTaskDriftHook_AdvisoryOnBranchDrift(t *testing.T) {
	root := driftGitProject(t)
	const sid = "sess-td-1"
	resetDriftMarker(t, root, sid)
	startTrackTask(t, root, sid, "feat/x")
	// 仓库切到任务分支之外。
	if out, err := exec.Command("git", "-C", root, "checkout", "-q", "-b", "other/y").CombinedOutput(); err != nil {
		t.Fatalf("git checkout -b other/y: %v\n%s", err, out)
	}

	out := runDriftHook(t, root, sid, "git add -A && git commit -m \"feat: m3 adapters\"")
	if !strings.Contains(out, "[task-drift]") {
		t.Errorf("drifted git commit must emit [task-drift] advisory, got: %q", out)
	}
	for _, want := range []string{"other/y", "feat/x", "forge task gate", "forge task abort"} {
		if !strings.Contains(out, want) {
			t.Errorf("advisory must mention %s (branch pair + exits), got: %q", want, out)
		}
	}
	if strings.Contains(out, `"decision":"block"`) {
		t.Errorf("task-drift must never block (P0 contract), got: %q", out)
	}
	entries := findDriftEntries(t, root)
	if len(entries) != 1 {
		t.Fatalf("entries = %d, want 1 on first drifted boundary verb", len(entries))
	}
	e := entries[0]
	if e.Passed {
		t.Errorf("drift row must be Passed=false (Level=warn), got pass")
	}
	if e.Meta["branch"] != "other/y" || e.Meta["task_branch"] != "feat/x" || e.Meta["occurrence"] != "1" {
		t.Errorf("drift meta = %v, want branch=other/y task_branch=feat/x occurrence=1", e.Meta)
	}
	if e.TaskRef == "" {
		t.Errorf("drift row must carry TaskRef via attribution stamp")
	}
}

// TestRunTaskDriftHook_SilentOnTaskBranch 钉住无误报（M3）:任务分支上的 commit
// 是既定合法顺序（commit-before-complete）,必须零输出零行。
func TestRunTaskDriftHook_SilentOnTaskBranch(t *testing.T) {
	root := driftGitProject(t)
	const sid = "sess-td-2"
	resetDriftMarker(t, root, sid)
	startTrackTask(t, root, sid, "feat/x")

	wantSilent(t, "commit on task branch is legit", runDriftHook(t, root, sid, "git add internal/ && git commit -m \"feat: x\""))
	if entries := findDriftEntries(t, root); len(entries) != 0 {
		t.Errorf("task-branch commit must leave 0 rows, got %d", len(entries))
	}
}

// TestRunTaskDriftHook_SilentWithoutTaskOrVerb 钉住静默契约:无活跃任务（探索/
// 任务尚未建）与非 git 边界动词 → 零输出零行（advisory 不做无差别巡逻）。
func TestRunTaskDriftHook_SilentWithoutTaskOrVerb(t *testing.T) {
	root := driftGitProject(t)
	const sid = "sess-td-3"
	resetDriftMarker(t, root, sid)
	// 无任务:漂移分支上的 commit 也静默（bootstrapping 流程合法）。
	if out, err := exec.Command("git", "-C", root, "checkout", "-q", "-b", "boot/strap").CombinedOutput(); err != nil {
		t.Fatalf("git checkout -b: %v\n%s", err, out)
	}
	wantSilent(t, "no active task → silent", runDriftHook(t, root, sid, "git commit -m wip"))
	if entries := findDriftEntries(t, root); len(entries) != 0 {
		t.Errorf("no-task session must leave 0 rows, got %d", len(entries))
	}
	// 有任务但非边界动词:静默。
	startTrackTask(t, root, sid, "feat/x")
	wantSilent(t, "non-boundary verb → silent", runDriftHook(t, root, sid, "go build ./... && go vet ./..."))
	if entries := findDriftEntries(t, root); len(entries) != 0 {
		t.Errorf("non-boundary verb must leave 0 rows, got %d", len(entries))
	}
}

// TestRunTaskDriftHook_LadderThrottlesRows 钉住阶梯节流:12 次漂移边界动词
// → 行落在 1/2/10,共 3 行;第 1、2 次与第 10 次有 advisory,其余静默计数。
func TestRunTaskDriftHook_LadderThrottlesRows(t *testing.T) {
	root := driftGitProject(t)
	const sid = "sess-td-4"
	resetDriftMarker(t, root, sid)
	startTrackTask(t, root, sid, "feat/x")
	if out, err := exec.Command("git", "-C", root, "checkout", "-q", "-b", "runaway/m3").CombinedOutput(); err != nil {
		t.Fatalf("git checkout -b: %v\n%s", err, out)
	}

	emitted := 0
	for i := 1; i <= 12; i++ {
		out := runDriftHook(t, root, sid, fmt.Sprintf("git commit --allow-empty -m wip-%d", i))
		switch i {
		case 1, 2, 10:
			if !strings.Contains(out, "[task-drift]") {
				t.Errorf("occurrence %d must emit advisory, got: %q", i, out)
			}
			emitted++
		default:
			if out != "" {
				t.Errorf("occurrence %d must count silently, got: %q", i, out)
			}
		}
	}
	if emitted != 3 {
		t.Errorf("advisory emissions = %d, want 3 (occurrences 1/2/10)", emitted)
	}
	entries := findDriftEntries(t, root)
	if len(entries) != 3 {
		t.Fatalf("rows = %d, want 3 (occurrences 1/2/10)", len(entries))
	}
	wantOcc := map[string]bool{"1": true, "2": true, "10": true}
	for _, e := range entries {
		if !wantOcc[e.Meta["occurrence"]] {
			t.Errorf("unexpected occurrence %q (ladder must be 1/2/10n)", e.Meta["occurrence"])
		}
	}
}

// TestRunTaskDriftHook_BranchCreationIsBoundary 钉住动词面:checkout -b /
// switch -c / git branch <name> / git merge 都是边界动词——建分支即漂移动作,
// 不等 commit 才可见。
func TestRunTaskDriftHook_BranchCreationIsBoundary(t *testing.T) {
	root := driftGitProject(t)
	const sid = "sess-td-5"
	resetDriftMarker(t, root, sid)
	startTrackTask(t, root, sid, "feat/x")
	// 先真实切出任务分支——hook 判定读仓库当前分支,不执行命令串本身。
	if out, err := exec.Command("git", "-C", root, "checkout", "-q", "-b", "drift/zero").CombinedOutput(); err != nil {
		t.Fatalf("git checkout -b drift/zero: %v\n%s", err, out)
	}

	for _, verb := range []string{
		"git checkout -b drift/one",
		"git switch -c drift/two",
		"git branch drift/three",
		"git merge --no-ff drift/three",
	} {
		resetDriftMarker(t, root, sid) // 每个动词独立断言动词面——阶梯是另一个测试的职责
		if out := runDriftHook(t, root, sid, verb); !strings.Contains(out, "[task-drift]") {
			t.Errorf("boundary verb %q must trigger task-drift, got: %q", verb, out)
		}
	}
	if entries := findDriftEntries(t, root); len(entries) != 4 {
		t.Errorf("rows = %d, want 4 (each boundary verb)", len(entries))
	}
}

// TestRunTaskDriftHook_QueryAndFlagForms 钉住审查修复后的动词面精度:
// (a) `git branch -a/--list/-v/-d/-m` 是查询/删除/改名,不是建支——零行;
// (b) git 全局旗标跳过正确(`git -c a=b commit` / `git -C /p branch x` 命中);
// (c) forge 前缀豁免(spec M3):forge CLI 命令零行,但 `$(` 命令替换不豁免。
func TestRunTaskDriftHook_QueryAndFlagForms(t *testing.T) {
	root := driftGitProject(t)
	const sid = "sess-td-6"
	resetDriftMarker(t, root, sid)
	startTrackTask(t, root, sid, "feat/x")
	if out, err := exec.Command("git", "-C", root, "checkout", "-q", "-b", "q/y").CombinedOutput(); err != nil {
		t.Fatalf("git checkout -b q/y: %v\n%s", err, out)
	}

	// 查询/删除/改名形态:静默(误报面钉死——高频 `git branch -a` 曾被当边界动词)。
	for _, query := range []string{
		"git branch -a",
		"git branch --list",
		"git branch -v",
		"git branch -d feat/x",
		"git branch -m q/y q/z",
	} {
		resetDriftMarker(t, root, sid)
		if out := runDriftHook(t, root, sid, query); out != "" {
			t.Errorf("query form %q must be silent, got: %q", query, out)
		}
	}
	if entries := findDriftEntries(t, root); len(entries) != 0 {
		t.Fatalf("query forms must leave 0 rows, got %d", len(entries))
	}

	// 全局旗标跳过:带值旗标后的真子命令仍命中。
	for _, flagged := range []string{
		"git -c user.email=t@t commit -m wip",
		"git -C . branch newbranch",
	} {
		resetDriftMarker(t, root, sid)
		if out := runDriftHook(t, root, sid, flagged); !strings.Contains(out, "[task-drift]") {
			t.Errorf("flag-prefixed boundary verb %q must trigger, got: %q", flagged, out)
		}
	}

	// forge 前缀豁免(M3):任务 CLI 零行;命令替换形态不豁免。
	resetDriftMarker(t, root, sid)
	wantSilent(t, "forge CLI command is exempt", runDriftHook(t, root, sid, "forge task gate task-verify --ref fix/escape-hatch-hardening"))
	if out := runDriftHook(t, root, sid, "forge task gate --ref $(git commit -m wip)"); !strings.Contains(out, "[task-drift]") {
		t.Errorf("command-substitution form must NOT be exempt, got: %q", out)
	}
}

// TestTaskDriftBlockRatchet 钉住 P1 机械版本门:<1.66 advisory(含垃圾版本防御
// 回落);≥1.66 BLOCK(deny 发射 + Level=fail 行);FORGE_TASK_DRIFT=0 逃生降级
// 且落 escape-hatch 行;会话第 6 次起超上限降回 advisory(有界)。
func TestTaskDriftBlockRatchet(t *testing.T) {
	root := driftGitProject(t)
	const sid = "sess-td-ratchet"
	resetDriftMarker(t, root, sid)
	startTrackTask(t, root, sid, "feat/x")
	if out, err := exec.Command("git", "-C", root, "checkout", "-q", "-b", "ratchet/y").CombinedOutput(); err != nil {
		t.Fatalf("git checkout -b: %v\n%s", err, out)
	}
	in := driftInput(t, "git commit --allow-empty -m wip")

	run := func(version string) (string, error) {
		var errOut error
		out := captureStdout(t, func() {
			errOut = runTaskDriftHook(in, root, version, "")
		})
		return out, errOut
	}

	// < 1.66:advisory(现有行为),无 error。
	out, err := run("1.65.0")
	if err != nil || !strings.Contains(out, "[task-drift]") {
		t.Errorf("1.65 must stay advisory, err=%v out=%q", err, out)
	}
	// 垃圾版本(测试默认 "test")防御性回落 advisory。
	resetDriftMarker(t, root, sid)
	if out, err := run("test"); err != nil || !strings.Contains(out, "[task-drift]") {
		t.Errorf("garbage version must fall back to advisory, err=%v out=%q", err, out)
	}

	// ≥ 1.66:BLOCK——deny 发射 + 非 nil error(HookBlockError 形态)。
	resetDriftMarker(t, root, sid)
	out, err = run("1.66.0")
	if err == nil {
		t.Error("1.66 must return a block error (exit-2 class)")
	}
	if !strings.Contains(out, "permissionDecision") || !strings.Contains(out, "deny") {
		t.Errorf("block emission must carry deny JSON, got: %q", out)
	}
	// deny 文案必须自足(审查必改项 2 的回归钉):含出口动词与逃生,不依赖
	// "上文"——n=3..9 阶梯空档时 taskDriftAdvisory 为空,deny 不能拿空底拼话。
	if !strings.Contains(out, "出口") || !strings.Contains(out, "FORGE_TASK_DRIFT=0") {
		t.Errorf("deny text must be self-contained (exits + escape), got: %q", out)
	}
	if rows := findDriftEntries(t, root); len(rows) == 0 || rows[len(rows)-1].Level != "fail" {
		t.Errorf("block row must be Level=fail, got %+v", rows)
	}

	// env 逃生:降级 advisory + escape-hatch 行。
	resetDriftMarker(t, root, sid)
	t.Setenv(forgeTaskDriftEnv, "0")
	if out, err := run("1.66.0"); err != nil || !strings.Contains(out, "[task-drift]") {
		t.Errorf("FORGE_TASK_DRIFT=0 must downgrade to advisory, err=%v out=%q", err, out)
	}
	if escapes := findTrackEntries(t, root, checklog.CheckEscapeHatch); len(escapes) == 0 {
		t.Error("env escape must record an escape-hatch row")
	} else if escapes[len(escapes)-1].Meta["gate"] != "task-drift" {
		t.Errorf("escape row meta gate = %v, want task-drift", escapes[len(escapes)-1].Meta)
	}

	// 会话上限:连跑 5 次 BLOCK 后第 6 次降级 advisory(有界防死循环)。
	t.Setenv(forgeTaskDriftEnv, "")
	blocked := 0
	for i := 0; i < 7; i++ {
		_, err := run("1.66.0")
		if err != nil {
			blocked++
		}
	}
	if blocked != 5 {
		t.Errorf("session block cap must bound BLOCKs to %d, got %d", taskDriftBlockCap, blocked)
	}
}

// driftInput 构造漂移判定的 Bash HookInput(测试助手,匹配 runDriftHook 的输入形状)。
func driftInput(t *testing.T, command string) HookInput {
	t.Helper()
	raw, _ := json.Marshal(map[string]string{"command": command})
	return HookInput{HookEventName: "PreToolUse", SessionID: "sess-td-ratchet", ToolName: "Bash", ToolInput: raw}
}
