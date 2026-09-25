package taskpipeline

// plan_conversion.go —— advisory 转化率度量（artifact-chain-workflow.md §6）：
// plan-first advisory 已发的任务里，多少随后补了方案（Plan|Goal 非空）。
// 「先测量再翻转」的供数面：哪些叙事 stage 值得从 advisory 升档，由转化率
// 数据决定而非直觉。只读聚合，零新遥测（landing doc L4 飞轮的本地前置切片）。
//
// 语义边界：转化按「扫描时刻 state 里 Plan/Goal 是否非空」判定——advisory 发出
// 与方案落地之间没有因果钉（可能任务本就带方案，fired 前提已排除这一点：
// fired 只发生在 Plan/Goal 皆空的首个 implement 轮），但「之后人工/agent 补写」
// 与「另起炉灶」不可区分。这是本报告的已知噪声，宁粗勿缺。

// PlanConversionStats is the cohort-level conversion measurement.
type PlanConversionStats struct {
	Fired           int `json:"fired"`             // 已发 advisory 的任务数
	Converted       int `json:"converted"`         // 发后补了方案（Plan|Goal 非空）
	Unconverted     int `json:"unconverted"`       // 发后仍无方案
	UnfiredWithPlan int `json:"unfired_with_plan"` // 未发 advisory 但有方案（start 时自带 --plan-file/--goal）
}

// ConversionRate returns converted/fired (0 when nothing fired).
func (s PlanConversionStats) ConversionRate() float64 {
	if s.Fired == 0 {
		return 0
	}
	return float64(s.Converted) / float64(s.Fired)
}

// ComputePlanConversion aggregates conversion over the given task states.
func ComputePlanConversion(states []*TaskState) PlanConversionStats {
	var stats PlanConversionStats
	for _, s := range states {
		if s == nil {
			continue
		}
		hasPlan := s.Plan != "" || s.Goal != ""
		if s.PlanFirstAdvisoryFired {
			stats.Fired++
			if hasPlan {
				stats.Converted++
			} else {
				stats.Unconverted++
			}
		} else if hasPlan {
			stats.UnfiredWithPlan++
		}
	}
	return stats
}
