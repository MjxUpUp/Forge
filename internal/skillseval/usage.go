// Package skillseval provides skill usage metrics analysis (usage) and eval list generation (skill-eval).
//
// Package skillseval 提供 skill 使用度量分析（usage）与 eval 清单生成（skill-eval）。
//
// Usage metrics are based on toollog (agent-neutral collection layer): the tool-track hook plugs in across hosts, recording when the Skill tool
// event fires (currently only Claude Code has Skill tool events; cursor/codex etc. inject skills via mdc/AGENTS.md
// with no tool-call events, naturally producing no records — see ExtractSkillName for parser extension points). Replaces the broken old pi source
// (~/.pi/research/skill-usage.jsonl, after pi exited specialization no one writes it). The data layer is agent-neutral, consistent with the project's
// "outer framework must not depend on a specific agent" principle. Cross-task reading goes through LoadAllAll (including archived toollog-*.jsonl).
//
// 使用度量基于 toollog（agent-neutral 采集层）：tool-track hook 跨 host 接入，Skill 工具
// 事件触发时记录（当前仅 Claude Code 有 Skill 工具事件；cursor/codex 等 skill 经 mdc/AGENTS.md
// 注入、无工具调用事件，自然不产生记录——解析点扩展见 ExtractSkillName）。替代断链的 pi 旧源
// （~/.pi/research/skill-usage.jsonl，pi 退出专精后无人写）。数据层 agent-neutral，符合项目
// 「外层框架不得依赖某个 agent」的原则。跨任务读取走 LoadAllAll（含归档 toollog-*.jsonl）。
package skillseval

import (
	"cmp"
	"encoding/json"
	"slices"
	"strings"

	"github.com/MjxUpUp/Forge/internal/checklog"
	"github.com/MjxUpUp/Forge/internal/skillsdist"
	"github.com/MjxUpUp/Forge/internal/toolusage"
)

// ExtractSkillName extracts the skill name from the tool_input of a Skill tool call.
//
// ExtractSkillName 从 Skill 工具调用的 tool_input 提取 skill 名。
//
// Claude Code Skill tool input is JSON {"skill":"<name>","args":"..."}. When tool-track records it
// tool_input is truncated to 500 chars; the skill name in the JSON front is usually intact. Non-JSON / missing skill field /
// truncated-corrupted → returns empty (caller skips).
//
// Claude Code Skill 工具输入是 JSON {"skill":"<name>","args":"..."}。tool-track 记录时
// tool_input 截断到 500 字符，skill 名在 JSON 前部一般完好。非 JSON / 无 skill 字段 /
// 截断损坏 → 返回空（调用方跳过）。
//
// agent-neutral parsing point: if different hosts have different Skill tool input formats, extend this function rather than changing the data source.
// Hosts lacking the Skill tool concept (cursor/codex etc. inject skills via mdc/AGENTS.md, no tool-call events)
// naturally produce no records — skillseval still gives an honest undertrigger conclusion when data is missing (all canonical untriggered).
//
// agent-neutral 解析点：不同 host 的 Skill 工具输入格式若不同，扩展此函数而非改数据源。
// 缺少 Skill 工具概念的 host（cursor/codex 等 skill 经 mdc/AGENTS.md 注入，无工具调用事件）
// 自然不产生记录——skillseval 在数据缺失时仍给出诚实的 undertrigger 结论（全 canonical 未触发）。
func ExtractSkillName(toolInput string) string {
	if toolInput == "" {
		return ""
	}
	var v struct {
		Skill string `json:"skill"`
	}
	if json.Unmarshal([]byte(toolInput), &v) != nil {
		return ""
	}
	return strings.TrimSpace(v.Skill)
}

