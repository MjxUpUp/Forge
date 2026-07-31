package cli

import (
	"strings"
	"testing"
)

// TestFilterSkillNames pins the --skill whitelist behavior: known names filter
// (preserving canonical order), and any requested name not in the canonical
// library is an error — a misspelled --skill must not silently filter down to
// an empty set (which would let `audit --gate` pass with 0 skills scanned,
// bypassing the CI security gate with a typo).
//
// TestFilterSkillNames 钉住 --skill 白名单行为：已知名字正常过滤（保持 canonical
// 顺序），任何不在 canonical 库中的请求名直接报错——拼错的 --skill 不得静默
// 过滤成空集（否则 `audit --gate` 扫 0 个 skill 通过，CI 安全门被拼写错误绕过）。
func TestFilterSkillNames(t *testing.T) {
	all := []string{"alpha", "beta", "gamma"}

	got, err := filterSkillNames(all, []string{"gamma", "alpha"})
	if err != nil {
		t.Fatalf("known names: unexpected error: %v", err)
	}
	if len(got) != 2 || got[0] != "alpha" || got[1] != "gamma" {
		t.Fatalf("known names = %v, want [alpha gamma] (canonical order)", got)
	}

	if _, err := filterSkillNames(all, []string{"alpah"}); err == nil {
		t.Fatal("misspelled name: want error, got nil (typo would bypass --gate)")
	} else if !strings.Contains(err.Error(), "alpah") {
		t.Fatalf("error should name the unmatched skill, got: %v", err)
	}

	if _, err := filterSkillNames(all, []string{"alpha", "../../etc"}); err == nil {
		t.Fatal("path-traversal name: want error, got nil")
	}
}
