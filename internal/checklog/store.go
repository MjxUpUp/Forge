package checklog

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"sync"
	"time"

	"github.com/MjxUpUp/Forge/internal/forgedata"
	"github.com/MjxUpUp/Forge/internal/nodestamp"
	"github.com/MjxUpUp/Forge/internal/util"
)

var mu sync.Mutex

// filePath 返回解析后的 runtime-state 目录下的 active checklog 路径——始终用户级
// （git：~/.forge/projects/<key>/；非 git：~/.forge/projects/<path-key>/），
// 不写项目树。见 dataDir。
func filePath(root string) string {
	return filepath.Join(dataDir(root), "checklog.jsonl")
}

// dataDir 通过共享的 forgedata.DataDirFor 解析 checklog 的 runtime-state 目录
// （始终用户级：git Key → ~/.forge/projects/<key>/，非 git PathKey →
// ~/.forge/projects/<path-key>/）。load-bearing 依据见 forgedata.DataDirFor
// （MkdirAll-stable 的解析——Record 不得在写入中途切换路径）。
func dataDir(root string) string { return forgedata.DataDirFor(root) }

// Record appends a check log entry to DataDir's checklog.jsonl (always user-level, see dataDir).
//
// Record 向 DataDir 的 checklog.jsonl 追加一条 check log entry（始终用户级，
// 见 dataDir）。把 RecordedAt 设为当前时间。线程安全。
func Record(root string, entry *Entry) error {
	mu.Lock()
	defer mu.Unlock()

	entry.RecordedAt = time.Now()
	// 机器归因戳（node-identity.md §4）：仅当调用方留零值时落章——import/merge
	// 路径携带的是源节点戳，必须保留。
	if entry.Stamp == (nodestamp.Stamp{}) {
		entry.Stamp = nodestamp.Next()
	}
	// 兜底推断证据来源：调用方未显式标注 Source 时，按 CheckName 给默认值。
	// 让历史记录点（未改）也自动带上 Source，证据链分桶不留空白。
	if entry.Source == "" {
		entry.Source = SourceForCheck(entry.Check)
	}
	// 兜底推断级别：调用方未显式设置 Level 时，从 Passed + Detail 前缀
	// （BLOCKED: / ADVISORY:）推导，与上方 Source 兜底同款模式。显式 Level 恒优先。
	if entry.Level == "" {
		entry.Level = DeriveLevel(entry)
	}

	path := filePath(root)
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}

	// 追加前的尺寸触发轮转（feat/checklog-janitor）：active 文件曾在生产环境无限增长
	// （实测 15946 行）——Clear 已无生产调用方、Prune 只 glob 归档——Record 必须自己约束
	// 它增大的东西。写前轮转让最新条目落进新开的 active，active 文件被钉在阈值 + 一条
	// 以内。在 mu 内，轮转与所有并发的 Record/AppendEntries/Clear 串行。
	rotateIfOversizedLocked(root)

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

// AppendEntries writes pre-built entries (carried in by a cross-machine task import) to the active checklog.jsonl, preserving each entry's original fields.
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
	// 与 Record 相同的尺寸触发轮转（理由见该处）：跨机器 import 的 bundle 一次就能把
	// active 文件顶过阈值，轮转守卫属于所有写入路径，而不止 Record。
	rotateIfOversizedLocked(root)
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	defer f.Close()
	for i := range entries {
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

// LoadAll reads all check log entries from DataDir's checklog.jsonl (always user-level).
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
			// glob 命中的文件打不开是读失败、不是「无数据」——显式报错，
			// 不静默丢弃该文件的历史。
			return nil, fmt.Errorf("open %s: %w", path, err)
		}
		scanner := bufio.NewScanner(f)
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

// LoadAllAll reads all entries from the active checklog.jsonl AND every archived checklog-*.jsonl (chronological).
//
// LoadAllAll 从 active checklog.jsonl 与所有归档 checklog-*.jsonl 读取全部条目（时间序）。
// 是 LoadAll（仅 active）的跨归档对应：forge task start 归档上一份 checklog，LoadAll 只能看到
// 当前任务——跨整个项目历史聚合的消费者（如 skillseval usage 跨所有历史 task 读 CheckSkillTrigger）
// 需要本函数。对称 toolusage.LoadAllAll。尚无任何文件时返回 nil。
func LoadAllAll(root string) ([]Entry, error) {
	return loadAllArchives(root)
}

// LoadForTask filters by task ref across the active checklog and all archived checklog-*.jsonl, returning matches in chronological order.
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
// LatestByCheckForSession 返回限定在给定 session 内、每个 check name 的最新条目。
//
// 过滤规则（防止两个 Claude Code session 在共享 checkout 上并发时评分被对端污染）：
//   - sessionID 为空（legacy / 无 session）：不过滤——每条都计入。
//   - sessionID 非空：SessionID 非空且与 sessionID 不同的条目被排除。
//     SessionID 为空（全局/legacy）的条目始终保留，让全局适用的 check 仍能登记。
//
// 状态注记（2026-09 代码普查清扫）：生产读方走 LatestByCheckForSessionSince
// （cli/hook.go），本便捷包装当前无生产接线——会话级归一 key 契约被
// cli/hook_test.go 的注释引用、由本包测试钉住。接线前它是文档化的 API 面，
// 非死代码回收对象。
func LatestByCheckForSession(root, sessionID string) (map[CheckName]*Entry, error) {
	return LatestByCheckForSessionSince(root, sessionID, time.Time{})
}

