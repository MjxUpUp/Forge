package skillsqa

import "testing"

// TestValidConditions_Vocabulary pins the R12 condition vocabulary in skillsqa itself.
// The cross-package sync guard (TestValidConditions_MatchEngine) lives in
// internal/skilltrigger's tests because skillsqa cannot import skilltrigger
// (import cycle); this test pins the skillsqa-side copy so a condition added here
// without the engine side (or vice versa) fails at least one of the two guards.
//
// TestValidConditions_Vocabulary 钉住 skillsqa 侧的 R12 condition 词汇表。
// 跨包同步守卫（TestValidConditions_MatchEngine）住在 internal/skilltrigger 的测试里
// （skillsqa 不能 import skilltrigger——import cycle）；本测试钉 skillsqa 侧副本，
// 任何一侧新增 condition 而漏改另一侧，两个守卫至少一个会红。
func TestValidConditions_Vocabulary(t *testing.T) {
	want := []string{
		"source_changed_uncommitted",
		"test_command_failed",
		"coding_intent",
		"task_active_no_review",
		"skill_file_touched", // P1b 新增：编辑 SKILL.md 时触发编写规范（skill-authoring-standard）
	}
	if len(ValidConditions) != len(want) {
		t.Fatalf("ValidConditions 词表数=%d want %d（%v）——新增 condition 须同步 skilltrigger.Conditions 并更新本测试",
			len(ValidConditions), len(want), validConditionsSorted())
	}
	for _, w := range want {
		if !ValidConditions[w] {
			t.Errorf("ValidConditions 缺 %q（实际=%v）", w, validConditionsSorted())
		}
	}
}

// TestValidConditionsSorted_Stable: R12 issue 文案用的排序输出必须稳定（字母序），
// 否则同一 skill 两次 audit 的 issue 文案抖动，checklog/golden 对比漂移。
func TestValidConditionsSorted_Stable(t *testing.T) {
	got := validConditionsSorted()
	if len(got) == 0 {
		t.Fatal("validConditionsSorted 不应为空")
	}
	for i := 1; i < len(got); i++ {
		if got[i-1] >= got[i] {
			t.Fatalf("validConditionsSorted 非字母序: %v", got)
		}
	}
}
