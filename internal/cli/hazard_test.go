package cli

import (
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// TestRunHazardConfirm_RejectsInvalidFingerprint 钉住 confirm --fingerprint 路径的格式
// 校验从 cli 层生效：非法指纹（残缺/非 hex）必须返回 error，而非打印"✅ 已确认"虚假成功
// （2026-07 AgentWorld 事故根因：agent 抄错指纹，confirm 不校验直接写入错文件名报成功，
// hook 用真指纹查不到、继续拦）。
//
// 校验逻辑在 internal/hazard.ConfirmByFingerprint（见
// TestConfirmByFingerprint_RejectsInvalidFormat），本测试确认 cli 层 runHazardConfirm
// 透传该 error，不会走到 fmt.Printf 的"✅ 已确认"。直接调 runHazardConfirm 绕开 hazard-guard
// 对 "confirm --fingerprint" 的既有误伤（confirm 含 rm、--fingerprint 含 -f...r 被误判 rm -rf）。
//
// 传 &cobra.Command{} 而非 nil：runHazardConfirm 当前不用 cmd，但传真实对象防未来有人加
// cmd.Flags()/cmd.OutOrStdout() 时测试 panic。
func TestRunHazardConfirm_RejectsInvalidFingerprint(t *testing.T) {
	// "abc" 既非 64 字符也非合法 hex——校验必拒，且不落盘。
	hazardConfirmFingerprint = "abc"
	t.Cleanup(func() { hazardConfirmFingerprint = "" })

	err := runHazardConfirm(&cobra.Command{}, nil)
	if err == nil {
		t.Fatal("runHazardConfirm must reject invalid --fingerprint instead of printing success")
	}
	// 断言 error 确实来自指纹校验，而非 findProjectRoot 失败——否则在"项目根找不到"
	// 时测试也会通过（假阳性），与测试名承诺不符。
	if !strings.Contains(err.Error(), "invalid fingerprint") {
		t.Fatalf("error must come from fingerprint validation, got: %v", err)
	}
}
