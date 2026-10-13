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

import "github.com/MjxUpUp/Forge/internal/tasktypes"

// PlanConversionStats is the cohort-level conversion measurement.
type PlanConversionStats struct {
	Fired           int `json:"fired"`             // 已发 advisory 的任务数
	Converted       int `json:"converted"`         // 发后补了方案（Plan|Goal 非空）
	Unconverted     int `json:"unconverted"`       // 发后仍无方案
	UnfiredWithPlan int `json:"unfired_with_plan"` // 未发 advisory 但有方案（start 时自带 --plan-file/--goal）
	// ByTier is the per-design-tier health slice (design-stage-hardening W0.2):
	// tasks without a tier decision are not counted here.
	//
	// ByTier 是按方案档位切分的健康度（design-stage-hardening W0.2）：没有档位
	// 判定的任务不计入。
	ByTier map[string]TierStats `json:"by_tier,omitempty"`
}

// TierStats counts one design tier's tasks and its health signals: Skipped = the
// tier asked for a design (mini/full) but no design artifact and no plan exist;
// Overridden = the declared tier undercut the hard triggers with a reason.
// A rising skip/override rate means the gate is too heavy (fix the gate, not the users).
//
// TierStats 统计单个方案档位的任务数与健康信号：Skipped = 档位要求方案
// （mini/full）但既无 design 产物也无 plan；Overridden = 声明档位带理由压低了硬
// 触发器。跳过率/否决率持续上升 = 门禁太重（改门禁，不改用户）。
type TierStats struct {
	Tasks      int `json:"tasks"`
	Skipped    int `json:"skipped"`
	Overridden int `json:"overridden"`
}

// ConversionRate returns converted/fired (0 when nothing fired).
func (s PlanConversionStats) ConversionRate() float64 {
	if s.Fired == 0 {
		return 0
	}
	return float64(s.Converted) / float64(s.Fired)
}

// ComputePlanConversion aggregates conversion (and the per-tier slice) over the given task states.
func ComputePlanConversion(states []*TaskState) PlanConversionStats {
	var stats PlanConversionStats
	for _, s := range states {
		if s == nil {
			continue
		}
		hasPlan := s.Plan != "" || s.Goal != ""
		if s.DesignTier != nil && s.DesignTier.Final != "" {
			if stats.ByTier == nil {
				stats.ByTier = map[string]TierStats{}
			}
			ts := stats.ByTier[s.DesignTier.Final]
			ts.Tasks++
			_, hasDesign := s.SpecArtifacts["design"]
			if s.DesignTier.Final != tasktypes.DesignTierNone && !hasDesign && s.Plan == "" {
				ts.Skipped++
			}
			if s.DesignTier.Reason != "" {
				ts.Overridden++
			}
			stats.ByTier[s.DesignTier.Final] = ts
		}
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
