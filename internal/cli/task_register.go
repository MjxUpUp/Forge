package cli

import (
	"github.com/MjxUpUp/Forge/internal/clitask"
)

// task_register.go —— task 命令簇的 cli 侧注册器（2026-09 代码普查 A2-3：
// task.go + task_*.go 已整体迁 internal/clitask，cli 只保留装配）。三件事：
//  1. harness 提交钩子接缝注入（clitask.CommitBestEffort = HarnessCommitBestEffort
//     ——会话语义属于 cli，clitask 不 import cli 反向成环）；
//  2. 挂载 Root（forge task）与 WorktreeCmd（forge worktree——定义随簇迁出，
//     命令面是根级）到 rootCmd；
//  3. verify 的 --collect-golden flag 留在 verify 域（verify.go init），执行体
//     clitask.RunCollectGoldenMode。
func init() {
	clitask.CommitBestEffort = HarnessCommitBestEffort
	rootCmd.AddCommand(clitask.Root)
	rootCmd.AddCommand(clitask.WorktreeCmd)
}
