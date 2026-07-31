package protocol

import (
	"fmt"
	"strings"
)

// StandardRenderStyle parameterizes how Standards render into markdown bullet lines.
// The five host renderers (skillgen's quality skill, the cursor/cline/windsurf/copilot
// translators) share one loop — skip disabled, map severity to a label, optionally
// format the enforce-hook note, emit one line — and differ only in the labels and the
// line format. This style struct carries exactly those differences so the loop lives
// in one place.
//
// StandardRenderStyle 参数化 Standards 到 markdown 列表行的渲染。五个 host 渲染器
// （skillgen 的 quality skill、cursor/cline/windsurf/copilot translator）共享同一个
// 循环——跳过 disabled、severity 映射 label、可选格式化 enforce-hook 注记、输出一行——
// 差异只在 label 与行格式。本结构恰好承载这些差异，让循环只存在一份。
type StandardRenderStyle struct {
	// SeverityLabel maps Standard.Severity (error/warning/info) to its display label.
	//
	// SeverityLabel 把 Standard.Severity（error/warning/info）映射为展示 label。
	SeverityLabel func(severity string) string
	// HookInfoFormat formats a non-empty Standard.EnforceHook (exactly one %s verb).
	//
	// HookInfoFormat 格式化非空的 Standard.EnforceHook（恰好一个 %s 动词）。
	HookInfoFormat string
	// LineFormat receives (severityLabel, name, description, hookInfo) in order.
	//
	// LineFormat 按序接收 (severityLabel, name, description, hookInfo)。
	LineFormat string
}

// RenderStandards writes one markdown bullet per enabled standard to sb.
//
// RenderStandards 为每个 enabled standard 向 sb 写一行 markdown 列表项。
func RenderStandards(sb *strings.Builder, standards []Standard, style StandardRenderStyle) {
	for _, s := range standards {
		if !s.Enabled {
			continue
		}
		hookInfo := ""
		if s.EnforceHook != "" {
			hookInfo = fmt.Sprintf(style.HookInfoFormat, s.EnforceHook)
		}
		sb.WriteString(fmt.Sprintf(style.LineFormat, style.SeverityLabel(s.Severity), s.Name, s.Description, hookInfo))
	}
}

// SessionRuleRenderStyle parameterizes how SessionRules render into markdown bullet
// lines — same five-way dedup as StandardRenderStyle.
//
// SessionRuleRenderStyle 参数化 SessionRules 到 markdown 列表行的渲染——与
// StandardRenderStyle 同样的五方去重。
type SessionRuleRenderStyle struct {
	// MandatoryLabel maps SessionRule.Mandatory to its display label.
	//
	// MandatoryLabel 把 SessionRule.Mandatory 映射为展示 label。
	MandatoryLabel func(mandatory bool) string
	// TriggerSuffix maps SessionRule.Trigger to an optional display suffix; nil means
	// the host renders no trigger suffix.
	//
	// TriggerSuffix 把 SessionRule.Trigger 映射为可选展示后缀；nil 表示该 host 不渲染
	// trigger 后缀。
	TriggerSuffix func(trigger string) string
	// LineFormat receives (mandatoryLabel, instruction, triggerSuffix) in order. Hosts
	// that skip the suffix must use explicit argument indexes (e.g. "- %[1]s %[2]s\n") —
	// once a format uses indexes, unreferenced operands are silently dropped.
	//
	// LineFormat 按序接收 (mandatoryLabel, instruction, triggerSuffix)。不用后缀的 host
	// 必须用显式参数索引（如 "- %[1]s %[2]s\n"）——格式串一旦使用索引，未被引用的
	// 操作数会被静默丢弃。
	LineFormat string
}

// RenderSessionRules writes one markdown bullet per session rule to sb.
//
// RenderSessionRules 为每条 session rule 向 sb 写一行 markdown 列表项。
func RenderSessionRules(sb *strings.Builder, rules []SessionRule, style SessionRuleRenderStyle) {
	for _, r := range rules {
		suffix := ""
		if style.TriggerSuffix != nil {
			suffix = style.TriggerSuffix(r.Trigger)
		}
		sb.WriteString(fmt.Sprintf(style.LineFormat, style.MandatoryLabel(r.Mandatory), r.Instruction, suffix))
	}
}

// EmojiSeverityLabel renders severity as an emoji (error 🔴 / warning 🟡 / info 🔵);
// unknown severities default to 🔴. Used by the quality skill and the cursor/cline
// guidance files.
//
// EmojiSeverityLabel 把 severity 渲染为 emoji（error 🔴 / warning 🟡 / info 🔵）；
// 未知 severity 默认 🔴。quality skill 与 cursor/cline guidance 文件使用。
func EmojiSeverityLabel(severity string) string {
	switch severity {
	case "warning":
		return "🟡"
	case "info":
		return "🔵"
	default:
		return "🔴"
	}
}

// WordSeverityLabel renders severity as an upper-case word (ERROR/WARNING/INFO);
// unknown severities default to ERROR. Used by the windsurf/copilot files.
//
// WordSeverityLabel 把 severity 渲染为大写单词（ERROR/WARNING/INFO）；未知 severity
// 默认 ERROR。windsurf/copilot 文件使用。
func WordSeverityLabel(severity string) string {
	switch severity {
	case "warning":
		return "WARNING"
	case "info":
		return "INFO"
	default:
		return "ERROR"
	}
}

// CNMandatoryLabel renders mandatory as 必须/建议 (quality skill session rules).
//
// CNMandatoryLabel 把 mandatory 渲染为 必须/建议（quality skill 会话规则）。
func CNMandatoryLabel(mandatory bool) string {
	if mandatory {
		return "必须"
	}
	return "建议"
}

// CNTriggerSuffix renders a rule trigger as a Chinese parenthetical (on_edit/on_commit);
// other triggers get no suffix. Used by the quality skill session rules.
//
// CNTriggerSuffix 把 rule trigger 渲染为中文括号注记（on_edit/on_commit）；其他
// trigger 无后缀。quality skill 会话规则使用。
func CNTriggerSuffix(trigger string) string {
	switch trigger {
	case "on_edit":
		return "（修改代码时）"
	case "on_commit":
		return "（提交代码时）"
	}
	return ""
}

// MustShouldLabel renders mandatory as [MUST]/[SHOULD] (cursor/cline session rules).
//
// MustShouldLabel 把 mandatory 渲染为 [MUST]/[SHOULD]（cursor/cline 会话规则）。
func MustShouldLabel(mandatory bool) string {
	if mandatory {
		return "[MUST]"
	}
	return "[SHOULD]"
}

// AlwaysPreferLabel renders mandatory as ALWAYS/PREFER (windsurf/copilot session rules).
//
// AlwaysPreferLabel 把 mandatory 渲染为 ALWAYS/PREFER（windsurf/copilot 会话规则）。
func AlwaysPreferLabel(mandatory bool) string {
	if mandatory {
		return "ALWAYS"
	}
	return "PREFER"
}
