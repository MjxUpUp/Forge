package clitask

import (
	"fmt"
	"strings"

	"github.com/MjxUpUp/Forge/internal/taskpipeline"
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
