package cli

import (
	"testing"

	"github.com/MjxUpUp/Forge/internal/checklog"
	"github.com/MjxUpUp/Forge/internal/taskpipeline"
)

// TestScoreTask_EvidenceFromChecklog guards the CLI wiring point: scoreTask aggregates
// evidence buckets from the checklog and writes them into ScoreResult.Evidence. It pins the
// end-to-end chain checklog.ForTask → EvaluateInput → ScoreResult.Evidence — if ForTask's
// signature changes or the wiring breaks, the foundation would silently degrade (every task
// scoring nil Evidence); this test turns it into a regression-verifiable contract.
//
// The wiring point (the evDeterministic/evAgentClaim assignments in task.go scoreTask) is
// the only link in the evidence-chain foundation without unit-test coverage; the rest is
// guarded by TestForTask_LoadsAndBuckets (checklog layer) + TestEvaluate_EvidenceSummary
// (scoring layer).
//
// TestScoreTask_EvidenceFromChecklog 守卫 CLI 接线点：scoreTask 从 checklog 聚合
// 证据桶写入 ScoreResult.Evidence。锁住 checklog.ForTask → EvaluateInput →
// ScoreResult.Evidence 的端到端链路——若 ForTask 签名变更或接线断裂，底座会
// 静默失效（所有任务评出 nil Evidence），本测试把它钉成可回归验证。
//
// 接线点（task.go scoreTask 的 evDeterministic/evAgentClaim 赋值）是证据链底座
// 唯一无单元测试覆盖的环节，其余由 TestForTask_LoadsAndBuckets（checklog 层）+
// TestEvaluate_EvidenceSummary（scoring 层）守。
func TestScoreTask_EvidenceFromChecklog(t *testing.T) {
	dir := t.TempDir()

	// Write 3 checklog entries: auto-compile + assertion = deterministic(2), task-verify = agent-claim(1).
	// Source fallback is inferred at write time (SourceForCheck), no explicit annotation needed.
	//
	// 写 3 条 checklog：auto-compile + assertion = deterministic(2)，task-verify = agent-claim(1)。
	// Source 兜底由 Record 写盘时推断（SourceForCheck），无需显式标注。
	for _, c := range []checklog.CheckName{checklog.CheckAutoCompile, checklog.CheckAssertion, checklog.CheckTaskVerify} {
		if err := checklog.Record(dir, &checklog.Entry{Check: c, Passed: true, TaskRef: "t-ev"}); err != nil {
			t.Fatalf(`Record %s: %v`, c, err)
		}
	}

	state := &taskpipeline.TaskState{TaskRef: "t-ev", Branch: "feat/x"}
	if err := scoreTask(dir, state); err != nil {
		t.Fatalf(`scoreTask: %v`, err)
	}
	if state.Score == nil {
		t.Fatal(`state.Score nil after scoreTask`)
	}
	if state.Score.Evidence == nil {
		t.Fatalf(`expected non-nil Score.Evidence (checklog has 3 entries for t-ev), got nil — ForTask wiring broken?`)
	}
	ev := state.Score.Evidence
	if ev.Deterministic != 2 || ev.AgentClaim != 1 {
		t.Fatalf(`evidence buckets: got det=%d claim=%d, want 2/1 (auto-compile+assertion=deterministic, task-verify=agent-claim)`,
			ev.Deterministic, ev.AgentClaim)
	}
	if ev.Total != 3 {
		t.Fatalf(`evidence total: got %d, want 3`, ev.Total)
	}
}
