package doclint

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ruleIDs extracts the sorted set of rule IDs from issues for terse asserts.
//
// ruleIDs 抽取 issues 的规则编号集合，便于简短断言。
func ruleIDs(issues []Issue) map[string]bool {
	m := map[string]bool{}
	for _, i := range issues {
		m[i.Rule] = true
	}
	return m
}

func TestLintTextBannedPhrases(t *testing.T) {
	issues := LintText("notes.md", "综上所述，本次改动整体符合预期。\n这个方案基本可以接受。\n风险问题不大。\n")
	ids := ruleIDs(issues)
	if !ids["D1"] {
		t.Fatalf("D1 应命中禁令短语（综上所述/基本可以/问题不大），got %v", ids)
	}
	for _, i := range issues {
		if i.Rule == "D1" && i.Severity != Hard {
			t.Errorf("D1 应为 hard，got %s", i.Severity)
		}
		if i.Rule == "D1" && i.Line == 0 {
			t.Errorf("D1 应带行号")
		}
	}
}

func TestLintTextEvidenceFreeConclusion(t *testing.T) {
	issues := LintText("review.md", "审查完成，整体良好。\n看起来没有问题。\n")
	if !ruleIDs(issues)["D2"] {
		t.Fatalf("D2 应命中无证据整体结论，got %v", ruleIDs(issues))
	}
}

func TestLintTextInlineCodeAndFenceExemption(t *testing.T) {
	// Quoting phrases inside backticks (rule tables) or fenced blocks (docs)
	// is data, not usage.
	//
	// 反引号（规则表）或代码块内（文档）引用短语是数据不是使用。
	text := "禁令清单：`综上所述`、`基本可以`。\n\n```text\n综上所述，此为示例。\n```\n正文不含任何命中词。\n"
	ids := ruleIDs(LintText("rules-table.md", text))
	if ids["D1"] || ids["D2"] {
		t.Fatalf("行内代码与围栏内引用应豁免，got %v", ids)
	}
}

func TestLintTextDiffRestatement(t *testing.T) {
	// Raw unified-diff paste outside fences: 3 fingerprint lines → advisory.
	//
	// 围栏外粘贴原始 unified diff：3 行指纹 → advisory。
	raw := "diff --git a/x.go b/x.go\n+++ b/x.go\n@@ -1,3 +1,4 @@\n+new line\n"
	issues := LintText("pr-draft.md", raw)
	if !ruleIDs(issues)["D3"] {
		t.Fatalf("D3 应命中围栏外 diff 复述，got %v", ruleIDs(issues))
	}
	for _, i := range issues {
		if i.Rule == "D3" && i.Hard() {
			t.Errorf("D3 应为 advisory")
		}
	}

	// The same diff inside a fence is deliberate and must not fire.
	//
	// 同一 diff 在围栏内是刻意引用，不应命中。
	fenced := "示例：\n\n```diff\ndiff --git a/x.go b/x.go\n+++ b/x.go\n@@ -1,3 +1,4 @@\n+new line\n```\n"
	if ids := ruleIDs(LintText("pr-draft.md", fenced)); ids["D3"] {
		t.Fatalf("围栏内 diff 应豁免，got %v", ids)
	}
}

func TestLintTextPassClaimNeedsEvidence(t *testing.T) {
	issues := LintText("summary.md", "本功能测试通过。\n")
	if !ruleIDs(issues)["D4"] {
		t.Fatalf("D4 应命中无证据通过断言，got %v", ruleIDs(issues))
	}

	withEvidence := "本功能测试通过：`go test ./...` 全绿，覆盖率 87%。\n"
	if ids := ruleIDs(LintText("summary.md", withEvidence)); ids["D4"] {
		t.Fatalf("带证据标记（反引号/百分比）应豁免 D4，got %v", ids)
	}
}

func TestLintTextDocTypeRules(t *testing.T) {
	// test-report: missing 结论 heading → D5 hard.
	//
	// test-report：缺「结论」章节 → D5 hard。
	issues := LintText("e2e-test-report.md", "## 用例\n全部 30 条。\n")
	ids := ruleIDs(issues)
	if !ids["D5"] {
		t.Fatalf("D5 应命中 test-report 缺必填章节，got %v", ids)
	}

	// With the heading present the D5 issue disappears.
	//
	// 有章节则 D5 消失。
	ok := LintText("e2e-test-report.md", "## 结论\n通过率 100%（`forge verify`）。\n")
	if ruleIDs(ok)["D5"] {
		t.Fatalf("含「结论」章节不应报 D5")
	}

	// checklist: conclusion enum GO/NO-GO required → D6.
	//
	// checklist：须含 GO/NO-GO 枚举 → D6。
	issues = LintText("release-checklist.md", "# 发布清单\n- [x] 全部完成\n")
	if !ruleIDs(issues)["D6"] {
		t.Fatalf("D6 应命中 checklist 缺结论枚举，got %v", ruleIDs(issues))
	}
	withGo := LintText("release-checklist.md", "# 发布清单\n- [x] 全部完成\n\n结论：GO\n")
	if ruleIDs(withGo)["D6"] {
		t.Fatalf("含 GO 枚举不应报 D6")
	}

	// retrospective: 行动 heading + advisory line cap.
	//
	// retrospective：行动章节 + 建议性篇幅上限。
	long := "# 复盘\n## 背景\n" + strings.Repeat("填充行\n", 160)
	issues = LintText("sprint-retrospective.md", long)
	ids = ruleIDs(issues)
	if !ids["D5"] || !ids["D7"] {
		t.Fatalf("retrospective 应命中 D5（缺行动章节）与 D7（超篇幅），got %v", ids)
	}
	for _, i := range issues {
		if i.Rule == "D7" && i.Hard() {
			t.Errorf("D7 应为 advisory")
		}
	}
}

