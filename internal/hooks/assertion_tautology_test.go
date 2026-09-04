package hooks

// assertion_tautology_test.go — 恒真断言检测的守卫（2026-09-03 eval traps 缺口
// 闭环）：字符串级断言沿 embed_test 惯例；行为级验证由 eval traps 的
// trap-weakened-assertion（expect_detected: true）承载——该 trap 转红即检测被删。

import (
	"os/exec"
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

// TestAssertionCheckHook_BashSyntaxSmoke 对内嵌脚本做 bash -n 语法检查——
// raw string 内嵌的脚本没有编译期保障，语法错会让 hook 整体 fail-open。
func TestAssertionCheckHook_BashSyntaxSmoke(t *testing.T) {
	cmd := exec.Command("bash", "-n")
	cmd.Stdin = strings.NewReader(AssertionCheckHook)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("assertion-check 脚本 bash 语法错误: %v\n%s", err, out)
	}
}
