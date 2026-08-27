package checklog

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"time"

	"github.com/MjxUpUp/Forge/internal/forgedata"
	"github.com/MjxUpUp/Forge/internal/nodestamp"
	"github.com/MjxUpUp/Forge/internal/util"
)

var mu sync.Mutex

// filePath returns the active checklog path under the resolved runtime-state directory,
// which is ALWAYS user-level (git: ~/.forge/projects/<key>/; non-git:
// ~/.forge/projects/<path-key>/) — nothing is written into the project tree. See dataDir.
//
// filePath 返回解析后的 runtime-state 目录下的 active checklog 路径——始终用户级
// （git：~/.forge/projects/<key>/；非 git：~/.forge/projects/<path-key>/），
// 不写项目树。见 dataDir。
func filePath(root string) string {
	return filepath.Join(dataDir(root), "checklog.jsonl")
}

// dataDir resolves the runtime-state directory for checklog via the shared forgedata.DataDirFor
// (always user-level: git Key → ~/.forge/projects/<key>/, non-git PathKey →
// ~/.forge/projects/<path-key>/). The load-bearing rationale is in forgedata.DataDirFor
// (a MkdirAll-stable resolution — Record must not switch paths mid-write).
//
// dataDir 通过共享的 forgedata.DataDirFor 解析 checklog 的 runtime-state 目录
// （始终用户级：git Key → ~/.forge/projects/<key>/，非 git PathKey →
// ~/.forge/projects/<path-key>/）。load-bearing 依据见 forgedata.DataDirFor
// （MkdirAll-stable 的解析——Record 不得在写入中途切换路径）。
func dataDir(root string) string { return forgedata.DataDirFor(root) }

// Record appends a check log entry to DataDir's checklog.jsonl (always user-level,
// see dataDir). It sets RecordedAt to the current time. Thread-safe.
//
// Record 向 DataDir 的 checklog.jsonl 追加一条 check log entry（始终用户级，
// 见 dataDir）。把 RecordedAt 设为当前时间。线程安全。
func Record(root string, entry *Entry) error {
	mu.Lock()
	defer mu.Unlock()

	entry.RecordedAt = time.Now()
	// Machine-attribution stamp (node-identity.md §4): only when the caller left it
	// zero — import/merge paths carry the ORIGIN node's stamp and must keep it.
	//
	// 机器归因戳（node-identity.md §4）：仅当调用方留零值时落章——import/merge
	// 路径携带的是源节点戳，必须保留。
	if entry.Stamp == (nodestamp.Stamp{}) {
		entry.Stamp = nodestamp.Next()
	}
	// Fallback inference for evidence source: when the caller does not explicitly set Source, assign a default by CheckName.
	// This lets legacy recording points (unchanged) also carry Source automatically, leaving no gaps in evidence-chain bucketing.
	//
	// 兜底推断证据来源：调用方未显式标注 Source 时，按 CheckName 给默认值。
	// 让历史记录点（未改）也自动带上 Source，证据链分桶不留空白。
	if entry.Source == "" {
		entry.Source = SourceForCheck(entry.Check)
	}
	// Fallback inference for severity level: when the caller does not explicitly set
	// Level, derive it from Passed + Detail prefixes (BLOCKED: / ADVISORY:), same
	// pattern as the Source fallback above. Explicit Level always wins.
	//
	// 兜底推断级别：调用方未显式设置 Level 时，从 Passed + Detail 前缀
	// （BLOCKED: / ADVISORY:）推导，与上方 Source 兜底同款模式。显式 Level 恒优先。
	if entry.Level == "" {
		entry.Level = DeriveLevel(entry)
	}

	path := filePath(root)
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}

	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	defer f.Close()

	data, err := json.Marshal(entry)
	if err != nil {
		return err
	}
	_, err = f.Write(append(data, '\n'))
	return err
}

