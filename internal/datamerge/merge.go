// Package datamerge merges one forge project data directory into another. Extracted
// from cli/registry_rekey.go (project-sync) so two callers share one implementation:
//
//   - `forge registry rekey` — identity-split repair, legacy semantics (to-side wins
//     conflicts, from-dir preserved as a .rekey-backup);
//   - `forge project import` — cross-machine sync, union semantics (tasks merged via
//     taskpipeline.MergeTaskState(Sync), JSONL lines deduped for idempotent re-import,
//     disposable from-dir).
//
// Merge map (from → to):
//   - tasks/*.json ......... policy-controlled: TaskToWins (skip, keep to) |
//     TaskUnion (state merge) | TaskSkip (caller handles)
//   - checklog*.jsonl / toollog*.jsonl / sessions.jsonl / act conclusions.jsonl:
//     stable timestamp-ordered merge (events never lost/reordered), optional
//     exact-line dedup
//   - everything else ...... file-level union, conflicts keep the to-side
//
// Chinese strings use raw string literals (Windows quote-corruption rule).
//
// Package datamerge 把一个 forge 项目数据目录合并进另一个。从 cli/registry_rekey.go
// 抽出（project-sync），两个调用方共享一份实现：
//
//   - `forge registry rekey` —— 身份分裂修复，legacy 语义（冲突保 to 侧，from 目录
//     整体保为 .rekey-backup）；
//   - `forge project import` —— 跨机器同步，并集语义（任务经 taskpipeline.
//     MergeTaskState(Sync) 合并，JSONL 行去重保证重导入幂等，from 目录一次性）。
//
// 合并映射（from → to）：
//   - tasks/*.json ......... 按策略：TaskToWins（跳过保 to）| TaskUnion（状态合并）|
//     TaskSkip（调用方自理）
//   - checklog*.jsonl / toollog*.jsonl / sessions.jsonl / act conclusions.jsonl：
//     按时间戳稳定有序合并（事件不丢不重排），可选精确行去重
//   - 其余 .................. 文件级并集，冲突保 to 侧
package datamerge

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/MjxUpUp/Forge/internal/taskpipeline"
	"github.com/MjxUpUp/Forge/internal/util"
)

// TaskConflictPolicy controls how conflicting tasks/*.json entries (same file name on
// both sides) resolve.
//
// TaskConflictPolicy 控制冲突的 tasks/*.json 条目（两侧同名文件）如何解决。
type TaskConflictPolicy int

const (
	// TaskToWins keeps the to-side file on conflict (legacy rekey semantics: the
	// to-side identity is authoritative, the from copy rides the whole-dir backup).
	//
	// TaskToWins 冲突保留 to 侧文件（legacy rekey 语义：to 侧身份权威，from 副本
	// 随整目录备份保留）。
	TaskToWins TaskConflictPolicy = iota
	// TaskUnion merges the two TaskStates (collaborative records unioned by ID/key;
	// TrustResults upgrades to the monotonic sync variant) — the cross-machine sync
	// policy: neither side's history is thrown away.
	//
	// TaskUnion 合并两侧 TaskState（协作记录按 ID/键并集；TrustResults 升级为
	// 单调 sync 变体）——跨机同步策略：任一侧的历史都不丢弃。
	TaskUnion
	// TaskSkip leaves tasks/*.json untouched entirely — the caller handles task
	// merging itself (e.g. project import's same-key path merges under per-task
	// locks in the command layer).
	//
	// TaskSkip 完全不动 tasks/*.json——调用方自己处理任务合并（如 project import
	// 的 same-key 路径在命令层按 per-task 锁合并）。
	TaskSkip
)

