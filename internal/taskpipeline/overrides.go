package taskpipeline

import (
	"os"

	"github.com/MjxUpUp/Forge/internal/checklog"
)

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
		case "doc-gate":
			if state.Overrides.DocGate == "disable" {
				return true
			}
		}
	}
	return os.Getenv(envVar) == "disable"
}

// usedAnyOverride 报告是否有任一 per-task 逃生舱被设为 "disable"。ScoreTask 用它
// 作封顶的两个逃生信号之一：设了 override 但没走到 bypass 分支的任务 checklog
// 无条目，但逃生意图已留痕，必须付同样代价。互补信号是
// taskEscapeHatchRecorded——覆盖不动 state.Overrides 的 env 形式逃生。
func usedAnyOverride(o TaskOverrides) bool {
	return o.WorkActivity == "disable" || o.TestCoverage == "disable" ||
		o.AcceptanceGate == "disable" || o.SkillDecisions == "disable" ||
		o.DocGate == "disable"
}

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
	escapeDocGate        = "doc-gate"
	// envWorkActivity: the global env for the work-activity escape hatch (executor getDisableWorkActivity).
	// envWorkActivity: work-activity 逃生舱对应的全局 env（executor getDisableWorkActivity）。
	envWorkActivity = "FORGE_WORK_ACTIVITY"
	// envSkillDecisions: the global env for the skill-decisions escape hatch (CI/test fallback).
	// envSkillDecisions: skill-decisions 逃生舱对应的全局 env（CI/测试 fallback）。
	envSkillDecisions = "FORGE_SKILL_DECISIONS"
)
