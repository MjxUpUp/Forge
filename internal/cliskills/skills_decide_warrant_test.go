package cliskills

// skills_decide_warrant_test.go — 锚点解析与核验的单元钉：evaluateWarrant 的格式/
// 类型分派、checkDocWarrant 的路径越界与符号链接逃逸、warrantMessage 的文案契约。
// decide 端到端（写入 + checklog 行）见 skills_decide_test.go。

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MjxUpUp/Forge/internal/skillseval"
)

// TestEvaluateWarrant_Parsing: empty is missing; malformed, unknown-kind and
// out-of-project values are invalid; surrounding whitespace is trimmed.
//
// TestEvaluateWarrant_Parsing：空值判 missing；格式错误、未知类型、项目外的值判
// invalid；首尾空白被裁掉。
func TestEvaluateWarrant_Parsing(t *testing.T) {
	cases := []struct {
		name, raw, repoRoot, wantVerdict, wantProblem string
	}{
		{"空", "", "/r", warrantMissing, ""},
		{"纯空白", "  \t ", "/r", warrantMissing, ""},
		{"无冒号", "evidence.md", "/r", warrantInvalid, "格式应为"},
		{"空值", "doc:   ", "/r", warrantInvalid, "格式应为"},
		{"多行", "doc:a\nb", "/r", warrantInvalid, "单行"},
		{"未知类型", "jira:X-1", "/r", warrantInvalid, "未知锚点类型"},
		{"类型大小写敏感", "DOC:a.md", "/r", warrantInvalid, "未知锚点类型"},
		{"项目外 commit", "commit:abcd1234", "", warrantInvalid, "不在 forge 项目内"},
		{"项目外 doc", "doc:a.md", "", warrantInvalid, "不在 forge 项目内"},
		{"非十六进制 commit", "commit:HEAD", "/r", warrantInvalid, "十六进制"},
		{"过短 commit", "commit:abc", "/r", warrantInvalid, "十六进制"},
		{"无 eval 目录的 run", "run:r1", "/r", warrantInvalid, "eval 数据目录"},
	}
	for _, c := range cases {
		got := evaluateWarrant(c.raw, "demo", "", c.repoRoot)
		if got.Verdict != c.wantVerdict {
			t.Errorf(`%s：verdict 应为 %q，got %q（%v）`, c.name, c.wantVerdict, got.Verdict, got.Problems)
			continue
		}
		if c.wantProblem != "" && !strings.Contains(strings.Join(got.Problems, "；"), c.wantProblem) {
			t.Errorf(`%s：问题描述应含 %q，got %v`, c.name, c.wantProblem, got.Problems)
		}
		if got.passed() {
			t.Errorf(`%s：%s 不得判为通过`, c.name, got.Verdict)
		}
	}
}

