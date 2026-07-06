// Package health 把 task 级结论（act.Conclusion）上卷成 project 级质量趋势——PDCA 在
// project 粒度的 Act。单个任务的盲区/低分是个例，跨任务聚合才暴露系统性问题：某维度反复
// 低分说明该方向有共性缺口，完成声明盲区率高说明 agent 系统性"声明完成却没真验证"。
// 喂给 session-retrospective 在项目层面决策"该把什么沉淀成 CLAUDE.md 铁律 / 守卫测试"。
//
// 全字段从 act.Conclusion 聚合（结论本身源自 checklog 证据 + 评分，deterministic），
// 非 agent 叙述——与 evidence chain 的不可伪造原则一致。
package health

import (
	"sort"
	"time"

	"github.com/MjxUpUp/Forge/internal/act"
	"github.com/MjxUpUp/Forge/internal/checklog"
)

// DimFreq 是一个低分维度（<70）及其跨任务出现次数。
type DimFreq struct {
	Dimension string `json:"dimension"`
	Count     int    `json:"count"`
}

// Span 是结论时间范围（最早 ~ 最晚完成时刻）。
type Span struct {
	Earliest time.Time `json:"earliest"`
	Latest   time.Time `json:"latest"`
}

// Summary 是 project 级质量上卷。BlindSpotRate 是头条信号：完成声明主要靠 agent 自述
// （Unverified/Weak）的任务占比——项目级 LLM-judge 盲区率，高 = 系统性验证缺口。
type Summary struct {
	TotalTasks     int            `json:"total_tasks"`
	AvgScore       float64        `json:"avg_score"`
	MedianScore    float64        `json:"median_score"`
	GradeDist      map[string]int `json:"grade_dist"`    // A/B/C/D/F → count
	StrengthDist   map[string]int `json:"strength_dist"` // Strong/Weak/Unverified/NoData → count
	BlindSpotCount int            `json:"blind_spot_count"`
	BlindSpotRate  float64        `json:"blind_spot_rate"` // 0-1
	NudgeCount     int            `json:"nudge_count"`     // RetrospectiveNudge=true 任务数
	LowDims        []DimFreq      `json:"low_dims,omitempty"`
	Span           Span           `json:"span"`
	EarlierAvg     float64        `json:"earlier_avg"` // 前半段均分
	RecentAvg      float64        `json:"recent_avg"`  // 后半段均分
	Trend          string         `json:"trend"`       // improving/regressing/stable/insufficient

	// PhasePassRate 是 phase-aware 质量报告（Phase 2 回路接入）。
	// key=设计阶段（requirement/api/backend...），value=该阶段任务通过率（0-1）。
	// 用于 R3 advisory 闭环：review 子 agent 调 health_query 读高频问题注入 prompt。
	PhasePassRate map[string]float64 `json:"phase_pass_rate,omitempty"`
}

// Summarize 是纯函数：从结论切片聚合出 project Summary。不碰磁盘，便于单测。LoadAll
// 已按时间排序，但此处对副本再排一次防外部调用方传入乱序切片。空切片 → 零值（TotalTasks=0）。
func Summarize(cs []act.Conclusion) Summary {
	var s Summary
	s.TotalTasks = len(cs)
	if len(cs) == 0 {
		return s
	}
	s.GradeDist = map[string]int{}
	s.StrengthDist = map[string]int{}
	lowCounts := map[string]int{}
	sum := 0.0
	// Phase 追踪：每个 (phase, grade) 计数，用于计算 phase_pass_rate
	phaseGrades := map[string]map[string]int{}
	for _, c := range cs {
		sum += c.Score
		if c.Grade != "" {
			s.GradeDist[c.Grade]++
			// 按 phase 分组统计
			for _, phase := range c.DesignPhases {
				if phaseGrades[phase] == nil {
					phaseGrades[phase] = map[string]int{}
				}
				phaseGrades[phase][c.Grade]++
			}
		}
		s.StrengthDist[c.Strength]++
		if c.Strength == checklog.Unverified.String() || c.Strength == checklog.Weak.String() {
			s.BlindSpotCount++
		}
		if c.RetrospectiveNudge {
			s.NudgeCount++
		}
		for _, d := range c.LowDimensions {
			lowCounts[d]++
		}
	}
	s.AvgScore = sum / float64(len(cs))
	s.MedianScore = median(scoresOf(cs))
	s.BlindSpotRate = float64(s.BlindSpotCount) / float64(len(cs))
	for d, n := range lowCounts {
		s.LowDims = append(s.LowDims, DimFreq{Dimension: d, Count: n})
	}
	// 频次降序；同频次按维度名稳定排序（可复现输出，便于断言）。
	sort.Slice(s.LowDims, func(i, j int) bool {
		if s.LowDims[i].Count != s.LowDims[j].Count {
			return s.LowDims[i].Count > s.LowDims[j].Count
		}
		return s.LowDims[i].Dimension < s.LowDims[j].Dimension
	})

	byTime := make([]act.Conclusion, len(cs))
	copy(byTime, cs)
	sort.SliceStable(byTime, func(i, j int) bool {
		return byTime[i].CompletedAt.Before(byTime[j].CompletedAt)
	})
	s.Span = Span{Earliest: byTime[0].CompletedAt, Latest: byTime[len(byTime)-1].CompletedAt}
	s.EarlierAvg, s.RecentAvg, s.Trend = trend(byTime)

	// 计算 phase_pass_rate：通过率 = A 级占比（≥90 分视为通过）
	if len(phaseGrades) > 0 {
		s.PhasePassRate = map[string]float64{}
		for phase, grades := range phaseGrades {
			total := 0
			passed := 0
			for g, n := range grades {
				total += n
				if g == "A" || g == "B" {
					passed += n
				}
			}
			if total > 0 {
				s.PhasePassRate[phase] = float64(passed) / float64(total)
			}
		}
	}
	return s
}

func scoresOf(cs []act.Conclusion) []float64 {
	out := make([]float64, 0, len(cs))
	for _, c := range cs {
		out = append(out, c.Score)
	}
	return out
}

func median(xs []float64) float64 {
	if len(xs) == 0 {
		return 0
	}
	s := make([]float64, len(xs))
	copy(s, xs)
	sort.Float64s(s)
	n := len(s)
	if n%2 == 1 {
		return s[n/2]
	}
	return (s[n/2-1] + s[n/2]) / 2
}

// trend 按完成时间对半切比前/后半段均分。<4 样本标 insufficient（无统计意义）。阈值 3 分：
// 差<3 视为 stable，避免噪声误判趋势。
func trend(byTime []act.Conclusion) (earlier, recent float64, label string) {
	n := len(byTime)
	if n < 4 {
		return 0, 0, `insufficient`
	}
	mid := n / 2
	eSum, rSum := 0.0, 0.0
	for i := 0; i < mid; i++ {
		eSum += byTime[i].Score
	}
	for i := mid; i < n; i++ {
		rSum += byTime[i].Score
	}
	earlier = eSum / float64(mid)
	recent = rSum / float64(n-mid)
	switch {
	case recent > earlier+3:
		label = `improving`
	case recent < earlier-3:
		label = `regressing`
	default:
		label = `stable`
	}
	return
}
