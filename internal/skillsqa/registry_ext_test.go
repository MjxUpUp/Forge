package skillsqa

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// R13-R17（forge 本地扩展，2026-08 价值审计清单项 11）的表驱动测试，含正反例。
// 规则文本定义见 rules.go RuleDescriptions。
//
// Table-driven tests for R13-R17 (forge-local extensions, 2026-08 value audit
// item 11), positive and negative cases. Rule text definitions: RuleDescriptions
// in rules.go.

func TestAuditSkill_R13_BodyLines(t *testing.T) {
	// fmBlockRe 的 `\n---\s*\n?` 里 `\s*` 会吞掉 --- 后的全部空行，故 fm.Body
	// 直接以 "# 标题" 开头。body = "# 标题\n\n决策树：先做这个。\n" + Repeat("填充行\n", N)
	// 时 bodyLines = N+4：N=496 → 500 行（不过线）、N=497 → 501 行（过线）。边界钉死 > 的严格性。
	//
	// The `\s*` in fmBlockRe's `\n---\s*\n?` swallows all blank lines after ---, so
	// fm.Body starts directly at "# 标题". With body = "# 标题\n\n决策树：先做这个。\n"
	// + Repeat("填充行\n", N), bodyLines = N+4: N=496 → 500 lines (under),
	// N=497 → 501 lines (over). Pins the strictness of >.
	cases := []struct {
		name      string
		fillLines int
		wantIssue bool
	}{
		{"短正文", 10, false},
		{"边界=500行不触发", 496, false},
		{"边界=501行触发", 497, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			body := "# 标题\n\n决策树：先做这个。\n" + strings.Repeat("填充行\n", c.fillLines)
			sd := writeSkill(t, t.TempDir(), "my-skill", makeSkill("my-skill", longDesc(), "pipeline", body))
			r, err := AuditSkill(sd)
			must(t, err)
			has := false
			for _, iss := range r.Issues {
				if strings.Contains(iss, "正文过长") {
					has = true
				}
			}
			if has != c.wantIssue {
				t.Errorf("fillLines=%d: R13 正文过长 issue=%v want=%v (issues: %v)", c.fillLines, has, c.wantIssue, r.Issues)
			}
		})
	}
}

func TestAuditSkill_R14_RequiredFrontmatter(t *testing.T) {
	cases := []struct {
		name       string
		md         string
		wantSubstr string // 空串 = 不应有 R14 issue
	}{
		{
			"齐全省无issue",
			makeSkill("my-skill", longDesc(), "pipeline", signalBody()),
			"",
		},
		{
			"缺name",
			"---\ndescription: \"" + longDesc() + "\"\nmetadata:\n  pattern: pipeline\n---\n\n" + signalBody(),
			"缺 name",
		},
		{
			"缺description",
			"---\nname: my-skill\nmetadata:\n  pattern: pipeline\n---\n\n" + signalBody(),
			"缺 description",
		},
		{
			"无frontmatter块name与description双缺",
			signalBody(),
			"缺 name",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			sd := writeSkill(t, t.TempDir(), "my-skill", c.md)
			r, err := AuditSkill(sd)
			must(t, err)
			has := false
			for _, iss := range r.Issues {
				if strings.Contains(iss, "必填字段") {
					has = true
				}
			}
			if c.wantSubstr == "" && has {
				t.Errorf("齐全 frontmatter 不应有 R14 issue, got: %v", r.Issues)
			}
			if c.wantSubstr != "" {
				expectIssue(t, r, c.wantSubstr)
			}
		})
	}
}

func TestAuditSkill_R15_ImperativeDensity(t *testing.T) {
	cases := []struct {
		name         string
		body         string
		wantAdvisory bool
	}{
		{"无命令式词", signalBody(), false},
		{"边界=5次不触发", signalBody() + "ALWAYS a. NEVER b. MUST c. ALWAYS d. NEVER e.\n", false},
		{"6次触发", signalBody() + "ALWAYS a. NEVER b. MUST c. ALWAYS d. NEVER e. MUST f.\n", true},
		{"小写不计入", signalBody() + strings.Repeat("always never must ", 20), false},
		{"混合三词合计>5触发", signalBody() + "MUST x. MUST y. MUST z. MUST u. MUST v. MUST w.\n", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			sd := writeSkill(t, t.TempDir(), "my-skill", makeSkill("my-skill", longDesc(), "pipeline", c.body))
			r, err := AuditSkill(sd)
			must(t, err)
			if !r.Pass {
				t.Fatalf("R15 是 advisory 不应失败, issues: %v", r.Issues)
			}
			has := advisoryContains(r.Advisories, "命令式全大写词")
			if has != c.wantAdvisory {
				t.Errorf("R15 advisory=%v want=%v (advisories: %v)", has, c.wantAdvisory, r.Advisories)
			}
		})
	}
}