// TestCheckDocWarrant_Containment: relative paths inside the repo pass; absolute
// paths, ".." escapes, missing files and symlinks pointing outside the repo are
// rejected.
//
// TestCheckDocWarrant_Containment：仓库内相对路径通过；绝对路径、".." 越界、不存在的
// 文件、指向仓库外的符号链接一律拒绝。
func TestCheckDocWarrant_Containment(t *testing.T) {
	repo := t.TempDir()
	outside := t.TempDir()
	if err := os.MkdirAll(filepath.Join(repo, "docs"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "docs", "ok.md"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(outside, "secret.md"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	symlinkOK := os.Symlink(filepath.Join(outside, "secret.md"), filepath.Join(repo, "docs", "escape.md")) == nil
	if symlinkOK {
		// 仓库内指向仓库内的链接应放行（只拦逃逸，不拦链接本身）。
		if err := os.Symlink(filepath.Join(repo, "docs", "ok.md"), filepath.Join(repo, "docs", "inner.md")); err != nil {
			t.Fatal(err)
		}
	}

	cases := []struct {
		name, rel   string
		wantProblem string // "" = 应通过
		needSymlink bool
	}{
		{"仓库内文件", "docs/ok.md", "", false},
		{"带 ./ 前缀", "./docs/ok.md", "", false},
		{"内部 .. 仍在仓库内", "docs/../docs/ok.md", "", false},
		{"不存在", "docs/nope.md", "不存在", false},
		{"跳出仓库", "../x.md", "跳出了仓库根", false},
		{"绝对路径", filepath.Join(outside, "secret.md"), "绝对路径", false},
		{"斜杠开头的根路径（Windows 无盘符也须拦）", "/etc/hosts", "绝对路径", false},
		{"反斜杠开头的根路径", `\etc\hosts`, "绝对路径", false},
		{"目录不算文件", "docs", "普通文件", false},
		{"仓库根本身", ".", "普通文件", false},
		{"符号链接逃逸", "docs/escape.md", "经符号链接跳出", true},
		{"仓库内符号链接", "docs/inner.md", "", true},
	}
	for _, c := range cases {
		if c.needSymlink && !symlinkOK {
			t.Logf(`%s：本平台无法创建符号链接，跳过该行`, c.name)
			continue
		}
		got := checkDocWarrant(repo, c.rel)
		switch {
		case c.wantProblem == "" && got != "":
			t.Errorf(`%s：应通过，got %q`, c.name, got)
		case c.wantProblem != "" && !strings.Contains(got, c.wantProblem):
			t.Errorf(`%s：应含 %q，got %q`, c.name, c.wantProblem, got)
		}
	}
}

// TestEvaluateRunWarrant_Strength: a run anchor is strong only after a
// meaningful clean comparison with the skill's baseline; every way the
// comparison can be skipped or be uninformative yields weak with a stated
// reason, never strong.
//
// TestEvaluateRunWarrant_Strength：run 锚点只有与该 skill 的 baseline 做了有效且
// 干净的比对才算强；比对被跳过或无信息的每种情形都判弱并说明原因，绝不判强。
func TestEvaluateRunWarrant_Strength(t *testing.T) {
	res := func(id string, model string, cases map[string]bool) *skillseval.EvalRun {
		r := &skillseval.EvalRun{RunID: id, Skill: "demo", AgentModel: model}
		for _, cid := range []string{"c1", "c2"} {
			if pass, ok := cases[cid]; ok {
				r.Results = append(r.Results, skillseval.CaseResult{CaseID: cid, Kind: skillseval.KindTrigger, Pass: pass})
			}
		}
		return r
	}
	both := map[string]bool{"c1": true, "c2": true}
	type setup struct {
		runs     []*skillseval.EvalRun
		baseline string // "" = 不设 baseline
		corrupt  bool   // baselines.json 写坏
	}
	baseRuns := func(extra ...*skillseval.EvalRun) []*skillseval.EvalRun {
		return append([]*skillseval.EvalRun{res("r-base", "m", both)}, extra...)
	}
	cases := []struct {
		name, anchor, wantVerdict, wantNote string
		s                                   setup
	}{
		{"干净比对", "r-good", warrantStrong, "", setup{runs: baseRuns(res("r-good", "m", both)), baseline: "r-base"}},
		{"有回归", "r-bad", warrantRegression, "", setup{runs: baseRuns(res("r-bad", "m", map[string]bool{"c1": false, "c2": true})), baseline: "r-base"}},
		{"未设 baseline", "r-good", warrantWeak, "未设 baseline", setup{runs: baseRuns(res("r-good", "m", both))}},
		{"锚点就是 baseline", "r-base", warrantWeak, "baseline 本身", setup{runs: baseRuns(), baseline: "r-base"}},
		{"baseline 指向不存在的 run", "r-good", warrantWeak, "无法加载", setup{runs: []*skillseval.EvalRun{res("r-good", "m", both)}, baseline: "r-ghost"}},
		{"baselines.json 损坏", "r-good", warrantWeak, "无法读取", setup{runs: baseRuns(res("r-good", "m", both)), corrupt: true}},
		{"零结果 run", "r-empty", warrantWeak, "没有重叠", setup{runs: baseRuns(res("r-empty", "m", nil)), baseline: "r-base"}},
		{"缺 baseline 的 case", "r-part", warrantWeak, "覆盖不完整", setup{runs: baseRuns(res("r-part", "m", map[string]bool{"c1": true})), baseline: "r-base"}},
		{"不可比（模型变了）", "r-other", warrantWeak, "不可比", setup{runs: baseRuns(res("r-other", "m2", both)), baseline: "r-base"}},
		{"有回归且不可比", "r-bad-other", warrantRegression, "", setup{runs: baseRuns(res("r-bad-other", "m2", map[string]bool{"c1": false, "c2": true})), baseline: "r-base"}},
	}
	for _, c := range cases {
		dir := t.TempDir()
		for _, r := range c.s.runs {
			if err := skillseval.AppendRun(dir, "demo", r); err != nil {
				t.Fatal(err)
			}
		}
		if c.s.baseline != "" {
			if err := skillseval.SetBaseline(dir, "demo", c.s.baseline, "test"); err != nil {
				t.Fatal(err)
			}
		}
		if c.s.corrupt {
			if err := os.WriteFile(filepath.Join(dir, "baselines.json"), []byte("{not json"), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		got := evaluateRunWarrant("demo", c.anchor, dir)
		if got.Verdict != c.wantVerdict {
			t.Errorf(`%s：verdict 应为 %q，got %q（notes=%v problems=%v）`, c.name, c.wantVerdict, got.Verdict, got.Notes, got.Problems)
			continue
		}
		if c.wantNote != "" && !strings.Contains(strings.Join(got.Notes, "；"), c.wantNote) {
			t.Errorf(`%s：弱的原因应含 %q，got %v`, c.name, c.wantNote, got.Notes)
		}
		if got.Verdict == warrantWeak && !strings.Contains(warrantMessage(got), "但") {
			t.Errorf(`%s：弱提示应说明原因：%q`, c.name, warrantMessage(got))
		}
		if c.name == "有回归且不可比" && !strings.Contains(strings.Join(got.Problems, "；"), "不可比") {
			t.Errorf(`%s：回归明细应附不可比注释，got %v`, c.name, got.Problems)
		}
	}
}

// TestWarrantMessage_Contract: every non-passing verdict tells the user the
// decision was still recorded; regression counts only per-case lines.
//
// TestWarrantMessage_Contract：每个未通过结论都告知决策已照常记录；回归条数只数
// 逐 case 行（不计不可比注释）。
func TestWarrantMessage_Contract(t *testing.T) {
	for _, r := range []warrantResult{
		{Verdict: warrantMissing},
		{Kind: "doc", Value: "x", Verdict: warrantInvalid, Problems: []string{"doc x 在仓库中不存在"}},
		{Kind: "run", Value: "r1", Verdict: warrantRegression, Problems: []string{"case c1：…", "case c2：…", "注意：两次 run 不可比（…）"}},
	} {
		msg := warrantMessage(r)
		if !strings.Contains(msg, "决策已照常记录") {
			t.Errorf(`%s 的提示应注明决策已照常记录：%q`, r.Verdict, msg)
		}
	}
	reg := warrantMessage(warrantResult{Kind: "run", Value: "r1", Verdict: warrantRegression, Problems: []string{"case c1：…", "case c2：…", "注意：两次 run 不可比（…）"}})
	if !strings.Contains(reg, "2 条回归") {
		t.Errorf(`回归条数应只数 case 行（2），got %q`, reg)
	}
	if strong := warrantMessage(warrantResult{Kind: "run", Value: "r1", Verdict: warrantStrong}); strings.Contains(strong, "advisory") {
		t.Errorf(`强锚点不应带 advisory 告警尾巴：%q`, strong)
	}
}