// Options parameterizes Dirs. The zero value is the legacy rekey behavior.
//
// Options 参数化 Dirs。零值即 legacy rekey 行为。
type Options struct {
	// DryRun plans every action without touching disk.
	//
	// DryRun 只规划不落盘。
	DryRun bool
	// DedupExactLines drops byte-identical duplicate lines after the JSONL timestamp
	// merge. Sound for append-only single-writer logs (checklog/toollog/sessions are
	// json.Marshal-serialized whole records): the same event re-exported is the same
	// bytes, so a full-history bundle re-imported cannot duplicate events.
	//
	// DedupExactLines 在 JSONL 时间戳合并后剔除字节级重复行。对 append-only 单写者
	// 日志（checklog/toollog/sessions 是 json.Marshal 序列化的整条记录）可靠：同一
	// 事件重复导出即同一字节，全量历史 bundle 重导入不会翻倍事件。
	DedupExactLines bool
	// TaskPolicy selects the tasks/*.json conflict policy.
	//
	// TaskPolicy 选择 tasks/*.json 冲突策略。
	TaskPolicy TaskConflictPolicy
	// TrustResults modifies TaskUnion: true uses MergeTaskStateSync (monotonic,
	// preserves foreign result/completion fields — same-identity sync); false uses
	// MergeTaskState (local authority, never downgraded — untrusted sources).
	//
	// TrustResults 修饰 TaskUnion：true 用 MergeTaskStateSync（单调，保留外来结果/
	// 完成字段——同身份同步）；false 用 MergeTaskState（本地权威不被降级——不可信源）。
	TrustResults bool
	// MergeConclusions admits act/conclusions.jsonl into the timestamp-merge set.
	// OFF by default: legacy `registry rekey` treated conclusions.jsonl as a plain
	// file (conflict keeps the to-side) — keeping that ZERO-semantic-change
	// contract for rekey while project import (DedupExactLines on) merges it.
	//
	// MergeConclusions 把 act/conclusions.jsonl 纳入时间戳合并集。默认关：
	// legacy `registry rekey` 把 conclusions.jsonl 当普通文件（冲突保 to 侧）——
	// 为 rekey 保持零语义变化，而 project import（开 DedupExactLines）合并它。
	MergeConclusions bool
	// NoFromBackup replaces the final whole-dir backup move with a REMOVAL of the
	// from-dir. Use ONLY when from is disposable (import staging / a copy of the
	// bundle dir): the rollback guarantee is then the bundle file in the user's
	// hand, and a .rekey-backup inside the live DataDir would just accumulate
	// bundle copies. Skipped conflict copies are NOT preserved under this option.
	//
	// NoFromBackup 把最后的整目录备份搬移替换为删除 from 目录。仅当 from 一次性
	// （导入 staging / bundle 目录副本）时使用：回滚保障是用户手里的 bundle 原件，
	// 而活 DataDir 里的 .rekey-backup 只会堆积 bundle 副本。此选项下被跳过的冲突
	// 副本不保留。
	NoFromBackup bool
}

