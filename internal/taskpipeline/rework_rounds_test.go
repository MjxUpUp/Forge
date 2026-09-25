package taskpipeline

import (
	"testing"
)

// TestMarkReviewPassed_AppendsReviewRounds 钉住每次 review pass 都追加一条 ReviewRound
// （返工度量原料），且快照字段始终指向最新一轮——快照校验契约（executor.go）不变。
func TestMarkReviewPassed_AppendsReviewRounds(t *testing.T) {
	state := &TaskState{TaskRef: "t-rework"}
	state.MarkReviewPassed("head1", "hash1")
	state.MarkReviewPassed("head2", "hash2")

	if len(state.ReviewRounds) != 2 {
		t.Fatalf(`两次 pass 应追加 2 条 ReviewRound, got %d`, len(state.ReviewRounds))
	}
	if state.ReviewRounds[0].HeadCommit != "head1" || state.ReviewRounds[1].HeadCommit != "head2" {
		t.Errorf(`ReviewRounds 应按序记录两轮快照: %+v`, state.ReviewRounds)
	}
	if state.ReviewRounds[0].ReviewedAt.IsZero() {
		t.Error(`ReviewRound 应带 ReviewedAt 时间戳`)
	}
	// 快照字段指向最新一轮（门禁契约不变）。
	if state.ReviewedHeadCommit != "head2" || state.ReviewedChangeHash != "hash2" {
		t.Errorf(`快照字段应指向最新一轮, got %s/%s`, state.ReviewedHeadCommit, state.ReviewedChangeHash)
	}
	if !state.ReviewPassed {
		t.Error(`ReviewPassed 应为 true`)
	}
}

// TestReworkRounds 钉住推导：reviewPasses = len(ReviewRounds)；completeRejections =
// History 里 task-complete 的失败条数（通过条目与其他门禁不计）。
func TestReworkRounds(t *testing.T) {
	state := &TaskState{TaskRef: "t-rework2"}
	// Zero state.
	if p, r := state.ReworkRounds(); p != 0 || r != 0 {
		t.Errorf(`零状态应 (0,0), got (%d,%d)`, p, r)
	}
	state.RecordGateResult("task-implement", true, "")
	state.RecordGateResult("task-verify", true, "")
	state.RecordGateResult("task-complete", false, "") // 被拒 1 次
	state.RecordGateResult("task-complete", false, "") // 被拒 2 次
	state.RecordGateResult("task-complete", true, "")
	state.MarkReviewPassed("h1", "")
	state.MarkReviewPassed("h2", "")

	passes, rejections := state.ReworkRounds()
	if passes != 2 {
		t.Errorf(`reviewPasses 应为 2, got %d`, passes)
	}
	if rejections != 2 {
		t.Errorf(`completeRejections 应为 2, got %d`, rejections)
	}
}

// TestScoreTask_AttachesReworkEvidence 钉住 ScoreTask 把返工度量折进
// ScoreResult.Evidence（ReviewPasses / CompleteRejections）——仅可观测，加权总分
// 不受影响。同时钉住无返工时字段保持零值。
func TestScoreTask_AttachesReworkEvidence(t *testing.T) {
	dir, state := setupScoreableTask(t, "rework-evidence")
	state.MarkReviewPassed("h1", "c1")
	state.MarkReviewPassed("h2", "c2")
	// 重建 history：先有一次被拒的 complete 再通过（setupScoreableTask 记的是干净
	// 通过；加一次拒绝让度量可观测）。
	state.History = nil
	state.CurrentGate = ""
	state.RecordGateResult("task-implement", true, "")
	state.RecordGateResult("task-verify", true, "")
	state.RecordGateResult("task-complete", false, "")
	state.RecordGateResult("task-complete", true, "")

	if err := ScoreTask(dir, state); err != nil {
		t.Fatalf("ScoreTask: %v", err)
	}
	if state.Score == nil || state.Score.Evidence == nil {
		t.Fatal("Score/Evidence 应非空")
	}
	if state.Score.Evidence.ReviewPasses != 2 {
		t.Errorf(`ReviewPasses 应为 2, got %d`, state.Score.Evidence.ReviewPasses)
	}
	if state.Score.Evidence.CompleteRejections != 1 {
		t.Errorf(`CompleteRejections 应为 1, got %d`, state.Score.Evidence.CompleteRejections)
	}

	// 对照：无审查/返工 → 字段保持零值。
	dir2, state2 := setupScoreableTask(t, "rework-none")
	if err := ScoreTask(dir2, state2); err != nil {
		t.Fatalf("ScoreTask: %v", err)
	}
	if state2.Score.Evidence != nil &&
		(state2.Score.Evidence.ReviewPasses != 0 || state2.Score.Evidence.CompleteRejections != 0) {
		t.Errorf(`无返工任务的度量应为零值, got %+v`, state2.Score.Evidence)
	}
}
