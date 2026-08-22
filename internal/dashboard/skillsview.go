// skillsview.go — skill aggregation for the pulse panel: a per-skill overview (health from
// the latest eval run, hits merged from passive checklog firings + active toollog Skill
// calls, effectiveness joined to act conclusions, never-triggered list) and a single-skill
// detail view (run series, baseline compare, decision history, trigger accuracy).
//
// Reuse principle: all counting/joining goes through skillseval (AnalyzeEffectiveness /
// SkillCountsFromChecklog / SkillCountsFromToollog / LoadRuns / CompareRuns) and
// skillsdecisions — nothing re-parses jsonl here. Known blind spots are carried in the
// data (coverage note, null liveFalsePositiveRate) instead of being papered over.
//
// skillsview.go —— pulse 面板的 skill 聚合：总览（健康分取最新 eval run、命中数合并
// 被动 checklog 触发 + 主动 toollog Skill 调用、成效 join act 结论、从未触发名单）与
// 单 skill 详情（run 序列、baseline 比对、决策史、触发准确率）。
//
// 复用原则：所有计数/关联都走 skillseval（AnalyzeEffectiveness / SkillCountsFromChecklog /
// SkillCountsFromToollog / LoadRuns / CompareRuns）与 skillsdecisions——此处不重解析
// jsonl。已知盲区体现在数据里（coverage 说明、liveFalsePositiveRate 为 null），不粉饰。
package dashboard

import (
	"slices"
	"strings"
	"time"

	"github.com/MjxUpUp/Forge/internal/skillsdist"
	"github.com/MjxUpUp/Forge/internal/skillseval"
	"github.com/MjxUpUp/Forge/internal/skillsfm"
)

// skillsCoverageNote surfaces the known measurement blind spots in the payload itself:
// active Skill calls are only recorded for Claude Code (other hosts inject skills without
// tool-call events); passive reach comes from the skill-trigger hook; the live false-positive
// rate has no data source at all.
//
// skillsCoverageNote 把已知度量盲区显式写进载荷：主动 Skill 调用仅 Claude Code 有记录
// （其他 host 注入 skill 无工具调用事件）；被动触达来自 skill-trigger hook；线上误触发率
// 完全没有数据源。
const skillsCoverageNote = "主动 Skill 调用仅 Claude Code 有 toollog 记录（cursor/codex 等经 mdc/AGENTS.md 注入无工具事件）；被动触达来自 skill-trigger hook；线上误触发率无数据源，恒为 null"

// SkillSummary is one row of the skills overview. Health/AvgScore are pointers so "no
// data" serializes as null instead of a fabricated 0.
//
// SkillSummary 是 skills 总览的一行。Health/AvgScore 用指针，「无数据」序列化为 null
// 而非编造的 0。
type SkillSummary struct {
	Name      string   `json:"name"`
	Health    *float64 `json:"health"`    // 最新 EvalRun.HealthScore；无 run 为 null
	Hits      int      `json:"hits"`      // 被动 skill-trigger + 主动 Skill 调用合并
	TaskCount int      `json:"taskCount"` // 涉及的不同 task 数（被动触发+主动调用合并，per-task 去重）
	AvgScore  *float64 `json:"avgScore"`  // 关联任务均分（Score>0 才计）；无为 null
}

// SkillsOverview is the /api/pulse/skills.json payload.
//
// SkillsOverview 是 /api/pulse/skills.json 载荷。
type SkillsOverview struct {
	Skills         []SkillSummary `json:"skills"`
	NeverTriggered []string       `json:"neverTriggered"`
	Coverage       string         `json:"coverage"`
}

// SkillRunView is one eval run in the detail time series.
//
// SkillRunView 是详情时间序列里的一条 eval run。
type SkillRunView struct {
	RunID         string    `json:"runId"`
	Ts            time.Time `json:"ts"`
	Health        float64   `json:"health"`
	TriggerAcc    *float64  `json:"triggerAcc"`    // 无 trigger 类 case 时 null
	NotTriggerAcc *float64  `json:"notTriggerAcc"` // 无 not-trigger 类 case 时 null
	Cases         int       `json:"cases"`
}

