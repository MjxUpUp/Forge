package cli

// task_override_test.go — unit tests for describeOverrides. Covers the skill-decisions branch added in
// component B + the symmetry of the three flags (work-activity/test-coverage/skill-decisions) + empty state.
// The end-to-end effectiveness of the CLI flag→state.Overrides mapping (the case block of task.go runTaskOverride)
// is guarded by the executor integration test TestTaskVerify_SkillDecisionsGuardrail_EscapeHatchBypasses
// (state.Overrides.SkillDecisions=`disable` → not blocked + records CheckEscapeHatch).
//
// task_override_test.go — describeOverrides 单元测试。覆盖 B 组件新增的 skill-decisions 分支
// + 三 flag 对称（work-activity/test-coverage/skill-decisions）+ 空状态。CLI flag→state.Overrides
// 映射（task.go runTaskOverride 的 case 块）的端到端生效由 executor 集成测试
// TestTaskVerify_SkillDecisionsGuardrail_EscapeHatchBypasses（state.Overrides.SkillDecisions="disable"
// → 不阻断 + 落 CheckEscapeHatch）守护。

import (
	"strings"
	"testing"

	"github.com/MjxUpUp/Forge/internal/taskpipeline"
)

func TestDescribeOverrides_SkillDecisions(t *testing.T) {
	got := describeOverrides(taskpipeline.TaskOverrides{SkillDecisions: "disable"})
	if !strings.Contains(got, "skill-decisions=disable") {
		t.Errorf(`describeOverrides 应含 "skill-decisions=disable"，got: %s`, got)
	}
}

func TestDescribeOverrides_AllThreeFlags(t *testing.T) {
	got := describeOverrides(taskpipeline.TaskOverrides{
		WorkActivity:   "disable",
		TestCoverage:   "disable",
		SkillDecisions: "disable",
	})
	for _, want := range []string{"work-activity=disable", "test-coverage=disable", "skill-decisions=disable"} {
		if !strings.Contains(got, want) {
			t.Errorf(`describeOverrides 三 flag 都设时应全出现，缺 %q，got: %s`, want, got)
		}
	}
}

func TestDescribeOverrides_Empty(t *testing.T) {
	got := describeOverrides(taskpipeline.TaskOverrides{})
	if got != "（无）" {
		t.Errorf(`空状态应显示 "（无）"，got: %q`, got)
	}
}
