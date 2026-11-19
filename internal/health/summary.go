// Package health rolls up task-level conclusions (act.Conclusion) into project-level
// facts — PDCA at project granularity Act.
//
// Naming note (2026-09 census P8): "health" appears in three distinct roles —
// this package (rollup), the `forge health` command (internal/cli/health.go,
// per-project report on this package), and task_health (internal/clitask,
// zombie/deadlock scanning; it also registers a `health [--json]` subcommand of
// its own). Same word, three layers: aggregation, reporting, and triage.
//
// 命名说明（2026-09 普查 P8）：「health」在仓内有三种互不重叠的角色——本包
// （上卷）、`forge health` 命令（internal/cli/health.go，基于本包的项目级
// 报告）、task_health（internal/clitask，僵尸/死锁扫描；其自身亦注册 health
// 子命令）。同一个词、三层职责：聚合、呈现、分诊。
//
// Package health 把 task 级结论（act.Conclusion）上卷成 project 级事实——PDCA 在
// project 粒度的 Act。单个任务的盲区是个例，跨任务聚合才暴露系统性问题：完成声明
// 盲区率高说明 agent 系统性"声明完成却没真验证"。喂给 session-retrospective 在项目
// 层面决策"该把什么沉淀成 CLAUDE.md 铁律 / 守卫测试"。
//
// 2.3 起刻意不再上卷合成分：没有均分、中位、grade 分布、低分维度直方图和趋势判定。
// 要量化就数事实——判据通过数、先失败证明数、评审轮次、逃生次数。合成分会掩盖
// 「高分但零实跑证据」的盲区（项目级 LLM-judge 盲区正是本包的头条信号）。
//
// All fields are aggregated from act.Conclusion facts (which derive from checklog
// evidence + the task fact sheet, deterministic), not agent narration — consistent
// with the evidence chain's unforgeability principle.
package health