// writeRef 在 skill 目录下建 references/<name> 并写入内容。
//
// writeRef creates references/<name> under the skill dir with the given content.
func writeRef(t *testing.T, sd, name, content string) {
	t.Helper()
	must(t, os.MkdirAll(filepath.Join(sd, "references"), 0755))
	must(t, os.WriteFile(filepath.Join(sd, "references", name), []byte(content), 0644))
}

func TestAuditSkill_R16_OversizedRefs(t *testing.T) {
	cases := []struct {
		name         string
		refName      string
		refContent   string
		wantAdvisory bool
	}{
		{"非markdown超300行无ToC触发", "notes.txt", strings.Repeat("资料行\n", 310), true},
		{"非markdown超300行有ToC不触发", "notes.txt", "## 目录\n\n- [x](#x)\n\n" + strings.Repeat("资料行\n", 310), false},
		{"非markdown短文件不触发", "notes.txt", strings.Repeat("资料行\n", 50), false},
		// markdown >300 行无 ToC：R11（>100 门槛）已报，R16 跳过 markdown 不重复报
		{"markdown超300行R16不报R11覆盖", "long.md", strings.Repeat("参考内容行\n", 310), false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			sd := writeSkill(t, t.TempDir(), "my-skill", makeSkill("my-skill", longDesc(), "pipeline", signalBody()))
			writeRef(t, sd, c.refName, c.refContent)
			r, err := AuditSkill(sd)
			must(t, err)
			if !r.Pass {
				t.Fatalf("R16 是 advisory 不应失败, issues: %v", r.Issues)
			}
			has := advisoryContains(r.Advisories, ">300")
			if has != c.wantAdvisory {
				t.Errorf("R16 advisory=%v want=%v (advisories: %v)", has, c.wantAdvisory, r.Advisories)
			}
		})
	}
}

// writeEvals 在 skill 目录下建 evals/evals.json。
//
// writeEvals creates evals/evals.json under the skill dir.
func writeEvals(t *testing.T, sd, content string) {
	t.Helper()
	must(t, os.MkdirAll(filepath.Join(sd, "evals"), 0755))
	must(t, os.WriteFile(filepath.Join(sd, "evals", "evals.json"), []byte(content), 0644))
}

func TestAuditSkill_R17_EvalsSchema(t *testing.T) {
	cases := []struct {
		name       string
		content    string // 空串 = 不写 evals.json
		wantSubstr string // 空串 = 不应有 R17 advisory
	}{
		{"无evals目录合法", "", ""},
		{"合法schema", `{"trigger_cases":[{"query":"帮我审查代码","should_trigger":true},{"query":"今天天气","should_trigger":false}]}`, ""},
		{"空trigger_cases数组合法", `{"trigger_cases":[]}`, ""},
		{"非法JSON", `not-json`, "不符 schema"},
		{"顶层非对象", `[{"query":"x","should_trigger":true}]`, "不符 schema"},
		{"缺trigger_cases", `{"other":true}`, "缺 trigger_cases"},
		{"trigger_cases非数组", `{"trigger_cases":"foo"}`, "不符 schema"},
		{"元素缺query", `{"trigger_cases":[{"should_trigger":true}]}`, "缺 query"},
		{"元素缺should_trigger", `{"trigger_cases":[{"query":"x"}]}`, "缺 should_trigger"},
		{"query非string", `{"trigger_cases":[{"query":1,"should_trigger":true}]}`, "不符 schema"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			sd := writeSkill(t, t.TempDir(), "my-skill", makeSkill("my-skill", longDesc(), "pipeline", signalBody()))
			if c.content != "" {
				writeEvals(t, sd, c.content)
			}
			r, err := AuditSkill(sd)
			must(t, err)
			if !r.Pass {
				t.Fatalf("R17 是 advisory 不应失败, issues: %v", r.Issues)
			}
			if c.wantSubstr == "" {
				if advisoryContains(r.Advisories, "evals") {
					t.Errorf("不应有 R17 advisory, got: %v", r.Advisories)
				}
			} else {
				expectAdvisory(t, r, c.wantSubstr)
			}
		})
	}
}

// RuleDescriptions 完整性守卫：R1-R17 每条都有定义（G2 文档生成 grep 本表，
// 漏一条文档就缺一条规则）。
//
// RuleDescriptions completeness guard: every rule R1-R17 has a definition
// (the G2 docs generation greps this table — a missing entry means a missing rule).
func TestRuleDescriptions_Complete(t *testing.T) {
	want := []string{"R1", "R2", "R3", "R4", "R5", "R6", "R7", "R8", "R9", "R10",
		"R11", "R12", "R13", "R14", "R15", "R16", "R17"}
	for _, id := range want {
		if RuleDescriptions[id] == "" {
			t.Errorf("RuleDescriptions 缺 %s 定义", id)
		}
	}
}
