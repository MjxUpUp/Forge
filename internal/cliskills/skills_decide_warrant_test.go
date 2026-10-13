package cliskills

// skills_decide_warrant_test.go — 锚点解析与核验的单元钉：evaluateWarrant 的格式/
// 类型分派、checkDocWarrant 的路径越界与符号链接逃逸、warrantMessage 的文案契约。
// decide 端到端（写入 + checklog 行）见 skills_decide_test.go。

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
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
