package taskpipeline

import "testing"

// TestComputePlanConversion 钉住四计数口径（§6）：fired 以 PlanFirstAdvisoryFired
// 为准；转化按扫描时刻 Plan|Goal 非空判定；未发但有方案单列（start 自带方案的
// cohort 不该混进「发后转化」）。
func TestComputePlanConversion(t *testing.T) {
	states := []*TaskState{
		{TaskRef: "a", PlanFirstAdvisoryFired: true, Plan: "后来补了方案"},
		{TaskRef: "b", PlanFirstAdvisoryFired: true, Goal: "后来补了目标"},
		{TaskRef: "c", PlanFirstAdvisoryFired: true},
		{TaskRef: "d", Plan: "start 时自带"},
		{TaskRef: "e"},
		nil,
	}
	stats := ComputePlanConversion(states)
	if stats.Fired != 3 || stats.Converted != 2 || stats.Unconverted != 1 || stats.UnfiredWithPlan != 1 {
		t.Fatalf("四计数口径不符: %+v", stats)
	}
	if r := stats.ConversionRate(); r < 0.66 || r > 0.67 {
		t.Fatalf("转化率应 2/3, got %f", r)
	}
	if (PlanConversionStats{}).ConversionRate() != 0 {
		t.Fatal("零样本转化率应 0（不 panic 不 NaN）")
	}
}