// SkillCompareView is the latest-vs-baseline regression summary (counts, not full case
// lists — the panel shows numbers; drill-down stays in forge skills eval-report).
//
// SkillCompareView 是 latest-vs-baseline 回归摘要（计数而非完整 case 列表——面板看数字，
// 下钻留给 forge skills eval-report）。
type SkillCompareView struct {
	NetRegressions int  `json:"netRegressions"`
	Regressions    int  `json:"regressions"`
	Improvements   int  `json:"improvements"`
	Comparable     bool `json:"comparable"`
}

// SkillDecisionView is one decision from the skill's decisions.md history.
//
// SkillDecisionView 是 skill decisions.md 决策史里的一条。
type SkillDecisionView struct {
	ID        string    `json:"id"`
	Ts        time.Time `json:"ts"`
	Outcome   string    `json:"outcome"`
	Diagnosis string    `json:"diagnosis"`
	Rationale string    `json:"rationale,omitempty"`
	Commit    string    `json:"commit,omitempty"`
}

// TriggerQualityView is the trigger accuracy of the latest run.
//
// TriggerQualityView 是最新 run 的触发准确率。
type TriggerQualityView struct {
	TriggerAcc    *float64 `json:"triggerAcc"`
	NotTriggerAcc *float64 `json:"notTriggerAcc"`
	FromRun       string   `json:"fromRun"`
	Cases         int      `json:"cases"`
}

// SkillDetailView is the /api/pulse/skill.json payload.
//
// SkillDetailView 是 /api/pulse/skill.json 载荷。
type SkillDetailView struct {
	Name                  string              `json:"name"`
	Runs                  []SkillRunView      `json:"runs"`
	BaselineRunID         string              `json:"baselineRunId,omitempty"`
	Compare               *SkillCompareView   `json:"compare"` // 无 baseline/run 时 null
	Decisions             []SkillDecisionView `json:"decisions"`
	TriggerQuality        *TriggerQualityView `json:"triggerQuality"`        // 无 run 时 null
	LiveFalsePositiveRate *float64            `json:"liveFalsePositiveRate"` // 无数据源，恒 null
}

