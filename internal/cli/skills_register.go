package cli

import (
	"github.com/spf13/cobra"

	"github.com/MjxUpUp/Forge/internal/cliskills"
)

// skills_register.go —— skills 命令簇的 cli 侧注册器（2026-09 代码普查 A2-1：
// 25 个 skills_*.go 已整体迁至 internal/cliskills，cli 只保留装配）。三件事：
//  1. 版本注入（cliskills.Version seam——cliskills 不 import cli，反向成环）；
//  2. PersistentPreRunE 注入（覆盖 root 的 findProjectRoot+autoSync，保留 update
//     检查——让 skill 分发在非 forge 项目也能跑，语义与迁出前完全一致）；
//  3. 挂到 rootCmd。
func init() {
	cliskills.VersionFn = func() string { return rootCmd.Version }
	cliskills.Root.PersistentPreRunE = func(cmd *cobra.Command, args []string) error {
		checkForUpdate(cmd.Root().Version, cmd)
		return nil
	}
	rootCmd.AddCommand(cliskills.Root)
}
