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
