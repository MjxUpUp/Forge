// Package health rolls up task-level conclusions (act.Conclusion) into project-level quality trends — PDCA at project granularity Act.
//
// Naming note (2026-09 census P8): "health" appears in three distinct roles —
// this package (trend rollup), the `forge health` command (internal/cli/health.go,
// per-project trend report on this package), and task_health (internal/clitask,
// zombie/deadlock scanning; it also registers a `health [--json]` subcommand of
// its own). Same word, three layers: aggregation, reporting, and triage.
// User-visible renames need a product decision; docs disambiguate.
//
// 命名说明（2026-09 普查 P8）：「health」在仓内有三种互不重叠的角色——本包
// （趋势上卷）、`forge health` 命令（internal/cli/health.go，基于本包的项目级
// 报告）、task_health（internal/clitask，僵尸/死锁扫描；其自身亦注册 health
// 子命令）。同一个词、三层职责：聚合、呈现、分诊。
// 用户可见改名需产品决策，先以文档明示。
//
// Package health 把 task 级结论（act.Conclusion）上卷成 project 级质量趋势——PDCA 在
// project 粒度的 Act。单个任务的盲区/低分是个例，跨任务聚合才暴露系统性问题：某维度反复
// 低分说明该方向有共性缺口，完成声明盲区率高说明 agent 系统性"声明完成却没真验证"。
// 喂给 session-retrospective 在项目层面决策"该把什么沉淀成 CLAUDE.md 铁律 / 守卫测试"。
//
// All fields are aggregated from act.Conclusion (the conclusion itself derives from checklog evidence + scoring, deterministic),
// not agent narration — consistent with the evidence chain's unforgeability principle.
// 全字段从 act.Conclusion 聚合（结论本身源自 checklog 证据 + 评分，deterministic），
// 非 agent 叙述——与 evidence chain 的不可伪造原则一致。
package health

import (
	"cmp"
	"slices"
	"time"

	"github.com/MjxUpUp/Forge/internal/act"
	"github.com/MjxUpUp/Forge/internal/checklog"
)

// NudgeRecentWindow 是 Summary.NudgeRecent（窗口化 nudge 计数）的回看窗口。
// 动机（2026-08 告警疲劳校准）：NudgeCount 是无 ack 机制的全量累计，喂给 dashboard 只会
// 只增不减——56 条陈旧 nudge（0 条真盲区）把面板红灯挂了数周，未来真信号必被淹没。
// NudgeRecent 把面向告警的计数限定在最近 14 天内完成的结论：超过该窗的 nudge 已过可行动
// 窗口（session 早已关闭、上下文消失），而全量计数仍可用于趋势分析。14 天贴合迭代回顾节奏。
const NudgeRecentWindow = 14 * 24 * time.Hour

// DimFreq is a low-score dimension (<70) and its cross-task occurrence count.
//
// DimFreq 是一个低分维度（<70）及其跨任务出现次数。
type DimFreq struct {
	Dimension string `json:"dimension"`
	Count     int    `json:"count"`
}

// Span is the conclusion time range (earliest ~ latest completion time).
//
// Span 是结论时间范围（最早 ~ 最晚完成时刻）。
type Span struct {
	Earliest time.Time `json:"earliest"`
	Latest   time.Time `json:"latest"`
}

// Summary is the project-level quality rollup.
//
// Summary 是 project 级质量上卷。BlindSpotRate 是头条信号：完成声明主要靠 agent 自述
// （Unverified/Weak）的任务占比——项目级 LLM-judge 盲区率，高 = 系统性验证缺口。
type Summary struct {
	TotalTasks     int            `json:"total_tasks"`
	AvgScore       float64        `json:"avg_score"`
	MedianScore    float64        `json:"median_score"`
	GradeDist      map[string]int `json:"grade_dist"`    // A/B/C/D/F → count
	StrengthDist   map[string]int `json:"strength_dist"` // Strong/Weak/Unverified/NoData → count
	BlindSpotCount int            `json:"blind_spot_count"`
	// CappedWeakCount: Weak conclusions that still carry deterministic evidence.
	//
	// CappedWeakCount：仍带 deterministic 证据的 Weak 结论——逃生舱封顶（override
	// 代价），与真正的证据盲区区分。
	CappedWeakCount int     `json:"capped_weak_count"`
	BlindSpotRate   float64 `json:"blind_spot_rate"` // 0-1
	NudgeCount      int     `json:"nudge_count"`     // RetrospectiveNudge=true 任务数（全量真相）
	// NudgeRecent counts RetrospectiveNudge=true conclusions completed within
	// NudgeRecentWindow of the SummarizeAt `now`.
	//
	// NudgeRecent 数 SummarizeAt 的 `now` 前 NudgeRecentWindow 窗口内完成且
	// RetrospectiveNudge=true 的结论——喂给 dashboard 的面向告警计数，让陈旧 nudge
	// 不再把面板红灯永远挂着（告警疲劳校准见 NudgeRecentWindow）。旧 Summarize
	//（无 `now`）填 NudgeRecent = NudgeCount，无窗口调用方字段语义仍完整。
	NudgeRecent int       `json:"nudge_recent"`
	LowDims     []DimFreq `json:"low_dims,omitempty"`
	Span        Span      `json:"span"`
	EarlierAvg  float64   `json:"earlier_avg"` // 前半段均分
	RecentAvg   float64   `json:"recent_avg"`  // 后半段均分
	Trend       string    `json:"trend"`       // improving/regressing/stable/insufficient

	// PhasePassRate is the phase-aware quality report (Phase 2 loop integrated). key
	// = design phase (requirement/api/backend...), value = task pass rate for that
	// phase (0-1).
	//
	// PhasePassRate 是 phase-aware 质量报告（Phase 2 回路接入）。
	// key=设计阶段（requirement/api/backend...），value=该阶段任务通过率（0-1）。
	// 用于 R3 advisory 闭环：review 子 agent 调 health_query 读高频问题注入 prompt。
	PhasePassRate map[string]float64 `json:"phase_pass_rate,omitempty"`
}

