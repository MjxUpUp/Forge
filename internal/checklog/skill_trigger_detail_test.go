package checklog

import "testing"

// TestDetailForSkillTrigger_RoundTrip pins the single-source-of-truth contract: every Detail the writer
// produces must round-trip through the reader. This is the whole point of co-locating them — a format change
// to DetailForSkillTrigger automatically updates SkillFromTriggerDetail's inverse, so passive skill-trigger
// signals can never silently vanish from usage analytics the way a hand-mirrored format string on each side
// could (the minor-1 silent-failure risk the code review surfaced).
//
// TestDetailForSkillTrigger_RoundTrip 钉死单一真相源契约：写方产出的每个 Detail 必须经读方往返还原。
// 这正是把两者同位置的全部意义——DetailForSkillTrigger 的格式改动自动更新 SkillFromTriggerDetail 的
// 反转，故被动 skill-trigger 信号绝不可能像两侧手工镜像格式串那样从 usage 分析里静默消失
// （代码审查指出的 minor-1 silent-failure 风险）。
func TestDetailForSkillTrigger_RoundTrip(t *testing.T) {
	cases := []struct{ skill, event, reason string }{
		{"tdd-cycle", "Stop", "test_keyword"},
		{"implementation-discipline", "UserPromptSubmit", "coding_intent"},
		{"a", "b", "c"},
		{"skill-with-dashes", "PreToolUse", "edit_source"},
	}
	for _, c := range cases {
		detail := DetailForSkillTrigger(c.skill, c.event, c.reason)
		got := SkillFromTriggerDetail(detail)
		if got != c.skill {
			t.Fatalf("round-trip DetailForSkillTrigger(%q,%q,%q) = %q, SkillFromTriggerDetail = %q, want %q",
				c.skill, c.event, c.reason, detail, got, c.skill)
		}
	}
}

// TestSkillFromTriggerDetail_RejectsNonContract pins the parser's fail-safe: Details that do not match the
// contract (wrong prefix / no marker / empty) return "", so callers skip them rather than crashing or
// misattributing.
//
// TestSkillFromTriggerDetail_RejectsNonContract 钉死解析器的 fail-safe：不匹配契约的 Detail（前缀错 /
// 无 marker / 空）返回 ""，让调用方跳过而非崩溃或误归。
func TestSkillFromTriggerDetail_RejectsNonContract(t *testing.T) {
	cases := []struct{ in, want string }{
		{"skill-trigger: no-marker-here", ""}, // 无 " hit (" marker
		{"some-other-check detail", ""},       // 无前缀
		{"", ""},
	}
	for _, c := range cases {
		if got := SkillFromTriggerDetail(c.in); got != c.want {
			t.Errorf("SkillFromTriggerDetail(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// TestMetaKeyContract pins the MetaKey* contract: all keys non-empty, mutually distinct,
// and snake_case (stable wire format — writers and readers hand-join on these literals,
// a typo'd key would silently split the payload). Guards the v2 structured-evidence seam
// the same way TestDetailForSkillTrigger_RoundTrip guards the Detail seam.
//
// TestMetaKeyContract 钉死 MetaKey* 契约：全部键非空、互异、snake_case（稳定线格式——
// 写读两侧手工 join 这些字面量，键打错会静默劈裂载荷）。以 TestDetailForSkillTrigger_
// RoundTrip 守 Detail 缝的同样方式守 v2 结构化证据缝。
func TestMetaKeyContract(t *testing.T) {
	keys := map[string]string{
		"matched_keyword":        MetaKeyMatchedKeyword,
		"match_source":           MetaKeyMatchSource,
		"when":                   MetaKeyWhen,
		"trigger_index":          MetaKeyTriggerIndex,
		"trigger_sig":            MetaKeyTriggerSig,
		"prompt_hash":            MetaKeyPromptHash,
		"prompt_len":             MetaKeyPromptLen,
		"excerpt":                MetaKeyExcerpt,
		"suppressed_since_last":  MetaKeySuppressedSinceLast,
		"cause":                  MetaKeyCause,
		"skills":                 MetaKeySkills,
	}
	seen := map[string]bool{}
	for want, got := range keys {
		if got == "" {
			t.Errorf("MetaKey %s 为空串（线格式断裂）", want)
			continue
		}
		if got != want {
			t.Errorf("MetaKey 值漂移: got %q, want %q", got, want)
		}
		if seen[got] {
			t.Errorf("MetaKey 重复: %q", got)
		}
		seen[got] = true
	}
}
