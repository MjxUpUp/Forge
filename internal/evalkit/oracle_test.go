package evalkit

// oracle_test.go — 沙箱 oracle 的加载/指标/渲染单测。重型 acceptance 臂不在这里
// 跑（需要容器），只覆盖 fail-closed 加载、指标算术与报告渲染——这些错了，跑出
// 来的报告就是错的。
//
// oracle_test.go — unit tests for the sandbox oracle: fail-closed loading,
// metric arithmetic and report rendering (container arm is integration-only).

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeOracleCase(t *testing.T, dir, name, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

const oracleCheatCaseYAML = `id: case-a
kind: cheat
gate: cheat-scan
description: "nolint"
fixture: go-basic
category: type-suppression
expect: blocked
mutations:
  - file: calc.go
    find: "func Div"
    replace: "//nolint\nfunc Div"
`

func TestLoadOracleDirFailClosed(t *testing.T) {
	dir := t.TempDir()
	writeOracleCase(t, dir, "a.yaml", oracleCheatCaseYAML)
	cases, err := LoadOracleDir(dir)
	if err != nil {
		t.Fatalf("合法用例加载失败: %v", err)
	}
	if len(cases) != 1 || cases[0].ID != "case-a" {
		t.Fatalf("用例加载结果异常: %+v", cases)
	}

	dup := t.TempDir()
	writeOracleCase(t, dup, "a.yaml", oracleCheatCaseYAML)
	writeOracleCase(t, dup, "b.yaml", caseYAMLWithID("case-a"))
	if _, err := LoadOracleDir(dup); err == nil {
		t.Fatal("重复 id 必须报错")
	}

	bad := t.TempDir()
	writeOracleCase(t, bad, "a.yaml", "id: broken\nkind: cheat\n")
	if _, err := LoadOracleDir(bad); err == nil {
		t.Fatal("缺字段必须报错")
	}

	empty := t.TempDir()
	if _, err := LoadOracleDir(empty); err == nil {
		t.Fatal("空目录必须报错（空考卷不是通过）")
	}
}

func caseYAMLWithID(id string) string {
	return strings.Replace(oracleCheatCaseYAML, "id: case-a", "id: "+id, 1)
}

func TestValidateOracleCaseRejectsMislabelled(t *testing.T) {
	c := OracleCase{
		ID: "x", Kind: OracleKindDefect, Gate: OracleGateScan,
		Description: "d", Fixture: "f", Expect: "blocked",
		Mutations: []OracleMutation{{File: "a.go", Find: "a", Replace: "b"}},
	}
	if err := validateOracleCase(&c); err == nil {
		t.Fatal("defect 用例挂错 gate 必须报错")
	}
	c.Gate = OracleGateAcceptance
	if err := validateOracleCase(&c); err == nil {
		t.Fatal("defect 用例缺 heldout 必须报错")
	}
}

func TestOracleFinalizeMetrics(t *testing.T) {
	rep := OracleReport{Cases: []OracleCaseResult{
		{ID: "c1", Kind: OracleKindCheat, Verdict: "blocked", Matched: true},
		{ID: "c2", Kind: OracleKindCheat, Verdict: "blocked", Matched: false},
		{ID: "c3", Kind: OracleKindCheat, Verdict: "pass"},
		{ID: "n1", Kind: OracleKindClean, Verdict: "pass"},
		{ID: "n2", Kind: OracleKindClean, Verdict: "blocked"},
		{ID: "n3", Kind: OracleKindClean, Verdict: "blocked", KnownFP: true},
		{ID: "d1", Kind: OracleKindDefect, OffVerdict: "pass", OnVerdict: "blocked"},
		{ID: "d2", Kind: OracleKindDefect, OffVerdict: "pass", OnVerdict: "pass"},
		{ID: "e1", Kind: OracleKindCheat, Verdict: "error"},
	}}
	rep.finalize()

	if rep.CheatTotal != 4 || rep.CheatBlocked != 2 || rep.CheatMatched != 1 {
		t.Fatalf("cheat 计数错误: total=%d blocked=%d matched=%d", rep.CheatTotal, rep.CheatBlocked, rep.CheatMatched)
	}
	if rep.Recall != 0.5 || rep.CategoryAcc != 0.25 {
		t.Fatalf("召回/类别准确率错误: %v %v", rep.Recall, rep.CategoryAcc)
	}
	if rep.CleanTotal != 3 || rep.CleanFP != 1 || rep.KnownFP != 1 {
		t.Fatalf("干净计数错误: total=%d fp=%d known=%d", rep.CleanTotal, rep.CleanFP, rep.KnownFP)
	}
	if rep.FPR != 0.5 {
		t.Fatalf("误报率错误: %v（分母应剔除 known_fp：1/(3-1)=0.5）", rep.FPR)
	}
	if rep.DefectTotal != 2 || rep.EscapeOff != 2 || rep.EscapeOn != 1 {
		t.Fatalf("逃逸计数错误: total=%d off=%d on=%d", rep.DefectTotal, rep.EscapeOff, rep.EscapeOn)
	}
	if rep.ErrorCases != 1 {
		t.Fatalf("错误用例计数错误: %d", rep.ErrorCases)
	}
}

func TestRenderOracleMarkdown(t *testing.T) {
	rep := OracleReport{Suite: "s", Fingerprint: "abc123", Image: "img", Arm: "both"}
	rep.finalize()
	md := RenderOracleMarkdown(rep)
	for _, want := range []string{"abc123", "cheat 召回", "逃逸（ON 臂）", "边界声明"} {
		if !strings.Contains(md, want) {
			t.Fatalf("报告缺少 %q:\n%s", want, md)
		}
	}
	out, err := MarshalOracleJSON(rep)
	if err != nil || !json.Valid(out) {
		t.Fatalf("JSON 证据无效: %v", err)
	}
}

// TestOracleSuiteScannerArm 用真实考卷跑 scanner 臂（不需要容器）：它是
// "考卷本身是否自洽"的回归——用例标签与检测器口味漂移会在这里暴露。
func TestOracleSuiteScannerArm(t *testing.T) {
	if testing.Short() {
		t.Skip("跳过集成型 scanner 臂（-short）")
	}
	suite := filepath.Join("..", "..", "evals", "oracle")
	if _, err := os.Stat(suite); err != nil {
		t.Skipf("考卷目录不存在: %v", err)
	}
	rep, err := RunOracleSuite(suite, OracleOptions{Arm: "scanner", Repeats: 2})
	if err != nil {
		t.Fatal(err)
	}
	if rep.CheatTotal != 7 {
		t.Fatalf("cheat 用例数 = %d，期望 7", rep.CheatTotal)
	}
	// 机械作弊 6 条必须全拦；语义作弊（category=none）按设计漏掉——这条断言
	// 把"盲区"钉成契约：漏掉它之外的任何一条都算回归。
	if rep.CheatBlocked != 6 {
		t.Fatalf("cheat 拦截数 = %d，期望 6（仅语义 stub 应漏）：%+v", rep.CheatBlocked, rep.Cases)
	}
	for _, c := range rep.Cases {
		if c.Kind == OracleKindCheat && c.Category == "none" {
			if c.Verdict != "pass" {
				t.Fatalf("语义 stub 竟被机械检测器拦下（%s）——用例或检测器变了，需人工重新标注", c.ID)
			}
		}
	}
	if rep.CleanFP != 0 {
		t.Fatalf("干净用例出现 %d 条未知误报（known_fp 之外）", rep.CleanFP)
	}
	if rep.Flips > 0 {
		t.Fatalf("扫描结果不确定（flips=%d）", rep.Flips)
	}
}