// AggregateSkills builds the skills overview across the projects in scope. canonical is the
// resolved canonical skill dir ("" = unavailable → observed-only skill set, neverTriggered
// unknowable); evalDir is the skillseval eval dir ("" = no eval data → all health null).
// Per-source failures degrade non-fatally, consistent with AggregateFeed. All counting
// reads through sharedPulseCache — unchanged files are not re-parsed on each poll.
//
// AggregateSkills 跨范围内项目构建 skills 总览。canonical 是解析出的 canonical skill
// 目录（"" = 不可用 → 仅按观测到的 skill 集，neverTriggered 不可知）；evalDir 是
// skillseval eval 目录（"" = 无 eval 数据 → health 全 null）。单源失败降级不致命，
// 与 AggregateFeed 一致。所有计数读取都走 sharedPulseCache——文件未变时轮询不重解析。
func AggregateSkills(opts Options, canonical, evalDir string) (SkillsOverview, error) {
	hits := map[string]int{}
	effTasks := map[string]int{}        // skill → 关联 task 数（跨项目求和）
	effScoreSum := map[string]float64{} // skill → Σ(avgScore×taskCount)，跨项目加权合并
	effScoreN := map[string]int{}       // skill → ΣtaskCount（上面加权的分母）

	for _, pr := range resolvePulseRoots(opts) {
		d, err := sharedPulseCache.projectData(pr)
		if err != nil {
			continue
		}
		d.derived(pr.root)
		for name, n := range d.passive {
			hits[name] += n
		}
		for name, n := range d.active {
			hits[name] += n
		}
		for _, e := range d.effs {
			effTasks[e.Skill] += e.TaskCount
			// Cross-project merge weights each project's AvgScore by its task count —
			// exact for a single project (the common case); a multi-project merge is a
			// dashboard-level approximation since SkillEffectiveness does not export the
			// scored-task denominator.
			//
			// 跨项目合并按各项目 task 数加权其 AvgScore——单项目（常见情形）精确；
			// 多项目合并是看板级近似，因 SkillEffectiveness 不导出 scored-task 分母。
			effScoreSum[e.Skill] += e.AvgScore * float64(e.TaskCount)
			effScoreN[e.Skill] += e.TaskCount
		}
	}

	// Skill set: canonical when resolvable (neverTriggered only meaningful against the full
	// declared set); otherwise degrade to the observed set so the panel is not empty.
	//
	// skill 集：可解析时用 canonical（neverTriggered 只对完整声明集有意义）；否则降级
	// 为观测集，面板不空转。
	var names []string
	canonicalSet := map[string]bool{}
	if canonical != "" {
		if all, err := skillsdist.ListSkills(canonical); err == nil {
			names = all
			for _, n := range all {
				canonicalSet[n] = true
			}
		}
	}
	if len(names) == 0 {
		seen := map[string]bool{}
		for n := range hits {
			seen[n] = true
		}
		for n := range effTasks {
			seen[n] = true
		}
		for n := range seen {
			names = append(names, n)
		}
		slices.Sort(names)
	}

	skills := make([]SkillSummary, 0, len(names))
	never := []string{}
	for _, name := range names {
		sum := SkillSummary{
			Name:      name,
			Hits:      hits[name],
			TaskCount: effTasks[name],
		}
		if effScoreN[name] > 0 {
			avg := effScoreSum[name] / float64(effScoreN[name])
			sum.AvgScore = &avg
		}
		if evalDir != "" {
			if runs := sharedPulseCache.skillEval(canonical, evalDir, name).runs; len(runs) > 0 {
				h := runs[len(runs)-1].HealthScore
				sum.Health = &h
			}
		}
		skills = append(skills, sum)
		if len(canonicalSet) > 0 && canonicalSet[name] && hits[name] == 0 {
			never = append(never, name)
		}
	}
	slices.SortFunc(skills, func(a, b SkillSummary) int {
		if a.Hits != b.Hits {
			return b.Hits - a.Hits // 命中多的在前
		}
		return strings.Compare(a.Name, b.Name)
	})
	slices.Sort(never)
	return SkillsOverview{Skills: skills, NeverTriggered: never, Coverage: skillsCoverageNote}, nil
}

// SkillQualityView is one row of /api/pulse/quality.json: just what the 触发质量 cards
// render (trigger accuracies of the latest run + baseline compare), without the full
// detail payload.
//
// SkillQualityView 是 /api/pulse/quality.json 的一行：触发质量卡片渲染所需的最小集
// （最新 run 的触发准确率 + baseline 比对），不带完整详情载荷。
type SkillQualityView struct {
	Name           string              `json:"name"`
	TriggerQuality *TriggerQualityView `json:"triggerQuality"` // 无 run 时 null
	Compare        *SkillCompareView   `json:"compare"`        // 无 baseline/run 时 null
}

// AggregateQuality builds the 触发质量 tab payload in one shot: every skill from the
// overview with its triggerQuality + compare. Per-skill detail failures are skipped
// non-fatally (consistent with the overview's degradation). Replaces the frontend's
// former N+1 fan-out (skills.json + one skill.json per skill).
//
// AggregateQuality 一次构建触发质量 tab 载荷：总览中的每个 skill 带其
// triggerQuality + compare。单 skill 详情失败跳过不致命（与总览降级一致）。替代
// 前端此前的 N+1 扇出（skills.json + 逐 skill 的 skill.json）。
func AggregateQuality(opts Options, canonical, evalDir string) ([]SkillQualityView, error) {
	ov, err := AggregateSkills(opts, canonical, evalDir)
	if err != nil {
		return nil, err
	}
	views := []SkillQualityView{}
	for _, s := range ov.Skills {
		d, err := LoadSkillDetail(canonical, evalDir, s.Name)
		if err != nil {
			continue
		}
		views = append(views, SkillQualityView{
			Name:           d.Name,
			TriggerQuality: d.TriggerQuality,
			Compare:        d.Compare,
		})
	}
	return views, nil
}