// SkillCountsFromToollog counts Skill tool calls from toollog (agent-neutral data source).
// Goes through LoadAllAll to read across archives (active + toollog-*.jsonl); otherwise after forge task start archives, cross-task
// aggregation would only see the current task. Returns skill→count and the total Skill call event count. Bad lines / non-Skill calls / failures to extract
// skill names are all skipped.
//
// SkillCountsFromToollog 从 toollog 统计 Skill 工具调用次数（agent-neutral 数据源）。
// 走 LoadAllAll 跨归档读（active + toollog-*.jsonl），否则 forge task start 归档后跨任务
// 聚合只剩当前任务。返回 skill→count 与总 Skill 调用事件数。坏行/非 Skill 调用/提取不到
// skill 名的均跳过。
func SkillCountsFromToollog(root string) (map[string]int, int, error) {
	calls, err := toolusage.LoadAllAll(root)
	if err != nil {
		return nil, 0, err
	}
	counts := map[string]int{}
	total := 0
	for _, c := range calls {
		if c.ToolName != "Skill" {
			continue
		}
		name := ExtractSkillName(c.ToolInput)
		if name == "" {
			continue
		}
		counts[name]++
		total++
	}
	return counts, total, nil
}

// SkillCount is the load count for a single skill.
//
// SkillCount 是单个 skill 的加载次数。
type SkillCount struct {
	Name  string `json:"name"`
	Count int    `json:"count"`
}

// UsageReport is the usage metrics analysis result.
//
// UsageReport 是使用度量分析结果。
type UsageReport struct {
	TotalEvents    int          `json:"total_events"`
	TotalSkills    int          `json:"total_skills"`
	UsedSkills     int          `json:"used_skills"`
	NeverTriggered []string     `json:"never_triggered"`
	HotSkills      []SkillCount `json:"hot_skills"`
}

// AnalyzeUsage merges two reach signals and crosses them with the canonical skill set to produce an
// undertrigger analysis: (1) active Skill tool calls from toollog, (2) passive skill-trigger firings from
// checklog (CheckSkillTrigger). Passive is the larger signal in practice — skill-trigger fires on every
// matching event while the Skill tool is only invoked on explicit load — so counting only active calls would
// false-positive NeverTriggered on skills that fire passively but are never explicitly called.
//
// Both sources are agent-neutral (tool-track hook + skill-trigger engine, deterministic). The canonical set
// filters out "ghost skills" (log residue but canonical deleted), symmetric with NeverTriggered (canonical-only).
//
// AnalyzeUsage 合并两个触达信号并与 canonical skill 集交叉，产出 undertrigger 分析：（1）toollog 的
// 主动 Skill 工具调用，（2）checklog 的被动 skill-trigger 触发（CheckSkillTrigger）。实践中被动是更大
// 信号——skill-trigger 每个匹配事件都触发，而 Skill 工具只在显式加载时调用——故只数主动调用会让
// 「被动触发过但从未显式调用」的 skill 在 NeverTriggered 假阳性。
//
// 两源均 agent-neutral（tool-track hook + skill-trigger 引擎，deterministic）。canonical 集过滤
// "幽灵技能"（日志残留但 canonical 已删），与 NeverTriggered（仅 canonical）对称。
func AnalyzeUsage(root, canonical string) (*UsageReport, error) {
	activeCounts, activeTotal, err := SkillCountsFromToollog(root)
	if err != nil {
		return nil, err
	}
	passiveCounts, passiveTotal, err := SkillCountsFromChecklog(root)
	if err != nil {
		return nil, err
	}
	all, err := skillsdist.ListSkills(canonical)
	if err != nil {
		return nil, err
	}

	// Merge active (Skill tool calls, toollog) + passive (skill-trigger firings, checklog) into one reach
	// signal. skill-trigger injects skills passively via AdditionalContext — the agent then reads SKILL.md via
	// Read, NOT the Skill tool — so toollog's Skill-tool counts severely undercount skill reach (passive firings
	// are invisible there). Merging both stops NeverTriggered from false-positive on skills that fire passively
	// but are never explicitly called (the dogfood 0-trigger blind spot on the usage side).
	//
	// 合并主动（Skill 工具调用，toollog）+ 被动（skill-trigger 触发，checklog）为一个触达信号。
	// skill-trigger 经 AdditionalContext 被动注入——agent 随后用 Read 读 SKILL.md、而非 Skill 工具——
	// 故 toollog 的 Skill 工具计数严重低估 skill 触达（被动触发在那边不可见）。合并两者避免 NeverTriggered
	// 对「被动触发过但从未显式调用」的 skill 假阳性（usage 侧的 dogfood 0 触发盲区）。
	counts := map[string]int{}
	for n, c := range activeCounts {
		counts[n] += c
	}
	for n, c := range passiveCounts {
		counts[n] += c
	}
	total := activeTotal + passiveTotal

	never := []string{}
	for _, n := range all {
		if counts[n] == 0 {
			never = append(never, n)
		}
	}
	slices.Sort(never)

	// Canonical set: HotSkills/UsedSkills count only skills present in canonical, filtering out
	// "ghost skills" in the log (canonical deleted but log residue) — symmetric with NeverTriggered (canonical-only).
	//
	// canonical 集：HotSkills/UsedSkills 只计 canonical 中存在的 skill，过滤日志里的
	// "幽灵技能"（canonical 已删但日志残留）——与 NeverTriggered（仅 canonical）对称。
	canonicalSet := make(map[string]bool, len(all))
	for _, n := range all {
		canonicalSet[n] = true
	}
	hot := make([]SkillCount, 0, len(counts))
	used := 0
	for name, cnt := range counts {
		if !canonicalSet[name] {
			continue
		}
		hot = append(hot, SkillCount{Name: name, Count: cnt})
		used++
	}
	slices.SortFunc(hot, func(a, b SkillCount) int {
		if a.Count != b.Count {
			return cmp.Compare(b.Count, a.Count)
		}
		return cmp.Compare(a.Name, b.Name)
	})
	if len(hot) > 10 {
		hot = hot[:10]
	}

	return &UsageReport{
		TotalEvents:    total,
		TotalSkills:    len(all),
		UsedSkills:     used,
		NeverTriggered: never,
		HotSkills:      hot,
	}, nil
}

