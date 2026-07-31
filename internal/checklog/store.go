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
	"github.com/MjxUpUp/Forge/internal/util"
)

var mu sync.Mutex

// filePath returns the active checklog path under the resolved runtime-state directory: git projects use
// the user-level DataDir (~/.forge/projects/<key>/); non-git forge projects fall back to the legacy
// project-level <root>/.forge/ so hooks can still record checks. See dataDir.
//
// filePath 返回解析后的 runtime-state 目录下的 active checklog 路径：git 项目用
// 用户级 DataDir（~/.forge/projects/<key>/），非 git 的 forge 项目回退到 legacy 的
// 项目级 <root>/.forge/，让 hook 仍能记录 check。见 dataDir。
func filePath(root string) string {
	return filepath.Join(dataDir(root), "checklog.jsonl")
}

// dataDir resolves the runtime-state directory for checklog via the shared forgedata.DataDirFor
// (only git projects use Key → ~/.forge/projects/<key>/, falling back to <root>/.forge/).
// The load-bearing rationale for `git-only uses Key` is in forgedata.DataDirFor (a MkdirAll-stable
// resolution — Record must not switch paths mid-write).
//
// dataDir 通过共享的 forgedata.DataDirFor 解析 checklog 的 runtime-state 目录
// （仅 git 项目用 Key → ~/.forge/projects/<key>/，回退 <root>/.forge/）。
// load-bearing 的「仅 git 用 Key」依据见 forgedata.DataDirFor（MkdirAll-stable 的
// 解析——Record 不得在写入中途切换路径）。
func dataDir(root string) string { return forgedata.DataDirFor(root) }

// Record appends a check log entry to DataDir's checklog.jsonl (non-git projects fall back to
// <root>/.forge/, see dataDir). It sets RecordedAt to the current time. Thread-safe.
//
// Record 向 DataDir 的 checklog.jsonl 追加一条 check log entry（非 git 项目回退到
// <root>/.forge/，见 dataDir）。把 RecordedAt 设为当前时间。线程安全。
func Record(root string, entry *Entry) error {
	mu.Lock()
	defer mu.Unlock()

	entry.RecordedAt = time.Now()
	// Fallback inference for evidence source: when the caller does not explicitly set Source, assign a default by CheckName.
	// This lets legacy recording points (unchanged) also carry Source automatically, leaving no gaps in evidence-chain bucketing.
	//
	// 兜底推断证据来源：调用方未显式标注 Source 时，按 CheckName 给默认值。
	// 让历史记录点（未改）也自动带上 Source，证据链分桶不留空白。
	if entry.Source == "" {
		entry.Source = SourceForCheck(entry.Check)
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

// LoadAll reads all check log entries from DataDir's checklog.jsonl (non-git projects fall back to
// <root>/.forge/). Returns entries in chronological order. Returns nil if the file does not exist.
//
// LoadAll 从 DataDir 的 checklog.jsonl 读取全部 check log entry（非 git 项目回退到
// <root>/.forge/）。按时间顺序返回。文件不存在时返回 nil。
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

// LoadForTask filters by task ref and reads check entries from the active checklog and all archived checklog-*.jsonl,
// returning them in chronological order. Used by `forge trace <ref>` to reconstruct a task's full event timeline.
// LoadAll only reads the active checklog.jsonl and misses archived history; this function globs all checklog*.jsonl,
// letting trace cover tasks whose logs were archived at the next task start. Entries with mismatched TaskRef are excluded.
//
// LoadForTask 按 task ref 过滤，从 active checklog 与所有归档 checklog-*.jsonl 中
// 读取 check entry，按时间序返回。供 forge trace <ref> 重建 task 完整事件时间线。
// LoadAll 只读 active checklog.jsonl、错过归档历史；本函数 glob 全部 checklog*.jsonl，
// 让 trace 覆盖那些日志在下一次 task 启动时被归档的 task。TaskRef 不一致的条目被排除。
func LoadForTask(root, taskRef string) ([]Entry, error) {
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
			if e.TaskRef == taskRef {
				entries = append(entries, e)
			}
		}
		// A scanner error (oversized line beyond 1MB, I/O error) means everything after
		// that point in the file was silently truncated — make it explicit instead of
		// returning a partial timeline as if complete.
		//
		// scanner 出错（超 1MB 的行、I/O 错误）意味着该行之后的内容被静默截断——
		// 显式报错，不把残缺时间线当完整结果返回。
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

// LatestByCheck returns the latest entry per check name (across all sessions). Equivalent to
// LatestByCheckForSession with an empty session id (no filtering).
//
// LatestByCheck 返回每个 check name 的最新条目（所有 session）。等价于
// LatestByCheckForSession 传空 session id（不过滤）。
func LatestByCheck(root string) (map[CheckName]*Entry, error) {
	return LatestByCheckForSession(root, "")
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

// archiveLocked renames the existing checklog to a timestamped backup but does **not** acquire the lock; the caller must hold mu.
// See Archive for the deadlock rationale. Uses nanosecond-precision naming (util.ArchivedName) so multiple rotations within the same second do not collide.
//
// archiveLocked 把现存 checklog 重命名为带时间戳的备份，但**不**加锁；调用方必须持有 mu。
// 死锁原因见 Archive。用纳秒精度命名（util.ArchivedName），同一秒内的多次轮转不会撞名。
func archiveLocked(root string) error {
	src := filePath(root)
	if _, err := os.Stat(src); os.IsNotExist(err) {
		return nil
	}
	dst := util.ArchivedName(filepath.Dir(src), "checklog", time.Now())
	return os.Rename(src, dst)
}

// Archive renames the existing checklog to a timestamped backup file, preserving an audit trail across task starts.
// It holds the same mutex as Record so concurrent entry appends and rotations do not interleave. Returns nil when
// the checklog does not exist (idempotent).
//
// Archive 把现存 checklog 重命名为带时间戳的备份文件，跨 task 启动保留审计轨迹。
// 持有与 Record 同一把 mutex，使并发的 entry 追加与轮转不会交错。checklog 不存在时
// 返回 nil（幂等）。
func Archive(root string) error {
	mu.Lock()
	defer mu.Unlock()
	return archiveLocked(root)
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
