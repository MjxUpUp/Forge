package taskpipeline

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/MjxUpUp/Forge/internal/act"
	"github.com/MjxUpUp/Forge/internal/checklog"
	"github.com/MjxUpUp/Forge/internal/forgedata"
)

// setupRecurrentRepo 建临时 git 仓库 + .forge/ + FORGE_DATA_HOME 隔离。复发门禁从用户级 DataDir
// 读已完成任务结论，故测试须 (a) 建 .forge/（ProjectFor 要求）且 (b) 把 DataDir 重定向到临时目录，
// 使写入历史与读取历史落到同一路径——且不污染真实 ~/.forge。
func setupRecurrentRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	initRepoWithMaster(t, dir)
	if err := os.MkdirAll(filepath.Join(dir, ".forge"), 0755); err != nil {
		t.Fatalf(`mkdir .forge: %v`, err)
	}
	t.Setenv("FORGE_DATA_HOME", dir)
	return dir
}

// seedRecurrentHistory 追加 n 个都在 dim 上低分的已完成任务结论，构造「项目在 dim 上有系统性缺口」
// 的履历——复发轴所键入的。用 act.Append（生产写入路径）使 fixture 与真实历史字节一致。
func seedRecurrentHistory(t *testing.T, dir, dim string, n int) {
	t.Helper()
	proj, err := forgedata.ProjectFor(dir)
	if err != nil {
		t.Fatalf(`ProjectFor: %v`, err)
	}
	for i := 0; i < n; i++ {
		if err := act.Append(proj, &act.Conclusion{
			TaskRef:       fmt.Sprintf("prior-%d", i),
			Score:         60,
			Grade:         "C",
			Strength:      "Strong",
			LowDimensions: []string{dim},
			CompletedAt:   time.Now(),
		}); err != nil {
			t.Fatalf(`Append: %v`, err)
		}
	}
}

// TestRecurrent_TestCoverage_Hardens: project has 3 testing-low tasks in history (advisory self-discipline proven to fail) AND this task adds foo.go with no foo_test.go → task-verify promotes the advisory to a HARD block (the core soft→hard balance contract).
//
// TestRecurrent_TestCoverage_Hardens：项目历史有 3 个 testing 低分任务（advisory 自律已被证明失效）
// 且本任务加 foo.go 无 foo_test.go → task-verify 把 advisory 升为 HARD 阻断（核心软→硬平衡契约）。
func TestRecurrent_TestCoverage_Hardens(t *testing.T) {
	dir := setupRecurrentRepo(t)
	seedRecurrentHistory(t, dir, dimTesting, 3)

	writeCommitSource(t, dir, map[string]string{
		"foo.go": "package main\n\nfunc Foo() int { return 1 }\n",
	}, "add foo no test")

	state := newVerifyState(t, dir, "recur-testcov")
	var execErr error
	captureStderr(t, func() {
		_, execErr = ExecuteTaskGate(dir, "task-verify", state)
	})
	if !strings.HasPrefix(execErr.Error(), blockedPrefix) {
		t.Fatalf(`项目 testing 复发(3) + foo.go 无测试 → 应复发升硬 BLOCKED, got: %v`, execErr)
	}
	if !strings.Contains(execErr.Error(), "testing") {
		t.Errorf(`BLOCKED 消息应点名 testing 维度复发: %v`, execErr)
	}
}

// TestRecurrent_TestCoverage_NoHistoryStaysAdvisory: same foo.go-without-test, but NO recurrence history → stays advisory (gate PASSes).
//
// TestRecurrent_TestCoverage_NoHistoryStaysAdvisory：同样 foo.go 无测试，但无复发历史 → 保持 advisory
// （gate PASS）。守护 fail-open 契约：无履历的项目永不升硬（不误伤陌生项目）。
func TestRecurrent_TestCoverage_NoHistoryStaysAdvisory(t *testing.T) {
	dir := setupRecurrentRepo(t)
	// 无复发历史
	writeCommitSource(t, dir, map[string]string{
		"foo.go": "package main\n\nfunc Foo() int { return 1 }\n",
	}, "add foo no test")
	state := newVerifyState(t, dir, "no-history")
	if _, err := ExecuteTaskGate(dir, "task-verify", state); err != nil {
		t.Fatalf(`无复发历史 → 应保持 advisory PASS, got: %v`, err)
	}
}