// Summarize is the legacy no-window entry: equivalent to SummarizeAt with
// NudgeRecent filled as the all-history NudgeCount (no `now` to window against).
//
// Summarize 是旧的无窗口入口：等价于 SummarizeAt，只是 NudgeRecent 填全量 NudgeCount
// （没有 `now` 可开窗）。为既有调用方（cli/health.go、pulse 项目卡）保留；面向告警的新
// 消费方应优先 SummarizeAt。
func Summarize(cs []act.Conclusion) Summary {
	s := SummarizeAt(cs, time.Time{})
	s.NudgeRecent = s.NudgeCount
	return s
}

// SummarizeAt aggregates like Summarize plus the time-windowed NudgeRecent:
// nudged conclusions completed within the HALF-OPEN window
// (now-NudgeRecentWindow, now] are counted.
//
// SummarizeAt 在 Summarize 聚合之上增加时间窗口化的 NudgeRecent：只数完成于半开
// 窗口 (now-NudgeRecentWindow, now] 内的 nudge 结论——恰好落在 now-Window 的不计
// （After 严格比较；由 TestSummarizeAt_NudgeRecentWindow 钉住）。`now` 为零值时
// NudgeRecent 保持 0——只有事后覆写 NudgeRecent=NudgeCount 的 Summarize 包装可以
// 传零；直接调用方必须传真实 now。纯函数、不碰磁盘。
func SummarizeAt(cs []act.Conclusion, now time.Time) Summary {
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
		// 与上面 Grade 同款的非空守卫：空 Strength 不得落入无名桶（也不得进入下方的
		// BlindSpotCount）。不变量：写入侧（act.BuildConclusion）的 Strength 恒由
		// checklog.EvidenceStrength.String() 赋值，必为 Strong/Weak/Unverified/NoData
		// 之一——不会为空——此守卫只防手工构造/历史脏数据。
		if c.Strength != "" {
			s.StrengthDist[c.Strength]++
		}
		// Blind-spot = claims with NO deterministic backing. A Weak that carries
		// deterministic evidence is an escape-hatch CAP (ratio 0.8x capped to Weak by
		// the override cost), not a blind spot — counting it here reported "100% 盲区"
		// for tasks that did run verification (2026-08-29 functional finding). Track
		// capped-Weak separately so the project view keeps both signals honestly.
		//
		// 盲区 = 无 deterministic 支撑的完成声明。带 deterministic 证据的 Weak 是
		// 逃生舱【封顶】（0.8x ratio 被 override 代价压成 Weak），不是盲区——计入
		// 此处曾把真跑过验证的任务报成「100% 盲区」（2026-08-29 功能发现）。封顶
		// Weak 单独计数，项目视图两个信号都诚实保留。
		if c.Strength == checklog.Unverified.String() {
			s.BlindSpotCount++
		} else if c.Strength == checklog.Weak.String() {
			if c.Deterministic > 0 {
				s.CappedWeakCount++
			} else {
				s.BlindSpotCount++
			}
		}
		if c.RetrospectiveNudge {
			s.NudgeCount++
			// 窗口计数：完成于半开区间 (now-Window, now] 内——恰好落在 now-Window 的不计
			//（After 严格比较；由 TestSummarizeAt_NudgeRecentWindow 的窗口沿断言钉住）。
			// now 非零才判定——旧 Summarize 包装传零值，事后会覆写 NudgeRecent。
			if !now.IsZero() && c.CompletedAt.After(now.Add(-NudgeRecentWindow)) && !c.CompletedAt.After(now) {
				s.NudgeRecent++
			}
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
	slices.SortFunc(s.LowDims, func(a, b DimFreq) int {
		if a.Count != b.Count {
			return cmp.Compare(b.Count, a.Count)
		}
		return cmp.Compare(a.Dimension, b.Dimension)
	})

	byTime := make([]act.Conclusion, len(cs))
	copy(byTime, cs)
	slices.SortStableFunc(byTime, func(a, b act.Conclusion) int {
		return a.CompletedAt.Compare(b.CompletedAt)
	})
	s.Span = Span{Earliest: byTime[0].CompletedAt, Latest: byTime[len(byTime)-1].CompletedAt}
	s.EarlierAvg, s.RecentAvg, s.Trend = trend(byTime)

	// 计算 phase_pass_rate：通过率 = (A+B) 占比（≥80 视为通过，与 grade 分级一致）
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
	slices.Sort(s)
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
