package skilltrigger

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/MjxUpUp/Forge/internal/skillintegrate"
	"github.com/MjxUpUp/Forge/internal/util"
)

// Render renders hits into additionalContext text (HANDOFF style, cf. renderResume).
//
// Render 把 hits 渲染成 additionalContext 文本（HANDOFF 风格，参考 renderResume）。
// 无命中返 ""。输出不含 ASCII 双引号字面量（用中文标点，避免终端/引号腐蚀），
// reason 经 stripControl 压成单行 + util.TruncateRunes(200) 兜底，整体再由 runHook 的
// truncate(_, 9500) 二次截断。
//
// overflow 是因 MaxHitsPerEvent 单次上限落选、未注入的 skill 名（可为 nil）——尾部一句
// 带过，让 agent 知道还有命中存在而不付全量 context 成本。
func Render(hits []Hit, ctx Context, overflow []string) string {
	if len(hits) == 0 {
		return ""
	}
	var b strings.Builder
	w := func(s string) { b.WriteString(s + "\n") }
	bar := strings.Repeat("─", 60)
	w(bar)
	// #5 (2026-08-22 adherence audit): factual statement, not imperative. The official
	// additionalContext guidance says context works best as facts the model can use;
	// imperative phrasing（请加载/必须处理）reads as an injected instruction and can trip
	// prompt-injection defenses — and the audit showed imperatives did not lift conversion
	// anyway (kimi 0, claude 22%). Same change on the loading-method footer.
	//
	// #5（2026-08-22 遵循度审计）：事实陈述，非祈使。官方 additionalContext 指南指出
	// 上下文以「模型可用的事实」形态最有效；祈使措辞（请加载/必须处理）会被读成
	// 注入指令、可能触发 prompt-injection 防御——且审计显示祈使并未抬高转化率
	// （kimi 0、claude 22%）。加载方式脚注同步改。
	w(fmt.Sprintf("Skill 触发（%d）：本次事件与以下 skill 的触发条件匹配，供参考", len(hits)))
	w(bar)
	for _, h := range hits {
		cond := h.Trigger.When
		if cond == "" {
			cond = "keywords"
		}
		if h.Reminder {
			// 重复注入（session 预算内第 2 次）：agent 上下文中已有完整指引（2026-08 wire
			// 证据：重复注入从不被重读），压成一行短提醒 + 路径，不再重复 reason 全文。
			w(fmt.Sprintf("  【%s】  事件 %s · %s（本 session 已注入过，短提醒）%s", h.Skill, ctx.Event, cond, matchEvidenceSuffix(h)))
			w("    路径：" + filepath.ToSlash(filepath.Join(h.SkillDir, "SKILL.md")))
			w("")
			continue
		}
		w(fmt.Sprintf("  【%s】  事件 %s · %s%s", h.Skill, ctx.Event, cond, matchEvidenceSuffix(h)))
		reason := reasonOneLine(h.Reason)
		if reason == "" {
			reason = "—"
		}
		w("    " + util.TruncateRunes(reason, 200))
		w("    路径：" + filepath.ToSlash(filepath.Join(h.SkillDir, "SKILL.md")))
		// forge 集成指针：该 skill 有 forge 侧集成笔记时追加一行（零反向依赖契约的
		// 承接面——skill 正文不含 forge 内容，forge 用户经 forge skills integration 拿
		// 集成知识）。非 forge 环境此行也只是不可达的指引，无害。
		if p := skillintegrate.PointerLine(h.Skill); p != "" {
			w(p)
		}
		w("")
	}
	if len(overflow) > 0 {
		w(fmt.Sprintf("  另有 %d 个 skill 命中未注入（单次上限 %d）：%s", len(overflow), MaxHitsPerEvent, strings.Join(overflow, ", ")))
		w("")
	}
	w(bar)
	w("说明：以上为命中 skill 的绝对路径，读取对应 SKILL.md 可获得完整方法论。此提示由 forge 生成，FORGE_SKILL_TRIGGER=0 可关闭。")
	return b.String()
}

// matchEvidenceSuffix 把命中证据（哪个词、来自哪段输入）渲染成行内后缀——模板化文案
// 不含命中词是 2026-08 噪音审计的核心抱怨（agent 不知道为何切题）。condition-only
// 命中（无关键词）返 ""。
func matchEvidenceSuffix(h Hit) string {
	if h.MatchedKeyword == "" {
		return ""
	}
	return fmt.Sprintf("；命中关键词「%s」（来自%s）", sanitizeEvidence(h.MatchedKeyword), matchSourceLabel(h.MatchSource))
}

// sanitizeEvidence 清理要渲染进文案的命中关键词：reasonOneLine 剥控制字符之外，再把
// ASCII 双引号剥掉——关键词来自 skill 作者声明（SKILL.md triggers），含 " 会直接打破
// 「render 输出不含 ASCII 双引号」的契约（防终端/引号腐蚀），这里把暴露面收掉。
func sanitizeEvidence(s string) string {
	s = reasonOneLine(s)
	return strings.ReplaceAll(s, `"`, "")
}

// matchSourceLabel 把 MatchSource* 常量译成人类可读的来源名。
func matchSourceLabel(source string) string {
	switch source {
	case MatchSourcePrompt:
		return "你的输入"
	case MatchSourceCommand:
		return "命令"
	case MatchSourceStdout:
		return "标准输出"
	case MatchSourceStderr:
		return "标准错误"
	case MatchSourceOutput:
		return "工具输出"
	default:
		return "事件上下文"
	}
}

// reasonOneLine 去所有 C0 控制字符与 DEL（含换行），把 reason 压成单行，防多行破坏渲染格式。
func reasonOneLine(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r < 0x20 || r == 0x7f {
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}
