package cli

import (
	"fmt"

	"github.com/MjxUpUp/Forge/internal/forgedata"
	"github.com/spf13/cobra"
)

func init() {
	rootCmd.AddCommand(migrateCmd)
	migrateCmd.Flags().BoolVar(&migrateDryRun, "dry-run", false, "预览将迁移的条目，不实际执行")
	migrateCmd.Flags().BoolVar(&migrateForce, "force", false, "DataDir 已有同名时覆盖（默认跳过）")
}

var (
	migrateDryRun bool
	migrateForce  bool
)

// migrateCmd 把项目级 .forge/ 下的 runtime state（tasks/gates/checklog/toollog/act/
// sessions/quarantine/active-task-ref/.task-verify-throttle.last 等）迁到用户级
// DataDir（~/.forge/projects/<key>/）。refactor-data-home 后老版本积累的 runtime state
// 仍在 .forge/，升级后用本命令一次性搬到 DataDir。项目配置（hooks/protocol.yml/
// CLAUDE.md/AGENTS.md/.sync-version）不迁，仍留 .forge/；老版本残留的 pipeline.yml/
// state.json（项目级管道已删除）若存在也留原地不动。
//
// 幂等：重复跑无害（已迁的不再动）。
var migrateCmd = &cobra.Command{
	Use:   "migrate",
	Short: "把旧 .forge/ runtime state 迁到用户级 DataDir",
	Long: `forge migrate —— 旧 .forge/ runtime state → 用户级 DataDir 迁移

refactor-data-home 之后，runtime state 从项目级 .forge/ 搬到用户级
~/.forge/projects/<key>/（DataDir）。升级 forge 后，老版本积累在 .forge/ 的
runtime state（tasks/gates/checklog/toollog/act/sessions/quarantine/
active-task-ref/.task-verify-throttle.last 等）不会自动迁移——本命令一次性
搬到 DataDir，让历史 task/gate/checklog 数据在新版本继续可见。

项目配置不迁，仍留 .forge/：hooks/（项目配置 hook）/protocol.yml/CLAUDE.md/
AGENTS.md/.sync-version（同步戳）。老版本残留的 pipeline.yml/state.json（项目级
管道已删除）若存在也留原地不动——死文件无害，本命令只搬 runtime state。

安全：白名单迁移（不盲目搬整个 .forge/），幂等（重复跑无害）。
--dry-run 预览，--force 覆盖 DataDir 已有同名（默认跳过）。`,
	RunE: func(cmd *cobra.Command, args []string) error {
		p, err := findProject()
		if err != nil {
			return fmt.Errorf("forge migrate 需在 forge 项目（含 .forge/）中运行: %w", err)
		}
		res, err := forgedata.MigrateProject(p, forgedata.MigrateOptions{
			DryRun: migrateDryRun,
			Force:  migrateForce,
		})
		if err != nil {
			return err
		}
		out := cmd.OutOrStdout()
		if migrateDryRun {
			fmt.Fprintln(out, `[dry-run] 以下条目将被迁移到 DataDir（不实际执行）：`)
		}
		for _, m := range res.Moved {
			fmt.Fprintln(out, `  迁移  `+m)
		}
		for _, s := range res.Skipped {
			fmt.Fprintln(out, `  跳过  `+s+`（DataDir 已有，--force 覆盖）`)
		}
		if len(res.Moved) == 0 && len(res.Skipped) == 0 {
			fmt.Fprintln(out, `无 runtime state 需迁移（.forge/ 已是纯配置）`)
		}
		if !migrateDryRun && len(res.Left) > 0 {
			fmt.Fprintln(out)
			fmt.Fprintln(out, `.forge/ 保留（项目配置）：`+fmt.Sprint(res.Left))
		}
		fmt.Fprintln(out)
		fmt.Fprintln(out, `DataDir: `+p.DataDir)
		return nil
	},
}
