package skillseval

import (
	"cmp"
	"slices"

	"github.com/MjxUpUp/Forge/internal/act"
	"github.com/MjxUpUp/Forge/internal/forgedata"
	"github.com/MjxUpUp/Forge/internal/toolusage"
)

// strengthWeak / strengthUnverified / strengthNoData align with act.Conclusion.Strength literal values
// (checklog.EvidenceStrength.String). Literal string comparison rather than importing checklog keeps
// skillseval dependencies minimal.
//
// WeakRate counts NoData as weak too: effectiveness context is exposing evaluation blind spots, NoData (zero run evidence)
// is blinder than Weak (scant evidence), should be counted. This differs from act.Conclusion.RetrospectiveNudge criterion (only
// Weak/Unverified trigger retrospective) — Nudge is triggering session retrospective, effectiveness is evaluation blind spot,
// the two contexts differ, each criterion is sensible on its own.
//
// strengthWeak / strengthUnverified / strengthNoData 与 act.Conclusion.Strength 字面值对齐
// （checklog.EvidenceStrength.String）。字符串字面比较而非 import checklog，保持
// skillseval 依赖最小。
//
// WeakRate 把 NoData 也算弱：effectiveness 语境是「暴露评估盲区」，NoData（零实跑证据）
// 比 Weak（少量证据）更盲，理应计入。这与 act.Conclusion.RetrospectiveNudge 判据（只
// Weak/Unverified 触发回顾）不同——Nudge 是「触发会话回顾」，effectiveness 是「评估盲区」，
// 两者语境不同，判据各自合理。
const (
	strengthWeak       = "Weak"
	strengthUnverified = "Unverified"
	strengthNoData     = "NoData"
)

// SkillEffectiveness correlates skill hits (toollog) with task effectiveness (act conclusion).
//
// All fields deterministic: hit counts come from toollog (tool-track collection), effectiveness from scoring + evidence chain
// (act conclusion), no agent subjective judgment — sidesteps Agent-as-a-Judge meta-evaluation red line.
// This is the agent-neutral implementation of the reuse-rate + success-rate signal in Forge.
//
// SkillEffectiveness 关联 skill 命中（toollog）与 task 成效（act conclusion）。
//
// 全字段 deterministic：命中数来自 toollog（tool-track 采集），成效来自评分 + 证据链
// （act 结论），无 agent 主观判断——绕开 Agent-as-a-Judge 的 meta-evaluation 红线。
// 这是"复用率 + 成功率"信号在 Forge 的 agent-neutral 实现。
type SkillEffectiveness struct {
	Skill     string  `json:"skill"`
	HitCount  int     `json:"hit_count"`  // 总命中次数（同一 task 多次调用累加）
	TaskCount int     `json:"task_count"` // 涉及不同 task 数
	AvgScore  float64 `json:"avg_score"`  // 有评分 task（Score>0）的平均分；未评分 task 不计入分母
	AvgRatio  float64 `json:"avg_ratio"`  // 平均证据 ratio（deterministic/total），分母=有 conclusion 的 task
	WeakRate  float64 `json:"weak_rate"`  // Strength=Weak/Unverified/NoData 的 task 占比（分母同 AvgRatio）
}

// effectivenessAggregator is the internal accumulator (task dedup + effectiveness summation).
//
// effectivenessAggregator 内部累加器（task 去重 + 成效求和）。
type effectivenessAggregator struct {
	hits     int
	tasks    map[string]bool
	scoreSum float64
	ratioSum float64
	weak     int
	// conN is the number of tasks with a corresponding act conclusion (AvgRatio/WeakRate denominator).
	conN int // 有对应 act conclusion 的 task 数（AvgRatio/WeakRate 分母）
	// scoredN is the number of those tasks with Score>0 (AvgScore denominator, excluding unscored).
	scoredN int // 其中 Score>0 的 task 数（AvgScore 分母，排除未评分）
}