// SkillCountsFromChecklog counts passive skill-trigger firings from checklog (CheckSkillTrigger entries) —
// the second data source for usage analysis. skill-trigger injects skills passively via AdditionalContext
// (the agent then reads SKILL.md via Read, NOT the Skill tool), so toollog's Skill-tool counts severely
// undercount skill reach — passive firings are invisible there. This closes the dogfood 0-trigger blind spot
// on the usage side. Goes through checklog.LoadAllAll (active + archived) for cross-task coverage, symmetric
// with SkillCountsFromToollog. Returns skill→count and total trigger events. Detail that fails to parse
// (format drift / corruption) is skipped.
//
// SkillCountsFromChecklog 从 checklog（CheckSkillTrigger 条目）统计被动 skill-trigger 触发——
// usage 分析的第二数据源。skill-trigger 经 AdditionalContext 被动注入 skill（agent 随后用 Read
// 读 SKILL.md、而非 Skill 工具），故 toollog 的 Skill 工具计数严重低估 skill 触达——被动触发
// 在那边不可见。本函数在 usage 侧闭合 dogfood 0 触发盲区。走 checklog.LoadAllAll
// （active + 归档）获跨任务覆盖，对称 SkillCountsFromToollog。返回 skill→count 与总触发事件数。
// 解析失败（格式漂移/损坏）的 Detail 被跳过。
func SkillCountsFromChecklog(root string) (map[string]int, int, error) {
	entries, err := checklog.LoadAllAll(root)
	if err != nil {
		return nil, 0, err
	}
	counts := map[string]int{}
	total := 0
	for _, e := range entries {
		if e.Check != checklog.CheckSkillTrigger {
			continue
		}
		name := checklog.SkillFromTriggerDetail(e.Detail)
		if name == "" {
			continue
		}
		counts[name]++
		total++
	}
	return counts, total, nil
}
