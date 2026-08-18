package cli

import (
	"fmt"
	"os"

	"github.com/MjxUpUp/Forge/internal/datamerge"
	"github.com/MjxUpUp/Forge/internal/forgedata"
	"github.com/MjxUpUp/Forge/internal/registry"
	"github.com/spf13/cobra"
)

// forge registry rekey merges one project data directory into another:
// `forge registry rekey --from <old-key> --to <new-key> [--dry-run]`.
//
// Root cause it repairs: on macOS's default case-insensitive APFS the same project
// could be registered under two path spellings (Forge vs forge), deriving two
// identity keys; tasks/checklog/sessions written under the variant spelling landed in
// a split data directory (2026-08-18 dogfood: 8+2 task split). After the derivation
// layer converges to canonical case (forgedata.CanonicalCase), the EXISTING split
// data still needs an explicit merge — this command. It is deliberately explicit
// (not a silent lazy migration): merging involves ordered JSONL merges and conflict
// trade-offs, so it must be dry-runnable, previewable, and backed up for rollback.
//
// Merge semantics live in internal/datamerge (project-sync extraction: rekey and
// `forge project import` share ONE merge implementation). Legacy rekey policy =
// to-side wins conflicts, JSONL timestamp-ordered merge, and the from-dir survives
// whole as <to>/.rekey-backup-<ts>/ (never deleted; every replaced/conflicted
// from-side file is preserved for rollback). Registry sync via registry.Rekey.
//
// --dry-run lists every planned action without touching disk.
//
// forge registry rekey 把一个项目数据目录并入另一个：
// `forge registry rekey --from <old-key> --to <new-key> [--dry-run]`。
//
// 它修复的根因：macOS 默认大小写不敏感 APFS 上同一项目可能按两种路径拼写登记
// （Forge vs forge）衍生两个身份 key；变体拼写下写入的 tasks/checklog/sessions
// 落进了分裂的数据目录（2026-08-18 dogfood：8+2 任务分裂）。推导层收敛到
// canonical case（forgedata.CanonicalCase）之后，存量分裂数据仍需显式合并——即
// 本命令。刻意做成显式命令（不做静默惰性迁移）：合并涉及 jsonl 有序合并与冲突
// 取舍，必须可 dry-run 预览、有备份可回滚。
//
// 合并语义在 internal/datamerge（project-sync 抽包：rekey 与
// `forge project import` 共享同一份合并实现）。legacy rekey 策略 = 冲突保 to 侧、
// jsonl 按时间戳有序合并、from 目录整体移入 <to>/.rekey-backup-<ts>/（不删除；
// 所有被替换/冲突的 from 侧文件随之保留可回滚）。注册表同步走 registry.Rekey。
//
// --dry-run 列出将执行的动作，不落盘。

func init() {
	registryCmd.AddCommand(registryRekeyCmd)
	registryRekeyCmd.Flags().String(`from`, ``, `源项目 key（其数据目录被并入 --to）`)
	registryRekeyCmd.Flags().String(`to`, ``, `目标项目 key（保留身份）`)
	registryRekeyCmd.Flags().Bool(`dry-run`, false, `只列出将执行的动作，不落盘`)
	_ = registryRekeyCmd.MarkFlagRequired(`from`)
	_ = registryRekeyCmd.MarkFlagRequired(`to`)
}

var registryRekeyCmd = &cobra.Command{
	Use:   `rekey --from <old-key> --to <new-key> [--dry-run]`,
	Short: `把 from key 的项目数据目录并入 to key（修复身份分裂存量数据）`,
	RunE:  runRegistryRekey,
}

func runRegistryRekey(cmd *cobra.Command, args []string) error {
	fromKey, _ := cmd.Flags().GetString(`from`)
	toKey, _ := cmd.Flags().GetString(`to`)
	dryRun, _ := cmd.Flags().GetBool(`dry-run`)
	out := cmd.OutOrStdout()

	if fromKey == toKey {
		return fmt.Errorf(`--from 与 --to 相同（%s），无需 rekey`, fromKey)
	}
	fromDir := forgedata.RootDir(fromKey)
	toDir := forgedata.RootDir(toKey)
	if fromDir == `` || toDir == `` {
		return fmt.Errorf(`无法解析数据目录（from=%q to=%q）`, fromDir, toDir)
	}
	if _, err := os.Stat(fromDir); err != nil {
		return fmt.Errorf(`from 数据目录不存在：%s`, fromDir)
	}

	// 零值 Options 即 legacy rekey 语义：TaskToWins / 无行去重 / from 整目录备份。
	actions, err := datamerge.Dirs(fromDir, toDir, datamerge.Options{DryRun: dryRun})
	if err != nil {
		return err
	}
	for _, a := range actions {
		fmt.Fprintln(out, a)
	}
	if dryRun {
		fmt.Fprintln(out, `（dry-run：以上动作未落盘）`)
		return nil
	}
	removed, rerr := registry.Rekey(fromKey, toKey)
	if rerr != nil {
		fmt.Fprintf(out, `warn: 注册表同步失败（数据已合并）：%v\n`, rerr)
	} else if removed > 0 {
		fmt.Fprintf(out, `注册表：移除 %d 条 from key 条目\n`, removed)
	}
	fmt.Fprintf(out, `✅ rekey 完成：%s → %s（from 目录已移入备份，未删除）\n`, fromKey, toKey)
	return nil
}