// LatestByCheckForSessionSince is LatestByCheckForSession with a lower time bound (multi-task-concurrency M2 fix).
//
// LatestByCheckForSessionSince 是带时间下界的 LatestByCheckForSession
// （multi-task-concurrency M2 修正）：active 日志现在是跨任务累积的 append-only 时
// 间线（task start 不再 Clear）——意图是「本任务期间」的会话级读方必须同时按任务
// StartedAt 设界，否则新任务继承上一任务的 PASS 与评分信用。since 零值 = 旧的无界
// 行为（真正想要会话全史的调用方）。SessionID 为空的旧条目依旧总是保留（与父函数
// 同语义）。
func LatestByCheckForSessionSince(root, sessionID string, since time.Time) (map[CheckName]*Entry, error) {
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
		if !since.IsZero() && e.RecordedAt.Before(since) {
			continue
		}
		if existing, ok := result[e.Check]; !ok || e.RecordedAt.After(existing.RecordedAt) {
			result[e.Check] = e
		}
	}
	return result, nil
}

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

// defaultRotateBytes 是 active checklog.jsonl 被轮转成归档的默认尺寸阈值（5MB）。
// 5MB ≈ 数万条 entry——远超单个 task 窗口的 hook 流量，又小到不让 active 文件
// （LoadAll/LatestByCheckForSessionSince 这些无锁读者在每个 hook 事件都线性扫它）
// 退化成实测过的 15946 行无界状态。
const defaultRotateBytes int64 = 5 << 20 // 5MB

// rotateBytesLimit 从 env FORGE_CHECKLOG_ROTATE_BYTES 解析 active 日志轮转阈值
// （默认 5MB），镜像 util.RetentionDays 的约定：缺失/非法 → 默认；≤0 完全禁用轮转
// （写入路径退回修复前的无限增长——按需逃生阀，非推荐的常态）。
func rotateBytesLimit() int64 {
	raw := os.Getenv("FORGE_CHECKLOG_ROTATE_BYTES")
	if raw == "" {
		return defaultRotateBytes
	}
	n, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return defaultRotateBytes
	}
	return n
}

// rotateIfOversizedLocked 在 active checklog.jsonl 超过轮转阈值时把它轮转成带时间戳
// 的归档，随后顺手清一遍过期归档——轮转自身也让归档集有界。调用方必须持有 mu
// （与所有 Record/AppendEntries/Clear 串行）。归档名复用 archiveLocked →
// util.ArchivedName，即既有的 checklog-<stamp>.jsonl 约定：被 pruneArchives 的 glob
// 命中、其 archiveTimestamp 可解析出龄（按文件名时间戳的清理有效）、也被
// loadAllArchives 的 checklog*.jsonl glob 读到——LoadAllAll/LoadForTask 仍能看到轮转
// 走的历史。刻意 best-effort：轮转失败——如 Windows 上无锁的 LoadAll 读者还握着
// active 文件时（Go 的 os.Open 不带 FILE_SHARE_DELETE）rename 报 sharing violation——
// 不得拖垮调用方的主要结果「追加」；active 文件完好（rename 失败 = 什么都没动），
// 读者消失后下一次 Record 重试。rename 前不 fsync：已追加的行与追加本身的持久性
// 等同，无需更强。
func rotateIfOversizedLocked(root string) {
	limit := rotateBytesLimit()
	if limit <= 0 {
		return // 轮转禁用（≤0）：显式逃生阀
	}
	path := filePath(root)
	info, err := os.Stat(path)
	if err != nil || info.Size() <= limit {
		return
	}
	if err := archiveLocked(root); err != nil {
		// best-effort：轮转失败不阻断追加（见函数注释）；下次 Record 重试。
		return
	}
	pruneArchives(filepath.Dir(path))
}

// Clear deletes the check log file after archiving.
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

// Prune is the non-destructive half of Clear: retention cleanup of checklog-*.jsonl archives only, never touching the active file.
//
// Prune 是 Clear 的非破坏性半边：只做 checklog-*.jsonl 归档的 retention 清理，绝不碰
// active 文件。task start 处 Clear 退役后（multi-task-concurrency 设计 §5）改调本函数：
// 日志现在是按 task-started 边界事件分段的 append-only 时间线，开任务不得归档或删除
// 任何东西——只保持 retention 窗口有界。
//
// 关于约束 active 文件（feat/checklog-janitor）：Prune 刻意不去裁 checklog.jsonl 本身。
// 裁剪意味着原地重写 append-only 时间线，与无锁的 LoadAll/LatestByCheckForSessionSince
// 读者（它们不持 mu）竞态，还可能丢掉并发 Record 正在追加的行——损坏比尺寸恶劣得多。
// 因此 active 文件的有界性由写入侧轮转负责（每次 Record/AppendEntries 的
// rotateIfOversizedLocked），且这是结构性的：它长在唯一让文件增长的路径上，不可能
// 「漏察觉」增长。即便通过 FORGE_CHECKLOG_ROTATE_BYTES<=0 禁用轮转，最坏也只是修复前
// 的行为（active 无界增长），绝不丢数据。
func Prune(root string) {
	mu.Lock()
	defer mu.Unlock()
	pruneArchives(filepath.Dir(filePath(root)))
}
