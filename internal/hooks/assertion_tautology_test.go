package hooks

// assertion_tautology_test.go — 恒真断言检测的守卫（2026-09-03 eval traps 缺口
// 闭环）：字符串级断言沿 embed_test 惯例；行为级验证由 eval traps 的
// trap-weakened-assertion（expect_detected: true）承载——该 trap 转红即检测被删。

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestAssertionCheck_TautologyPatternPresent 守卫：TAUTO_PAT 与其消费点必须在
// 内嵌脚本里同时在场（删检测不删变量 = 静默失效；删两者则 trap e2e 转红）。
func TestAssertionCheck_TautologyPatternPresent(t *testing.T) {
	if !strings.Contains(AssertionCheckHook, "TAUTO_PAT=") {
		t.Fatal("AssertionCheckHook 缺 TAUTO_PAT 定义——恒真断言检测被移除（eval trap 将转红）")
	}
	uses := strings.Count(AssertionCheckHook, "$TAUTO_PAT") + strings.Count(AssertionCheckHook, "${TAUTO_PAT}")
	if uses < 2 {
		t.Fatalf("TAUTO_PAT 应被 per-edit 与 batch 两处消费，实际 %d 处", uses)
	}
}

// TestAssertionCheckHook_GateModeExitsZero 行为级回归（2026-11-11）：内嵌脚本
// 在 batch/gate 模式（无 FILE_PATH）必须 exit 0——set -e 下一条 and-list 以
// `[ -z "$VAR" ] && VAR=x` 收尾时，测试为假会让整条列表返回 1、脚本静默 abort，
// gate 侧只能看到空的 "assertion check failed"，task-implement 对所有任务恒红。
func TestAssertionCheckHook_GateModeExitsZero(t *testing.T) {
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash 不可用（与 hook 运行时同前置）")
	}
	dir := t.TempDir()
	script := filepath.Join(t.TempDir(), "assertion-check.sh")
	if err := os.WriteFile(script, []byte(AssertionCheckHook), 0o600); err != nil {
		t.Fatalf("write script: %v", err)
	}
	cmd := exec.Command("bash", filepath.ToSlash(script), filepath.ToSlash(dir))
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("gate 模式必须 exit 0（set -e 下 and-list 收尾会静默 abort）: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "PASS [assertion-check]") {
		t.Fatalf("gate 模式应输出 PASS 结论，实际: %q", out)
	}
}

// TestAssertionCheckHook_BashSyntaxSmoke 对内嵌脚本做 bash -n 语法检查——
// raw string 内嵌的脚本没有编译期保障，语法错会让 hook 整体 fail-open。
func TestAssertionCheckHook_BashSyntaxSmoke(t *testing.T) {
	cmd := exec.Command("bash", "-n")
	cmd.Stdin = strings.NewReader(AssertionCheckHook)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("assertion-check 脚本 bash 语法错误: %v\n%s", err, out)
	}
}
