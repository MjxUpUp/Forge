package evalkit

// judgeaudit_test.go — 判分器审计的测试：MVVP 可选轮次（retest/position/cue）
// 与向后兼容、披露清单契约。

import (
	"strings"
	"testing"
	"time"
)

// TestRunJudgeAudit_MVVP 可选轮次：retest 一致率 / position bias / cue 翻转率
// 与 finding 触发；旧格式（零可选字段）向后兼容（三项 = -1 未运行）。
func TestRunJudgeAudit_MVVP(t *testing.T) {
	// 旧格式：无 MVVP 字段。
	old := []JudgeAuditEntry{
		{DocID: "a", JudgeScores: []int{80, 82}, HumanScore: 85, Threshold: 75},
		{DocID: "b", JudgeScores: []int{60, 62}, HumanScore: 55, Threshold: 75},
	}
	rep, err := RunJudgeAudit(old)
	if err != nil {
		t.Fatal(err)
	}
	if rep.RetestAgreement != -1 || rep.PositionBias != -1 || rep.CueFlipRate != -1 {
		t.Fatalf("旧格式三项应 -1（未运行）: %+v", rep)
	}
	// MVVP 轮次：retest 一次翻转、swap 均值差 +8、cue 翻转。
	mvvp := []JudgeAuditEntry{
		{DocID: "a", JudgeScores: []int{80}, HumanScore: 85, Threshold: 75,
			RetestScores: []int{70}, SwappedScores: []int{90}, CueScores: []int{60}},
		{DocID: "b", JudgeScores: []int{60}, HumanScore: 55, Threshold: 75,
			RetestScores: []int{62}, SwappedScores: []int{66}, CueScores: []int{61}},
	}
	rep, err = RunJudgeAudit(mvvp)
	if err != nil {
		t.Fatal(err)
	}
	if rep.RetestAgreement != 0.5 {
		t.Fatalf("retest 一致率应 0.5: %v", rep.RetestAgreement)
	}
	if rep.PositionBias < 7.9 || rep.PositionBias > 8.1 {
		t.Fatalf("position bias 应 ≈+8: %v", rep.PositionBias)
	}
	if rep.CueFlipRate != 0.5 {
		t.Fatalf("cue 翻转率应 0.5: %v", rep.CueFlipRate)
	}
	joined := strings.Join(rep.Findings, "\n")
	for _, want := range []string{"test-retest", "position bias", "cue 敏感度"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("缺 finding %q: %s", want, joined)
		}
	}
}

// TestHarnessDisclosure 披露清单：四元组要素 + mix 排序稳定。
func TestHarnessDisclosure(t *testing.T) {
	lines := harnessDisclosure(RunSpec{Profile: "full", ForgeRef: "v1.51.0", Repeats: 2,
		Budget: Budget{WallclockEach: 10 * time.Second}}, "docker", map[string]int{"exec": 1, "docker": 3})
	joined := strings.Join(lines, "\n")
	for _, want := range []string{"harness: forge (v1.51.0)", "layer-profile: full", "sandbox-backend: docker", "repeats-per-task: 2", "sandbox-mix: docker=3, exec=1"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("披露缺 %q:\n%s", want, joined)
		}
	}
}
