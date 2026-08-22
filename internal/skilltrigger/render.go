package skilltrigger

import (
	"fmt"
	"path/filepath"
	"strings"
)

// Render 把 hits 渲染成 additionalContext 文本（HANDOFF 风格，参考 renderResume）。
// 无命中返 ""。输出不含 ASCII 双引号字面量（用中文标点，避免终端/引号腐蚀），
// reason 经 stripControl 压成单行 + truncateRunes(200) 兜底，整体再由 runHook 的
// truncate(_, 9500) 二次截断。
func Render(hits []Hit, ctx Context) string {
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
		w(fmt.Sprintf("  【%s】  事件 %s · %s", h.Skill, ctx.Event, cond))
		reason := reasonOneLine(h.Reason)
		if reason == "" {
			reason = "—"
		}
		w("    " + truncateRunes(reason, 200))
		w("    路径：" + filepath.ToSlash(filepath.Join(h.SkillDir, "SKILL.md")))
		w("")
	}
	w(bar)
	w("说明：以上 SKILL.md 全文可 read。此提示由 forge 生成，FORGE_SKILL_TRIGGER=0 可关闭。")
	return b.String()
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

// truncateRunes 按 rune 计数截断（避免切坏 UTF-8 中文），超出加省略号。
func truncateRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "..."
}