// AppendEntries writes pre-built entries (carried in by a cross-machine task import) to the active
// checklog.jsonl, preserving each entry's original fields — notably RecordedAt is NOT rewritten, so
// imported evidence keeps its real source-machine timing in `forge trace`. Empty Source/Level are
// filled by the same fallback as Record (caller-set values win), so a legacy / hand-constructed
// entry buckets consistently with a locally-Recorded one. NOTE: the fallback is written back to the
// caller's slice in place (entries[i].Source/Level), so the input slice is mutated — callers that
// reuse the slice after the call will see the filled values. Lock-protected like Record. Entries
// are appended without de-dup: re-importing the same bundle duplicates lines — the caller controls
// that. A nil/empty slice is a no-op.
//
// AppendEntries 把预构建的条目（跨机器 task import 带入）写入 active checklog.jsonl，保留每条原
// 字段——特别是 RecordedAt 不重写，使导入证据在 forge trace 里保留真实源机器时序。注意：兜底值会写回
// 调用方切片本身（entries[i].Source/Level），即输入切片被原地修改——调用后再复用切片会看到填充后的值。
// 同 Record 加锁。条目原样追加（不去重）：重复 import 同一 bundle 会重复行——由调用方控制。nil/空切片
// 为 no-op。
func AppendEntries(root string, entries []Entry) error {
	if len(entries) == 0 {
		return nil
	}
	mu.Lock()
	defer mu.Unlock()

	path := filePath(root)
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	defer f.Close()
	for i := range entries {
		// Same Source/Level fallback as Record: an imported entry that never had Source/Level set
		// (legacy / hand-constructed) would otherwise land with empty fields and bucket inconsistently
		// vs a locally-Recorded entry of the same shape. Caller-set values always win (empty here
		// means MISSING, not an original value to preserve). RecordedAt is still NOT rewritten, so
		// imported evidence keeps its real source-machine timing in forge trace.
		//
		// 同 Record 的 Source/Level 兜底：导入条目若从未设过 Source/Level（legacy/手工构造）会以空字段
		// 落盘，与本地 Record 的同形条目分桶不一致。调用方设过的值恒优先（此处的空=缺失，非须保留的
		// 原值）。RecordedAt 仍不重写，导入证据在 forge trace 里保留真实源机器时序。
		if len(entries[i].Source) == 0 {
			entries[i].Source = SourceForCheck(entries[i].Check)
		}
		if len(entries[i].Level) == 0 {
			entries[i].Level = DeriveLevel(&entries[i])
		}
		data, err := json.Marshal(entries[i])
		if err != nil {
			return err
		}
		if _, err := f.Write(append(data, '\n')); err != nil {
			return err
		}
	}
	return nil
}

// LoadAll reads all check log entries from DataDir's checklog.jsonl (always
// user-level). Returns entries in chronological order. Returns nil if the file does not exist.
//
// LoadAll 从 DataDir 的 checklog.jsonl 读取全部 check log entry（始终用户级）。
// 按时间顺序返回。文件不存在时返回 nil。
func LoadAll(root string) ([]Entry, error) {
	f, err := os.Open(filePath(root))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer f.Close()

	var entries []Entry
	scanner := bufio.NewScanner(f)
	// Enlarged line cap (1MB, same as toolusage.LoadAll): entries can carry long Detail
	// payloads; the default 64KB cap would make one oversized line fail scoring/trace wholesale.
	//
	// 放大的单行上限（1MB，同 toolusage.LoadAll）：条目可能带长 Detail 载荷，
	// 默认 64KB 上限会让一条超限行拖垮 scoring/trace 全链路。
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		var e Entry
		if err := json.Unmarshal(scanner.Bytes(), &e); err != nil {
			// Skip malformed lines.
			continue // 跳过格式错误的行
		}
		entries = append(entries, e)
	}
	return entries, scanner.Err()
}

