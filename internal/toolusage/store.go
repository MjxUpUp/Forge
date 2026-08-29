package toolusage

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/MjxUpUp/Forge/internal/forgedata"
	"github.com/MjxUpUp/Forge/internal/nodestamp"
	"github.com/MjxUpUp/Forge/internal/util"
)

const toollogFile = "toollog.jsonl"

// dataDir 返回 root 的 runtime-state DataDir（refactor-data-home）：git 项目用
// 用户级 ~/.forge/projects/<key>/，非 git 回退到 <root>/.forge/，让 toollog 仍能记录。
// 见 forgedata.DataDirFor。
func dataDir(root string) string { return forgedata.DataDirFor(root) }

var mu sync.Mutex

// Record appends a ToolCall entry to DataDir/toollog.jsonl.
//
// Record 向 DataDir/toollog.jsonl 追加一条 ToolCall entry。
func Record(root string, call *ToolCall) error {
	mu.Lock()
	defer mu.Unlock()

	if call.Timestamp.IsZero() {
		call.Timestamp = time.Now()
	}
	if call.ID == "" {
		call.ID = computeID(*call)
	}
	// 机器归因戳在 computeID 之后落章：稳定 ID hash 的是身份字段，不得随戳值漂移；
	// 仅当调用方留零值（import 保留源节点戳）。
	if call.Stamp == (nodestamp.Stamp{}) {
		call.Stamp = nodestamp.Next()
	}

	forgeDir := dataDir(root)
	if err := os.MkdirAll(forgeDir, 0755); err != nil {
		return fmt.Errorf("failed to create forge data dir: %w", err)
	}

	path := filepath.Join(forgeDir, toollogFile)
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("failed to open toollog: %w", err)
	}
	defer f.Close()

	data, err := json.Marshal(call)
	if err != nil {
		return fmt.Errorf("failed to marshal tool call: %w", err)
	}

	_, err = fmt.Fprintln(f, string(data))
	return err
}

// LoadAll reads all ToolCall entries from DataDir/toollog.jsonl.
//
// LoadAll 从 DataDir/toollog.jsonl 读取全部 ToolCall entry。
func LoadAll(root string) ([]ToolCall, error) {
	path := filepath.Join(dataDir(root), toollogFile)
	return loadFromPath(path)
}

// LoadForTask reads ToolCall entries filtered by task reference.
//
// LoadForTask 按 task ref 过滤读取 ToolCall entry。
func LoadForTask(root string, taskRef string) ([]ToolCall, error) {
	all, err := LoadAll(root)
	if err != nil {
		return nil, err
	}
	var filtered []ToolCall
	for _, c := range all {
		if c.TaskRef == taskRef {
			filtered = append(filtered, c)
		}
	}
	return filtered, nil
}

// LoadForTaskAll filters by task ref, reading ToolCall entries from the active
// toollog and all archived toollog-*.jsonl.
//
// LoadForTaskAll 按 task ref 过滤，从 active toollog 与所有归档 toollog-*.jsonl 中
// 读取 ToolCall entry。与 checklog.LoadForTask 对称——供 forge trace 用，使 task 完整
// tool 历史能熬过 task 启动时清空 active toollog 的 Archive。无此函数，trace 对任何
// 已完成 task 都显示 0 次 tool 调用。
func LoadForTaskAll(root, taskRef string) ([]ToolCall, error) {
	matches, err := filepath.Glob(filepath.Join(dataDir(root), "toollog*.jsonl"))
	if err != nil {
		return nil, err
	}
	var filtered []ToolCall
	for _, path := range matches {
		calls, err := loadFromPath(path)
		if err != nil {
			continue
		}
		for _, c := range calls {
			if c.TaskRef == taskRef {
				filtered = append(filtered, c)
			}
		}
	}
	return filtered, nil
}

// LoadAllAll reads all ToolCall entries from the active toollog and every
// archived toollog-*.jsonl, symmetric with LoadForTaskAll / checklog.LoadAllAll.
//
// LoadAllAll 从 active toollog 与所有归档的 toollog-*.jsonl 读取全部 ToolCall 条目，
// 与 LoadForTaskAll / checklog.LoadAllAll 对称——供 skill usage 与 effectiveness
// 分析使用，使跨任务聚合（热门排名、hit×成效关联、undertrigger 候选）能扛过
// task start 时的 Archive（它会清空 active toollog）。没有它，skills
// usage/effectiveness 只反映当前任务。
func LoadAllAll(root string) ([]ToolCall, error) {
	matches, err := filepath.Glob(filepath.Join(dataDir(root), `toollog*.jsonl`))
	if err != nil {
		return nil, err
	}
	var all []ToolCall
	for _, path := range matches {
		calls, err := loadFromPath(path)
		if err != nil {
			// per-file 失败（IO/权限/文件锁占用）静默跳过：跨归档全量聚合中单个坏文件
			// 不应让整表失败——与 LoadForTaskAll 同策略。loadFromPath 内部已对单行 JSON
			// 损坏做 per-line 容错（json.Unmarshal 失败即 continue），这里只兜底整文件不可读。
			continue
		}
		all = append(all, calls...)
	}
	return all, nil
}

