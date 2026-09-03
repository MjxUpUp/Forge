package evalkit

// golden/traps_test.go — 用假 forge 二进制（shell 脚本）驱动重放机器，离线验证
// 检测信号语义、precision/fpr 聚合与陷阱 capture 语义（含 pristine 的语义反转）。

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeFakeForge(t *testing.T, dir, body string) string {
	t.Helper()
	return writeFakeForgeNamed(t, dir, "fakeforge", body)
}

// writeFakeForgeNamed 每个假二进制独立文件名——同名复写会让先前句柄指向新内容
// （曾致"误触应记 fpr"用例读到被覆写后的 exit 0 脚本）。
func writeFakeForgeNamed(t *testing.T, dir, name, body string) string {
	t.Helper()
	p := filepath.Join(dir, name+".sh")
	if err := os.WriteFile(p, []byte("#!/bin/sh\n"+body+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

func goldenDefectiveCase() GoldenCase {
	return GoldenCase{
		ID: "d1", Gate: "g", Kind: GoldenDefective, Expect: "flagged",
		Description: "defective", ProbeArgv: []string{"{forge}"},
		DetectAny: []string{"exit_nonzero"}, Deterministic: true,
	}
}

func goldenCleanCase() GoldenCase {
	c := goldenDefectiveCase()
	c.ID = "c1"
	c.Kind = GoldenClean
	c.Expect = "clean"
	return c
}

func TestRunGoldenDetection(t *testing.T) {
	dir := t.TempDir()
	// 缺陷样本：假 forge 退出 2（模拟 BLOCKED）→ captured。
	failBin := writeFakeForge(t, dir, "exit 2")
	rep, err := RunGolden(dir, []GoldenCase{goldenDefectiveCase()}, GoldenOptions{ForgeBin: failBin, Repetitions: 2})
	if err != nil {
		t.Fatal(err)
	}
	if rep.Cases[0].Outcome != OutcomeCaptured || rep.Precision.Value != 1 {
		t.Fatalf("缺陷应被捕获: %+v", rep.Cases[0])
	}
	if rep.Cases[0].Agreement != 1 {
		t.Fatalf("确定性重放应一致: %+v", rep.Cases[0])
	}
	// 干净样本：假 forge 退出 0 无输出 → clean；误触脚本 → false positive。
	okBin := writeFakeForgeNamed(t, dir, "fakeforge-ok", "exit 0")
	rep2, err := RunGolden(dir, []GoldenCase{goldenCleanCase()}, GoldenOptions{ForgeBin: okBin, Repetitions: 1})
	if err != nil {
		t.Fatal(err)
	}
	if rep2.Cases[0].Outcome != OutcomeCaptured || rep2.FalsePositive.Value != 0 {
		t.Fatalf("干净样本应放行: %+v", rep2.Cases[0])
	}
	flagBin := writeFakeForgeNamed(t, dir, "fakeforge-flag", "exit 2")
	rep3, err := RunGolden(dir, []GoldenCase{goldenCleanCase()}, GoldenOptions{ForgeBin: flagBin, Repetitions: 1})
	if err != nil {
		t.Fatal(err)
	}
	if rep3.Cases[0].Outcome != OutcomeFalsePositive || rep3.FalsePositive.Value != 1 {
		t.Fatalf("干净样本误触应记 fpr: %+v", rep3.Cases[0])
	}
	// 缺陷样本遇到"不拦截"的 forge → missed。
	rep4, err := RunGolden(dir, []GoldenCase{goldenDefectiveCase()}, GoldenOptions{ForgeBin: okBin, Repetitions: 1})
	if err != nil {
		t.Fatal(err)
	}
	if rep4.Cases[0].Outcome != OutcomeMissed || rep4.Precision.Value != 0 {
		t.Fatalf("缺陷漏拦应记 missed: %+v", rep4.Cases[0])
	}
}

func TestRunGoldenSetupError(t *testing.T) {
	rep, err := RunGolden(t.TempDir(), []GoldenCase{goldenDefectiveCase()}, GoldenOptions{ForgeBin: "/nonexistent-binary-evalkit", Repetitions: 1})
	if err != nil {
		t.Fatal(err)
	}
	if rep.Cases[0].Outcome != OutcomeSetupError {
		t.Fatalf("不可执行二进制应记 setup_error: %+v", rep.Cases[0])
	}
}

func TestGoldenLoaderFailClosed(t *testing.T) {
	dir := t.TempDir()
	bad := "id: x\ngate: g\nkind: weird\nexpect: flagged\ndescription: d\nprobe_argv: [a]\ndetect_any: [exit_nonzero]\n"
	if err := os.WriteFile(filepath.Join(dir, "bad.yaml"), []byte(bad), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadGoldenDir(dir); err == nil || !strings.Contains(err.Error(), "kind") {
		t.Fatalf("非法 kind 应 fail-closed: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "dup.yaml"), []byte(bad), 0o644); err == nil {
		_ = err
	}
	_ = os.WriteFile(filepath.Join(dir, "dup.yaml"), []byte(bad), 0o644)
	if _, err := LoadGoldenDir(dir); err == nil {
		t.Fatal("目录仍含坏用例应报错")
	}
	empty := t.TempDir()
	if _, err := LoadGoldenDir(empty); err == nil {
		t.Fatal("空目录应报错")
	}
}

func TestGoldenFingerprintStability(t *testing.T) {
	c := goldenDefectiveCase()
	a := GoldenFingerprint([]GoldenCase{c})
	b := GoldenFingerprint([]GoldenCase{c})
	if a != b {
		t.Fatal("同内容指纹应稳定")
	}
	c.Description = "changed"
	if GoldenFingerprint([]GoldenCase{c}) == a {
		t.Fatal("内容变更指纹应变化")
	}
}

func TestRunTrapsCaptureSemantics(t *testing.T) {
	dir := t.TempDir()
	// 假二进制按调用分流：data-dir 返回 fixture 路径（cwd），探测输出走 TRAP_OUT。
	silentBin := writeFakeForge(t, dir, `if [ "$1" = "data-dir" ]; then pwd; else echo "${TRAP_OUT:-}"; fi; exit 0`)
	t.Setenv("TRAP_OUT", "")
	flaggingBin := writeFakeForge(t, dir, `if [ "$1" = "data-dir" ]; then pwd; else echo "${TRAP_OUT:-}"; fi; exit 0`)
	// pristine 陷阱：无触发 = 识破。
	pristine := TrapCase{
		ID: "t1", Type: TrapPristineFalsePositive, Description: "d",
		ProbeArgv: []string{"{forge}"}, DetectAny: []string{"exit_nonzero"},
	}
	rep, err := RunTraps([]TrapCase{pristine}, GoldenOptions{ForgeBin: silentBin})
	if err != nil {
		t.Fatal(err)
	}
	if !rep.Traps[0].Captured || rep.CaptureRate.Value != 1 {
		t.Fatalf("pristine 无触发应记识破: %+v", rep.Traps[0])
	}
	// forged 陷阱：探测无信号 = 未识破（v1 无戳校验的现状）。
	forge := TrapCase{
		ID: "t2", Type: TrapEvidenceForgery, Description: "d",
		ForgedRow: &ForgedChecklogRow{Check: "task-verify", Detail: "fake", Sig: "bad"},
		ProbeArgv: []string{"{forge}"}, DetectAny: []string{"audit_row:forged"},
	}
	t.Setenv("TRAP_OUT", "")
	rep2, err := RunTraps([]TrapCase{forge}, GoldenOptions{ForgeBin: silentBin})
	if err != nil {
		t.Fatal(err)
	}
	if rep2.Traps[0].Captured {
		t.Fatalf("v1 无戳校验，伪造不应被识破: %+v", rep2.Traps[0])
	}
	// 信号命中路径：探测输出包含标记串 → 识破。
	t.Setenv("TRAP_OUT", "audit_row:forged")
	rep3, err := RunTraps([]TrapCase{forge}, GoldenOptions{ForgeBin: flaggingBin})
	if err != nil {
		t.Fatal(err)
	}
	if !rep3.Traps[0].Captured {
		t.Fatalf("检测信号命中应记识破: %+v", rep3.Traps[0])
	}
}