// TestRecurrent_TestCoverage_EscapeDisables: recurrence history present, but FORGE_RECURRENT_HARDEN=disable → reverts to pure advisory (PASS).
//
// TestRecurrent_TestCoverage_EscapeDisables：有复发历史，但 FORGE_RECURRENT_HARDEN=disable → 退回
// 纯 advisory（PASS），且本来会升硬的那一刻落一行 escape-hatch 审计（2026-10-22 裁决：按
// compat 第二节第 5 条 ① 类逃生处理，与其它逃生同价）。
func TestRecurrent_TestCoverage_EscapeDisables(t *testing.T) {
	dir := setupRecurrentRepo(t)
	seedRecurrentHistory(t, dir, dimTesting, 3)
	t.Setenv(recurrentHardenDisableEnv, "disable")

	writeCommitSource(t, dir, map[string]string{
		"foo.go": "package main\n\nfunc Foo() int { return 1 }\n",
	}, "add foo no test")
	state := newVerifyState(t, dir, "escape")
	if _, err := ExecuteTaskGate(dir, "task-verify", state); err != nil {
		t.Fatalf(`FORGE_RECURRENT_HARDEN=disable → 应回退 advisory PASS, got: %v`, err)
	}
	entries, err := checklog.LoadForTask(dir, state.TaskRef)
	if err != nil {
		t.Fatalf("load checklog: %v", err)
	}
	found := false
	for _, e := range entries {
		if e.Check == checklog.CheckEscapeHatch && e.Meta["escape.gate"] == "recurrent-harden" {
			found = true
		}
	}
	if !found {
		t.Fatalf("FORGE_RECURRENT_HARDEN=disable lifting a would-be block must leave an escape-hatch audit row for task %q", state.TaskRef)
	}
}

// recurrentEscapeRows counts the recurrent-harden escape-hatch rows recorded for a task.
//
// recurrentEscapeRows 统计某任务的复发升硬 escape-hatch 审计行数。
func recurrentEscapeRows(t *testing.T, dir, taskRef string) int {
	t.Helper()
	entries, err := checklog.LoadForTask(dir, taskRef)
	if err != nil {
		t.Fatalf("load checklog: %v", err)
	}
	n := 0
	for _, e := range entries {
		if e.Check == checklog.CheckEscapeHatch && e.Meta["escape.gate"] == "recurrent-harden" {
			n++
		}
	}
	return n
}

// TestRecurrent_EscapeWithoutRecurrenceLeavesNoRow: harden disabled on a project with no recurrence history → no escape row (nothing was bypassed, so nothing is priced).
//
// TestRecurrent_EscapeWithoutRecurrenceLeavesNoRow：无复发履历的项目关掉复发升硬 → 不留审计行
// （没有本来会触发的阻断，就没有逃生可计价）。
func TestRecurrent_EscapeWithoutRecurrenceLeavesNoRow(t *testing.T) {
	dir := setupRecurrentRepo(t)
	t.Setenv(recurrentHardenDisableEnv, "disable")
	writeCommitSource(t, dir, map[string]string{
		"foo.go": "package main\n\nfunc Foo() int { return 1 }\n",
	}, "add foo no test")
	state := newVerifyState(t, dir, "escape-no-history")
	if _, err := ExecuteTaskGate(dir, "task-verify", state); err != nil {
		t.Fatalf(`无复发履历 → 应 advisory PASS, got: %v`, err)
	}
	if n := recurrentEscapeRows(t, dir, state.TaskRef); n != 0 {
		t.Fatalf("no recurrence history → no would-be block → want 0 recurrent-harden escape rows, got %d", n)
	}
}

// TestRecurrent_ScopeDrift_EscapeRecorded: scope recurrence + severe drift + harden disabled → PASS and one escape row (the scope branch is priced the same as the testing branch).
//
// TestRecurrent_ScopeDrift_EscapeRecorded：scope 复发 + 严重 drift + 关掉复发升硬 → PASS，且留
// 一行逃生审计（scope 分支与 testing 分支同价）。
func TestRecurrent_ScopeDrift_EscapeRecorded(t *testing.T) {
	dir := setupRecurrentRepo(t)
	seedRecurrentHistory(t, dir, dimScope, 3)
	t.Setenv(recurrentHardenDisableEnv, "disable")
	writeCommitSource(t, dir, map[string]string{
		"a.go": "package main\n",
		"b.go": "package main\n",
		"c.go": "package main\n",
	}, "add 3 out-of-scope")
	state := newVerifyState(t, dir, "escape-scope")
	state.PlanScope = []string{"declared.go"}
	var execErr error
	captureStderr(t, func() {
		_, execErr = ExecuteTaskGate(dir, "task-verify", state)
	})
	if execErr != nil && strings.HasPrefix(execErr.Error(), blockedPrefix) {
		t.Fatalf(`FORGE_RECURRENT_HARDEN=disable → scope 分支不应升硬, got: %v`, execErr)
	}
	if n := recurrentEscapeRows(t, dir, state.TaskRef); n < 1 {
		t.Fatalf("scope would-be block lifted by FORGE_RECURRENT_HARDEN=disable must leave an escape row, got %d", n)
	}
}

