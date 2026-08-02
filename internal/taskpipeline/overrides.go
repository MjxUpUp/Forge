package taskpipeline

import (
	"os"

	"github.com/MjxUpUp/Forge/internal/checklog"
)

// TaskOverrides holds the per-task escape-hatch settings. It takes precedence over the global env and is the
// anti-leak mechanism of plan 5: one task escaping (via `forge task override`) does not pollute other tasks in the
// same shell — the global env FORGE_WORK_ACTIVITY / FORGE_TEST_COVERAGE / FORGE_SKILL_DECISIONS still serves as a
// CI/test fallback, but per-task override is the recommended path. The value disable = disable the corresponding gate.
//
// Using any escape hatch → checklog CheckEscapeHatch → evidence Strength capped at Weak (giving escape a cost, to
// counter the hard-gate-plus-global-escape-hatch-equals-fake-hard-gate backlash).
//
// TaskOverrides 承载 per-task 逃生舱设置。优先于全局 env，是方案5 的「防泄漏」机制：
// 一个任务逃生（经 `forge task override`）不污染同 shell 的其他任务——全局 env
// FORGE_WORK_ACTIVITY / FORGE_TEST_COVERAGE / FORGE_SKILL_DECISIONS 仍作 CI/测试
// fallback，但 per-task override 是推荐路径。值"disable"= 禁用对应门禁。
//
// 用了任一逃生舱 → checklog CheckEscapeHatch → evidence Strength cap Weak（让逃生
// 有代价，对冲「硬门禁 + 全局逃生舱 = 假硬门禁」反噬）。
type TaskOverrides struct {
	WorkActivity   string `json:"work_activity,omitempty"`   // "disable" 跳过 read-before-edit / work-activity 门禁
	TestCoverage   string `json:"test_coverage,omitempty"`   // "disable" 跳过 test-coverage 门禁
	AcceptanceGate string `json:"acceptance_gate,omitempty"` // "disable" 跳过 task-complete acceptance pre-flight 门禁
	SkillDecisions string `json:"skill_decisions,omitempty"` // "disable" 跳过 skill-decisions guardrail（改 SKILL.md 必须记决策）
}

// escapeDisabled reports whether the escape hatch named by which (work-activity / test-coverage / skill-decisions) is
// in effect for this task. per-task Overrides take precedence over the process-global env (the anti-leak path); the env
// remains a CI/test fallback. Callers: the work-activity gate (executor), the test-coverage gate (testcoverage), and the
// skill-decisions guardrail (executor).
//
// escapeDisabled 报告 which（"work-activity"/"test-coverage"/"skill-decisions"）逃生舱
// 对本任务是否生效。per-task Overrides 优先于 process-global env（防泄漏路径）；env 留作
// CI/测试 fallback。调用方：work-activity 门禁（executor）、test-coverage 门禁（testcoverage）、
// 以及 skill-decisions guardrail（executor）。
func escapeDisabled(state *TaskState, which, envVar string) bool {
	if state != nil {
		switch which {
		case "work-activity":
			if state.Overrides.WorkActivity == "disable" {
				return true
			}
		case "test-coverage":
			if state.Overrides.TestCoverage == "disable" {
				return true
			}
		case "acceptance-gate":
			if state.Overrides.AcceptanceGate == "disable" {
				return true
			}
		case "skill-decisions":
			if state.Overrides.SkillDecisions == "disable" {
				return true
			}
		}
	}
	return os.Getenv(envVar) == "disable"
}

// usedAnyOverride reports whether any per-task escape hatch is set to "disable".
// ScoreTask uses this (rather than checklog escape-hatch entries) as one of the two
// escape signals for score capping: a task that SET an override but never hit the
// bypass branch has no checklog entry, yet the intent to escape is on record and
// must cost the same. The complementary signal is taskEscapeHatchRecorded, which
// covers env-form escapes that bypass without touching state.Overrides.
//
// usedAnyOverride 报告是否有任一 per-task 逃生舱被设为 "disable"。ScoreTask 用它
// 作封顶的两个逃生信号之一：设了 override 但没走到 bypass 分支的任务 checklog
// 无条目，但逃生意图已留痕，必须付同样代价。互补信号是
// taskEscapeHatchRecorded——覆盖不动 state.Overrides 的 env 形式逃生。
func usedAnyOverride(o TaskOverrides) bool {
	return o.WorkActivity == "disable" || o.TestCoverage == "disable" ||
		o.AcceptanceGate == "disable" || o.SkillDecisions == "disable"
}

// taskEscapeHatchRecorded reports whether the task's checklog contains any
// CheckEscapeHatch entry. This is the cap signal for env-form escapes
// (FORGE_TEST_COVERAGE=disable and friends): they bypass via escapeDisabled without
// touching state.Overrides, but every bypass branch records the escape-hatch entry.
//
// taskEscapeHatchRecorded 报告任务的 checklog 是否含任一 CheckEscapeHatch 条目。
// 这是 env 形式逃生（FORGE_TEST_COVERAGE=disable 等）的封顶信号：它们经
// escapeDisabled 绕过、不动 state.Overrides，但每个 bypass 分支都会记录逃生舱条目。
func taskEscapeHatchRecorded(root, taskRef string) (bool, error) {
	entries, err := checklog.LoadForTask(root, taskRef)
	if err != nil {
		return false, err
	}
	for _, e := range entries {
		if e.Check == checklog.CheckEscapeHatch {
			return true, nil
		}
	}
	return false, nil
}

const (
	// escapeWorkActivity / escapeTestCoverage / escapeAcceptanceGate / escapeSkillDecisions: the which keys of escapeDisabled.
	// escapeWorkActivity / escapeTestCoverage / escapeAcceptanceGate / escapeSkillDecisions: escapeDisabled 的 which 键。
	escapeWorkActivity   = "work-activity"
	escapeTestCoverage   = "test-coverage"
	escapeAcceptanceGate = "acceptance-gate"
	escapeSkillDecisions = "skill-decisions"
	// envWorkActivity: the global env for the work-activity escape hatch (executor getDisableWorkActivity).
	// envWorkActivity: work-activity 逃生舱对应的全局 env（executor getDisableWorkActivity）。
	envWorkActivity = "FORGE_WORK_ACTIVITY"
	// envSkillDecisions: the global env for the skill-decisions escape hatch (CI/test fallback).
	// envSkillDecisions: skill-decisions 逃生舱对应的全局 env（CI/测试 fallback）。
	envSkillDecisions = "FORGE_SKILL_DECISIONS"
)
