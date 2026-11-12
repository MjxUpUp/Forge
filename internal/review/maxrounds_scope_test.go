package review

import (
	"testing"
	"time"
)

// TestEvaluateFor_MaxRoundsScopedStampWithForeignChanges 钉住撞轮条件的第一个析取
// （stamp.DiffHash == hash）：共享检出里既有本会话的变更、也有别的会话的点缀时，
// 作用域戳的 hash 与整树戳不同——此时兜底放行只能靠「戳等于当前作用域指纹」。
// 把它改成 != 后本用例从 advisory 变回需审，测试即失败。
//
// 既有 TestEvaluateFor_MaxRoundsAdvisory 没有外来变更，scoped == whole，
// 第二个析取会替第一个兜底，因此杀不掉这处变异。
func TestEvaluateFor_MaxRoundsScopedStampWithForeignChanges(t *testing.T) {
	pinEnv(t)
	root := initGitRepo(t)
	write(t, root, "a.go", "package a\n")
	write(t, root, "b.go", "package b\n")
	recordLedger(t, root, "sess-a", "a.go", time.Now())
	recordLedger(t, root, "sess-b", "b.go", time.Now())

	// 前提守卫：本夹具的杀灭力依赖「排除 b.go 后 scope 与 whole 不同」。
	scoped, whole, _, err := scopeAndWholeFingerprint(root, "", map[string]bool{"b.go": true})
	if err != nil {
		t.Fatalf("作用域指纹: %v", err)
	}
	if whole == "" || scoped == whole {
		t.Fatalf("前提不成立：scope=%q whole=%q（外来路径未被排除，第二个析取会替第一处兜底）", scoped, whole)
	}

	for i := 1; i <= MaxReviewRounds; i++ {
		dec, reason, err := EvaluateFor(root, "sess-a")
		if err != nil {
			t.Fatalf("round %d: %v", i, err)
		}
		if dec != DecisionNeedReview {
			t.Fatalf("round %d 应为需审，得 %v（%s）", i, dec, reason)
		}
	}
	dec, reason, err := EvaluateFor(root, "sess-a")
	if err != nil {
		t.Fatalf("超限轮: %v", err)
	}
	if dec != DecisionPassAdvisory {
		t.Fatalf("撞满 %d 轮应 advisory 放行（戳=作用域指纹、整树戳不同），得 %v（%s）", MaxReviewRounds, dec, reason)
	}
}
