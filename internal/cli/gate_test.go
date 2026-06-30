package cli

import (
	"testing"

	"github.com/spf13/cobra"
)

// newGateCmd 构造一个带 gate 命令全部 flag 的 cobra.Command 并解析给定 flag。
// runGate 通过 cmd.Flags().GetBool 读 flag，手动调 runGate 时需先挂上这些 flag。
func newGateCmd(args ...string) *cobra.Command {
	cmd := &cobra.Command{Use: "gate"}
	cmd.Flags().Bool("force", false, "")
	cmd.Flags().Bool("retry", false, "")
	cmd.Flags().Bool("silent", false, "")
	cmd.Flags().Bool("current", false, "")
	_ = cmd.ParseFlags(args)
	return cmd
}

// TestRunGate_CurrentFlag_NonForgeProjectSilent：Stop hook (forge gate --current
// --silent) 在非 forge 项目必须静默退出（nil），不能报 "not in a forge project"
// 污染 hook 输出。回归守护：若有人把 findProjectRoot 失败改回 return err，此测试失败。
func TestRunGate_CurrentFlag_NonForgeProjectSilent(t *testing.T) {
	t.Chdir(t.TempDir()) // 非 forge 目录（无 .forge/），t.Chdir 自动恢复

	cmd := newGateCmd("--current", "--silent")
	if err := runGate(cmd, nil); err != nil {
		t.Fatalf("non-forge project should exit silently under --current, got: %v", err)
	}
}

// TestRunGate_GateID_NonForgeProjectErrors：对比——带显式 gate-id（非 --current，
// 用户手动调用）在非 forge 项目仍应报错。确保静默只针对 hook 用的 --current，
// 不掩盖手动调用的真实错误（loadPipeline → findProjectRoot）。
func TestRunGate_GateID_NonForgeProjectErrors(t *testing.T) {
	t.Chdir(t.TempDir())

	cmd := newGateCmd()
	if err := runGate(cmd, []string{"gate-1"}); err == nil {
		t.Fatal("non-forge project with explicit gate-id should error (loadPipeline)")
	}
}
