package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

// versionCmd 是 `forge --version` 的子命令等价物：同一份 rootCmd.Version，
// 便于脚本与 agent 用统一的「子命令」形态取版本（dogfood 反馈：`forge version`
// 报 unknown command，而文档/习惯都期待它存在）。
var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "打印 forge 版本（等价于 forge --version）",
	Long: `打印 forge 版本。

与 ` + "`forge --version`" + ` 等价：版本、commit 与构建时间来自构建期注入（-ldflags）。
脚本里两种写法都可以，` + "`forge version`" + ` 只是更适合「命令族」形态的调用。`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		fmt.Fprintf(cmd.OutOrStdout(), "forge version %s\n", cmd.Root().Version)
		return nil
	},
}

func init() {
	versionCmd.GroupID = "lifecycle"
	rootCmd.AddCommand(versionCmd)
}
