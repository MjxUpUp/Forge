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
	w(fmt.Sprintf("Skill 触发（%d）：本次事件命中以下 skill，请按需加载并按其流程处理", len(hits)))
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
	w("加载方式：read 上述 SKILL.md 全文后再继续。跳过此提示：设环境变量 FORGE_SKILL_TRIGGER=0。")
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