// loadAllArchives reads every entry from the active checklog.jsonl and all archived checklog-*.jsonl,
// in chronological order. Shared core of LoadAllAll (no filter) and LoadForTask (task filter): the glob
// matches checklog*.jsonl (active checklog.jsonl included — * matches empty), so active + archived history
// is read in one pass then sorted by RecordedAt. A glob-matched file that cannot be opened is a read failure,
// not "no data"; a scanner error (oversized line >1MB, I/O error) surfaces instead of silently truncating.
//
// loadAllArchives 从 active checklog.jsonl 与所有归档 checklog-*.jsonl 读取全部条目，按时间序。
// 是 LoadAllAll（无过滤）与 LoadForTask（task 过滤）的共用核心：glob 匹配 checklog*.jsonl
// （active checklog.jsonl 也命中——* 可为空），故 active + 归档历史一次读全，再按 RecordedAt 排序。
// glob 命中的文件打不开是读失败、不是「无数据」；scanner 出错（超 1MB 行、I/O 错误）显式上抛而非静默截断。
func loadAllArchives(root string) ([]Entry, error) {
	matches, err := filepath.Glob(filepath.Join(dataDir(root), "checklog*.jsonl"))
	if err != nil {
		return nil, err
	}
	var entries []Entry
	for _, path := range matches {
		f, err := os.Open(path)
		if err != nil {
			// A glob-matched file that cannot be opened is a read failure, not "no data" —
			// surface it instead of silently dropping that file's history.
			//
			// glob 命中的文件打不开是读失败、不是「无数据」——显式报错，
			// 不静默丢弃该文件的历史。
			return nil, fmt.Errorf("open %s: %w", path, err)
		}
		scanner := bufio.NewScanner(f)
		// 1MB line cap, same as LoadAll (see there for rationale).
		//
		// 1MB 单行上限，同 LoadAll（理由见该处）。
		scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
		for scanner.Scan() {
			var e Entry
			if err := json.Unmarshal(scanner.Bytes(), &e); err != nil {
				// Skip malformed lines.
				continue // 跳过格式错误的行
			}
			entries = append(entries, e)
		}
		// A scanner error (oversized line beyond 1MB, I/O error) means everything after
		// that point in the file was silently truncated — make it explicit instead of
		// returning a partial set as if complete.
		//
		// scanner 出错（超 1MB 的行、I/O 错误）意味着该行之后的内容被静默截断——
		// 显式报错，不把残缺结果当完整返回。
		serr := scanner.Err()
		f.Close()
		if serr != nil {
			return nil, fmt.Errorf("read %s: %w", path, serr)
		}
	}
	slices.SortFunc(entries, func(a, b Entry) int {
		return a.RecordedAt.Compare(b.RecordedAt)
	})
	return entries, nil
}

// LoadAllAll reads all entries from the active checklog.jsonl AND every archived checklog-*.jsonl
// (chronological). Cross-archive counterpart to LoadAll (active-only): forge task start archives the
// previous checklog, so LoadAll sees only the current task — consumers aggregating across the whole project
// history (e.g. skillseval usage reading CheckSkillTrigger across all past tasks) need this. Mirrors
// toolusage.LoadAllAll. Returns nil if nothing exists yet.
//
// LoadAllAll 从 active checklog.jsonl 与所有归档 checklog-*.jsonl 读取全部条目（时间序）。
// 是 LoadAll（仅 active）的跨归档对应：forge task start 归档上一份 checklog，LoadAll 只能看到
// 当前任务——跨整个项目历史聚合的消费者（如 skillseval usage 跨所有历史 task 读 CheckSkillTrigger）
// 需要本函数。对称 toolusage.LoadAllAll。尚无任何文件时返回 nil。
func LoadAllAll(root string) ([]Entry, error) {
	return loadAllArchives(root)
}

// LoadForTask filters by task ref across the active checklog and all archived checklog-*.jsonl,
// returning matches in chronological order. Used by `forge trace <ref>` to reconstruct a task's full event
// timeline. Built on loadAllArchives (active + archived in one pass); entries with mismatched TaskRef are excluded.
//
// LoadForTask 按 task ref 跨 active checklog 与所有归档 checklog-*.jsonl 过滤，按时间序返回命中。
// 供 forge trace <ref> 重建 task 完整事件时间线。基于 loadAllArchives（active + 归档一次读全）；
// TaskRef 不一致的条目被排除。
func LoadForTask(root, taskRef string) ([]Entry, error) {
	all, err := loadAllArchives(root)
	if err != nil {
		return nil, err
	}
	out := make([]Entry, 0, len(all))
	for _, e := range all {
		if e.TaskRef == taskRef {
			out = append(out, e)
		}
	}
	return out, nil
}

// LatestByCheckForSession returns the latest entry per check name, scoped to the given session.
//
// Filtering rules (preventing scoring pollution when two Claude Code sessions run concurrently on a shared checkout):
//   - sessionID empty (legacy / no session): no filtering — every entry counts.
//   - sessionID non-empty: entries with non-empty SessionID that differs from sessionID are excluded.
//     Entries with empty SessionID (global/legacy) are always retained so globally-applicable checks can still register.
//
// LatestByCheckForSession 返回限定在给定 session 内、每个 check name 的最新条目。
//
// 过滤规则（防止两个 Claude Code session 在共享 checkout 上并发时评分被对端污染）：
//   - sessionID 为空（legacy / 无 session）：不过滤——每条都计入。
//   - sessionID 非空：SessionID 非空且与 sessionID 不同的条目被排除。
//     SessionID 为空（全局/legacy）的条目始终保留，让全局适用的 check 仍能登记。
func LatestByCheckForSession(root, sessionID string) (map[CheckName]*Entry, error) {
	entries, err := LoadAll(root)
	if err != nil {
		return nil, err
	}

	result := make(map[CheckName]*Entry)
	for i := range entries {
		e := &entries[i]
		if sessionID != "" && e.SessionID != "" && e.SessionID != sessionID {
			continue
		}
		if existing, ok := result[e.Check]; !ok || e.RecordedAt.After(existing.RecordedAt) {
			result[e.Check] = e
		}
	}
	return result, nil
}