// Dirs performs (or plans, when DryRun) the from→to directory merge and returns the
// human-readable action log. fromDir must exist; toDir is created when missing.
//
// Anchor files (active-task-ref-*, session.json) take the plain move/skip path — they
// are moved only, never merged (a live session must not be disturbed), same as rekey.
//
// Dirs 执行（DryRun 时仅规划）from→to 目录合并，返回人类可读动作日志。fromDir 必须存在；toDir 缺失时创建。
//
// 锚文件（active-task-ref-*、session.json）走普通搬移/跳过路径——只搬不并（不扰动
// 活会话），与 rekey 一致。
func Dirs(fromDir, toDir string, opts Options) ([]string, error) {
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
		// filepath.Rel returns OS separators — \ on Windows — while the merge-map
		// classification (isTaskFile) and the action messages are slash-path based.
		// Normalize once here so both platforms agree (filepath.Join below tolerates
		// slash components on Windows).
		//
		// filepath.Rel 返回 OS 分隔符——Windows 上是 \——而合并映射的分类
		// （isTaskFile）与动作消息都基于斜杠路径。在此归一一次，让两平台一致
		// （下面的 filepath.Join 在 Windows 上容忍斜杠分量）。
		rel = filepath.ToSlash(rel)
		dst := filepath.Join(toDir, rel)
		base := filepath.Base(src)
		_, dstExists := os.Stat(dst)

		switch {
		case isTaskFile(rel) && opts.TaskPolicy == TaskSkip:
			actions = append(actions, fmt.Sprintf(`skip   %s（TaskSkip：由调用方处理）`, rel))
		case isTaskFile(rel) && dstExists == nil && opts.TaskPolicy == TaskUnion:
			// Both sides have the task: state merge under the selected trust posture.
			//
			// 两侧都有该任务：按所选信任姿态做状态合并。
			action, merr := mergeTaskFile(dst, src, opts)
			if merr != nil {
				// A corrupt/unparseable task file degrades to the safe legacy
				// behavior (keep the to-side) instead of failing the whole merge —
				// the from copy survives when NoFromBackup is unset.
				//
				// 损坏/不可解析的任务文件降级到安全的 legacy 行为（保 to 侧）而非
				// 让整个合并失败——NoFromBackup 未设时 from 副本仍保留。
				actions = append(actions, fmt.Sprintf(`skip   %s（任务合并失败，保留 to 侧: %v）`, rel, merr))
			} else {
				actions = append(actions, action)
			}
		case isTaskFile(rel) && dstExists != nil && opts.TaskPolicy == TaskUnion:
			// Task exists only on the from side under TaskUnion: it must still be
			// validated as a TaskState (a bundle-crafted garbage file must not enter
			// tasks/ verbatim through the union policy) — round-trip it.
			//
			// TaskUnion 下任务只在 from 侧：仍须校验它是 TaskState（union 策略不得
			// 让 bundle 伪造的垃圾文件逐字进 tasks/）——做往返校验。
			action, merr := adoptTaskFile(dst, src, opts)
			if merr != nil {
				actions = append(actions, fmt.Sprintf(`skip   %s（任务校验失败，不并入: %v）`, rel, merr))
			} else {
				actions = append(actions, action)
			}
		case dstExists == nil && isMergeable(base, opts):
			// Both sides have the log: stable timestamp-ordered merge (+ dedup).
			//
			// 两侧都有该日志：按时间戳稳定有序合并（+ 去重）。
			merged, merr := MergeJSONLFiles(dst, src, opts.DedupExactLines)
			if merr != nil {
				return fmt.Errorf(`合并 %s: %w`, rel, merr)
			}
			note := `按行内时间戳有序合并`
			if opts.DedupExactLines {
				note += `，精确行去重`
			}
			actions = append(actions, fmt.Sprintf(`merge  %s（%s）`, rel, note))
			if !opts.DryRun {
				// Atomic write: an in-place truncate+overwrite of the to-side main
				// log would corrupt it irrecoverably on crash mid-write (the to-side
				// original has no other backup when NoFromBackup skips the backup).
				//
				// 原子写：原地截断覆盖 to 侧主日志，崩溃中途会不可恢复地损坏
				// （NoFromBackup 跳过备份时 to 侧原件没有其他副本）。
				if werr := util.AtomicWrite(dst, merged, 0644); werr != nil {
					return werr
				}
			}
		case dstExists == nil:
			// Conflict: to-side wins; the from-side copy is preserved by the final
			// whole-dir backup move (unless NoFromBackup).
			//
			// 冲突：保留 to 侧；from 副本由最后的整目录备份搬移保留
			// （NoFromBackup 除外）。
			actions = append(actions, fmt.Sprintf(`skip   %s（to 侧已存在，保留 to）`, rel))
		default:
			// No conflict: move.
			//
			// 无冲突：搬移。
			actions = append(actions, fmt.Sprintf(`move   %s`, rel))
			if !opts.DryRun {
				if merr := os.MkdirAll(filepath.Dir(dst), 0755); merr != nil {
					return merr
				}
				if merr := MoveFile(src, dst); merr != nil {
					return fmt.Errorf(`搬移 %s: %w`, rel, merr)
				}
			}
		}
		return nil
	})
	if err != nil {
		return actions, err
	}

	if opts.DryRun {
		return actions, nil
	}

	if opts.NoFromBackup {
		// From is disposable (staging / bundle copy): remove the remnants outright.
		// The rollback guarantee is the bundle file itself.
		//
		// from 一次性（staging / bundle 副本）：直接删除残余。回滚保障是 bundle
		// 原件本身。
		actions = append(actions, fmt.Sprintf(`remove %s（NoFromBackup：一次性源）`, fromDir))
		if rerr := os.RemoveAll(fromDir); rerr != nil {
			return actions, fmt.Errorf(`清理一次性 from 目录失败: %w`, rerr)
		}
		return actions, nil
	}

	// Whole-dir backup: whatever was skipped (conflicts, emptied dirs) moves into
	// <to>/.rekey-backup-<ts>/ — nothing is deleted outright.
	//
	// 整目录备份：所有跳过的（冲突文件、空目录）移入 <to>/.rekey-backup-<ts>/
	// ——不直接删除任何东西。
	backup := filepath.Join(toDir, `.rekey-backup-`+time.Now().Format(`20060102-150405`))
	actions = append(actions, fmt.Sprintf(`backup %s → %s`, fromDir, backup))
	if err := os.MkdirAll(toDir, 0755); err != nil {
		return actions, err
	}
	if err := os.Rename(fromDir, backup); err != nil {
		return actions, fmt.Errorf(`from 目录移入备份失败: %w`, err)
	}
	return actions, nil
}

// isTaskFile reports whether rel (slash-separated, relative to the data dir root)
// names a task state file under tasks/. *.lock files are NOT task files (they are
// per-task lock residue, not state).
//
// isTaskFile 判断 rel（相对数据目录根的斜杠路径）是否 tasks/ 下的任务状态文件。
// *.lock 不是任务文件（是 per-task 锁残留，不是状态）。
func isTaskFile(rel string) bool {
	if !strings.HasPrefix(rel, `tasks/`) {
		return false
	}
	return strings.HasSuffix(rel, `.json`)
}

