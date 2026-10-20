package taskpipeline

import "testing"

// loopAgeFixture 建一个预算 max_rounds=2、已过 3 轮复核的回环测试项目。
func loopAgeFixture(t *testing.T) string {
	t.Helper()
	t.Setenv("FORGE_DATA_HOME", t.TempDir())
	dir := t.TempDir()
	initRepoWithMaster(t, dir)
	writeLoopSchema(t, dir, "version: 1\nstages:\n  - name: proposal\nedges:\n  - from: review\n    to: implement\n    max_rounds: 2\n")
	return dir
}

// TestLoopAgeCountsFromFindingRound 钉住轮龄从 finding 自身的提出轮起算：第 3 轮
// 才提出的 open finding 在 3 轮复核后轮龄为 1，不得因"总轮数已超预算"而耗尽
// （mutation 位点 findingLoopAge 的 round<1 下限判断存活所补）。
func TestLoopAgeCountsFromFindingRound(t *testing.T) {
	dir := loopAgeFixture(t)
	state := &TaskState{TaskRef: "loop-age-round", Branch: "feat/loop"}
	state.ReviewRounds = []ReviewRound{{}, {}, {}}
	state.Findings = []Finding{{ID: "f1", Content: "刚提的问题", Status: "open", Round: 3}}
	if marker := evaluateLoopExhaustion(dir, state); marker != nil {
		t.Fatalf("Round=3 的 finding 轮龄 1 < 预算 2，不应耗尽: %+v", marker)
	}
}

// TestLoopAgeIgnoresResolvedFindings 钉住只有 open（含旧数据的空状态）finding
// 计龄：已修/放弃的超龄 finding 不得触发耗尽（mutation 位点 evaluateLoopExhaustion
// 状态过滤判断存活所补）。
func TestLoopAgeIgnoresResolvedFindings(t *testing.T) {
	dir := loopAgeFixture(t)
	state := &TaskState{TaskRef: "loop-age-resolved", Branch: "feat/loop"}
	state.ReviewRounds = []ReviewRound{{}, {}, {}}
	state.Findings = []Finding{
		{ID: "f1", Content: "已修", Status: "fixed", Round: 1},
		{ID: "f2", Content: "放弃", Status: "wontfix", Round: 1},
	}
	if marker := evaluateLoopExhaustion(dir, state); marker != nil {
		t.Fatalf("只有已修/放弃的 finding，不应耗尽: %+v", marker)
	}
	// 旧数据的空状态视同 open：同样超龄应耗尽。
	state.Findings = append(state.Findings, Finding{ID: "f3", Content: "旧数据", Status: "", Round: 1})
	if marker := evaluateLoopExhaustion(dir, state); marker == nil {
		t.Fatal("空状态（旧数据）视同 open，超龄应耗尽")
	}
}
