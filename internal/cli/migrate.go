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

// migrateCmd moves runtime state under project-level .forge/ (tasks/gates/checklog/
// toollog/act/sessions/quarantine/active-task-ref/.task-verify-throttle.last, etc.)
// to the user-level DataDir (~/.forge/projects/<key>/). After refactor-data-home,
// runtime state accumulated by older versions still sits in .forge/; this command
// moves it to DataDir in one pass after upgrade. Project config (hooks/protocol.yml/
// CLAUDE.md/AGENTS.md/.sync-version) is not migrated and stays in .forge/; legacy
// pipeline.yml/state.json residue (project-level pipeline was deleted) is also left
// untouched——autoSync cleans them up automatically on upgrade
// (see sync.cleanupLegacyDeadFiles), so dead files need no manual handling.
//
// Idempotent: repeat runs are harmless (already-migrated items are not touched).
//
// migrateCmd 把项目级 .forge/ 下的 runtime state（tasks/gates/checklog/toollog/act/
// sessions/quarantine/active-task-ref/.task-verify-throttle.last 等）迁到用户级
// DataDir（~/.forge/projects/<key>/）。refactor-data-home 后老版本积累的 runtime state
// 仍在 .forge/，升级后用本命令一次性搬到 DataDir。项目配置（hooks/protocol.yml/
// CLAUDE.md/AGENTS.md/.sync-version）不迁，仍留 .forge/；老版本残留的 pipeline.yml/
// state.json（项目级管道已删除）本命令也不动——但 autoSync 升级时会自动清理
// （见 sync.cleanupLegacyDeadFiles），死文件无需手动处理。
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
管道已删除）本命令只搬 runtime state 不删死文件——autoSync 升级时会自动清理它们
（死文件无害，无需手动处理）。

安全：白名单迁移（不盲目搬整个 .forge/），幂等（重复跑无害）。
--dry-run 预览，--force 覆盖 DataDir 已有同名（默认跳过）。

注意：本命令只做版本升级后的本地搬迁（项目级 .forge/ → 用户级 DataDir）；
跨机器迁移用 forge project export/import。`,
	RunE: func(cmd *cobra.Command, args []string) error {
		p, err := findProject()
		if err != nil {
			return fmt.Errorf("forge migrate 需在 forge 项目（含 .forge/）中运行: %w", err)
		}
		res, err := forgedata.MigrateProject(p, forgedata.MigrateOptions{
			DryRun: migrateDryRun,
			Force:  migrateForce,
		})
		// Trust boundary (2026-08-15): task files promoted from repo-committable .forge/ carry
		// attacker-authorable gate/trust signals — strip them right after they land (single source
		// of truth shared with task import; see migrate_sanitize.go). This must ALSO run when
		// MigrateProject returns an error with a partial result (tasks already moved, a later
		// entry failed): the promoted files are in the trusted DataDir regardless of how the run
		// ended, and a re-run would SKIP the move (dst exists) without ever sanitizing. DryRun
		// moves nothing, so it never triggers there.
		//
		// 信任边界（2026-08-15）：从可提交 .forge/ 提升的 task 文件携带攻击者可书写的门禁/信任
		// 信号——落地后立即剥离（与 task import 共用单一真相源；见 migrate_sanitize.go）。
		// MigrateProject 带部分结果返回 error 时（tasks 已迁移，后续条目失败）同样必须清洗：
		// 无论本次运行怎么结束，已提升的文件都在受信 DataDir 里，而重跑会 SKIP 该次迁移
		// （dst 已存在）且永不清洗。DryRun 不移动，永不触发。
		var sanitized int
		var sanErr error
		if !migrateDryRun && res != nil {
			sanitized, sanErr = sanitizeAfterMigration(p.Root, res.Moved)
		}
		if err != nil {
			// Partial-migration path: sanitize may ALSO have failed right after the tasks dir moved —
			// returning only the migrate error would swallow the sanitize error (the pending marker
			// keeps it retryable, but the user must see it; review 2026-08-16). Join both.
			//
			// 部分迁移路径：tasks 目录刚搬完、清洗也可能随即失败——只返回迁移错误会把清洗错误
			// 吞掉（pending 标记保其可重试，但用户必须看见；2026-08-16 复审）。两个都报。
			if sanErr != nil {
				return fmt.Errorf("%w（另：外来门禁信号清洗也失败: %v——pending 标记已记录，修复后重跑会重试清洗）", err, sanErr)
			}
			return err
		}
		if sanErr != nil {
			// Fail-closed (2026-08-15 review): the migration already happened, so a sanitize
			// failure with exit 0 would leave attacker-authorable gate signals as live local
			// trust state. The user must see the non-zero exit and fix the cause.
			//
			// fail-closed（2026-08-15 审查）：迁移已经发生，清洗失败却以 0 退出会把攻击者
			// 可书写的门禁信号留成活的本机信任状态。用户必须看到非零退出并修复原因。
			return fmt.Errorf("迁移了 tasks 但外来门禁信号清洗失败（hostile task 状态可能仍在 DataDir，须修复后重跑）: %w", sanErr)
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
		if sanitized > 0 {
			fmt.Fprintln(out)
			fmt.Fprintf(out, "已清洗 %d 个迁移任务的外来门禁信号（review/验收/评分/完成/逃生舱须本机重跑；验收命令带外来标记，verify-acceptance 首跑须 --trust-foreign）\n", sanitized)
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