// mergeTaskFile merges the from-side TaskState into the to-side file under the
// policy's trust posture and atomically rewrites the to-side file.
//
// mergeTaskFile 按策略信任姿态把 from 侧 TaskState 合并进 to 侧文件并原子重写。
func mergeTaskFile(dstPath, srcPath string, opts Options) (string, error) {
	local, err := loadTaskFile(dstPath)
	if err != nil {
		return ``, fmt.Errorf(`读 to 侧任务: %w`, err)
	}
	incoming, err := loadTaskFile(srcPath)
	if err != nil {
		return ``, fmt.Errorf(`读 from 侧任务: %w`, err)
	}
	if local.TaskRef != incoming.TaskRef {
		// Same file name must mean same ref — a mismatch means one side was renamed
		// by hand; keep both stories apart by refusing the merge (keep to-side).
		//
		// 同名文件必须同 ref——不一致说明有一侧被手改过改名；拒绝合并保住两侧
		// 故事的边界（保 to 侧）。
		return ``, fmt.Errorf(`task_ref 不一致（to=%q from=%q）`, local.TaskRef, incoming.TaskRef)
	}
	if opts.TrustResults {
		taskpipeline.MergeTaskStateSync(local, incoming)
	} else {
		taskpipeline.MergeTaskState(local, incoming)
	}
	if err := writeTaskFile(dstPath, local); err != nil {
		return ``, err
	}
	variant := `并集`
	if opts.TrustResults {
		variant = `单调同步`
	}
	return fmt.Sprintf(`merge-task %s（%s）`, filepath.Base(srcPath), variant), nil
}

// adoptTaskFile validates a from-only task as a real TaskState and moves it in
// (round-trip: bundle-crafted garbage never enters tasks/ verbatim under TaskUnion).
//
// adoptTaskFile 校验仅 from 侧有的任务是真实 TaskState 后搬入（往返校验：union
// 策略下 bundle 伪造的垃圾文件绝不逐字进 tasks/）。
func adoptTaskFile(dstPath, srcPath string, _ Options) (string, error) {
	st, err := loadTaskFile(srcPath)
	if err != nil {
		return ``, err
	}
	if st.TaskRef == `` {
		return ``, fmt.Errorf(`task_ref 为空`)
	}
	if err := writeTaskFile(dstPath, st); err != nil {
		return ``, err
	}
	if err := os.Remove(srcPath); err != nil {
		return ``, err
	}
	return fmt.Sprintf(`move   tasks/%s（已校验 TaskState）`, filepath.Base(srcPath)), nil
}

// loadTaskFile / writeTaskFile mirror taskpipeline.Save/LoadTaskState's plain JSON
// contract (MarshalIndent 2-space + AtomicWrite) but operate on explicit paths —
// Dirs merges arbitrary dirs (staging, another key's DataDir), not a project root.
//
// loadTaskFile / writeTaskFile 镜像 taskpipeline.Save/LoadTaskState 的纯 JSON 契约
// （MarshalIndent 两空格 + AtomicWrite），但操作显式路径——Dirs 合并的是任意目录
// （staging、另一 key 的 DataDir），不是项目根。
func loadTaskFile(path string) (*taskpipeline.TaskState, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var s taskpipeline.TaskState
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, fmt.Errorf(`解析 %s: %w`, filepath.Base(path), err)
	}
	return &s, nil
}

func writeTaskFile(path string, s *taskpipeline.TaskState) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(s, ``, `  `)
	if err != nil {
		return err
	}
	return util.AtomicWrite(path, append(data, '\n'), 0644)
}

// IsMergeableJSONL reports whether a file name is an event log that should be
// timestamp-merged rather than moved — the LEGACY rekey set: checklog*.jsonl /
// toollog*.jsonl (rotated suffixes included) and sessions.jsonl. conclusions.jsonl
// joins only via Options.MergeConclusions (project import); it stays a plain
// move/skip file for rekey, exactly as before the extraction.
//
// IsMergeableJSONL 判定文件名是否是应按时间戳合并（而非搬移）的事件日志——
// legacy rekey 集合：checklog*.jsonl / toollog*.jsonl（含 rotated 后缀）与
// sessions.jsonl。conclusions.jsonl 仅经 Options.MergeConclusions（project
// import）加入；对 rekey 它保持抽包前的普通搬移/跳过文件行为。
func IsMergeableJSONL(base string) bool {
	if base == `sessions.jsonl` {
		return true
	}
	if !strings.HasSuffix(base, `.jsonl`) {
		return false
	}
	return strings.HasPrefix(base, `checklog`) || strings.HasPrefix(base, `toollog`)
}

