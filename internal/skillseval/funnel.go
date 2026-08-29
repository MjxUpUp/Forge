// Package skillseval funnel.go — 被动触发漏斗（命中 → 送达 → 加载）。
//
// 背景（2026-08-16 触发臂审查）：skill-trigger 的可观测原本只到「引擎命中」——134 次命中
// 0 次可验证转化，无法区分「模型读了注入并照做」与「注入被无视」。但测量信号其实全在盘上：
// checklog 条目带 session/时间/skill 名，toollog 每条 Read/Skill 带 session/时间/输入。
// 「模型是否照做」≈ 同 session、命中后 N 分钟内、出现对该 skill SKILL.md 的 Read（或同名
// Skill 调用）。本文件就是这个 join——零新增运行时埋点，把 0/134 的盲区变成可算的漏斗。
package skillseval

import (
	"cmp"
	"encoding/json"
	"slices"
	"strings"
	"time"

	"github.com/MjxUpUp/Forge/internal/checklog"
	"github.com/MjxUpUp/Forge/internal/toolusage"
)

// 漏斗参数。promptDedupeWindow 收敛同一 prompt 上多机制重复命中（skill-trigger advisory
// 与强制路由可同时命中同一 skill——两次命中一次遵循，不去重会虚增分母）；engageWindow 是
// 「命中 → 遵循」的归因窗口：注入文本要求模型先读 SKILL.md 再继续，正常遵循在几分钟内
// 发生；窗口过长会把无关加载误归因，过短会漏掉长回合里的延后读取。10 分钟是折中。
const (
	TriggerPromptDedupeWindow = 60 * time.Second
	TriggerEngageWindow       = 10 * time.Minute
)

// SkillFunnel is the per-skill funnel stats.
//
// SkillFunnel 是单个 skill 的漏斗统计。
// Hits 已按 (session, skill) 去重（同 prompt 双机制命中算一次）；Delivered 只计
// Delivered=true 的章；DeliveryUnknown 是字段引入前的旧条目（nil）——诚实单列、
// 不假装已送达；Engaged 是去重后命中里命中后 EngageWindow 内同 session 出现
// Read(<skill>/SKILL.md) 或 Skill(<skill>) 的条数。
type SkillFunnel struct {
	Name            string `json:"name"`
	Hits            int    `json:"hits"`
	Delivered       int    `json:"delivered"`
	DeliveryUnknown int    `json:"delivery_unknown"`
	Engaged         int    `json:"engaged"`
}

// FunnelReport is the aggregate funnel.
//
// FunnelReport 是全量漏斗聚合。Totals 是各 skill 之和；Skills 按 Hits 降序、同名稳定。
type FunnelReport struct {
	Window         time.Duration `json:"window"`
	Skills         []SkillFunnel `json:"skills"`
	TotalHits      int           `json:"total_hits"`
	TotalDelivered int           `json:"total_delivered"`
	TotalEngaged   int           `json:"total_engaged"`
}

// funnelHit 是去重前的单条命中（内部态）。
// funnelHit is one raw hit before dedupe (internal).
type funnelHit struct {
	session   string
	skill     string
	at        time.Time
	delivered *bool
}

// AnalyzeTriggerFunnel builds the passive-trigger funnel from disk (checklog + toollog, archives included).
//
// AnalyzeTriggerFunnel 从磁盘（checklog + toollog，含归档）构建被动触发漏斗。
// CLI 便捷入口；纯逻辑在 BuildTriggerFunnel（可测）。
func AnalyzeTriggerFunnel(root string) (*FunnelReport, error) {
	entries, err := checklog.LoadAllAll(root)
	if err != nil {
		return nil, err
	}
	calls, err := toolusage.LoadAllAll(root)
	if err != nil {
		return nil, err
	}
	rep := BuildTriggerFunnel(entries, calls)
	return rep, nil
}

