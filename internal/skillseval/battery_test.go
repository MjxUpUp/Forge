package skillseval

// battery_test.go — BuildBattery 的测试：回归 reject 阻断、无回归 accept、过期标记/
// 无 run 的 advisory 降级、未锚定覆盖缺口显性化、可比性降级。

import (
	"strings"
	"testing"
	"time"
)

// writeRun 追加手工构造的 EvalRun（绕过 SubmitRun——电池消费落盘 run，而 SubmitRun 需要
// 现存 case 集；直接构造 EvalRun 隔离测 BuildBattery）。
func writeRun(t *testing.T, dir, skill, runID, descHash string, results []CaseResult) {
	t.Helper()
	run := &EvalRun{
		RunID:        runID,
		Skill:        skill,
		Timestamp:    time.Now(),
		ForgeVersion: "v-test",
		AgentModel:   "m-test",
		DescHash:     descHash,
		Results:      results,
		HealthScore:  HealthScore(results, 0),
	}
	if err := AppendRun(dir, skill, run); err != nil {
		t.Fatalf("AppendRun(%s): %v", runID, err)
	}
}

func TestBuildBattery_Empty(t *testing.T) {
	dir := t.TempDir()
	rep, err := BuildBattery(dir)
	if err != nil {
		t.Fatalf("BuildBattery: %v", err)
	}
	if rep.Total != 0 || rep.GateBlocked || len(rep.Skills) != 0 {
		t.Errorf("空目录应为零行零阻断: %+v", rep)
	}
}

func TestBuildBattery_NoRegressionAccepted(t *testing.T) {
	dir := t.TempDir()
	results := []CaseResult{
		{CaseID: "c1", Kind: KindTrigger, Pass: true},
		{CaseID: "c2", Kind: KindNotTrigger, Pass: true},
	}
	writeRun(t, dir, "good-skill", "run-base", "dh1", results)
	writeRun(t, dir, "good-skill", "run-latest", "dh1", results)
	if err := SetBaseline(dir, "good-skill", "run-base", "test"); err != nil {
		t.Fatalf("SetBaseline: %v", err)
	}

	rep, err := BuildBattery(dir)
	if err != nil {
		t.Fatalf("BuildBattery: %v", err)
	}
	if rep.Total != 1 || rep.Rejected != 0 || rep.Accepted != 1 {
		t.Fatalf("应 1 行全 accept: %+v", rep)
	}
	row := rep.Skills[0]
	if !row.Accept || len(row.Reasons) != 0 || !row.Comparable || row.RegressionCount != 0 {
		t.Errorf("无回归行应为纯 accept: %+v", row)
	}
	if row.BaselineRun != "run-base" || row.LatestRun != "run-latest" {
		t.Errorf("run 指向错: %+v", row)
	}
	if rep.GateBlocked {
		t.Error("无回归不应阻断")
	}
	if len(rep.Unanchored) != 0 {
		t.Errorf("已锚定不应出现在 unanchored: %v", rep.Unanchored)
	}
}

func TestBuildBattery_RegressionRejected(t *testing.T) {
	dir := t.TempDir()
	base := []CaseResult{
		{CaseID: "c1", Kind: KindTrigger, Pass: true},
		{CaseID: "c2", Kind: KindTrigger, Pass: true},
	}
	latest := []CaseResult{
		{CaseID: "c1", Kind: KindTrigger, Pass: true},
		{CaseID: "c2", Kind: KindTrigger, Pass: false}, // matched 退化：baseline pass → latest fail
	}
	writeRun(t, dir, "bad-skill", "run-base", "dh1", base)
	writeRun(t, dir, "bad-skill", "run-latest", "dh1", latest)
	if err := SetBaseline(dir, "bad-skill", "run-base", "test"); err != nil {
		t.Fatalf("SetBaseline: %v", err)
	}

	rep, err := BuildBattery(dir)
	if err != nil {
		t.Fatalf("BuildBattery: %v", err)
	}
	if rep.Total != 1 || rep.Rejected != 1 || rep.Accepted != 0 {
		t.Fatalf("应 1 行 reject: %+v", rep)
	}
	row := rep.Skills[0]
	if row.Accept || row.RegressionCount != 1 || row.NetRegressions != 1 {
		t.Errorf("matched 退化应 reject: %+v", row)
	}
	if len(row.Reasons) == 0 || !strings.Contains(strings.Join(row.Reasons, "; "), "c2") {
		t.Errorf("reject 理由应含退化 case id: %v", row.Reasons)
	}
	if !rep.GateBlocked {
		t.Error("存在回归应 GateBlocked（Eq 6 不回归半边失败）")
	}
}