func TestLintFileSkipMarkerAndPathExempt(t *testing.T) {
	dir := t.TempDir()

	// Skip marker in the file head opts the whole file out.
	//
	// 文件头的 skip 标记让整个文件退出。
	marked := filepath.Join(dir, "design.md")
	content := "<!-- forge-doc-lint: skip -->\n综上所述，这是引用禁令清单的设计文档。\n"
	if err := os.WriteFile(marked, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	issues, err := LintFile(marked)
	if err != nil {
		t.Fatal(err)
	}
	if len(issues) != 0 {
		t.Fatalf("skip 标记文件不应产出 issues，got %v", issues)
	}

	// Skip marker beyond the scanned head still counts (head scan window).
	//
	// 扫描窗口内的 skip 标记均生效。
	markedLate := filepath.Join(dir, "design2.md")
	late := "# 标题\n\n<!-- forge-doc-lint: skip -->\n综上所述。\n"
	if err := os.WriteFile(markedLate, []byte(late), 0644); err != nil {
		t.Fatal(err)
	}
	issues, err = LintFile(markedLate)
	if err != nil {
		t.Fatal(err)
	}
	if len(issues) != 0 {
		t.Fatalf("头部窗口内的 skip 标记应生效，got %v", issues)
	}

	if !PathExempt("internal/x/testdata/sample.md") {
		t.Error("testdata/ 应豁免")
	}
	if !PathExempt("docs\\skillhub-archive\\a.md") {
		t.Error("反斜杠路径的归档目录应豁免")
	}
	if PathExempt("docs/design/real.md") {
		t.Error("普通 docs 路径不应豁免")
	}
}

func TestRenderBannedPhrasesForSkill(t *testing.T) {
	out := RenderBannedPhrasesForSkill()
	for _, want := range []string{"综上所述", "基本可以", "问题不大", "整体良好"} {
		if !strings.Contains(out, want) {
			t.Errorf("渲染输出应包含禁令短语 %q", want)
		}
	}
}

func TestLintTextTemplateFilesSkipInstanceRules(t *testing.T) {
	// doc-generator templates define structure (骨架 in fences) — D5 must not
	// demand the instance heading from the template itself, while D1 still
	// applies to bare banned-phrase usage.
	//
	// doc-generator 模板是结构定义（骨架在围栏内）——D5 不得向模板本身索要
	// 实例章节，但 D1 仍约束裸用禁令短语。
	tmpl := "# 测试报告模板\n\n## 章节结构\n\n```\n# 测试报告\n## 结论\n通过率\n```\n"
	if ids := ruleIDs(LintText("template-test-report.md", tmpl)); ids["D5"] {
		t.Fatalf("模板文件应豁免 D5 实例规则，got %v", ids)
	}
	// A filled instance with the same name shape still gets D5.
	//
	// 同名形态的填写实例仍受 D5 约束。
	inst := "# 测试报告\n\n## 用例\n30 条。\n"
	if ids := ruleIDs(LintText("e2e-test-report.md", inst)); !ids["D5"] {
		t.Fatalf("实例文件应命中 D5，got %v", ids)
	}
	// Templates are NOT exempt from universal rules.
	//
	// 模板不豁免通用规则。
	badTmpl := "# 测试报告模板\n综上所述。\n"
	if ids := ruleIDs(LintText("template-test-report.md", badTmpl)); !ids["D1"] {
		t.Fatalf("模板裸用禁令短语应命中 D1，got %v", ids)
	}
}

func TestMatchDocTypeBaseNameOnly(t *testing.T) {
	// Regression: path-based matching made skills/session-retrospective/SKILL.md
	// a "retrospective report" (D5 demanded 行动 from a skill doc living in a
	// directory named retrospective). Type hints match the BASE name only.
	//
	// 回归：按全路径匹配曾把 skills/session-retrospective/SKILL.md 判成
	// 「复盘报告」（D5 向住在 retrospective 目录里的 skill 文档索要「行动」
	// 章节）。类型提示只匹配 BASE 名。
	if dt := matchDocType("skills/session-retrospective/SKILL.md"); dt != nil {
		t.Fatalf("目录名含 retrospective 的 skill 文档不应命中类型规则, got %s", dt.ID)
	}
	if dt := matchDocType("docs/sprint-retrospective.md"); dt == nil || dt.ID != "retrospective" {
		t.Fatalf("文件名命中的复盘报告应匹配, got %+v", dt)
	}
	if !PathExempt("skills/doc-generator/decisions.md") {
		t.Error("decisions.md 是 append-only 治理日志，应豁免（逐字引用诊断散文）")
	}
}

func TestLintTextFenceRunLength(t *testing.T) {
	// A 4-backtick outer fence containing a 3-backtick example must not be
	// closed by the inner marker — the example's prose stays fenced.
	//
	// 4 反引号外栏内嵌 3 反引号示例不得被内层提前闭栏——示例散文保持围栏内。
	text := "正文。\n\n````\n示例：\n```diff\n+++ b/x.go\n@@ -1 +1 @@\n```\n综上所述（示例内，不应命中）。\n````\n"
	ids := ruleIDs(LintText("doc.md", text))
	if ids["D1"] || ids["D3"] {
		t.Fatalf("外层长围栏内的示例不应被 lint，got %v", ids)
	}
}

func TestLintTextD4FencedEvidenceDoesNotAcquit(t *testing.T) {
	// Evidence markers inside fences are examples, not proof of a real run —
	// D4 must look at prose only.
	//
	// 围栏内的证据标记是示例不是实跑证明——D4 只看散文。
	fencedOnly := "本功能测试通过。\n\n```bash\ngo test ./...\n```\n"
	if ids := ruleIDs(LintText("summary.md", fencedOnly)); !ids["D4"] {
		t.Fatalf("仅围栏内证据不应赦免 D4，got %v", ids)
	}
	proseEvidence := "本功能测试通过：`go test ./...` 全绿。\n"
	if ids := ruleIDs(LintText("summary.md", proseEvidence)); ids["D4"] {
		t.Fatalf("散文行内代码证据应赦免 D4，got %v", ids)
	}
}

func TestLintTextD7CountsNonFencedOnly(t *testing.T) {
	// A retrospective with 160 raw lines but most of them fenced output is a
	// structure choice, not prose bloat.
	//
	// 160 原始行但大部分是围栏输出的复盘是结构选择，不是散文膨胀。
	text := "# 复盘\n## 行动项\n- x\n\n```\n" + strings.Repeat("输出行\n", 155) + "```\n"
	if ids := ruleIDs(LintText("sprint-retrospective.md", text)); ids["D7"] {
		t.Fatalf("非围栏行未超限不应报 D7，got %v", ids)
	}
}

func TestStripInlineCodeUnpairedDropsTail(t *testing.T) {
	// Odd backtick count: the trailing (unterminated) segment is dropped —
	// conservative direction is fewer matches, not more.
	//
	// 奇数反引号：尾部未闭合片段丢弃——保守方向是匹配面更小不是更大。
	got := stripInlineCode("代码`未闭合的尾部 综上所述")
	if strings.Contains(got, "综上所述") {
		t.Fatalf("未闭合尾部应被丢弃, got %q", got)
	}
}

func TestPathExemptChangeLogCaseInsensitive(t *testing.T) {
	// Regression C1: the fragment table must be lowercase — PathExempt
	// lowercases the path before Contains, a mixed-case fragment never matched.
	//
	// 回归 C1：片段表必须小写——PathExempt 比较前把路径转小写，
	// 混合大小写片段永不匹配。
	if !PathExempt("CHANGELOG.md") || !PathExempt("docs/Changelog.md") {
		t.Error("CHANGELOG 大小写变体应豁免（生成文件非人写交付物）")
	}
}

func TestChecklistTypeNarrowedToRelease(t *testing.T) {
	// Regression C2: plain *-checklist.md reference files (review-checklist,
	// maintainability-checklist) are working checklists, not release gates —
	// the GO/NO-GO enum belongs to release checklists only.
	//
	// 回归 C2：普通 *-checklist.md 参考清单（review-checklist、
	// maintainability-checklist）是工作清单不是发布门——GO/NO-GO 枚举只属于
	// 发布 checklist。
	if ids := ruleIDs(LintText("review-checklist.md", "- [ ] 检查 X\n")); ids["D6"] {
		t.Fatalf("普通工作清单不应要求 GO/NO-GO，got %v", ids)
	}
	if ids := ruleIDs(LintText("release-checklist.md", "- [ ] 全部完成\n")); !ids["D6"] {
		t.Fatalf("发布清单应要求结论枚举，got %v", ids)
	}
}
