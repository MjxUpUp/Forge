package taskpipeline

import (
	"strings"
	"testing"
)

// TestNextDecision_CompleteHintMentionsFacts 钉住收尾提示的措辞：3.0 起完成面只采集事实，
// 提示里不能再出现「评分」（评分包已删除，写评分会误导 agent 去找分数）。
func TestNextDecision_CompleteHintMentionsFacts(t *testing.T) {
	st := &TaskState{TaskRef: "feat/x"}
	st.History = []TaskGateResult{
		{Gate: GateImplement, Passed: true},
		{Gate: GateComplete, Passed: true},
	}
	st.ReviewPassed = true
	st.Acceptance = []AcceptanceCriterion{{Run: "go test ./...", AcceptedHeadCommit: "abc123"}}

	got := NextDecision("feat/x", false, 0, st)
	if got.Next != "forge task complete" {
		t.Fatalf("Next = %q, want forge task complete", got.Next)
	}
	if strings.Contains(got.Reason, "评分") {
		t.Fatalf("Reason 仍提评分（3.0 已删评分）: %q", got.Reason)
	}
	if !strings.Contains(got.Reason, "采集完成事实") {
		t.Fatalf("Reason 应说明采集完成事实: %q", got.Reason)
	}
}
