package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/MjxUpUp/Forge/internal/forgedata"
	"github.com/MjxUpUp/Forge/internal/registry"
	"github.com/MjxUpUp/Forge/internal/util"
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
// Merge semantics (~/.forge/projects/<from>/ → ~/.forge/projects/<to>/):
//   - tasks/*.json: moved when no conflict; on same-ref conflict the to-side wins (warn)
//   - checklog*.jsonl / toollog*.jsonl / sessions.jsonl: stable merge of both sides
//     ordered by the in-line timestamp (events are never lost or reordered);
//     rotated files (checklog-<date>.jsonl) merge the same way when both sides have
//     them, otherwise move as-is
//   - sessions/ stamps/ hazards/ act/ freeze/ quarantine/ and any other subdir:
//     file-level union, conflicts keep the to-side
//   - protocol.yml, hooks/: conflicts keep the to-side
//   - active-task-ref-*, session.json (live-session anchors): moved only, never
//     merged; on conflict the to-side wins (a live session must not be disturbed)
//   - after the merge the WHOLE remaining <from>/ dir is moved to
//     <to>/.rekey-backup-<ts>/ — never deleted; every replaced/conflicted from-side
//     file is thereby preserved for rollback
//   - registry sync: the fromKey entry is removed (or re-keyed when no toKey entry
//     exists) via registry.Rekey
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
// 合并语义（~/.forge/projects/<from>/ → ~/.forge/projects/<to>/）：
//   - tasks/*.json：不冲突直接搬；同 ref 冲突保留 to 侧（告警）
//   - checklog*.jsonl / toollog*.jsonl / sessions.jsonl：按行内时间戳稳定有序
//     合并（事件不丢、不重排）；rotated 文件（checklog-<date>.jsonl）两侧都有
//     时同样合并，否则原样搬移
//   - sessions/ stamps/ hazards/ act/ freeze/ quarantine/ 及其他子目录：文件级
//     并集，冲突保留 to 侧
//   - protocol.yml、hooks/：冲突保留 to 侧
//   - active-task-ref-*、session.json（活会话锚文件）：只搬不并；冲突保留 to 侧
//     （不破坏活会话）
//   - 合并后 <from>/ 整体移入 <to>/.rekey-backup-<ts>/——不直接删除；所有被替换/
//     冲突的 from 侧文件随之保留可回滚
//   - 注册表同步：fromKey 条目移除（无 toKey 条目时改 key）走 registry.Rekey
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

// rekeyTimestampFields are the JSONL line fields probed for the merge-order
// timestamp, covering the known event schemas: checklog (recorded_at), toollog
// (timestamp), sessions (started_at), hazards (ts), dashboard feed (time).
//
// rekeyTimestampFields 是合并排序探测的 JSONL 行时间戳字段，覆盖已知事件 schema：
// checklog（recorded_at）、toollog（timestamp）、sessions（started_at）、
// hazards（ts）、dashboard feed（time）。
var rekeyTimestampFields = []string{`recorded_at`, `timestamp`, `ts`, `time`, `started_at`, `created_at`}

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

	actions, err := rekeyDataDirs(fromDir, toDir, dryRun)
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

// rekeyDataDirs performs (or plans, when dryRun) the directory merge and returns the
// human-readable action log. Live-session anchor files are moved only when the
// to-side lacks them; everything conflicting keeps the to-side; the remaining
// from-dir is finally renamed into <toDir>/.rekey-backup-<ts>/.
//
// rekeyDataDirs 执行（dryRun 时仅规划）目录合并并返回人类可读动作日志。活会话锚
// 文件仅在 to 侧缺失时搬移；一切冲突保留 to 侧；剩余 from 目录最后 rename 进
// <toDir>/.rekey-backup-<ts>/。
func rekeyDataDirs(fromDir, toDir string, dryRun bool) ([]string, error) {
	var actions []string
	err := filepath.WalkDir(fromDir, func(src string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if src == fromDir || d.IsDir() {
			return nil
		}
		rel, rerr := filepath.Rel(fromDir, src)
		if rerr != nil {
			return rerr
		}
		dst := filepath.Join(toDir, rel)
		base := filepath.Base(src)
		_, dstExists := os.Stat(dst)

		switch {
		case dstExists == nil && isMergeableJSONL(base):
			// Both sides have the log: stable timestamp-ordered merge.
			//
			// 两侧都有该日志：按时间戳稳定有序合并。
			merged, merr := mergeJSONLFiles(dst, src)
			if merr != nil {
				return fmt.Errorf(`合并 %s: %w`, rel, merr)
			}
			actions = append(actions, fmt.Sprintf(`merge  %s（按行内时间戳有序合并）`, rel))
			if !dryRun {
				// Atomic write: an in-place truncate+overwrite of the to-side main
				// log would corrupt it irrecoverably on crash/power loss mid-write
				// (the to-side original has no other backup). The from-side source
				// is NOT removed here — it stays in fromDir and rides the final
				// whole-dir backup move, so the original from-side log survives
				// for rollback (code-review finding #1/#2).
				//
				// 原子写：原地截断覆盖 to 侧主日志，崩溃/断电中途会让它不可恢复
				// 地损坏（to 侧原件没有其他备份）。from 侧源文件此处不删——留在
				// fromDir 随最后的整目录备份搬移，from 侧原日志可回滚
				// （code-review 发现 #1/#2）。
				if werr := util.AtomicWrite(dst, merged, 0644); werr != nil {
					return werr
				}
			}
		case dstExists == nil:
			// Conflict: to-side wins; the from-side copy is preserved by the final
			// whole-dir backup move.
			//
			// 冲突：保留 to 侧；from 侧副本由最后的整目录备份搬移保留。
			actions = append(actions, fmt.Sprintf(`skip   %s（to 侧已存在，保留 to；from 副本随备份保留）`, rel))
		default:
			// No conflict: move. Anchor files (active-task-ref-*, session.json)
			// take this same path — move only, never merged.
			//
			// 无冲突：搬移。锚文件（active-task-ref-*、session.json）同样走这条路
			// ——只搬不并。
			actions = append(actions, fmt.Sprintf(`move   %s`, rel))
			if !dryRun {
				if merr := os.MkdirAll(filepath.Dir(dst), 0755); merr != nil {
					return merr
				}
				if merr := moveFile(src, dst); merr != nil {
					return fmt.Errorf(`搬移 %s: %w`, rel, merr)
				}
			}
		}
		return nil
	})
	if err != nil {
		return actions, err
	}

	// Whole-dir backup: whatever was skipped (conflicts, emptied dirs) moves into
	// <to>/.rekey-backup-<ts>/ — nothing is deleted outright.
	//
	// 整目录备份：所有跳过的（冲突文件、空目录）移入 <to>/.rekey-backup-<ts>/
	// ——不直接删除任何东西。
	backup := filepath.Join(toDir, `.rekey-backup-`+time.Now().Format(`20060102-150405`))
	actions = append(actions, fmt.Sprintf(`backup %s → %s`, fromDir, backup))
	if !dryRun {
		if err := os.MkdirAll(toDir, 0755); err != nil {
			return actions, err
		}
		if err := os.Rename(fromDir, backup); err != nil {
			return actions, fmt.Errorf(`from 目录移入备份失败: %w`, err)
		}
	}
	return actions, nil
}

