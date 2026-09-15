package cli

import (
	"strings"
	"testing"
)

// TestDocsLintHelpCoversAllRules 钉住 CLI 帮助文案与 doclint 规则集不漂移：
// Long 文本的规则枚举须覆盖 D1-D8 全部规则编号，且不得残留过期范围
// （「D1-D7」在 D8 落地后即陈旧——帮助文案是用户可复现门禁的契约面，
// forge docs lint --base 的 BLOCKED 文案承诺由它承载）。
func TestDocsLintHelpCoversAllRules(t *testing.T) {
	long := docsLintCmd.Long
	for _, rule := range []string{"D1", "D2", "D3", "D4", "D5", "D6", "D7", "D8"} {
		if !strings.Contains(long, rule) {
			t.Errorf("docs lint 帮助文案缺规则 %s——新增 lint 规则须同步 CLI 文案", rule)
		}
	}
	if strings.Contains(long, "D1-D7") {
		t.Error("docs lint 帮助文案残留过期规则范围 D1-D7（现为 D1-D8）")
	}
}
