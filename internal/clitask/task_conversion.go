package clitask

import (
	"fmt"
	"strings"

	"github.com/MjxUpUp/Forge/internal/taskpipeline"
	"github.com/MjxUpUp/Forge/internal/tasktypes"
)

// task_conversion.go —— plan-first advisory 转化率报告的 CLI 渲染
// （artifact-chain-workflow.md §6，`forge task list --plan-conversion`）。
func runPlanConversionReport(states []*taskpipeline.TaskState) error {
	stats := taskpipeline.ComputePlanConversion(states)
	fmt.Println("Plan-first advisory 转化率（先测量再翻转——升档由数据决定）：")
	fmt.Println(strings.Repeat("─", 60))
	fmt.Printf("  已发 advisory      : %d\n", stats.Fired)
	fmt.Printf("  发后已补方案       : %d\n", stats.Converted)
	fmt.Printf("  发后仍无方案       : %d\n", stats.Unconverted)
	fmt.Printf("  未发但自带方案     : %d\n", stats.UnfiredWithPlan)
	renderTierStats(stats.ByTier)
	if stats.Fired == 0 {
		fmt.Println("  转化率             : n/a（尚无 advisory 触达样本）")
		return nil
	}
	fmt.Printf("  转化率             : %.0f%%（%d/%d）\n", stats.ConversionRate()*100, stats.Converted, stats.Fired)
	if stats.ConversionRate() < 0.5 {
		fmt.Println("  → 提示：转化率 <50%，advisory 提醒对该 cohort 行为改变有限——考虑对非 trivial 任务升档而非加频提醒")
	}
	return nil
}

// renderTierStats prints the per-design-tier skip/override rates (design-stage-hardening
// W0.2). Rising rates are the "gate too heavy" signal.
//
// renderTierStats 打印按方案档位的跳过率/否决率（design-stage-hardening W0.2）。
// 比率持续上升 = 门禁太重的信号。
func renderTierStats(byTier map[string]taskpipeline.TierStats) {
	if len(byTier) == 0 {
		return
	}
	fmt.Println("  方案档位健康度（跳过 = 要求方案却无 design/plan；否决 = 带理由压低硬触发器）：")
	for _, tier := range []string{tasktypes.DesignTierNone, tasktypes.DesignTierMini, tasktypes.DesignTierFull} {
		ts, ok := byTier[tier]
		if !ok || ts.Tasks == 0 {
			continue
		}
		fmt.Printf("    %-5s 任务 %d · 跳过率 %.0f%% · 否决率 %.0f%%\n", tier, ts.Tasks,
			float64(ts.Skipped)*100/float64(ts.Tasks), float64(ts.Overridden)*100/float64(ts.Tasks))
	}
}