// isMergeableJSONL reports whether a file name is an event log that should be
// timestamp-merged rather than moved: checklog*.jsonl / toollog*.jsonl (rotated
// suffixes included) and sessions.jsonl.
//
// isMergeableJSONL 判定文件名是否是应按时间戳合并（而非搬移）的事件日志：
// checklog*.jsonl / toollog*.jsonl（含 rotated 后缀）与 sessions.jsonl。
func isMergeableJSONL(base string) bool {
	if base == `sessions.jsonl` {
		return true
	}
	if !strings.HasSuffix(base, `.jsonl`) {
		return false
	}
	return strings.HasPrefix(base, `checklog`) || strings.HasPrefix(base, `toollog`)
}

// mergeJSONLFiles reads both JSONL files and returns their lines stably sorted by
// the in-line timestamp. Events are never lost or reordered: lines carry a
// carry-forward timestamp (a line without one inherits its predecessor's, so header/
// context lines stay attached to their event), and equal timestamps keep the
// to-side-before-from-side original order (stable sort).
//
// mergeJSONLFiles 读两个 JSONL 文件，返回按行内时间戳稳定排序后的行。事件不丢、
// 不重排：行用携带式时间戳（无时间戳的行继承前一行的，让头部/上下文行贴着所属
// 事件），时间戳相等时保持 to 侧在前、from 侧在后的原顺序（稳定排序）。
func mergeJSONLFiles(dstPath, srcPath string) ([]byte, error) {
	dst, err := os.ReadFile(dstPath)
	if err != nil {
		return nil, err
	}
	src, err := os.ReadFile(srcPath)
	if err != nil {
		return nil, err
	}
	type tsLine struct {
		raw string
		ts  time.Time
	}
	var lines []tsLine
	for _, data := range [][]byte{dst, src} {
		// Carry-forward resets at each file boundary: a leading timestamp-less line
		// inherits its OWN file's predecessor, never the other file's tail
		// (code-review finding #4).
		//
		// 携带式继承在文件边界重置：开头无时间戳的行继承本文件内的前驱，绝不
		// 继承另一文件的末尾（code-review 发现 #4）。
		var last time.Time
		for _, raw := range strings.Split(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n") {
			if strings.TrimSpace(raw) == `` {
				continue
			}
			if ts, ok := jsonlLineTimestamp(raw); ok {
				last = ts
			}
			lines = append(lines, tsLine{raw: raw, ts: last})
		}
	}
	slices.SortStableFunc(lines, func(a, b tsLine) int {
		return a.ts.Compare(b.ts)
	})
	var b strings.Builder
	for _, l := range lines {
		b.WriteString(l.raw)
		b.WriteByte('\n')
	}
	return []byte(b.String()), nil
}

// jsonlLineTimestamp extracts the event timestamp from one JSONL line by probing the
// known schema fields (rekeyTimestampFields). Unparseable/missing → ok=false; the
// caller falls back to carry-forward.
//
// jsonlLineTimestamp 按已知 schema 字段（rekeyTimestampFields）从一行 JSONL 提取
// 事件时间戳。无法解析/缺失 → ok=false；调用方回落到携带式继承。
func jsonlLineTimestamp(line string) (time.Time, bool) {
	var m map[string]json.RawMessage
	if json.Unmarshal([]byte(line), &m) != nil {
		return time.Time{}, false
	}
	for _, f := range rekeyTimestampFields {
		raw, ok := m[f]
		if !ok {
			continue
		}
		var ts time.Time
		if json.Unmarshal(raw, &ts) == nil && !ts.IsZero() {
			return ts, true
		}
	}
	return time.Time{}, false
}

// moveFile renames src to dst, falling back to copy+remove across devices.
//
// moveFile 把 src rename 到 dst，跨设备时回落 copy+remove。
func moveFile(src, dst string) error {
	if err := os.Rename(src, dst); err == nil {
		return nil
	}
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	if err := os.WriteFile(dst, data, 0644); err != nil {
		return err
	}
	return os.Remove(src)
}