// LoadSkillDetail builds the single-skill detail view. The name is validated against
// skillsfm.IsValidSkillName before touching any path (it arrives as an HTTP query param —
// an unvalidated name would be a path traversal). Raw reads (runs/baseline/decisions) go
// through the fingerprint-gated cache; runPassRates / CompareRuns are pure and run fresh
// each call. Missing runs/baselines/decisions degrade to null/empty sections, never to
// an error.
//
// LoadSkillDetail 构建单 skill 详情视图。name 先过 skillsfm.IsValidSkillName 再碰任何
// 路径（它来自 HTTP query 参数——不校验就是路径遍历）。原始读取（runs/baseline/
// decisions）走指纹门控缓存；runPassRates / CompareRuns 是纯函数每次现算。
// runs/baselines/decisions 缺失降级为 null/空段，绝不报错。
func LoadSkillDetail(canonical, evalDir, name string) (SkillDetailView, error) {
	if !skillsfm.IsValidSkillName(name) {
		return SkillDetailView{}, errInvalidSkillName(name)
	}
	view := SkillDetailView{
		Name:      name,
		Runs:      []SkillRunView{},
		Decisions: []SkillDecisionView{},
	}

	ev := sharedPulseCache.skillEval(canonical, evalDir, name)
	runs := ev.runs
	for _, r := range runs {
		trig, notTrig := runPassRates(r)
		view.Runs = append(view.Runs, SkillRunView{
			RunID: r.RunID, Ts: r.Timestamp, Health: r.HealthScore,
			TriggerAcc: trig, NotTriggerAcc: notTrig, Cases: len(r.Results),
		})
	}

	baselineID := ev.baseline.RunID
	view.BaselineRunID = baselineID

	if len(runs) > 0 {
		latest := runs[len(runs)-1]
		trig, notTrig := runPassRates(latest)
		view.TriggerQuality = &TriggerQualityView{
			TriggerAcc: trig, NotTriggerAcc: notTrig,
			FromRun: latest.RunID, Cases: len(latest.Results),
		}
		if baselineID != "" {
			// CompareRuns 需要完整的 baseline run——从缓存的 runs 里按 ID 找（纯内存，
			// 替代此前的 LoadRunByID 磁盘重读）。参数序保持 CompareRuns(latest, baseline)。
			for i := range runs {
				if runs[i].RunID != baselineID {
					continue
				}
				rep := skillseval.CompareRuns(&latest, &runs[i])
				view.Compare = &SkillCompareView{
					NetRegressions: rep.NetRegressions,
					Regressions:    len(rep.Regressions),
					Improvements:   len(rep.Improvements),
					Comparable:     rep.Comparable,
				}
				break
			}
		}
	}

	for _, d := range ev.decisions {
		view.Decisions = append(view.Decisions, SkillDecisionView{
			ID: d.ID, Ts: d.DecidedAt, Outcome: d.Outcome,
			Diagnosis: d.Diagnosis, Rationale: d.Rationale, Commit: d.CommitHash,
		})
	}
	return view, nil
}

// runPassRates computes the per-kind pass rates of one run; a kind with zero cases yields
// nil (null in JSON) rather than a fabricated 0.
//
// runPassRates 算单 run 的 per-kind 通过率；某类零 case 时给 nil（JSON null）而非
// 编造的 0。
func runPassRates(r skillseval.EvalRun) (trigger, notTrigger *float64) {
	var tp, tt, np, nt int
	for _, res := range r.Results {
		switch res.Kind {
		case skillseval.KindTrigger:
			tt++
			if res.Pass {
				tp++
			}
		case skillseval.KindNotTrigger:
			nt++
			if res.Pass {
				np++
			}
		}
	}
	if tt > 0 {
		v := float64(tp) / float64(tt)
		trigger = &v
	}
	if nt > 0 {
		v := float64(np) / float64(nt)
		notTrigger = &v
	}
	return trigger, notTrigger
}