import (
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

// Span is the conclusion time range (earliest ~ latest completion time).
//
// Span 是结论时间范围（最早 ~ 最晚完成时刻）。
type Span struct {
	Earliest time.Time `json:"earliest"`
	Latest   time.Time `json:"latest"`
}

// Summary is the project-level facts rollup.
//
// Summary 是 project 级事实上卷。BlindSpotRate 是头条信号：完成声明主要靠 agent 自述
// （Unverified/Weak）的任务占比——项目级 LLM-judge 盲区率，高 = 系统性验证缺口。
// 其余字段都是可数的的事实（计数），不是评分或派生率。
type Summary struct {
	TotalTasks     int            `json:"total_tasks"`
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
	// 不再把面板红灯永远挂着（告警疲劳校准见 NudgeRecentWindow）。`forge health` /
	// `forge status` 也传真实 now，与 pulse 面板同一 14 天窗口（但不扣 ack：pulse 走
	// SummarizeAtAcked，health/status 的 nudge_recent 可能大于面板值）。
	NudgeRecent int `json:"nudge_recent"`
	// NudgeActionable counts windowed nudges that are NOT escape-hatch-capped
	// Weak — i.e. the human-facing ALERT share of NudgeRecent: true-weak claims
	// (Ratio < 0.5) or Unverified. A capped Weak already paid its escape tax at
	// completion time (Strength downgraded Strong→Weak); the dashboard re-displaying
	// it adds no information (2026-09 finding: 8/11 alerts were capped noise,
	// blind-spot rate 0). Window semantics are identical to NudgeRecent
	// ((now-Window, now]); NudgeRecent keeps the full windowed total for
	// transparent breakdown — alerts = zombies + actionable. 分类谓词单一真相源：
	// act.Conclusion.EscapeCapped。
	NudgeActionable int `json:"nudge_actionable"`

	// —— 事实计数（跨任务求和；没有均分/趋势/直方图）——
	Criteria       int `json:"criteria"`         // 判据总数
	CriteriaPassed int `json:"criteria_passed"`  // 判据通过数
	RedFirstTrials int `json:"red_first_trials"` // 先失败证明登记数
	RedFirstClosed int `json:"red_first_closed"` // 先失败证明已转绿数
	ReviewRounds   int `json:"review_rounds"`    // 评审轮次累计
	Escapes        int `json:"escapes"`          // 逃生舱累计

	Span Span `json:"span"`
}

// SummarizeAt aggregates the conclusions plus the time-windowed NudgeRecent:
// nudged conclusions completed within the HALF-OPEN window
// (now-NudgeRecentWindow, now] are counted. Equivalent to SummarizeAtAcked
// with a nil ack predicate.
//
// SummarizeAt 聚合结论，并给出时间窗口化的 NudgeRecent：只数完成于半开
// 窗口 (now-NudgeRecentWindow, now] 内的 nudge 结论——恰好落在 now-Window 的不计
// （After 严格比较；由 TestSummarizeAt_NudgeRecentWindow 钉住）。`now` 为零值时
// NudgeRecent/NudgeActionable 保持 0，调用方必须传真实 now。等价于 ack 谓词为 nil
// 的 SummarizeAtAcked。
// 纯函数、不碰磁盘。
func SummarizeAt(cs []act.Conclusion, now time.Time) Summary {
	return SummarizeAtAcked(cs, now, nil)
}

// SummarizeAtAcked aggregates like SummarizeAt plus evidence-based ack
// filtering: conclusions for which ack(c) returns true are EXCLUDED from the
// alert-facing windowed counts (NudgeRecent / NudgeActionable) — their
// retrospective was processed (act.Disposition, written by `forge act
// retro-done` at session-retrospective wrap-up). NudgeCount (all-history
// truth) and every other aggregate are unaffected: ack is a "processed" marker,
// not history rewriting. nil ack ≡ SummarizeAt.
// An un-acked nudge stays on the panel — a MISSED retrospective becomes
// visible for the first time.
//
// SummarizeAtAcked 在 SummarizeAt 之上增加证据式 ack 过滤：ack(c) 为真的结论退出
// 面向告警的窗口计数（NudgeRecent / NudgeActionable）——其回顾已被处理
// （act.Disposition，由 session-retrospective 收尾的 `forge act retro-done` 写入）。
// NudgeCount（全量真相）与其余聚合不受影响：ack 是「已处理」标记，不是历史篡改。
// nil ack ≡ SummarizeAt。未 ack 的 nudge 留在面板——「错过的回顾」第一次可见。
// 纯函数、不碰磁盘。
func SummarizeAtAcked(cs []act.Conclusion, now time.Time, ack func(act.Conclusion) bool) Summary {
	var s Summary
	s.TotalTasks = len(cs)
	if len(cs) == 0 {
		return s
	}
	s.StrengthDist = map[string]int{}
	for _, c := range cs {
		// 与 grade 同款的非空守卫：空 Strength 不得落入无名桶（也不得进入下方的
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
			// now 非零才判定——零值 now 不开窗，窗口计数保持 0。
			// 证据式 ack（2026-09）：被 ack（已回顾）的 nudge 退出窗口计数——告警只留
			// 未处理的；全量真相 NudgeCount 不动。
			acked := ack != nil && ack(c)
			if !acked && !now.IsZero() && c.CompletedAt.After(now.Add(-NudgeRecentWindow)) && !c.CompletedAt.After(now) {
				s.NudgeRecent++
				// 告警分级（2026-09）：降档 Weak 不进告警分量——税已收，重复展示是噪音。
				if !c.EscapeCapped() {
					s.NudgeActionable++
				}
			}
		}
		s.Criteria += c.AcceptanceTotal
		s.CriteriaPassed += c.AcceptancePass
		s.RedFirstTrials += c.RedFirstTrials
		s.RedFirstClosed += c.RedFirstClosed
		s.ReviewRounds += c.ReviewRounds
		s.Escapes += c.Escapes
	}
	s.BlindSpotRate = float64(s.BlindSpotCount) / float64(len(cs))

	earliest, latest := cs[0].CompletedAt, cs[0].CompletedAt
	for _, c := range cs[1:] {
		if c.CompletedAt.Before(earliest) {
			earliest = c.CompletedAt
		}
		if c.CompletedAt.After(latest) {
			latest = c.CompletedAt
		}
	}
	s.Span = Span{Earliest: earliest, Latest: latest}
	return s
}