// archiveLocked renames the existing checklog to a timestamped backup but does **not** acquire the lock; the caller must hold mu
// (the same mutex as Record, so concurrent entry appends and rotations do not interleave — callers Clear/…HoldMu variants serialize).
// Uses nanosecond-precision naming (util.ArchivedName) so multiple rotations within the same second do not collide.
//
// archiveLocked 把现存 checklog 重命名为带时间戳的备份，但**不**加锁；调用方必须持有 mu
// （与 Record 同一把 mutex，使并发的 entry 追加与轮转不会交错）。用纳秒精度命名（util.ArchivedName），
// 同一秒内的多次轮转不会撞名。
func archiveLocked(root string) error {
	src := filePath(root)
	if _, err := os.Stat(src); os.IsNotExist(err) {
		return nil
	}
	dst := util.ArchivedName(filepath.Dir(src), "checklog", time.Now())
	return os.Rename(src, dst)
}

// Clear deletes the check log file after archiving. Called at task start. Both archiving and deletion run inside the mutex
// so no Record append can happen between them. After archiving and deleting the active file, it makes a best-effort
// cleanup of archives beyond the retention window to prevent checklog-*.jsonl from growing unbounded across task starts.
//
// Clear 在归档后删除 check log 文件。在 task 启动时调用。归档与删除都在 mutex 内
// 执行，使两者之间不会有 Record 追加。归档+删除 active 文件后，尽力清理超出 retention
// 窗口的归档，避免 checklog-*.jsonl 跨 task 启动无限增长。
func Clear(root string) error {
	mu.Lock()
	defer mu.Unlock()
	if err := archiveLocked(root); err != nil {
		return err
	}
	if err := os.Remove(filePath(root)); err != nil && !os.IsNotExist(err) {
		return err
	}
	pruneArchives(filepath.Dir(filePath(root)))
	return nil
}

// pruneArchives deletes checklog-*.jsonl archives beyond the retention window
// (FORGE_LOG_RETENTION_DAYS, default 30; <=0 disables). Best-effort: PruneArchives only globs
// checklog-*.jsonl (never touching the active file checklog.jsonl — it lacks the dash the glob requires),
// so it cannot race with concurrent Record (which only writes the active file). Calling it within Clear's mutex is purely
// to make rotation+cleanup atomic in intent; failures here do not affect the primary outcome of Clear.
//
// pruneArchives 删除超过 retention 窗口的 checklog-*.jsonl 归档
// （FORGE_LOG_RETENTION_DAYS，默认 30；≤0 禁用）。尽力而为：PruneArchives 只 glob
// checklog-*.jsonl（绝不碰 active 文件 checklog.jsonl——它没有 glob 要求的那个 dash），
// 故不会与并发 Record（只写 active 文件）竞态。在 Clear 的 mutex 内调用纯粹是为了让
// 轮转+清理在意图上原子；此处的失败不影响 Clear 的主要结果。
func pruneArchives(dir string) {
	days := util.RetentionDays("FORGE_LOG_RETENTION_DAYS", 30)
	if days <= 0 {
		return
	}
	_, _ = util.PruneArchives(dir, "checklog", time.Now().AddDate(0, 0, -days))
}

// Prune is the non-destructive half of Clear: retention cleanup of checklog-*.jsonl
// archives only, never touching the active file. This is the replacement task start calls
// since Clear was retired there (multi-task-concurrency design §5): the log is now an
// append-only timeline segmented by task-started boundary events, so starting a task must
// not archive-or-delete anything — only keep the retention window bounded.
//
// Prune 是 Clear 的非破坏性半边：只做 checklog-*.jsonl 归档的 retention 清理，绝不碰
// active 文件。task start 处 Clear 退役后（multi-task-concurrency 设计 §5）改调本函数：
// 日志现在是按 task-started 边界事件分段的 append-only 时间线，开任务不得归档或删除
// 任何东西——只保持 retention 窗口有界。
func Prune(root string) {
	mu.Lock()
	defer mu.Unlock()
	pruneArchives(filepath.Dir(filePath(root)))
}