// isMergeable is the options-aware mergeability probe: the legacy set (what
// `registry rekey` has always merged) plus conclusions.jsonl ONLY when the caller
// opted in via MergeConclusions (project import). Rekey's zero-value Options thus
// keep its exact pre-extraction behavior.
//
// isMergeable 是 options 感知的可合并探测：legacy 集合（`registry rekey` 一直
// 合并的）+ 仅当调用方经 MergeConclusions 选入的 conclusions.jsonl（project
// import）。rekey 的零值 Options 因此保持抽包前的精确行为。
func isMergeable(base string, opts Options) bool {
	if opts.MergeConclusions && base == `conclusions.jsonl` {
		return true
	}
	return IsMergeableJSONL(base)
}

// timestampFields are the JSONL line fields probed for the merge-order timestamp,
// covering the known event schemas: checklog (recorded_at), toollog (timestamp),
// sessions (started_at), hazards (ts), dashboard feed (time), act conclusions
// (completed_at/created_at).
//
// timestampFields 是合并排序探测的 JSONL 行时间戳字段，覆盖已知事件 schema：
// checklog（recorded_at）、toollog（timestamp）、sessions（started_at）、hazards
// （ts）、dashboard feed（time）、act conclusions（completed_at/created_at）。
var timestampFields = []string{`recorded_at`, `timestamp`, `ts`, `time`, `started_at`, `created_at`, `completed_at`}

// MergeJSONLFiles reads both JSONL files and returns their lines stably sorted by
// the in-line timestamp. Events are never lost or reordered: lines carry a
// carry-forward timestamp (a line without one inherits its predecessor's, so
// header/context lines stay attached to their event), and equal timestamps keep the
// to-side-before-from-side original order (stable sort). dedup drops byte-identical
// duplicate lines after sorting (append-only logs: same event = same bytes).
//
// MergeJSONLFiles 读两个 JSONL 文件，返回按行内时间戳稳定排序后的行。事件不丢、
// 不重排：行用携带式时间戳（无时间戳的行继承前一行的，让头部/上下文行贴着所属
// 事件），时间戳相等时保持 to 侧在前、from 侧在后的原顺序（稳定排序）。dedup 在
// 排序后剔除字节级重复行（append-only 日志：同一事件 = 同一字节）。
func MergeJSONLFiles(dstPath, srcPath string, dedup bool) ([]byte, error) {
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
		// inherits its OWN file's predecessor, never the other file's tail.
		//
		// 携带式继承在文件边界重置：开头无时间戳的行继承本文件内的前驱，绝不
		// 继承另一文件的末尾。
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
	seen := map[string]bool{}
	for _, l := range lines {
		if dedup {
			if seen[l.raw] {
				continue
			}
			seen[l.raw] = true
		}
		b.WriteString(l.raw)
		b.WriteByte('\n')
	}
	return []byte(b.String()), nil
}

// jsonlLineTimestamp extracts the event timestamp from one JSONL line by probing the
// known schema fields (timestampFields). Unparseable/missing → ok=false; the caller
// falls back to carry-forward.
//
// jsonlLineTimestamp 按已知 schema 字段（timestampFields）从一行 JSONL 提取事件
// 时间戳。无法解析/缺失 → ok=false；调用方回落携带式继承。
func jsonlLineTimestamp(line string) (time.Time, bool) {
	var m map[string]json.RawMessage
	if json.Unmarshal([]byte(line), &m) != nil {
		return time.Time{}, false
	}
	for _, f := range timestampFields {
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

// MoveFile renames src to dst, falling back to copy+remove across devices. The
// fallback writes dst atomically (temp+rename in dst's dir): staging often lives on
// the OS temp volume while DataDir is elsewhere, and a truncate-write interrupted
// mid-copy would leave a corrupted destination.
//
// MoveFile 把 src rename 到 dst，跨设备时回落 copy+remove。回退路径用原子写
// （dst 目录下 temp+rename）：staging 常在系统临时卷而 DataDir 在别处，截断式
// 写中途被打断会留下损坏的目标文件。
func MoveFile(src, dst string) error {
	if err := os.Rename(src, dst); err == nil {
		return nil
	}
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	if err := util.AtomicWrite(dst, data, 0644); err != nil {
		return err
	}
	return os.Remove(src)
}
