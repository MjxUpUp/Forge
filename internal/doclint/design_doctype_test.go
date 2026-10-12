package doclint

import (
	"strings"
	"testing"
)

// TestDesignDocType 钉住 design 类型（design-stage-hardening W2.1）：匹配面、必填章节、
// 同义写法、TL;DR 位置、tier 分档篇幅、观察窗口内全部 advisory。
func TestDesignDocType(t *testing.T) {
	good := "---\ntier: mini\n---\n# 方案 X\n\n## TL;DR\n采用 A。\n\n## Non-goals\n- 不做 B\n\n## 验证与回滚\n`go test ./...`\n"

	t.Run("匹配面：子串与精确文件名", func(t *testing.T) {
		for _, name := range []string{"docs/tech-plan-x.md", "技术方案-登录.md", "specs/feat-x/design.md", "my-design-doc.md"} {
			if dt := matchDocType(name); dt == nil || dt.ID != "design" {
				t.Errorf("%s 应匹配 design 类型, got %+v", name, dt)
			}
		}
		for _, name := range []string{"phase-test-design.md", "system-design.md", "docs/design/multi-task-concurrency.md"} {
			if dt := matchDocType(name); dt != nil && dt.ID == "design" {
				t.Errorf("%s 不应匹配 design 类型", name)
			}
		}
	})

	t.Run("合规文档无类型规则发现", func(t *testing.T) {
		for _, is := range LintText("design.md", good) {
			if strings.HasPrefix(is.Rule, "D5") || is.Rule == "D7" || is.Rule == "D8" {
				t.Errorf("合规文档不应有 %s: %s", is.Rule, is.Message)
			}
		}
	})

	t.Run("缺章节报 D5 且为 advisory（观察窗口）", func(t *testing.T) {
		issues := LintText("design.md", "# 方案\n\n## 背景\n只有背景。\n")
		var d5 int
		for _, is := range issues {
			if is.Rule == "D5" {
				d5++
				if is.Hard() {
					t.Errorf("design 类型的 D5 应为 advisory: %+v", is)
				}
			}
		}
		if d5 != 3 {
			t.Fatalf("应报 3 个缺失章节（TL;DR/Non-goals/验证）, got %d: %+v", d5, issues)
		}
	})

	t.Run("同义写法：不做 / verification 满足章节", func(t *testing.T) {
		text := "# 方案\n## TLDR\nA\n## 刻意不做\n- B\n## Verification\nx\n"
		for _, is := range LintText("tech-plan.md", text) {
			if is.Rule == "D5" {
				t.Errorf("同义写法应满足 D5: %s", is.Message)
			}
		}
	})

	t.Run("TL;DR 位置靠后报 D8，Non-goals 位置不检查", func(t *testing.T) {
		text := "# 方案\n## 背景\na\n## 问题\nb\n## 目标\nc\n## TL;DR\nd\n## Non-goals\ne\n## 验证\nf\n"
		var d8 []string
		for _, is := range LintText("design.md", text) {
			if is.Rule == "D8" {
				d8 = append(d8, is.Message)
			}
		}
		if len(d8) != 1 || !strings.Contains(d8[0], "tl;dr") {
			t.Fatalf("应只对 TL;DR 报 1 条 D8, got %v", d8)
		}
	})

	t.Run("篇幅按 tier 分档", func(t *testing.T) {
		body := strings.Repeat("一行正文\n", 200)
		mini := strings.Replace(good, "采用 A。\n", "采用 A。\n"+body, 1)
		full := strings.Replace(mini, "tier: mini", "tier: full", 1)
		if !hasRule(LintText("design.md", mini), "D7") {
			t.Error("mini 档 200+ 行应超 150 行上限")
		}
		if hasRule(LintText("design.md", full), "D7") {
			t.Error("full 档上限 400 行，200+ 行不应报 D7")
		}
	})

	t.Run("模板文件豁免实例规则", func(t *testing.T) {
		if hasRule(LintText("template-tech-plan.md", "# 模板\n"), "D5") {
			t.Error("template-* 应豁免 D5")
		}
	})
}

func hasRule(issues []Issue, rule string) bool {
	for _, is := range issues {
		if is.Rule == rule {
			return true
		}
	}
	return false
}