// BuildTriggerFunnel is the pure funnel core: entries × calls → hit→delivered→engaged join.
//
// BuildTriggerFunnel 是漏斗纯函数核心：entries × calls → 命中→送达→加载 join。
// 非本包关注面的条目（非 CheckSkillTrigger / 解析不出 skill 名）跳过。
func BuildTriggerFunnel(entries []checklog.Entry, calls []toolusage.ToolCall) *FunnelReport {
	var hits []funnelHit
	for _, e := range entries {
		if e.Check != checklog.CheckSkillTrigger {
			continue
		}
		name := checklog.SkillFromTriggerDetail(e.Detail)
		if name == "" {
			continue
		}
		hits = append(hits, funnelHit{session: e.SessionID, skill: name, at: e.RecordedAt, delivered: e.Delivered})
	}

	// 去重：按 (session, skill, 时间) 排序后，同 (session, skill) 且距团首 ≤
	// TriggerPromptDedupeWindow 的折成一团（同 prompt 双机制命中）。团内送达章取或
	// （任一机制送达即算送达——它们注入的是同一个指引）；仅 true 翻转状态——nil 保持
	// unknown、false 不改团（无 NotDelivered 计数，保守处理）。
	slices.SortFunc(hits, func(a, b funnelHit) int {
		if c := cmp.Compare(a.session, b.session); c != 0 {
			return c
		}
		if c := cmp.Compare(a.skill, b.skill); c != 0 {
			return c
		}
		return a.at.Compare(b.at)
	})
	type group struct {
		session   string
		skill     string
		at        time.Time
		delivered bool
		unknown   bool
	}
	var groups []group
	for _, h := range hits {
		if n := len(groups); n > 0 {
			last := &groups[n-1]
			if last.session == h.session && last.skill == h.skill && h.at.Sub(last.at) <= TriggerPromptDedupeWindow {
				if h.delivered != nil && *h.delivered {
					last.delivered = true
					last.unknown = false
				}
				continue
			}
		}
		g := group{session: h.session, skill: h.skill, at: h.at, unknown: h.delivered == nil}
		if h.delivered != nil && *h.delivered {
			g.delivered = true
			g.unknown = false
		}
		groups = append(groups, g)
	}

	// 热路径预处理（2026-08-29 perf 审查）：engagedAfter 原为每次全量扫 calls 并重复
	// JSON 解析（O(groups×calls) 次解析）。这里在聚合前建一次索引：按 SessionID 分桶、
	// 每条 call 的信号（skill 名或读路径）只提取一次，后续判定只扫本 session 桶做
	// 字符串比较。输出字节等价由 TestBuildTriggerFunnel_GoldenJSON 钉住。
	idx := buildEngagedIndex(calls)

	bySkill := map[string]*SkillFunnel{}
	order := []string{}
	for _, g := range groups {
		sf, ok := bySkill[g.skill]
		if !ok {
			sf = &SkillFunnel{Name: g.skill}
			bySkill[g.skill] = sf
			order = append(order, g.skill)
		}
		sf.Hits++
		if g.delivered {
			sf.Delivered++
		}
		if g.unknown {
			sf.DeliveryUnknown++
		}
		if idx.engagedAfter(g.session, g.skill, g.at) {
			sf.Engaged++
		}
	}

	rep := &FunnelReport{Window: TriggerEngageWindow}
	for _, name := range order {
		rep.Skills = append(rep.Skills, *bySkill[name])
	}
	slices.SortFunc(rep.Skills, func(a, b SkillFunnel) int {
		if a.Hits != b.Hits {
			return cmp.Compare(b.Hits, a.Hits)
		}
		return cmp.Compare(a.Name, b.Name)
	})
	for _, sf := range rep.Skills {
		rep.TotalHits += sf.Hits
		rep.TotalDelivered += sf.Delivered
		rep.TotalEngaged += sf.Engaged
	}
	return rep
}

// engagedSignal 是一条工具调用预提取后的 engaged 信号：Skill 调用取 skill 名、
// Read 调用取归一化路径（小写 + 反斜杠折叠），其余工具/提取失败无信号。提取逻辑
// 与逐条判定共用同一函数——单一判定真相源，索引路径与兼容路径不可能分叉。
type engagedSignal struct {
	at    time.Time
	isSK  bool   // true = Skill 调用；false = Read 调用
	value string // Skill: 提取到的名字；Read: 归一化路径
}

// signalOf 提取一条调用的 engaged 信号（无信号时 ok=false）。Read 的归一化与旧实现
// 逐字一致：ToLower + 反斜杠→斜杠，后缀匹配交给 matches。
func signalOf(c toolusage.ToolCall) (engagedSignal, bool) {
	switch c.ToolName {
	case "Skill":
		// EqualFold 与下面 Read 分支的大小写归一对齐——仅大小写差异的 Skill 调用
		// （Windows 路径下常见）不得被漏计。
		name := ExtractSkillName(c.ToolInput)
		if name == "" {
			return engagedSignal{}, false
		}
		return engagedSignal{at: c.Timestamp, isSK: true, value: name}, true
	case "Read":
		p := readFilePath(c.ToolInput)
		if p == "" {
			return engagedSignal{}, false
		}
		p = strings.ToLower(strings.ReplaceAll(p, "\\", "/"))
		return engagedSignal{at: c.Timestamp, value: p}, true
	}
	return engagedSignal{}, false
}