// AnalyzeEffectiveness correlates Skill calls in toollog with task effectiveness in act conclusion.
//
// Joins two per-project data sources by TaskRef (same DataDir): toollog records Skill calls + TaskRef,
// act conclusion records task scoring + evidence strength. Produces per-skill average effectiveness across its involved tasks.
//
// agent-neutral (core signal): act conclusion is deterministic for any task run by any agent —
// scoring + evidence chain, agent-agnostic. Skill hit data comes from Skill tool calls in toollog — currently
// only Claude Code produces them (cursor/codex etc inject skills via mdc/AGENTS.md, no tool-call events),
// other hosts with hooks installed still show no Skill counts (graceful degradation, not full agent-neutral).
// Consistent with the same caveat in usage.go package doc.
//
// Cross-task reads go through LoadAllAll (active + archived toollog-*.jsonl): forge task start archives the previous
// task toollog, so cross-task effectiveness correlation must read across archives or only the current task is visible.
//
// Returns empty slice on missing data, no error — evaluation system still works when data is missing (agent-neutral principle: no hard
// dependency on agent reports).
//
// AnalyzeEffectiveness 关联 toollog 的 Skill 调用与 act conclusion 的 task 成效。
//
// 按 TaskRef 连接两个 per-project 数据源（同 DataDir）：toollog 记 Skill 调用 + TaskRef，
// act conclusion 记 task 的评分 + 证据强度。产出每个 skill 在其涉及 task 上的平均成效。
//
// agent-neutral（核心信号）：act conclusion 是任何 agent 跑的 task 都有的 deterministic
// 评分 + 证据链，与具体 agent 无关。Skill 命中数据来自 toollog 的 Skill 工具调用——当前
// 仅 Claude Code 产生（cursor/codex 等 skill 经 mdc/AGENTS.md 注入、无工具调用事件），
// 其他 host 装了 hook 也不会出现 Skill 计数（graceful degradation，非全面 agent-neutral）。
// 与 usage.go 包注释的同类 caveat 一致。
//
// 跨任务读取走 LoadAllAll（active + 归档 toollog-*.jsonl）：forge task start 会归档上一
// 任务的 toollog，跨任务成效关联必须跨归档读，否则只能看到当前任务。
//
// 缺数据时返回空切片，不报错——评估体系在数据缺失时仍工作（agent-neutral 原则：不强依赖
// agent 报告）。
func AnalyzeEffectiveness(p *forgedata.Project) ([]SkillEffectiveness, error) {
	conclusions, err := act.LoadAll(p)
	if err != nil {
		return nil, err
	}
	// TaskRef maps to the latest Conclusion (a task completed multiple times takes the latest; LoadAll is time-ordered, later overwrites).
	//
	// TaskRef → 最新 Conclusion（一个 task 多次完成取最新；LoadAll 已按时序，后者覆盖）
	byTask := map[string]*act.Conclusion{}
	for i := range conclusions {
		byTask[conclusions[i].TaskRef] = &conclusions[i]
	}

	calls, err := toolusage.LoadAllAll(p.GitRoot)
	if err != nil {
		return nil, err
	}

	stats := map[string]*effectivenessAggregator{}
	for _, c := range calls {
		if c.ToolName != "Skill" {
			continue
		}
		name := ExtractSkillName(c.ToolInput)
		if name == "" {
			continue
		}
		a, ok := stats[name]
		if !ok {
			a = &effectivenessAggregator{tasks: map[string]bool{}}
			stats[name] = a
		}
		a.hits++
		// Effectiveness is accumulated only on first encounter of a task (dedup, avoiding same-task multi-call weight inflation).
		//
		// 成效只在首次遇到该 task 时累加一次（去重，避免同 task 多次调用放大权重）
		if c.TaskRef != "" && !a.tasks[c.TaskRef] {
			a.tasks[c.TaskRef] = true
			if con, ok := byTask[c.TaskRef]; ok {
				// conN counts tasks with a conclusion (AvgRatio/WeakRate denominator).
				a.conN++ // 有 conclusion 的 task（AvgRatio/WeakRate 分母）
				a.ratioSum += con.Ratio
				if con.Strength == strengthWeak || con.Strength == strengthUnverified || con.Strength == strengthNoData {
					a.weak++
				}
				// AvgScore denominator only counts Score>0: Score==0 is the sentinel from act.BuildConclusion when score==nil
				// (scoring not run / failed), including it would artificially depress the avg score.
				//
				// AvgScore 分母只计 Score>0：Score==0 是 act.BuildConclusion 在 score==nil
				// 时的哨兵值（评分未跑/失败），计入会人为拉低 avg 分。
				if con.Score > 0 {
					a.scoreSum += con.Score
					a.scoredN++
				}
			}
		}
	}

	out := make([]SkillEffectiveness, 0, len(stats))
	for name, a := range stats {
		se := SkillEffectiveness{
			Skill:     name,
			HitCount:  a.hits,
			TaskCount: len(a.tasks),
		}
		// AvgScore uses scoredN (Score>0); AvgRatio/WeakRate use conN (with conclusion) —
		// ratio/evidence strength are always valid in conclusion, unscored task Score does not pollute avg but its
		// evidence strength still participates in weak-rate.
		//
		// AvgScore 用 scoredN（Score>0）；AvgRatio/WeakRate 用 conN（有 conclusion）——
		// ratio/证据强度在 conclusion 里总有效，未评分 task 的 Score 不污染 avg 分但其
		// 证据强度仍参与弱占比。
		if a.scoredN > 0 {
			se.AvgScore = a.scoreSum / float64(a.scoredN)
		}
		if a.conN > 0 {
			se.AvgRatio = a.ratioSum / float64(a.conN)
			se.WeakRate = float64(a.weak) / float64(a.conN)
		}
		out = append(out, se)
	}
	slices.SortFunc(out, func(a, b SkillEffectiveness) int {
		if a.HitCount != b.HitCount {
			return cmp.Compare(b.HitCount, a.HitCount)
		}
		return cmp.Compare(a.Skill, b.Skill)
	})
	return out, nil
}
