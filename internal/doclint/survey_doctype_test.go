package doclint

import (
	"strings"
	"testing"
)

// TestSurveyDocType pins the survey type: match surface (filename substring and the docs/surveys/ path fragment), D9 sources (no URL at all; numeric paragraphs and table rows without a citation marker), D10 confidence markers, D11 internal pipeline labels, and that every survey issue stays advisory during the observation window.
//
// TestSurveyDocType 钉住 survey 类型：匹配面（文件名子串与 docs/surveys/ 路径片段）、D9 来源
// （全文无 URL；含百分比/倍数的段落与表格行无引用标记）、D10 信度标注、D11 调研流水线内部标签，
// 以及观察窗口内 survey 的问题全部为 advisory。
func TestSurveyDocType(t *testing.T) {
	good := "# AI 记忆调研\n\n## 结论\n\n两个独立来源一致：auto memory 读取前 200 行。[^1^] ✅ 多源一致\n\n" +
		"| 指标 | 数值 | 来源 |\n|---|---|---|\n| 采用率 | 59% | [^2^] |\n\n" +
		"[^1^]: https://code.claude.com/docs/en/memory — Anthropic, 2026-10\n" +
		"[^2^]: https://example.org/report — Example, 2026-09\n"

	rules := func(issues []Issue, rule string) []Issue {
		var out []Issue
		for _, is := range issues {
			if is.Rule == rule {
				out = append(out, is)
			}
		}
		return out
	}

	t.Run("匹配面", func(t *testing.T) {
		for _, name := range []string{"docs/surveys/2026-10-jev-community-fun-uses.md", "agent-survey.md", "前端框架调研.md", "/abs/repo/docs/surveys/x.md"} {
			if dt := matchDocTypePath(name); dt == nil || dt.ID != "survey" {
				t.Errorf("%s should match survey, got %+v", name, dt)
			}
		}
		for _, name := range []string{"docs/design/memory.md", "skills/research-workflow/SKILL.md", "docs/surveys/2026-10-research-workflow-retrospective.md", "mydocs/surveys/x.md"} {
			if dt := matchDocTypePath(name); dt != nil && dt.ID == "survey" {
				t.Errorf("%s should not match survey (retrospective wins by filename)", name)
			}
		}
	})

	t.Run("合规文档无 survey 发现", func(t *testing.T) {
		for _, is := range LintText("docs/surveys/x.md", good) {
			if is.Rule == "D9" || is.Rule == "D10" || is.Rule == "D11" {
				t.Errorf("compliant survey should not report %s: %s", is.Rule, is.Message)
			}
		}
	})

	t.Run("D9 全文无 URL", func(t *testing.T) {
		text := "# 调研\n\n## 结论\n\n社区普遍认为方案 A 更好。✅\n"
		got := rules(LintText("docs/surveys/x.md", text), "D9")
		if len(got) != 1 || !strings.Contains(got[0].Message, "URL") {
			t.Fatalf("want one D9 about missing URL, got %+v", got)
		}
	})

	t.Run("D9 数字段落与表格行无引用", func(t *testing.T) {
		// 表格紧贴一段带引用、不含数字的说明——表格行若不单独判，会并进这段被它的引用掩盖。
		text := good + "\n有 42％ 的团队已迁移。\n\n已核实的数据 [^1^]：\n| 指标 | 数值 |\n|---|---|\n| 留存 | 71% |\n"
		got := rules(LintText("docs/surveys/x.md", text), "D9")
		if len(got) != 2 {
			t.Fatalf("want 2 D9 (paragraph + table row), got %d: %+v", len(got), got)
		}
		for _, is := range got {
			if is.Line == 0 {
				t.Errorf("D9 must carry a line number: %+v", is)
			}
		}
	})

	t.Run("D10 无信度标注", func(t *testing.T) {
		text := strings.ReplaceAll(good, "✅ 多源一致", "")
		got := rules(LintText("docs/surveys/x.md", text), "D10")
		if len(got) != 1 {
			t.Fatalf("want one D10, got %+v", got)
		}
	})

	t.Run("D11 内部标签逐行报告", func(t *testing.T) {
		text := good + "\n本次按锁定模式完成，详见 dive_06。\n\n工序 4 的分级如下。\n"
		got := rules(LintText("docs/surveys/x.md", text), "D11")
		if len(got) != 2 {
			t.Fatalf("want 2 D11 lines, got %d: %+v", len(got), got)
		}
	})

	t.Run("观察窗口：survey 问题全为 advisory", func(t *testing.T) {
		text := "# 调研\n\n锁定模式下有 42% 的团队迁移。\n"
		for _, is := range LintText("docs/surveys/x.md", text) {
			if (is.Rule == "D9" || is.Rule == "D10" || is.Rule == "D11") && is.Hard() {
				t.Errorf("survey issue must be advisory: %+v", is)
			}
		}
	})

	t.Run("具名脚注算引用、行内代码里的标签豁免、裸 ⚠ 算信度", func(t *testing.T) {
		text := "# 调研\n\n采用率 59%[^survey-a] ⚠ 单源。讨论 `dive_06` 文件名。\n\n[^survey-a]: https://example.org — Example, 2026\n"
		for _, is := range LintText("docs/surveys/x.md", text) {
			if is.Rule == "D9" || is.Rule == "D10" || is.Rule == "D11" {
				t.Errorf("unexpected %s: %s", is.Rule, is.Message)
			}
		}
	})

	t.Run("围栏内的数字与标签不算", func(t *testing.T) {
		text := good + "\n```\n锁定模式 42%\n```\n"
		issues := LintText("docs/surveys/x.md", text)
		if n := len(rules(issues, "D9")) + len(rules(issues, "D11")); n != 0 {
			t.Fatalf("fenced content must be ignored, got %+v", issues)
		}
	})
}
