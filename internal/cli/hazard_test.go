package cli

import (
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// TestRunHazardConfirm_RejectsInvalidFingerprint pins the format validation of the confirm --fingerprint path
// taking effect at the cli layer: invalid fingerprints (truncated/non-hex) must return an error, not print a fake success message.
// (Root cause of the 2026-07 AgentWorld incident: the agent copied the fingerprint wrong, confirm did not validate and wrote to a wrong filename reporting success,
// the hook looked up with the real fingerprint and could not find it, kept blocking.)
//
// TestRunHazardConfirm_RejectsInvalidFingerprint 钉住 confirm --fingerprint 路径的格式
// 校验从 cli 层生效：非法指纹（残缺/非 hex）必须返回 error，而非打印"✅ 已确认"虚假成功
// （2026-07 AgentWorld 事故根因：agent 抄错指纹，confirm 不校验直接写入错文件名报成功，
// hook 用真指纹查不到、继续拦）。
//
// Validation logic lives in internal/hazard.ConfirmByFingerprint (see
// TestConfirmByFingerprint_RejectsInvalidFormat); this test confirms that the cli layer runHazardConfirm
// passes that error through and does not reach the fmt.Printf success message. We call runHazardConfirm directly to bypass hazard-guard's
// existing false-positive on confirm --fingerprint (confirm contains rm, --fingerprint contains -f...r, misjudged as rm -rf).
//
// 校验逻辑在 internal/hazard.ConfirmByFingerprint（见
// TestConfirmByFingerprint_RejectsInvalidFormat），本测试确认 cli 层 runHazardConfirm
// 透传该 error，不会走到 fmt.Printf 的"✅ 已确认"。直接调 runHazardConfirm 绕开 hazard-guard
// 对 "confirm --fingerprint" 的既有误伤（confirm 含 rm、--fingerprint 含 -f...r 被误判 rm -rf）。
//
// Pass &cobra.Command{} instead of nil: runHazardConfirm does not currently use cmd, but passing a real object prevents future tests from panicking when someone adds
// cmd.Flags()/cmd.OutOrStdout().
//
// 传 &cobra.Command{} 而非 nil：runHazardConfirm 当前不用 cmd，但传真实对象防未来有人加
// cmd.Flags()/cmd.OutOrStdout() 时测试 panic。
func TestRunHazardConfirm_RejectsInvalidFingerprint(t *testing.T) {
	// "abc" is neither 64 chars nor valid hex — validation must reject and must not persist.
	//
	// "abc" 既非 64 字符也非合法 hex——校验必拒，且不落盘。
	hazardConfirmFingerprint = "abc"
	t.Cleanup(func() { hazardConfirmFingerprint = "" })

	err := runHazardConfirm(&cobra.Command{}, nil)
	if err == nil {
		t.Fatal("runHazardConfirm must reject invalid --fingerprint instead of printing success")
	}
	// Assert the error truly comes from fingerprint validation, not a findProjectRoot failure — otherwise when project root is missing
	// the test would also pass (false positive), contradicting what the test name promises.
	//
	// 断言 error 确实来自指纹校验，而非 findProjectRoot 失败——否则在"项目根找不到"
	// 时测试也会通过（假阳性），与测试名承诺不符。
	if !strings.Contains(err.Error(), "invalid fingerprint") {
		t.Fatalf("error must come from fingerprint validation, got: %v", err)
	}
}

// TestShortFingerprint pins the length guard on confirmation fingerprints read
// back from disk (untrusted input): values shorter than 12 chars must not
// panic on the display slice.
//
// TestShortFingerprint 钉住从磁盘读回的确认指纹（不可信输入）的长度守卫：
// 短于 12 字符的值不得在展示切片上 panic。
func TestShortFingerprint(t *testing.T) {
	cases := map[string]string{
		"":               "",
		"abc":            "abc",
		"0123456789ab":   "0123456789ab",
		"0123456789abcd": "0123456789ab",
	}
	for in, want := range cases {
		if got := shortFingerprint(in); got != want {
			t.Errorf("shortFingerprint(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestHazardCmd_LongGuidanceCopy pins the 2026-08 help-text revision (the same
// copy fix as the hazard-guard block message): the Long help must carry the
// pre-authorization path (user already instructed/confirmed this turn → confirm
// --last directly, no second ask), must reference the host tool's confirmation
// mechanism generically (the AskUserQuestion enumeration missed
// kimi/copilot/zcode), and must not carry the FORGE_ALLOW_HAZARD migration note
// (changelog, not action guidance).
//
// TestHazardCmd_LongGuidanceCopy 钉死 2026-08 帮助文案修订（与 hazard-guard
// block 文案同批修复）：Long 必须含授权路径（用户本回合已明确指令/确认过 →
// 直接 confirm --last 无需二次确认）、工具指代须泛化（AskUserQuestion 枚举漏了
// kimi/copilot/zcode）、不得再带 FORGE_ALLOW_HAZARD 迁移说明（changelog 不是
// 行动指引）。
func TestHazardCmd_LongGuidanceCopy(t *testing.T) {
	for _, anchor := range []string{"无需二次确认", "confirm --last", "你所在工具的提问确认机制"} {
		if !strings.Contains(hazardCmd.Long, anchor) {
			t.Errorf("hazard Long help missing %q:\n%s", anchor, hazardCmd.Long)
		}
	}
	for _, gone := range []string{"FORGE_ALLOW_HAZARD", "AskUserQuestion"} {
		if strings.Contains(hazardCmd.Long, gone) {
			t.Errorf("hazard Long help must not contain %q anymore:\n%s", gone, hazardCmd.Long)
		}
	}
}
