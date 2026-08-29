package skillseval

// keyword.go — per-keyword 触发分析（skill-trigger v2 / P0.5 轻量分析层）。
//
// 数据前提：v2 起每条 CheckSkillTrigger 命中在 Entry.Meta 携带 matched_keyword /
// match_source / suppressed_since_last（键契约 checklog.MetaKey*）。本文件把这些横向
// 切成 per-keyword 统计——回答三个此前无法回答的问题：
//   1. 哪个关键词在驱动命中（噪声关键词候选：高命中低 engaged）；
//   2. 哪些声明过的关键词从未命中（死关键词：关键词表删除候选，ExpeL count-to-zero
//      的镜像）；
//   3. cooldown 抑制的分布（关键词 × suppressed 计数，cooldown 调参依据）。
//
// 边界（对抗辩论 G1/G3 的诚实约束）：
//   - engaged 判定复用 funnel.go 的 engagedAfter（单一判定真相源），但**不做漏斗的
//     同 prompt 去重**——本层是原始计数切片，漏斗视图仍以 usage 主输出为准；
//   - 宿主偏差：engaged 信号依赖 Read/Skill 工具事件，codex/cursor 等注入型宿主天然
//     无信号（usage.go 包注释同款 caveat）——per-keyword engaged 率系统性偏 Claude Code
//     会话，消费方（cli）必须带偏差标注输出，不得裸报"低遵循率"；
//   - Meta 之前的旧条目（无 matched_keyword）单独归入 "condition-only/legacy" 行，
//     不与关键词行混算。

import (
	"cmp"
	"slices"
	"strconv"

	"github.com/MjxUpUp/Forge/internal/checklog"
	"github.com/MjxUpUp/Forge/internal/toolusage"
)

// KeywordStat is the trigger stats of one (skill, keyword).
//
// KeywordStat 是单个 (skill, keyword) 的触发统计。Condition-only 触发的 Keyword 为
// 空串（渲染层显示为 condition 行）。
type KeywordStat struct {
	Skill   string `json:"skill"`
	Keyword string `json:"keyword"`
	Hits    int    `json:"hits"`
	Engaged int    `json:"engaged"`
	// Suppressed sums suppressed_since_last (cooldown backfill).
	//
	// Suppressed sums suppressed_since_last（cooldown 抑制回填）。归因近似（review m6）：
	// 计数器是 per-(session, skill) 粒度，回填挂在**触发那条命中**的 matched_keyword
	// 行上——被抑制的近失配可能属于同 skill 的其他关键词。跨词比较该列调 cooldown
	// 会错挂；按 skill 汇总看才是安全读法。
	Suppressed int `json:"suppressed"`
}

// KeywordReport carries per-keyword analysis output; V2Hits gates dead-keyword detection.
//
// KeywordReport 是 per-keyword 分析产出。DeadKeywords 按声明集减命中集得出（cli 层
// 从 skilltrigger.LoadAll 提取声明传入——本包不 import skilltrigger，防 import 环）。
//
// V2Hits 是带 v2 Meta（trigger_index 键）的命中数——死关键词检测的**前提门槛**：
// V2Hits == TotalHits（窗口内全部命中可归因）才开启检测。只要窗口混有任何 v1 条目
// （retention 30 天内必然如此），某关键词最后一次命中可能落在归因上线前（v1 条目对
// hit 集零贡献）——"零命中"混同「从未命中」与「归因上线前命中过」，检测整体停用
// （生产实况：v2 上线首日全窗口 v1，382 个"死词"全是噪声；窗口级门槛而非条目级是
// 保守取舍——宁可漏报死词，不可误删活词）。固有边界（接受并声明）：窗口截断前命中
// 过的词在满 v2 窗口下仍可能被误判死——30 天滚动窗外的历史不可见。
type KeywordReport struct {
	Stats         []KeywordStat `json:"stats"`          // (skill, keyword) 降序
	DeadKeywords  []KeywordStat `json:"dead_keywords"`  // 声明过但零命中（V2Hits<TotalHits 时恒 nil）
	ConditionOnly []KeywordStat `json:"condition_only"` // per-skill 无关键词命中行（condition 触发 + Meta 前旧条目）
	TotalHits     int           `json:"total_hits"`
	V2Hits        int           `json:"v2_hits"` // 带 v2 Meta 的命中数（死关键词检测门槛信号）
}

