package skillseval

import (
	"strings"
	"testing"
	"unicode/utf8"
)

// TestBlindPreamble_ListingContract pins the blind preamble contract: every canonical
// skill appears as a "- name: desc" line, descriptions are single-lined and truncated
// past 200 runes, and the routing question asks "which one" (not "does X trigger").
//
// TestBlindPreamble_ListingContract 钉住盲测前置契约：每个 canonical skill 都有一行
// "- name: desc"，description 单行化且超 200 rune 截断，路由问题问「该触发哪个」
// （而非「是否触发 X」）。
func TestBlindPreamble_ListingContract(t *testing.T) {
	canonical := t.TempDir()
	longDesc := "Use when: " + strings.Repeat("长", 300)
	writeSkill(t, canonical, "alpha-skill", "Use when: 短描述")
	writeSkill(t, canonical, "beta-skill", longDesc)
	// Exactly 200 runes: must NOT be truncated (no ellipsis) — pins the > boundary.
	//
	// 恰好 200 rune：不得截断（无省略号）——钉住 > 边界。
	exactDesc := strings.Repeat("恰", blindDescRunes)
	writeSkill(t, canonical, "gamma-skill", exactDesc)

	pre, err := BlindPreamble(canonical)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(pre, "你该加载哪个 skill？") {
		t.Fatal("前置须含「该加载哪个」路由问题")
	}
	if !strings.Contains(pre, "回答 none") {
		t.Fatal("前置须含 none 逃生语义")
	}
	if !strings.Contains(pre, "- alpha-skill: ") || !strings.Contains(pre, "- beta-skill: ") {
		t.Fatal("全库 skill 都须入清单")
	}
	for _, line := range strings.Split(pre, "\n") {
		if !strings.HasPrefix(line, "- ") {
			continue
		}
		desc := strings.SplitN(line, ": ", 2)[1]
		// 200 rune cap + "..." suffix (3 runes).
		if utf8.RuneCountInString(desc) > blindDescRunes+3 {
			t.Fatalf("清单行超截断上限（%d rune）: %s...", utf8.RuneCountInString(desc), firstNRunes(desc, 40))
		}
	}
	if !strings.Contains(pre, strings.Repeat("长", blindDescRunes-len("Use when: "))+"...") {
		t.Fatal("超 200 rune 的 description 须截断并带省略号")
	}
	if !strings.Contains(pre, "- gamma-skill: "+exactDesc+"\n") {
		t.Fatal("恰好 200 rune 的 description 不得截断（不应带省略号）")
	}
}

// TestBlindPrompt_SelfContained pins that each blind case prompt carries the preamble +
// the case prompt (fresh subagents see nothing else).
//
// TestBlindPrompt_SelfContained 钉住每条盲测 case prompt 自包含（前置 + case prompt），
// fresh subagent 除此之外什么都看不到。
func TestBlindPrompt_SelfContained(t *testing.T) {
	got := BlindPrompt("PREAMBLE", "用户说了什么")
	if !strings.HasPrefix(got, "PREAMBLE") || !strings.Contains(got, "## 用户 Prompt\n用户说了什么") {
		t.Fatalf("BlindPrompt 结构错误: %q", got)
	}
}