// matches 报告该信号是否构成 (skill, at) 的遵循：时间窗内 +（Skill 同名 EqualFold
// 或 Read 路径后缀命中 canonical / embed-cache 形态）。窗口边界与旧实现逐字一致
// （Before(at) 跳过、After(at+window) 跳过，两端含）。
func (s engagedSignal) matches(skill, lowerSkill string, at time.Time) bool {
	if s.at.Before(at) || s.at.After(at.Add(TriggerEngageWindow)) {
		return false
	}
	if s.isSK {
		return strings.EqualFold(s.value, skill)
	}
	return strings.HasSuffix(s.value, "/skills/"+lowerSkill+"/skill.md") ||
		strings.HasSuffix(s.value, "/skills-cache/embedded/"+lowerSkill+"/skill.md")
}

// engagedIndex 是 calls 的分桶索引：SessionID → 该 session 的信号切片（按原序）。
// 一次构建、多次查询，把 O(groups×calls) 的重复 JSON 解析压成 O(calls) 单次提取 +
// 每 group 一次本桶扫描。
type engagedIndex struct {
	bySession map[string][]engagedSignal
}

// buildEngagedIndex 扫一遍 calls 建分桶索引。无信号调用（无关工具、截断 JSON、
// 空提取）不入桶——它们在任何判定下都是 continue，等价于不存在。
func buildEngagedIndex(calls []toolusage.ToolCall) *engagedIndex {
	ix := &engagedIndex{bySession: make(map[string][]engagedSignal)}
	for _, c := range calls {
		sig, ok := signalOf(c)
		if !ok {
			continue
		}
		ix.bySession[c.SessionID] = append(ix.bySession[c.SessionID], sig)
	}
	return ix
}

// engagedAfter（索引版）只扫本 session 桶做字符串比较。空 session 无法归因（旧
// 条目），返 false——与逐条版一致。
func (ix *engagedIndex) engagedAfter(session, skill string, at time.Time) bool {
	if session == "" {
		return false
	}
	lower := strings.ToLower(skill)
	for _, sig := range ix.bySession[session] {
		if sig.matches(skill, lower, at) {
			return true
		}
	}
	return false
}

// engagedAfter 报告 (session, skill, 命中时刻) 之后 EngageWindow 内是否出现对该 skill 的
// 遵循信号：Read 其 SKILL.md（canonical 路径或 embed cache 路径，Windows 大小写与分隔符
// 归一后后缀匹配）或 Skill(<skill>) 显式调用。空 session 无法归因（旧条目），返 false。
//
// Read 的 tool_input 截断到 500 字符可能截坏 JSON——解析失败按无信号处理（诚实跳过），
// 与 ExtractSkillName 的失败语义一致。
//
// 本函数是兼容入口：keyword.go / mine.go 逐条调用它判定 engaged（单一判定真相源），
// 每次只查一条命中，为其整建索引反而劣化——故保留逐条扫描实现；批量入口
// （BuildTriggerFunnel）请用 buildEngagedIndex + 索引版 engagedAfter。
func engagedAfter(calls []toolusage.ToolCall, session, skill string, at time.Time) bool {
	if session == "" {
		return false
	}
	lower := strings.ToLower(skill)
	for _, c := range calls {
		if c.SessionID != session {
			continue
		}
		if c.Timestamp.Before(at) || c.Timestamp.After(at.Add(TriggerEngageWindow)) {
			continue
		}
		sig, ok := signalOf(c)
		if ok && sig.matches(skill, lower, at) {
			return true
		}
	}
	return false
}

// readFilePath 从 Read 工具调用的 tool_input JSON 提取 file_path。与 ExtractSkillName
// 同款失败语义：空串 = 提取不到，调用方跳过。
func readFilePath(toolInput string) string {
	if toolInput == "" {
		return ""
	}
	var v struct {
		FilePath string `json:"file_path"`
	}
	if json.Unmarshal([]byte(toolInput), &v) != nil {
		return ""
	}
	return strings.TrimSpace(v.FilePath)
}
