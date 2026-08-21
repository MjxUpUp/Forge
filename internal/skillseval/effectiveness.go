package skillseval

import (
	"cmp"
	"slices"

	"github.com/MjxUpUp/Forge/internal/act"
	"github.com/MjxUpUp/Forge/internal/checklog"
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

// SkillEffectiveness correlates skill hits (toollog active Skill calls + checklog
// passive CheckSkillTrigger entries) with task effectiveness (act conclusion).
//
// All fields deterministic: hit counts come from toollog (tool-track collection) and
// checklog (skill-trigger hook), effectiveness from scoring + evidence chain
// (act conclusion), no agent subjective judgment — sidesteps Agent-as-a-Judge
// meta-evaluation red line.
// This is the agent-neutral implementation of the reuse-rate + success-rate signal in Forge.
//
// SkillEffectiveness 关联 skill 命中（toollog 主动 Skill 调用 + checklog 被动
// CheckSkillTrigger 条目）与 task 成效（act conclusion）。
//
// 全字段 deterministic：命中数来自 toollog（tool-track 采集）与 checklog
// （skill-trigger hook），成效来自评分 + 证据链（act 结论），无 agent 主观判断
// ——绕开 Agent-as-a-Judge 的 meta-evaluation 红线。
// 这是"复用率 + 成功率"信号在 Forge 的 agent-neutral 实现。
type SkillEffectiveness struct {
	Skill     string  `json:"skill"`
	HitCount  int     `json:"hit_count"`  // 总命中次数（主动调用 + 被动触发，同一 task 多次累加）
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

// AnalyzeEffectiveness correlates skill hits (toollog active Skill calls + checklog
// passive CheckSkillTrigger entries) with task effectiveness in act conclusion.
//
// Joins per-project data sources by TaskRef (same DataDir): toollog records Skill
// calls + TaskRef, checklog records CheckSkillTrigger entries + TaskRef, act
// conclusion records task scoring + evidence strength. Produces per-skill average
// effectiveness across its involved tasks.
//
// The passive join (2026-08-22) closes the "panel shows hit counts only, quality
// columns empty" gap: most skills are hit via the skill-trigger hook (passive
// injection) and never via an active Skill tool call — joining only toollog left
// them absent from effectiveness entirely. Both sources feed the SAME aggregator:
// per-(skill, task) dedup means a task with both a passive trigger and an active
// load counts its outcome once, while HitCount sums both signals.
//
// agent-neutral (core signal): act conclusion is deterministic for any task run by any agent —
// scoring + evidence chain, agent-agnostic. Active skill-hit data comes from Skill tool calls in
// toollog — currently only Claude Code produces them (cursor/codex etc inject skills via
// mdc/AGENTS.md, no tool-call events); the passive checklog join is what gives every
// hook-wired host a hit signal. Consistent with the same caveat in usage.go package doc.
//
// Cross-task reads go through LoadAllAll (active + archived *.jsonl): forge task start
// archives the previous task's toollog and checklog, so cross-task effectiveness
// correlation must read across archives or only the current task is visible.
//
// Returns empty slice on missing data, no error — evaluation system still works when data is missing (agent-neutral principle: no hard
// dependency on agent reports).
//
// AnalyzeEffectiveness 关联 skill 命中（toollog 主动 Skill 调用 + checklog 被动
// CheckSkillTrigger 条目）与 act conclusion 的 task 成效。
//
// 按 TaskRef 连接 per-project 数据源（同 DataDir）：toollog 记 Skill 调用 + TaskRef，
// checklog 记 CheckSkillTrigger 条目 + TaskRef，act conclusion 记 task 的评分 + 证据
// 强度。产出每个 skill 在其涉及 task 上的平均成效。
//
// 被动 join（2026-08-22）闭合「面板只显示命中数、质量列全空」缺口：多数 skill 经
// skill-trigger hook 命中（被动注入）而非主动 Skill 工具调用——只 join toollog 时它们
// 在 effectiveness 里整体缺席。两源喂同一聚合器：per-(skill, task) 去重使「同一 task
// 既有被动触发又有主动加载」只计一次成效，而 HitCount 两种信号都累加。
//
// agent-neutral（核心信号）：act conclusion 是任何 agent 跑的 task 都有的 deterministic
// 评分 + 证据链，与具体 agent 无关。主动命中数据来自 toollog 的 Skill 工具调用——当前
// 仅 Claude Code 产生（cursor/codex 等 skill 经 mdc/AGENTS.md 注入、无工具调用事件）；
// 被动 checklog join 让每个接了 hook 的 host 都有命中信号。与 usage.go 包注释同类
// caveat 一致。
//
// 跨任务读取走 LoadAllAll（active + 归档 *.jsonl）：forge task start 会归档上一任务的
// toollog 与 checklog，跨任务成效关联必须跨归档读，否则只能看到当前任务。
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

	// Hit sources resolve their data dir via DataDirFor(root): pass p.Root, NOT
	// p.GitRoot — a non-git project has GitRoot=="" and DataDirFor("") falls back to
	// the process CWD's git repo (the dashboard runs inside some project), reading
	// that project's logs and cross-polluting conclusions on TaskRef collision. Git
	// projects have Root==GitRoot and non-git DataDirFor(Root)==p.DataDir — Root is
	// correct in both shapes.
	//
	// 命中源的数据目录经 DataDirFor(root) 解析：传 p.Root 而非 p.GitRoot——非 git 项目
	// GitRoot==""，DataDirFor("") 会回落到进程 CWD 所在 git 仓库（dashboard 就跑在某个
	// 项目里），读到该项目的日志，TaskRef 撞名时跨项目污染 conclusion。git 项目
	// Root==GitRoot、非 git DataDirFor(Root)==p.DataDir——Root 两种形态都正确。
	calls, err := toolusage.LoadAllAll(p.Root)
	if err != nil {
		return nil, err
	}

	stats := map[string]*effectivenessAggregator{}
	// record feeds one hit (active or passive) into the aggregator: hits always
	// accumulate; the task's outcome (score/ratio/strength) is folded in only on
	// first encounter of that (skill, task) pair — dedup across BOTH sources, so a
	// passive trigger followed by an active load in the same task counts one outcome.
	//
	// record 把一次命中（主动或被动）喂进聚合器：hits 恒累加；task 成效（分数/ratio/
	// 强度）只在首次遇到该 (skill, task) 对时折入——跨两源去重，同 task 内被动触发+
	// 主动加载只计一次成效。
	record := func(name, taskRef string) {
		a, ok := stats[name]
		if !ok {
			a = &effectivenessAggregator{tasks: map[string]bool{}}
			stats[name] = a
		}
		a.hits++
		// Effectiveness is accumulated only on first encounter of a task (dedup, avoiding same-task multi-call weight inflation).
		//
		// 成效只在首次遇到该 task 时累加一次（去重，避免同 task 多次调用放大权重）
		if taskRef == "" || a.tasks[taskRef] {
			return
		}
		a.tasks[taskRef] = true
		if con, ok := byTask[taskRef]; ok {
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

	// Active: Skill tool calls in toollog.
	//
	// 主动：toollog 的 Skill 工具调用。
	for _, c := range calls {
		if c.ToolName != "Skill" {
			continue
		}
		name := ExtractSkillName(c.ToolInput)
		if name == "" {
			continue
		}
		record(name, c.TaskRef)
	}

	// Passive: CheckSkillTrigger entries in checklog (skill-trigger hook injections).
	// Same dedup + outcome folding as the active path; entries without a TaskRef
	// still count as hits but attach no outcome.
	//
	// 被动：checklog 的 CheckSkillTrigger 条目（skill-trigger hook 注入）。与主动路径
	// 同去重 + 成效折入；无 TaskRef 的条目仍计命中但不挂成效。
	// Load error degrades to an empty passive source (same philosophy as toolusage's
	// per-file skip above): one corrupt archived checklog must not blank the whole
	// report — the dashboard caller ignores errors, so failing hard here just
	// recreates the empty-quality-columns symptom this join exists to fix.
	//
	// 加载错误降级为空被动源（与上面 toolusage 的 per-file 跳过同哲学）：单个损坏的
	// 归档 checklog 不能拖垮整表——dashboard 调用方忽略错误，这里硬失败只会复现
	// 本 join 要修的「质量列全空」症状。
	triggerEntries, err := checklog.LoadAllAll(p.Root)
	if err != nil {
		triggerEntries = nil
	}
	for _, e := range triggerEntries {
		if e.Check != checklog.CheckSkillTrigger {
			continue
		}
		name := checklog.SkillFromTriggerDetail(e.Detail)
		if name == "" {
			continue
		}
		record(name, e.TaskRef)
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
