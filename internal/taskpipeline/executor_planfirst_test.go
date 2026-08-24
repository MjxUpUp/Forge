package taskpipeline

import (
	"strings"
	"testing"

	"github.com/MjxUpUp/Forge/internal/checklog"
)

// findPlanFirstEntry finds the CheckPlanFirst entry in the checklog (nil if absent).
//
// findPlanFirstEntry 在 checklog 里找 CheckPlanFirst 条目（无则 nil）。
func findPlanFirstEntry(t *testing.T, dir string) *checklog.Entry {
	t.Helper()
	entries, err := checklog.LoadAll(dir)
	if err != nil {
		t.Fatalf(`LoadAll: %v`, err)
	}
	for i := range entries {
		if entries[i].Check == checklog.CheckPlanFirst {
			return &entries[i]
		}
	}
	return nil
}

// TestExecuteTaskGate_PlanFirstAdvisory pins the plan-first contract (variant A — advisory,
// never blocks): a code task reaching task-implement with neither Plan nor Goal still PASSES
// the gate, but a plan-first entry is recorded (Passed=false, Level=advisory) and stderr
// carries the ADVISORY nudge. When Plan/Goal is present the entry is Passed=true and stderr
// stays silent — the check always runs so trace shows it.
//
// TestExecuteTaskGate_PlanFirstAdvisory 钉住方案前置契约（变体 A——advisory，绝不
// 阻塞）：Plan/Goal 皆空的代码任务到达 task-implement 仍 PASS，但落一条 plan-first
// 条目（Passed=false，Level=advisory）且 stderr 带 ADVISORY 提示。有 Plan/Goal
// 时条目 Passed=true 且 stderr 静默——检查总是跑，trace 可见。
func TestExecuteTaskGate_PlanFirstAdvisory(t *testing.T) {
	run := func(t *testing.T, withGoal bool) (string, *checklog.Entry) {
		dir := t.TempDir()
		initRepoWithMaster(t, dir)
		writeCommitSource(t, dir, map[string]string{
			"foo.go": "package main\n\nfunc Foo() int { return 1 }\n",
		}, "add foo")
		state := &TaskState{TaskRef: "plan-first", Branch: "feat/testcov"}
		if withGoal {
			state.Goal = "演示方案前置"
		}
		var execErr error
		stderr := captureStderr(t, func() {
			_, execErr = ExecuteTaskGate(dir, "task-implement", state)
		})
		if execErr != nil {
			t.Fatalf(`task-implement 应 PASS（plan-first 是 advisory）: %v`, execErr)
		}
		return stderr, findPlanFirstEntry(t, dir)
	}

	// No Plan/Goal → advisory trail, gate passes.
	//
	// 无 Plan/Goal → advisory 留痕，门禁照常过。
	stderr, rec := run(t, false)
	if rec == nil {
		t.Fatal(`无方案任务应落 plan-first 条目`)
	}
	if rec.Passed {
		t.Error(`无方案任务的 plan-first 应 Passed=false`)
	}
	if rec.Level != checklog.LevelAdvisory {
		t.Errorf(`plan-first 应 Level=advisory, got %q`, rec.Level)
	}
	if !strings.Contains(stderr, "ADVISORY:") || !strings.Contains(stderr, "--plan-file") {
		t.Errorf(`stderr 应含方案前置 advisory: %q`, stderr)
	}

	// Goal present → Passed=true, silent stderr.
	//
	// 有 Goal → Passed=true，stderr 静默。
	stderr2, rec2 := run(t, true)
	if rec2 == nil || !rec2.Passed {
		t.Errorf(`有方案任务的 plan-first 应 Passed=true: %+v`, rec2)
	}
	if strings.Contains(stderr2, "plan-file") {
		t.Errorf(`有方案任务不应打印方案前置 advisory: %q`, stderr2)
	}
}

// countPlanFirstEntries 数 checklog 里 CheckPlanFirst 条目数。
//
// countPlanFirstEntries counts CheckPlanFirst entries in the checklog.
func countPlanFirstEntries(t *testing.T, dir string) int {
	t.Helper()
	entries, err := checklog.LoadAll(dir)
	if err != nil {
		t.Fatalf(`LoadAll: %v`, err)
	}
	n := 0
	for _, e := range entries {
		if e.Check == checklog.CheckPlanFirst {
			n++
		}
	}
	return n
}

// TestExecuteTaskGate_PlanFirstOncePerTask 钉住每任务一次契约（2026-08 噪音审计：同一
// advisory 单任务最多重复 3 次）：首次 implement 落条目 + stderr 提示并置
// PlanFirstAdvisoryFired；从磁盘重载 state 后的重试（模拟新一轮 forge 调用）条目与
// stderr 都静默，checklog 始终只有 1 条。
//
// TestExecuteTaskGate_PlanFirstOncePerTask pins the once-per-task contract (2026-08 noise
// audit: the identical advisory re-fired up to 3 times per task): the first implement
// records the entry + stderr nudge and sets PlanFirstAdvisoryFired; a retry on state
// reloaded from disk (simulating a new forge invocation) stays silent on both, and the
// checklog holds exactly 1 entry.
func TestExecuteTaskGate_PlanFirstOncePerTask(t *testing.T) {
	t.Setenv("FORGE_DATA_HOME", t.TempDir()) // 隔离 SaveTaskState 的全局 home
	dir := t.TempDir()
	initRepoWithMaster(t, dir)
	writeCommitSource(t, dir, map[string]string{
		"foo.go": "package main\n\nfunc Foo() int { return 1 }\n",
	}, "add foo")

	state := &TaskState{TaskRef: "plan-first-once", Branch: "feat/testcov"}
	stderr1 := captureStderr(t, func() {
		if _, err := ExecuteTaskGate(dir, "task-implement", state); err != nil {
			t.Fatalf(`首次 task-implement 应 PASS: %v`, err)
		}
	})
	if !strings.Contains(stderr1, "--plan-file") {
		t.Fatalf(`首次应打印方案前置 advisory: %q`, stderr1)
	}
	if !state.PlanFirstAdvisoryFired {
		t.Fatal(`首次后 PlanFirstAdvisoryFired 应置位`)
	}

	// 从磁盘重载（跨进程语义）——标记必须持久化。
	reloaded, err := LoadTaskState(dir, "plan-first-once")
	if err != nil {
		t.Fatalf(`LoadTaskState: %v`, err)
	}
	if !reloaded.PlanFirstAdvisoryFired {
		t.Fatal(`PlanFirstAdvisoryFired 应持久化到 task state`)
	}
	stderr2 := captureStderr(t, func() {
		if _, err := ExecuteTaskGate(dir, "task-implement", reloaded); err != nil {
			t.Fatalf(`重试 task-implement 应 PASS: %v`, err)
		}
	})
	if strings.Contains(stderr2, "--plan-file") {
		t.Errorf(`重试不应重发方案前置 advisory: %q`, stderr2)
	}
	if n := countPlanFirstEntries(t, dir); n != 1 {
		t.Fatalf(`CheckPlanFirst 条目应只有 1 条, got %d`, n)
	}
}
