package skillsqa

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Table-driven tests for R13-R17 (forge-local extensions, 2026-08 value audit item 11), positive and negative cases.
//
// R13-R17（forge 本地扩展，2026-08 价值审计清单项 11）的表驱动测试，含正反例。
// 规则文本定义见 rules.go RuleDescriptions。

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

// TestAuditSkill_R18_ForgeRefs — R18 零反向依赖契约（硬）的正文用例。2026-08 收紧
// 前条件块是合法形态，收紧后任何位置的操作性引用（含「> Forge 项目」条件块内）
// 一律触发——条件块形态废止，集成知识归 forge 侧。
//
// TestAuditSkill_R18_ForgeRefs — SKILL.md body cases for the R18
// zero-reverse-dependency contract (hard). Before the 2026-08 tightening
// conditional blocks were a sanctioned form; after it, operational references
// anywhere (including inside "> Forge 项目" blocks) trigger — the conditional
// form is retired and integration knowledge lives on the forge side.
func TestAuditSkill_R18_ForgeRefs(t *testing.T) {
	cases := []struct {
		name      string
		body      string
		wantIssue bool
	}{
		{"无引用", signalBody(), false},
		{"正文CLI引用触发", signalBody() + "完成后须 `forge review pass` 盖章。\n", true},
		{"条件块内引用同样触发（条件块形态已废止）", signalBody() + "> Forge 项目：先跑 `forge task resume` 拉回上下文，细节见集成文件。非 forge 项目跳过。\n", true},
		{"加粗变体条件块同样触发", signalBody() + "> **Forge 项目**：`forge task start --ref x` 跟踪。\n", true},
		{"条件块结束后正文引用触发", signalBody() + "> Forge 项目：历史形态示例。非 forge 项目跳过。\n跑 `forge docs lint` 后再评分。\n", true},
		{"用户级路径触发", signalBody() + "历史存储在 `~/.forge/doc-generator/history.jsonl`。\n", true},
		{"HOME变量路径触发", signalBody() + "STATS=\"${STATS_DIR:-$HOME/.forge/web-search-stats}\"\n", true},
		{"FORGE环境变量触发", signalBody() + "根目录取 `$FORGE_SKILLS_CANONICAL`。\n", true},
		{"forge-integration指针触发", signalBody() + "细节见 references/forge-integration.md。\n", true},
		{"forge后接非子命令不触发", signalBody() + "在 forge 项目与非 forge 项目中行为一致（forge 环境自动接线）。\n", false},
		{"行尾裸forge下行小写开头不跨行触发", signalBody() + "compatible with forge\nand other tools\n", false},
		{"CRLF跨行不触发", signalBody() + "runs in forge\r\nretry mode\n", false},
		{"项目级裸.forge路径不触发（反向列举非依赖）", signalBody() + "禁止混入 `.forge/` `.claude/` 等工具工作目录。\n", false},
		{"Forge仓库名案例叙述不触发", signalBody() + "Forge 仓库曾发生 docs 漂移事故，教训是单一真相源。\n", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			sd := writeSkill(t, t.TempDir(), "my-skill", makeSkill("my-skill", longDesc(), "pipeline", c.body))
			r, err := AuditSkill(sd)
			must(t, err)
			has := false
			for _, iss := range r.Issues {
				if strings.Contains(iss, "反向依赖") {
					has = true
				}
			}
			if has != c.wantIssue {
				t.Errorf("R18 issue=%v want=%v (issues: %v)", has, c.wantIssue, r.Issues)
			}
			if c.wantIssue && r.Pass {
				t.Fatalf("R18 是硬校验，命中应 Pass=false, issues: %v", r.Issues)
			}
		})
	}
}