func TestBuildBattery_StaleAnchorAdvisory(t *testing.T) {
	dir := t.TempDir()
	writeRun(t, dir, "stale-skill", "run-real", "dh1", []CaseResult{
		{CaseID: "c1", Kind: KindTrigger, Pass: true},
	})
	// 锚到不存在的 run id —— 标记过期。
	if err := SetBaseline(dir, "stale-skill", "run-gone", "test"); err != nil {
		t.Fatalf("SetBaseline: %v", err)
	}
	rep, err := BuildBattery(dir)
	if err != nil {
		t.Fatalf("BuildBattery: %v", err)
	}
	row := rep.Skills[0]
	if !row.JudgmentImpossible || !row.Accept || len(row.Reasons) == 0 {
		t.Errorf("过期标记应 advisory accept（判定不可能）: %+v", row)
	}
	if rep.GateBlocked {
		t.Error("advisory 行不应阻断")
	}
}

func TestBuildBattery_AnchoredNoRunsAdvisory(t *testing.T) {
	dir := t.TempDir()
	if err := SetBaseline(dir, "empty-skill", "run-base", "test"); err != nil {
		t.Fatalf("SetBaseline: %v", err)
	}
	rep, err := BuildBattery(dir)
	if err != nil {
		t.Fatalf("BuildBattery: %v", err)
	}
	row := rep.Skills[0]
	if !row.JudgmentImpossible || !row.Accept || row.LatestRun != "" {
		t.Errorf("有锚无 run 应 advisory accept: %+v", row)
	}
	if rep.GateBlocked {
		t.Error("advisory 行不应阻断")
	}
}

func TestBuildBattery_IncomparableAdvisory(t *testing.T) {
	dir := t.TempDir()
	results := []CaseResult{{CaseID: "c1", Kind: KindTrigger, Pass: true}}
	writeRun(t, dir, "swap-skill", "run-base", "dh1", results)
	writeRun(t, dir, "swap-skill", "run-latest", "dh2", results) // desc 变了 → 不可比
	if err := SetBaseline(dir, "swap-skill", "run-base", "test"); err != nil {
		t.Fatalf("SetBaseline: %v", err)
	}
	rep, err := BuildBattery(dir)
	if err != nil {
		t.Fatalf("BuildBattery: %v", err)
	}
	row := rep.Skills[0]
	if row.Comparable || !row.Accept || len(row.Reasons) == 0 {
		t.Errorf("不可比应 advisory accept（交人工复核）: %+v", row)
	}
	if rep.GateBlocked {
		t.Error("不可比不应阻断")
	}
}

func TestBuildBattery_UnanchoredVisible(t *testing.T) {
	dir := t.TempDir()
	// 有 run 无 baseline —— 覆盖缺口应显性化。
	writeRun(t, dir, "orphan-skill", "run-1", "dh1", []CaseResult{
		{CaseID: "c1", Kind: KindTrigger, Pass: true},
	})
	rep, err := BuildBattery(dir)
	if err != nil {
		t.Fatalf("BuildBattery: %v", err)
	}
	if len(rep.Unanchored) != 1 || rep.Unanchored[0] != "orphan-skill" {
		t.Errorf("orphan-skill 应列入 unanchored: %v", rep.Unanchored)
	}
	if rep.Total != 0 || rep.GateBlocked {
		t.Errorf("无锚定行不入电池主体: %+v", rep)
	}
}
