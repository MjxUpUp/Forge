// Package skillseval funnel.go — 被动触发漏斗（命中 → 送达 → 加载）。
//
// 背景（2026-08-16 触发臂审查）：skill-trigger 的可观测原本只到「引擎命中」——134 次命中
// 0 次可验证转化，无法区分「模型读了注入并照做」与「注入被无视」。但测量信号其实全在盘上：
// checklog 条目带 session/时间/skill 名，toollog 每条 Read/Skill 带 session/时间/输入。
// 「模型是否照做」≈ 同 session、命中后 N 分钟内、出现对该 skill SKILL.md 的 Read（或同名
// Skill 调用）。本文件就是这个 join——零新增运行时埋点，把 0/134 的盲区变成可算的漏斗。
//
// Package skillseval funnel.go — the passive-trigger funnel (hit → delivered → engaged).
//
// Background (2026-08-16 trigger-arm audit): skill-trigger observability used to stop at
// "engine hit" — 134 hits with 0 verifiable conversions, no way to tell "model read the
// injection and followed" from "injection ignored". But the measurement signals are all
// already on disk: checklog entries carry session/time/skill, toollog Read/Skill calls carry
// session/time/input. "Did the model act on it" ≈ same session, within N minutes after the
// hit, a Read of that skill's SKILL.md (or a same-name Skill call). This file is that join —
// zero new runtime instrumentation, turning the 0/134 blind spot into a computable funnel.
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
//
// Funnel parameters. promptDedupeWindow collapses same-prompt multi-mechanism double-fires
// (skill-trigger advisory and the forced router can hit the same skill at once — two hits,
// one engagement; without dedupe the denominator inflates). engageWindow is the attribution
// window for hit → engagement: the injection tells the model to read SKILL.md before
// continuing, so a genuine following happens within minutes; too long misattributes unrelated
// loads, too short misses deferred reads inside long turns. 10 minutes is the compromise.
const (
	TriggerPromptDedupeWindow = 60 * time.Second
	TriggerEngageWindow       = 10 * time.Minute
)

// SkillFunnel 是单个 skill 的漏斗统计。
// Hits 已按 (session, skill) 去重（同 prompt 双机制命中算一次）；Delivered 只计
// Delivered=true 的章；DeliveryUnknown 是字段引入前的旧条目（nil）——诚实单列、
// 不假装已送达；Engaged 是去重后命中里命中后 EngageWindow 内同 session 出现
// Read(<skill>/SKILL.md) 或 Skill(<skill>) 的条数。
//
// SkillFunnel is the per-skill funnel stats.
// Hits is deduped by (session, skill) (same-prompt double-fire counts once); Delivered counts
// only Delivered=true stamps; DeliveryUnknown covers legacy entries (nil) — listed honestly,
// never assumed delivered; Engaged counts deduped hits followed within EngageWindow, in the
// same session, by a Read of <skill>/SKILL.md or a Skill(<skill>) call.
type SkillFunnel struct {
	Name            string `json:"name"`
	Hits            int    `json:"hits"`
	Delivered       int    `json:"delivered"`
	DeliveryUnknown int    `json:"delivery_unknown"`
	Engaged         int    `json:"engaged"`
}

// FunnelReport 是全量漏斗聚合。Totals 是各 skill 之和；Skills 按 Hits 降序、同名稳定。
//
// FunnelReport is the aggregate funnel. Totals sum across skills; Skills sorts by Hits
// descending, name-stable.
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

// AnalyzeTriggerFunnel 从磁盘（checklog + toollog，含归档）构建被动触发漏斗。
// CLI 便捷入口；纯逻辑在 BuildTriggerFunnel（可测）。
//
// AnalyzeTriggerFunnel builds the passive-trigger funnel from disk (checklog + toollog,
// archives included). CLI convenience entry; the pure logic lives in BuildTriggerFunnel
// (testable).
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

// BuildTriggerFunnel 是漏斗纯函数核心：entries × calls → 命中→送达→加载 join。
// 非本包关注面的条目（非 CheckSkillTrigger / 解析不出 skill 名）跳过。
//
// BuildTriggerFunnel is the pure funnel core: entries × calls → hit→delivered→engaged join.
// Entries outside this concern (not CheckSkillTrigger / unparseable skill name) are skipped.
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
	//
	// Dedupe: sorted by (session, skill, time), entries with the same (session, skill)
	// within TriggerPromptDedupeWindow of the GROUP START collapse into one group (same-prompt
	// double-fire). A group's delivery stamp is OR-ed (any mechanism delivering counts — they
	// inject the same pointer); only a true stamp flips state — nil keeps unknown, false
	// leaves the group untouched (no NotDelivered counter; conservative).
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
		if engagedAfter(calls, g.session, g.skill, g.at) {
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

// engagedAfter 报告 (session, skill, 命中时刻) 之后 EngageWindow 内是否出现对该 skill 的
// 遵循信号：Read 其 SKILL.md（canonical 路径或 embed cache 路径，Windows 大小写与分隔符
// 归一后后缀匹配）或 Skill(<skill>) 显式调用。空 session 无法归因（旧条目），返 false。
//
// Read 的 tool_input 截断到 500 字符可能截坏 JSON——解析失败按无信号处理（诚实跳过），
// 与 ExtractSkillName 的失败语义一致。
//
// engagedAfter reports whether an engagement signal for (session, skill) appears within
// EngageWindow after the hit time: a Read of its SKILL.md (canonical path or embed-cache path,
// suffix-matched after Windows case/separator normalization) or an explicit Skill(<skill>)
// call. Empty session cannot be attributed (legacy entries) — returns false.
//
// Read tool_input is truncated to 500 chars, which can corrupt the JSON — parse failure is
// treated as no signal (honest skip), matching ExtractSkillName's failure semantics.
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
		switch c.ToolName {
		case "Skill":
			// EqualFold matches the Read branch's case normalization below — a Skill call
			// differing only in case (Windows paths make this common) must not undercount.
			//
			// EqualFold 与下面 Read 分支的大小写归一对齐——仅大小写差异的 Skill 调用
			// （Windows 路径下常见）不得被漏计。
			if strings.EqualFold(ExtractSkillName(c.ToolInput), skill) {
				return true
			}
		case "Read":
			p := readFilePath(c.ToolInput)
			if p == "" {
				continue
			}
			p = strings.ToLower(strings.ReplaceAll(p, "\\", "/"))
			if strings.HasSuffix(p, "/skills/"+lower+"/skill.md") ||
				strings.HasSuffix(p, "/skills-cache/embedded/"+lower+"/skill.md") {
				return true
			}
		}
	}
	return false
}

// readFilePath 从 Read 工具调用的 tool_input JSON 提取 file_path。与 ExtractSkillName
// 同款失败语义：空串 = 提取不到，调用方跳过。
//
// readFilePath extracts file_path from a Read tool call's tool_input JSON. Same failure
// semantics as ExtractSkillName: empty = unextractable, caller skips.
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