// ToollogHasData reports whether the active toollog.jsonl exists and is
// non-empty. loadFromPath returns nil,nil both for a missing file and an empty
// one, so callers cannot distinguish 'telemetry never arrived' from 'telemetry
// arrived but matched nothing' through the load path.
//
// ToollogHasData 报告 active toollog.jsonl 是否存在且非空。loadFromPath 对文件
// 不存在与文件为空都返回 nil,nil，调用方无法经 load 路径区分「遥测从未到达」与
// 「遥测到了但没匹配」——这个基于 stat 的探测补上该缺口。供 work-activity 门禁
// 区分「本 host 的 hook 分发未接」（toollog 缺失/为空）与「hook 分发正常但确实
// 零调用」。
func ToollogHasData(root string) bool {
	info, err := os.Stat(filepath.Join(dataDir(root), toollogFile))
	return err == nil && info.Size() > 0
}

// ReadEditCounts returns Read and Edit/Write tool call counts from toollog.jsonl
// since the given time, scoped to a task.
//
// ReadEditCounts 自给定时间起、按 task 从 toollog.jsonl 返回 Read 与 Edit/Write 的
// tool 调用数。与 checklog.WorkActivity（把所有 tool 折叠成一个标量计数）不同，本函数
// 把 read 与 edit 分开，让调用方能强制 read-before-edit ratio。
//   - reads = Read 调用数
//   - edits = Edit + Write 调用数
//
// Bash、Grep、Glob 等故意不计入——read-before-edit 信号只关心 read vs write。
func ReadEditCounts(root, taskRef string, since time.Time) (reads, edits int, err error) {
	calls, err := LoadForTask(root, taskRef)
	if err != nil {
		return 0, 0, err
	}
	for _, c := range calls {
		if !c.Timestamp.After(since) {
			continue
		}
		switch c.ToolName {
		case "Read":
			reads++
		case "Edit", "Write":
			edits++
		}
	}
	return reads, edits, nil
}

// ExploreCounts returns Grep+Glob tool call counts from toollog.jsonl since the
// given time, scoped to a task.
//
// ExploreCounts 自给定时间起、按 task 从 toollog.jsonl 返回 Grep+Glob 的
// tool 调用数。只供 work-activity 的「门禁间有无真实工作」判定——绝不供
// read-before-edit：浏览匹配不等于读过要改的文件，那个更严格的信号保持
// Read-only（见 ReadEditCounts）。存在缘由：CLAUDE.md 错误表建议门禁间
// 「用 Read/Grep/Glob 探索」；在 tool-track matcher 纳入 Grep/Glob 之前
// （2026-08-23），这些调用进不了 toollog，纯探索段落被计为零工作、照样
// 触发门禁拦截。
//   - explores = Grep + Glob 调用数
//
// Bash 保持排除（gate 命令本身走 Bash；与 ReadEditCounts 及
// checklog.WorkActivity 的排除同理）。
func ExploreCounts(root, taskRef string, since time.Time) (explores int, err error) {
	calls, err := LoadForTask(root, taskRef)
	if err != nil {
		return 0, err
	}
	for _, c := range calls {
		if !c.Timestamp.After(since) {
			continue
		}
		switch c.ToolName {
		case "Grep", "Glob":
			explores++
		}
	}
	return explores, nil
}

// ReadEditCountsGraceWindow counts Read calls whose timestamp falls in
// [since-window, ∞), regardless of TaskRef.
//
// ReadEditCountsGraceWindow 统计 timestamp 落在 [since-window, ∞) 内的 Read 调用数，
// 不论 TaskRef。它修复 task-start/Read 竞态：当 agent 与 forge task start 并发触发 Read
// 时，该 Read 会记到**上一个** task 的 ref（active ref 还没切），且其 timestamp 可能
// 略早于 StartedAt。两者都让它进不了按 task 的 ReadEditCounts(taskRef, StartedAt)，
// 误触 read-before-edit gate。grace window 跨所有 task 重计附近的 Read；executor 在
// 硬失败前把它当作第二意见。
func ReadEditCountsGraceWindow(root string, since time.Time, window time.Duration) (reads int, err error) {
	all, err := LoadAll(root)
	if err != nil {
		return 0, err
	}
	lo := since.Add(-window)
	for _, c := range all {
		if c.ToolName == "Read" && c.Timestamp.After(lo) {
			reads++
		}
	}
	return reads, nil
}

