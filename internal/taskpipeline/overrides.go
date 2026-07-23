package taskpipeline

import "os"

// TaskOverrides 承载 per-task 逃生舱设置。优先于全局 env，是方案5 的"防泄漏"机制：
// 一个任务逃生（经 `forge task override`）不污染同 shell 的其他任务——全局 env
// FORGE_WORK_ACTIVITY / FORGE_TEST_COVERAGE 仍作 CI/测试 fallback，但 per-task
// override 是推荐路径。值 "disable" = 禁用对应门禁。
//
// 用了任一逃生舱 → checklog CheckEscapeHatch → evidence Strength cap Weak（让逃生
// 有代价，对冲"硬门禁 + 全局逃生舱 = 假硬门禁"反噬）。
type TaskOverrides struct {
	WorkActivity string `json:"work_activity,omitempty"` // "disable" 跳过 read-before-edit / work-activity 门禁
	TestCoverage string `json:"test_coverage,omitempty"` // "disable" 跳过 test-coverage 门禁
}

// EscapeDisabled 报告 which（"work-activity"/"test-coverage"）逃生舱对本任务是否生效。
// per-task Overrides 优先于 process-global env（防泄漏路径）；env 留作 CI/测试 fallback。
// 调用方：work-activity 门禁（executor）、test-coverage 门禁（testcoverage）。
func EscapeDisabled(state *TaskState, which, envVar string) bool {
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
		}
	}
	return os.Getenv(envVar) == "disable"
}

const (
	// escapeWorkActivity / escapeTestCoverage: EscapeDisabled 的 which 键。
	escapeWorkActivity = "work-activity"
	escapeTestCoverage = "test-coverage"
	// envWorkActivity: work-activity 逃生舱对应的全局 env（executor getDisableWorkActivity）。
	envWorkActivity = "FORGE_WORK_ACTIVITY"
)
