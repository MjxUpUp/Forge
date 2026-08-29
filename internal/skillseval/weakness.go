package skillseval

// weakness.go — 只读弱点挖掘：把项目既有确定信号聚成一册弱点报告（Self-Harness 第一阶段，
// 只报告）。不决策、不落地——人（或受监督的 evolution 轮）读报告选题；由此产生的变更仍走
// battery / JudgeSkillAccept 门禁验收。这让它留在已拆除学习层边界的审计侧（2026-07-09）：
// 挖掘是可观测，不是自主进化。
//
// 信号源（全部 deterministic、全部已有计算——本文件是 join，不是新判据）：
//   - health.Summarize：低分维度频次（跨任务 <70 维度）、盲区率（完成声明靠自述的占比）、趋势
//   - AnalyzeUsage：从未触发的 canonical skill（undertrigger 候选）
//   - AnalyzeEffectiveness：涉及任务低分/弱证据的 skill

import (
	"fmt"

	"github.com/MjxUpUp/Forge/internal/act"
	"github.com/MjxUpUp/Forge/internal/forgedata"
	"github.com/MjxUpUp/Forge/internal/health"
)

// 弱点候选阈值。偏保守——报告给人读，但每列一项都耗注意力；单次出现的维度、单任务的
// skill 是噪声不是弱点。
const (
	// weakDimMinCount: a low dimension must recur in >=2 tasks to be listed.
	weakDimMinCount = 2 // 低分维度须在 >=2 个任务复现才列出
	// lowEffectMinTasks: effectiveness rows need >=2 involved tasks (n=1 proves nothing).
	lowEffectMinTasks = 2 // effectiveness 行须 >=2 个涉及任务（n=1 无证明力）
	// lowEffectWeakRate: weak-evidence task share at which a skill is listed.
	lowEffectWeakRate = 0.5 // 弱证据任务占比达到即列出
	// lowEffectScore: avg score below which a scored skill is listed (aligned with the
	// <70 low-dimension cut in act.BuildConclusion).
	lowEffectScore = 70 // 平均分低于即列出（对齐 act.BuildConclusion 的 <70 低分切线）
)

// WeaknessReport is the clustered read-only weakness mining result.
//
// WeaknessReport 是聚簇后的只读弱点挖掘结果。只报告：字段是证据不是裁决——DataCaveats
// 承载覆盖诚实性（小样本、无 Skill 事件的 host），避免任一行被过度解读。
type WeaknessReport struct {
	// Dimension weaknesses: low-score dims recurring across tasks (count >= weakDimMinCount).
	WeakDims []health.DimFreq `json:"weak_dims,omitempty"`
	// Verification blind spot: share of tasks whose completion claims rest on self-report.
	BlindSpotRate  float64 `json:"blind_spot_rate"`
	BlindSpotCount int     `json:"blind_spot_count"`
	TotalTasks     int     `json:"total_tasks"`
	// Trend (health.Summarize's, 3-point noise band built in).
	Trend      string  `json:"trend"`
	EarlierAvg float64 `json:"earlier_avg"`
	RecentAvg  float64 `json:"recent_avg"`
	// Never-triggered canonical skills (undertrigger candidates).
	NeverTriggered []string `json:"never_triggered,omitempty"`
	// Low-effectiveness skills (filters: TaskCount >= 2 && (WeakRate >= 0.5 || 0 < AvgScore < 70)).
	LowEffectiveness []SkillEffectiveness `json:"low_effectiveness,omitempty"`
	// DataCaveats: what the report cannot see (coverage honesty).
	DataCaveats []string `json:"data_caveats,omitempty"`
}

// AnalyzeWeaknesses joins the deterministic signals into a WeaknessReport. p
// resolves the project's act/toollog/checklog data.
//
// AnalyzeWeaknesses 把确定信号 join 成 WeaknessReport。p 解析项目的 act/toollog/checklog
// 数据；canonical 解析 skill 库（从未触发分析用）。数据源错误上抛——静默跳过失败数据源的
// 弱点报告会长得像「该维度无弱点」（fail-visible）。
func AnalyzeWeaknesses(p *forgedata.Project, canonical string) (*WeaknessReport, error) {
	rep := &WeaknessReport{DataCaveats: []string{}}

	conclusions, err := act.LoadAll(p)
	if err != nil {
		return nil, fmt.Errorf("load conclusions: %w", err)
	}
	sum := health.Summarize(conclusions)
	rep.TotalTasks = sum.TotalTasks
	rep.BlindSpotRate = sum.BlindSpotRate
	rep.BlindSpotCount = sum.BlindSpotCount
	rep.Trend = sum.Trend
	rep.EarlierAvg = sum.EarlierAvg
	rep.RecentAvg = sum.RecentAvg
	for _, d := range sum.LowDims {
		if d.Count >= weakDimMinCount {
			rep.WeakDims = append(rep.WeakDims, d)
		}
	}

	usage, err := AnalyzeUsage(p.GitRoot, canonical)
	if err != nil {
		return nil, fmt.Errorf("analyze usage: %w", err)
	}
	rep.NeverTriggered = usage.NeverTriggered

	eff, err := AnalyzeEffectiveness(p)
	if err != nil {
		return nil, fmt.Errorf("analyze effectiveness: %w", err)
	}
	for _, e := range eff {
		if e.TaskCount < lowEffectMinTasks {
			continue // n=1 无证明力，噪声不列
		}
		if e.WeakRate >= lowEffectWeakRate || (e.AvgScore > 0 && e.AvgScore < lowEffectScore) {
			rep.LowEffectiveness = append(rep.LowEffectiveness, e)
		}
	}

	// 覆盖诚实性——每条 caveat 点名一种行可能被过度解读的方式。
	if rep.TotalTasks == 0 {
		rep.DataCaveats = append(rep.DataCaveats,
			"无任务结论数据——维度弱点/盲区率/成效信号均为空，不代表无弱点")
	} else if rep.TotalTasks < 4 {
		rep.DataCaveats = append(rep.DataCaveats,
			fmt.Sprintf("仅 %d 个任务结论——样本不足，趋势与频次信号噪声大", rep.TotalTasks))
	}
	if usage.TotalEvents == 0 {
		rep.DataCaveats = append(rep.DataCaveats,
			"无 skill 触达事件——NeverTriggered 是全量 canonical（Skill 事件仅部分 host 产生，非证据）")
	}
	if len(eff) == 0 {
		rep.DataCaveats = append(rep.DataCaveats,
			"无 skill-任务成效关联数据——LowEffectiveness 为空不代表无低效 skill")
	}
	// 非 git 项目（审查 F6）：结论与 effectiveness 走项目 DataDir/Root，usage 仍走
	// GitRoot（非 git 为空 → 运行时 CWD 的 PathKey）。从别的子目录运行 analyze 时
	// usage 半边可能读到不同数据目录——报告内部的不一致，用 caveat 点名而非掩盖。
	if p.GitRoot == "" {
		rep.DataCaveats = append(rep.DataCaveats,
			"非 git 项目：usage 按运行时 CWD 解析数据目录，与结论/成效目录可能不一致（换目录运行 analyze 时 NeverTriggered 可能指向另一数据源）")
	}
	if len(rep.DataCaveats) == 0 {
		rep.DataCaveats = nil
	}
	return rep, nil
}