// pruneArchives 删除超过 retention 窗口的 toollog-*.jsonl 归档
// （FORGE_LOG_RETENTION_DAYS，默认 30；≤0 禁用）。尽力而为，理由同 checklog.Clear 的
// pruneArchives——让 toollog-*.jsonl 跨 task 启动保持有界，且不与 Record（只写 active
// 文件）竞态。
func pruneArchives(dir string) {
	days := util.RetentionDays("FORGE_LOG_RETENTION_DAYS", 30)
	if days <= 0 {
		return
	}
	_, _ = util.PruneArchives(dir, "toollog", time.Now().AddDate(0, 0, -days))
}

// Prune is the retention cleanup for toollog-*.jsonl archives (checklog.Prune's
// twin).
//
// Prune 是 toollog-*.jsonl 归档的 retention 清理（checklog.Prune 的孪生）。
// task start 不再截断 toollog（multi-task-concurrency 设计 §5）——读取按
// TaskRef 过滤（LoadForTask / ReadEditCounts），历史必须跨任务边界存活；只保持
// retention 窗口有界。旧的破坏性 Clear（归档 + 删 active）已作为死代码删除：
// task start 改用 CheckTaskStarted 边界标记后它再无生产调用方，有界增长 concerns
// 由 Prune 单独覆盖。
func Prune(root string) {
	mu.Lock()
	defer mu.Unlock()
	pruneArchives(dataDir(root))
}

// loadFromPath 从一个文件读取 JSONL entry。
func loadFromPath(path string) ([]ToolCall, error) {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer f.Close()

	var calls []ToolCall
	scanner := bufio.NewScanner(f)
	// 允许更长的行，以容纳大型 tool 输入。
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var call ToolCall
		if err := json.Unmarshal([]byte(line), &call); err != nil {
			continue // 跳过格式错误的行
		}
		call.ID = ensureID(call) // 为没带 ID 写入的 legacy 条目回填 ID
		calls = append(calls, call)
	}
	return calls, scanner.Err()
}

// TruncateInput truncates a string to maxToolInputLen characters (rune-safe,
// ellipsis-marked.
//
// TruncateInput 把字符串截断到 maxToolInputLen 个字符（rune-safe，
// 截断带省略号——见 util.TruncateRunes，与 hazard 包共享的单一真相源）。
func TruncateInput(s string) string {
	return util.TruncateRunes(s, maxToolInputLen)
}

// EstimateTokens roughly estimates the token count of a string (loop cost proxy,
// not an exact bill).
//
// EstimateTokens 粗估字符串 token 数（loop 成本代理，非精确账单）。
// 无 tiktoken 依赖：中文≈1字/1-2 token、英文≈4 char/token，折中用 rune/3。
// 用于 iteration breaker 与 trace 可见性——判断「loop 是否跑飞」，不用于计费，
// 精度够成本量级判断即可（1.5x 偏差不影响「该不该换策略」的决策）。
func EstimateTokens(s string) int {
	n := utf8.RuneCountInString(s)
	if n == 0 {
		return 0
	}
	return n/3 + 1
}

// SumEstTokens sums the estimated tokens across a set of ToolCalls (for
// trace/breaker aggregation).
//
// SumEstTokens 累加一组 ToolCall 的估算 token（trace/ breaker 聚合用）。
func SumEstTokens(calls []ToolCall) int {
	total := 0
	for i := range calls {
		total += calls[i].EstTokens
	}
	return total
}

// taskTokenWarnThreshold 是单个 task 累计估算 token 的 advisory 警示阈值（loop 成本上限）。
// EstimateTokens 是 rune/3 粗估，阈值按量级定：50 万估算 token 是明显跑飞的量级
// （正常 task 几万~十几万）。advisory 不硬阻断——只提示成本偏高，由人/agent 决定是否换策略。
const taskTokenWarnThreshold = 500000

// tokenBreakerWarning 是纯判断函数，可独立单测（不必造超 50 万 token 的文件数据）。
func tokenBreakerWarning(total int) string {
	if total >= taskTokenWarnThreshold {
		return fmt.Sprintf("task 累计估算 token 已达 %d（≥%d）——loop 成本偏高，检查无效往返/反复读大文件。", total, taskTokenWarnThreshold)
	}
	return ""
}

// TaskTokenBreaker is the task-level token cost circuit breaker (advisory).
//
// TaskTokenBreaker 是 task 级 token 成本熔断（advisory）。聚合 task 全部 tool 调用的
// 估算 token，超阈值返回警示字符串（CLI 写 stderr / MCP 塞进 result），未超返回空。
// 这是 EstimateTokens/SumEstTokens 真正参与 loop 成本控制的接入点——让 token 计量不止于
// forge trace 可观测，而是 task gate 推进时的成本上限警示，对齐「loop 成本上限」卖点。
func TaskTokenBreaker(root, taskRef string) (warning string, total int) {
	calls, err := LoadForTaskAll(root, taskRef)
	if err != nil || len(calls) == 0 {
		return "", 0
	}
	total = SumEstTokens(calls)
	return tokenBreakerWarning(total), total
}
