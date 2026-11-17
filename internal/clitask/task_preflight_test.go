package clitask

import (
	"strings"
	"testing"

	"github.com/MjxUpUp/Forge/internal/tasktypes"
)

// TestRunAcceptancePreflight_Counts 钉住预跑的基本计数语义：一条能跑通的命令
// 计入 Passed，一条不存在的命令计为失败（Exit!=0）并进 Fails；Total 按有 Run
// 的条数统计。
func TestRunAcceptancePreflight_Counts(t *testing.T) {
	root := t.TempDir()
	crits := []tasktypes.AcceptanceCriterion{
		{Run: "go version"},
		{Run: "forge-no-such-command-xyz"},
	}
	pre := RunAcceptancePreflight(root, crits)
	if pre.Total != 2 {
		t.Errorf("Total = %d, want 2", pre.Total)
	}
	if pre.Passed != 1 {
		t.Errorf("Passed = %d, want 1", pre.Passed)
	}
	if len(pre.Fails) != 1 {
		t.Fatalf("len(Fails) = %d, want 1: %+v", len(pre.Fails), pre.Fails)
	}
	if pre.Fails[0].Command != "forge-no-such-command-xyz" {
		t.Errorf("Fails[0].Command = %q, want %q", pre.Fails[0].Command, "forge-no-such-command-xyz")
	}
	if pre.Fails[0].Exit == 0 {
		t.Errorf("不存在的命令 Exit 不应为 0: %+v", pre.Fails[0])
	}
}

// TestRunAcceptancePreflight_Empty 钉住零判据/空 Run 不 panic 且不计入 Total。
func TestRunAcceptancePreflight_Empty(t *testing.T) {
	pre := RunAcceptancePreflight(t.TempDir(), nil)
	if pre.Total != 0 || pre.Passed != 0 || len(pre.Fails) != 0 {
		t.Errorf("nil 判据应得到零值结果，got %+v", pre)
	}

	pre = RunAcceptancePreflight(t.TempDir(), []tasktypes.AcceptanceCriterion{
		{Run: ""},
		{Run: "   "},
	})
	if pre.Total != 0 || pre.Passed != 0 || len(pre.Fails) != 0 {
		t.Errorf("空 Run 应被跳过（不计 Total），got %+v", pre)
	}
}

// TestTruncatePreflightOutput 钉住输出摘要的两条硬约束：300 字节上限与单行化。
func TestTruncatePreflightOutput(t *testing.T) {
	long := strings.Repeat("x", preflightOutputLimit+100)
	got := truncatePreflightOutput(long + "\nsecond line")
	if len(got) != preflightOutputLimit {
		t.Errorf("截断后长度 = %d, want %d", len(got), preflightOutputLimit)
	}

	got = truncatePreflightOutput("line1\r\nline2\nline3")
	if strings.ContainsAny(got, "\r\n") {
		t.Errorf("输出摘要不得含换行: %q", got)
	}
	if got != "line1 line2 line3" {
		t.Errorf("换行应替换为空格，got %q", got)
	}
}