// TestAuditSkill_R18_FileScope pins that R18's scan scope covers every content file in the skill dir.
//
// TestAuditSkill_R18_FileScope — R18 扫描面覆盖 skill 目录全部内容文件：
// references/ 里的 CLI 引用（research-workflow/frontend-feature-development
// 违例的实态——旧版只扫 SKILL.md 正文看不见它们）触发；decisions.md
// （append-only 决策日志）与 evals/（测试数据）豁免。
func TestAuditSkill_R18_FileScope(t *testing.T) {
	t.Run("references内CLI引用触发", func(t *testing.T) {
		sd := writeSkill(t, t.TempDir(), "my-skill", makeSkill("my-skill", longDesc(), "pipeline", signalBody()))
		writeRef(t, sd, "checklist.md", "改前/后运行 `forge review pass` 对比。\n")
		r, err := AuditSkill(sd)
		must(t, err)
		if r.Pass {
			t.Fatalf("references/ 内 forge CLI 引用应触发 R18, issues: %v", r.Issues)
		}
	})
	t.Run("references内路径引用触发", func(t *testing.T) {
		sd := writeSkill(t, t.TempDir(), "my-skill", makeSkill("my-skill", longDesc(), "pipeline", signalBody()))
		writeRef(t, sd, "engine.md", "mkdir -p ~/.forge/research/topic-20260828\n")
		r, err := AuditSkill(sd)
		must(t, err)
		if r.Pass {
			t.Fatalf("references/ 内 ~/.forge 路径应触发 R18, issues: %v", r.Issues)
		}
	})
	t.Run("decisionsmd豁免", func(t *testing.T) {
		sd := writeSkill(t, t.TempDir(), "my-skill", makeSkill("my-skill", longDesc(), "pipeline", signalBody()))
		must(t, os.WriteFile(filepath.Join(sd, "decisions.md"), []byte("- 2026-01-01 历史决策：曾用 `forge task start` 跟踪，路径 ~/.forge/x\n"), 0644))
		r, err := AuditSkill(sd)
		must(t, err)
		if !r.Pass {
			t.Fatalf("decisions.md 是决策日志不应触发 R18, issues: %v", r.Issues)
		}
	})
	t.Run("evals豁免", func(t *testing.T) {
		sd := writeSkill(t, t.TempDir(), "my-skill", makeSkill("my-skill", longDesc(), "pipeline", signalBody()))
		writeEvals(t, sd, `{"trigger_cases":[{"query":"forge task 怎么用","should_trigger":false}]}`)
		r, err := AuditSkill(sd)
		must(t, err)
		if !r.Pass {
			t.Fatalf("evals/ 测试数据不应触发 R18, issues: %v", r.Issues)
		}
	})
	t.Run("scripts内路径引用触发", func(t *testing.T) {
		sd := writeSkill(t, t.TempDir(), "my-skill", makeSkill("my-skill", longDesc(), "pipeline", signalBody()))
		must(t, os.MkdirAll(filepath.Join(sd, "scripts"), 0755))
		must(t, os.WriteFile(filepath.Join(sd, "scripts", "quota.sh"), []byte("STATS_DIR=\"${OVR:-$HOME/.forge/web-search-stats}\"\n"), 0644))
		r, err := AuditSkill(sd)
		must(t, err)
		if r.Pass {
			t.Fatalf("scripts/ 内 $HOME/.forge 路径应触发 R18, issues: %v", r.Issues)
		}
	})
}

// TestAuditSkill_R18_GrandfatheredAdvisory — legacy exemption path: a R18Grandfathered skill's hits downgrade to an advisory (Pass unaffected).
//
// TestAuditSkill_R18_GrandfatheredAdvisory — 存量豁免路径：R18Grandfathered
// 表内 skill 命中时降为 advisory（不阻断 Pass）。2026-08 迁移完成后生产表已清空，
// 测试临时注入一条豁免再 defer 清除——机制仍在（未来过渡债务的通道），测试钉住
// 其行为不回归。
func TestAuditSkill_R18_GrandfatheredAdvisory(t *testing.T) {
	R18Grandfathered["code-review-gate"] = true
	defer delete(R18Grandfathered, "code-review-gate")
	body := signalBody() + "完成后 `forge task complete` 收尾。\n"
	sd := writeSkill(t, t.TempDir(), "code-review-gate", makeSkill("code-review-gate", longDesc(), "gate", body))
	r, err := AuditSkill(sd)
	must(t, err)
	if !r.Pass {
		t.Fatalf("豁免 skill 的命中不应阻断 Pass, issues: %v", r.Issues)
	}
	if !advisoryContains(r.Advisories, "存量 forge 反向依赖豁免中") {
		t.Fatalf("豁免 skill 命中应报存量 advisory, advisories: %v", r.Advisories)
	}
}

// TestAuditSkill_R18_RequiresForgeExempt guards the production-in-use exemption branch.
//
// TestAuditSkill_R18_RequiresForgeExempt 守护生产在用的豁免分支：标记
// `metadata.requires_forge: "true"` 的 forge 原生 skill（skill-evolution /
// skill-routing / skill-authoring-standard）整体跳过 R18。此处回归（如条件写反）
// 会让这三个 skill 直接 fail，而其余测试照常全绿——零保护即零感知。
func TestAuditSkill_R18_RequiresForgeExempt(t *testing.T) {
	body := signalBody() + "完成后 `forge task complete` 收尾、`forge review pass` 盖章。\n"
	raw := "---\nname: my-skill\ndescription: \"" + longDesc() + "\"\n" +
		"metadata:\n  pattern: pipeline\n  requires_forge: \"true\"\n---\n\n" + body
	sd := writeSkill(t, t.TempDir(), "my-skill", raw)
	r, err := AuditSkill(sd)
	must(t, err)
	if !r.Pass {
		t.Fatalf("requires_forge 豁免的 skill 不应失败, issues: %v", r.Issues)
	}
	if advisoryContains(r.Advisories, "反向依赖") {
		t.Fatalf("requires_forge 标记的 forge 原生 skill 应豁免 R18, got: %v", r.Advisories)
	}
}

// RuleDescriptions completeness guard: every rule R1-R18 has a definition (the G2 docs generation greps this table — a missing entry means a missing rule).
//
// RuleDescriptions 完整性守卫：R1-R18 每条都有定义（G2 文档生成 grep 本表，
// 漏一条文档就缺一条规则）。
func TestRuleDescriptions_Complete(t *testing.T) {
	want := []string{"R1", "R2", "R3", "R4", "R5", "R6", "R7", "R8", "R9", "R10",
		"R11", "R12", "R13", "R14", "R15", "R16", "R17", "R18"}
	for _, id := range want {
		if RuleDescriptions[id] == "" {
			t.Errorf("RuleDescriptions 缺 %s 定义", id)
		}
	}
}