// TestRecurrent_ScopeDrift_Hardens: project recurrent on scope AND this task drifts ≥3 source files beyond PlanScope → HARD block.
//
// TestRecurrent_ScopeDrift_Hardens：项目 scope 复发 且 本任务 ≥3 源文件超 PlanScope → HARD 阻断。
// 两轴（复发 + 严重）皆真。
func TestRecurrent_ScopeDrift_Hardens(t *testing.T) {
	dir := setupRecurrentRepo(t)
	seedRecurrentHistory(t, dir, dimScope, 3)

	writeCommitSource(t, dir, map[string]string{
		"a.go": "package main\n",
		"b.go": "package main\n",
		"c.go": "package main\n",
	}, "add 3 out-of-scope")

	state := newVerifyState(t, dir, "recur-scope")
	state.PlanScope = []string{"declared.go"} // a/b/c.go 均超 scope

	var execErr error
	captureStderr(t, func() {
		_, execErr = ExecuteTaskGate(dir, "task-verify", state)
	})
	if !strings.HasPrefix(execErr.Error(), blockedPrefix) {
		t.Fatalf(`项目 scope 复发(3) + 3 文件 drift → 应复发升硬 BLOCKED, got: %v`, execErr)
	}
	if !strings.Contains(execErr.Error(), "scope") {
		t.Errorf(`BLOCKED 消息应点名 scope 维度复发: %v`, execErr)
	}
}

// TestRecurrent_ScopeDrift_SingleFileStaysAdvisory: project recurrent on scope, but only 1 file drifts (a normal impact-prediction miss, recall ~44%) → stays advisory (PASS).
//
// TestRecurrent_ScopeDrift_SingleFileStaysAdvisory：项目 scope 复发，但仅 1 文件 drift（正常影响预测
// 失误，召回率 ~44%）→ 保持 advisory（PASS）。严重度轴保护复发项目也不对预测噪声升硬。
func TestRecurrent_ScopeDrift_SingleFileStaysAdvisory(t *testing.T) {
	dir := setupRecurrentRepo(t)
	seedRecurrentHistory(t, dir, dimScope, 3)

	writeCommitSource(t, dir, map[string]string{
		"foo.go": "package main\n",
	}, "add 1 out-of-scope")

	state := newVerifyState(t, dir, "single-drift")
	state.PlanScope = []string{"declared.go"} // foo.go 超 scope（单文件）
	if _, err := ExecuteTaskGate(dir, "task-verify", state); err != nil {
		t.Fatalf(`单文件 drift 即便复发项目也应保持 advisory PASS（严重度轴不满足）, got: %v`, err)
	}
}

// TestBehaviorSurfaceHits pins the advisory's surface matching: exact files and directory prefixes hit; unrelated paths (tests, docs, skills) miss.
//
// TestBehaviorSurfaceHits 钉死 advisory 的行为面匹配：精确文件与目录前缀命中；
// 无关路径（测试、文档、skills）不命中。
func TestBehaviorSurfaceHits(t *testing.T) {
	changed := []string{
		"internal/cli/init.go",
		"internal/agentbridge/codex.go",
		"internal/skillgen/claudemd.go",
		"internal/cli/init_test.go", // 不在行为面清单（测试文件非行为面）
		"docs/plans/x.md",
		"skills/foo/SKILL.md",
		"README.md",
	}
	got := behaviorSurfaceHits(changed)
	if len(got) != 3 {
		t.Fatalf("behaviorSurfaceHits = %v, want 3 hits（init.go/codex.go/claudemd.go）", got)
	}
	if got2 := behaviorSurfaceHits([]string{"internal/cli/root.go"}); len(got2) != 0 {
		t.Errorf("非行为面文件应零命中，got %v", got2)
	}
}
