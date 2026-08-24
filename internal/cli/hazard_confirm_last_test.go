package cli

// hazard_confirm_last_test.go — pins the CLI wiring of `hazard confirm --last` at the
// argument-validation layer: --last permits a zero-arg invocation (the newest block
// event carries everything), while the bare command without --last/--fingerprint still
// demands a command argument. The RunE body (project resolution + ConfirmLastBlock) is
// covered by the hazard package's TestConfirmLastBlock and the E2E run recorded in the
// fix commit; re-driving the full RunE here would need project isolation for a path
// already integration-tested.
//
// hazard_confirm_last_test.go — 钉死 `hazard confirm --last` 的 CLI 参数校验层：
// --last 允许零参数调用（最新 block 事件已含全部信息），不带 --last/--fingerprint
// 的裸命令仍要求命令参数。RunE 主体（项目解析 + ConfirmLastBlock）由 hazard 包的
// TestConfirmLastBlock 与修复 commit 记录的 E2E 覆盖；在此重驱完整 RunE 需要项目
// 隔离，而该路径已有集成测试。

import (
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func TestHazardConfirm_LastFlagArgs(t *testing.T) {
	// Fresh command instance per case: cobra flag state (Flags().Changed) is per-instance.
	//
	// 每例新建命令实例：cobra 的 flag 状态（Flags().Changed）是实例级的。
	newCmd := func(setLast bool) *cobra.Command {
		c := &cobra.Command{Use: "confirm"}
		c.Flags().Bool("last", false, "")
		c.Flags().String("fingerprint", "", "")
		if setLast {
			if err := c.Flags().Set("last", "true"); err != nil {
				t.Fatal(err)
			}
		}
		return c
	}

	// With --last: zero args must pass Args validation.
	//
	// 带 --last：零参数必须通过 Args 校验。
	if err := hazardConfirmCmd.Args(newCmd(true), []string{}); err != nil {
		t.Fatalf("--last with zero args must be accepted, got: %v", err)
	}
	// With --last: extra positional args are also tolerated (ignored by the --last path).
	//
	// 带 --last：多余位置参数也被容忍（--last 路径忽略它们）。
	if err := hazardConfirmCmd.Args(newCmd(true), []string{"stray"}); err != nil {
		t.Fatalf("--last with stray args must be accepted, got: %v", err)
	}
	// Without --last: zero args must error and name both alternatives.
	//
	// 不带 --last：零参数必须报错并给出两个替代路径。
	err := hazardConfirmCmd.Args(newCmd(false), []string{})
	if err == nil {
		t.Fatal("bare confirm with zero args must be rejected")
	}
	if !strings.Contains(err.Error(), "--last") || !strings.Contains(err.Error(), "--fingerprint") {
		t.Fatalf("error must name --last and --fingerprint alternatives, got: %v", err)
	}
}
