package checklog

import "testing"

// TestSkillFromTriggerDetail_RejectsNonContract pins the parser's fail-safe:
// Details that do not match the contract (wrong prefix / no marker / empty)
// return "", so callers skip them rather than crashing or misattributing.
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

// TestMetaKeyContract pins the MetaKey* contract: all keys non-empty, mutually
// distinct, and snake_case (stable wire format.
//
// TestMetaKeyContract 钉死 MetaKey* 契约：全部键非空、互异、snake_case（稳定线格式——
// 写读两侧手工 join 这些字面量，键打错会静默劈裂载荷）。以 TestDetailForSkillTrigger_
// RoundTrip 守 Detail 缝的同样方式守 v2 结构化证据缝。
func TestMetaKeyContract(t *testing.T) {
	keys := map[string]string{
		"matched_keyword":       MetaKeyMatchedKeyword,
		"match_source":          MetaKeyMatchSource,
		"when":                  MetaKeyWhen,
		"trigger_index":         MetaKeyTriggerIndex,
		"trigger_sig":           MetaKeyTriggerSig,
		"prompt_hash":           MetaKeyPromptHash,
		"prompt_len":            MetaKeyPromptLen,
		"excerpt":               MetaKeyExcerpt,
		"suppressed_since_last": MetaKeySuppressedSinceLast,
		"cause":                 MetaKeyCause,
		"skills":                MetaKeySkills,
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