// AnalyzeKeywords builds per-keyword stats from checklog (CheckSkillTrigger + Meta) × toollog (engagement). declared is skill→declared keyword set (nil skips dead-keyword detection).
//
// AnalyzeKeywords 从 checklog（CheckSkillTrigger + Meta）× toollog（engaged 判定）构建
// per-keyword 统计。declared 为 skill→声明关键词集（可为 nil——跳过死关键词检测）。
// 纯函数（可测）。stop-cap advisory 无 " hit (" 标记，SkillFromTriggerDetail 返回 ""
// 天然跳过。
func AnalyzeKeywords(entries []checklog.Entry, calls []toolusage.ToolCall, declared map[string][]string) *KeywordReport {
	type key struct{ skill, keyword string }
	order := []key{}
	agg := map[key]*KeywordStat{}

	get := func(skill, kw string) *KeywordStat {
		k := key{skill, kw}
		if st, ok := agg[k]; ok {
			return st
		}
		st := &KeywordStat{Skill: skill, Keyword: kw}
		agg[k] = st
		order = append(order, k)
		return st
	}

	rep := &KeywordReport{}
	for _, e := range entries {
		if e.Check != checklog.CheckSkillTrigger {
			continue
		}
		name := checklog.SkillFromTriggerDetail(e.Detail)
		if name == "" {
			continue
		}
		rep.TotalHits++
		// v2 门槛信号：trigger_index 键在每条 v2 命中上恒落（无论有无关键词）。
		if e.Meta[checklog.MetaKeyTriggerIndex] != "" {
			rep.V2Hits++
		}
		kw := e.Meta[checklog.MetaKeyMatchedKeyword]
		st := get(name, kw)
		st.Hits++
		if engagedAfter(calls, e.SessionID, name, e.RecordedAt) {
			st.Engaged++
		}
		if n, err := strconv.Atoi(e.Meta[checklog.MetaKeySuppressedSinceLast]); err == nil && n > 0 {
			st.Suppressed += n
		}
	}
	for _, k := range order {
		if k.keyword == "" {
			// per-skill condition-only 行（曾误设单行结构：多 skill 时 last-writer-wins，
			// 14 条 condition-only 只显示 1——TestAnalyzeKeywords_Basic 钉住）。
			rep.ConditionOnly = append(rep.ConditionOnly, *agg[k])
			continue
		}
		rep.Stats = append(rep.Stats, *agg[k])
	}
	// ConditionOnly 也排序（review n1）：与 Stats 的确定性对齐——首现顺序对 JSON 消费方
	// 是不稳定输出。
	slices.SortFunc(rep.ConditionOnly, func(a, b KeywordStat) int {
		if c := cmp.Compare(b.Hits, a.Hits); c != 0 {
			return c
		}
		return cmp.Compare(a.Skill, b.Skill)
	})
	// 命中降序、关键词稳定——噪声关键词（高频低 engaged）自然浮顶。
	slices.SortFunc(rep.Stats, func(a, b KeywordStat) int {
		if c := cmp.Compare(b.Hits, a.Hits); c != 0 {
			return c
		}
		return cmp.Compare(a.Skill+"|"+a.Keyword, b.Skill+"|"+b.Keyword)
	})

	// 死关键词：声明 ∖ 命中。门槛是**归因完备**（review M1）：declared 传入，且
	// V2Hits == TotalHits——窗口混有 v1 条目时，某词的最后一次命中可能落在归因上线
	// 前（对 hit 集零贡献），"零命中"会混同「从未命中」与「归因上线前命中过」，
	// 检测整体停用（v2 上线首日 382 个幻影死词的生产实况；宁可漏报死词不误删活词）。
	if declared != nil && rep.TotalHits > 0 && rep.V2Hits == rep.TotalHits {
		hit := map[key]bool{}
		for k := range agg {
			if k.keyword != "" {
				hit[k] = true
			}
		}
		skills := make([]string, 0, len(declared))
		for s := range declared {
			skills = append(skills, s)
		}
		slices.Sort(skills)
		for _, s := range skills {
			kws := slices.Clone(declared[s])
			slices.Sort(kws)
			for _, kw := range kws {
				if kw == "" || hit[key{s, kw}] {
					continue
				}
				rep.DeadKeywords = append(rep.DeadKeywords, KeywordStat{Skill: s, Keyword: kw})
			}
		}
	}
	return rep
}
