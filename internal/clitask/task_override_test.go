package clitask

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

// TestTaskOverrideCmdShortNoWorkActivityLie 守卫 override 命令的 Short 帮助不得再对
// work-activity 谎报强度（复审第二轮，2026-08）：该命令在验证类逃生舱之外还接受
// --work-activity，笼统声称"降强度到 Weak"对 work-activity 用户是谎言（它是节奏
// 门禁——checklog.isRhythmEscapeHatch 从不 cap Strength）。Short 必须区分两类，
// 与 task.go runTaskOverride 的详细输出一致。
func TestTaskOverrideCmdShortNoWorkActivityLie(t *testing.T) {
	short := taskOverrideCmd.Short
	if !strings.Contains(short, "work-activity 是节奏门禁不降强度") {
		t.Errorf(`override Short 必须声明 work-activity 是节奏门禁不降强度（防谎报回归），got: %q`, short)
	}
	if !strings.Contains(short, "重证据按证据缩放豁免") {
		t.Errorf(`override Short 必须提及验证类逃生的证据缩放豁免，got: %q`, short)
	}
}
