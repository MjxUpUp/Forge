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

// AnalyzeUsage crosses toollog Skill calls with the canonical skill set to produce an undertrigger analysis.
//
// AnalyzeUsage 交叉 toollog 的 Skill 调用与 canonical skill 集，产出 undertrigger 分析。
//
// The data source is toollog (agent-neutral) — the fundamental difference from the old pi source
// (agent-coupled, broken after pi exited specialization). The canonical set filters out "ghost skills" (toollog residue but canonical deleted), symmetric with NeverTriggered (canonical-only).
//
// 数据源是 toollog（agent-neutral）——与旧 pi 源（agent-coupled，pi 退出专精后断链）的根本
// 区别。canonical 集过滤"幽灵技能"（toollog 残留但 canonical 已删），与 NeverTriggered（仅
// canonical）对称。
func AnalyzeUsage(root, canonical string) (*UsageReport, error) {
	counts, total, err := SkillCountsFromToollog(root)
	if err != nil {
		return nil, err
	}
	all, err := skillsdist.ListSkills(canonical)
	if err != nil {
		return nil, err
	}

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
