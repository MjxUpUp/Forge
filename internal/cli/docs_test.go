package cli

import (
	"strings"
	"testing"
)

// TestDocsLintHelpCoversAllRules 钉住 CLI 帮助文案与 doclint 规则集不漂移：
// Long 文本的规则枚举须覆盖 D1-D8 全部规则编号（bullet 级断言——「D1-D8」
// 范围词可自满足，删掉单条 bullet 不应仍绿），Short 须含结论位置；且不得
// 残留过期范围（「D1-D7」在 D8 落地后即陈旧——帮助文案是用户可复现门禁
// 的契约面，forge docs lint --base 的 BLOCKED 文案承诺由它承载）。
func TestDocsLintHelpCoversAllRules(t *testing.T) {
	long := docsLintCmd.Long
	for _, rule := range []string{"D1", "D2", "D3", "D4", "D5", "D6", "D7", "D8"} {
		if !strings.Contains(long, rule) {
			t.Errorf("docs lint 帮助文案缺规则 %s——新增 lint 规则须同步 CLI 文案", rule)
		}
	}
	for _, bullet := range []string{"D8 结论位置", "结论枚举"} {
		if !strings.Contains(long, bullet) {
			t.Errorf("docs lint 帮助文案缺 bullet %q（范围词自满足不等于逐条覆盖）", bullet)
		}
	}
	if !strings.Contains(docsLintCmd.Short, "结论位置") {
		t.Error("docs lint Short 缺「结论位置」——与 Long 的规则面漂移")
	}
	if strings.Contains(long, "D1-D7") {
		t.Error("docs lint 帮助文案残留过期规则范围 D1-D7（现为 D1-D8）")
	}
}
