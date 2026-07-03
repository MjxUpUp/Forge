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

	"github.com/MjxUpUp/Forge/internal/util"
)

const toollogFile = "toollog.jsonl"

var mu sync.Mutex

// Record appends a ToolCall entry to .forge/toollog.jsonl.
func Record(root string, call *ToolCall) error {
	mu.Lock()
	defer mu.Unlock()

	if call.Timestamp.IsZero() {
		call.Timestamp = time.Now()
	}
	if call.ID == "" {
		call.ID = computeID(*call)
	}

	forgeDir := filepath.Join(root, ".forge")
	if err := os.MkdirAll(forgeDir, 0755); err != nil {
		return fmt.Errorf("failed to create .forge dir: %w", err)
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

// LoadAll reads all ToolCall entries from .forge/toollog.jsonl.
func LoadAll(root string) ([]ToolCall, error) {
	path := filepath.Join(root, ".forge", toollogFile)
	return loadFromPath(path)
}

// LoadForTask reads ToolCall entries filtered by task reference.
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

// LoadForTaskAll reads ToolCall entries filtered by task reference from the
// active toollog AND all archived toollog-*.jsonl files. Symmetric to
// checklog.LoadForTask — used by `forge trace` so a task's full tool history
// survives the Archive-on-task-start that clears the active toollog. Without
// this, trace shows 0 tool calls for any completed task.
func LoadForTaskAll(root, taskRef string) ([]ToolCall, error) {
	matches, err := filepath.Glob(filepath.Join(root, ".forge", "toollog*.jsonl"))
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

// ReadEditCounts returns the number of Read vs Edit/Write tool calls for a task
// since the given time, sourced from toollog.jsonl. Unlike checklog.WorkActivity
// (which collapses all tools into one scalar count), this separates reads from
// edits so callers can enforce a read-before-edit ratio.
//   - reads = "Read" calls
//   - edits = "Edit" + "Write" calls
//
// Bash, Grep, Glob etc. are intentionally excluded — only read vs write matters
// for the read-before-edit signal.
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

// ReadEditCountsGraceWindow counts Read calls whose timestamp falls within
// [since-window, ∞), regardless of TaskRef. It recovers the task-start/Read
// race: when an agent fires Read concurrently with `forge task start`, the Read
// is logged under the PREVIOUS task's ref (active ref hasn't switched yet) and
// its timestamp may land just before StartedAt. Both exclude it from the
// per-task ReadEditCounts(taskRef, StartedAt), falsely tripping the
// read-before-edit gate. The grace window re-counts nearby Reads across all
// tasks; the executor uses it as a second opinion before hard-failing.
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

// archiveLocked renames the current toollog to a timestamped backup WITHOUT
// taking the mutex; the caller must hold mu. Split from Archive so Clear can
// archive-then-remove under a single lock acquisition — calling the public
// Archive (which locks) from Clear (also locked) would deadlock, since
// sync.Mutex is non-reentrant. Uses nanosecond-precision naming
// (util.ArchivedName) so two rotations in the same second don't clobber.
func archiveLocked(root string) error {
	path := filepath.Join(root, ".forge", toollogFile)
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return nil
	}
	archived := util.ArchivedName(filepath.Join(root, ".forge"), "toollog", time.Now())
	return os.Rename(path, archived)
}

// Clear archives the current toollog and removes the active file. Both steps
// run under the mutex so no Record can append between the rename and the remove
// (which would leak the appended entry into a fresh active file). After
// archiving+removing, it best-effort prunes archives older than the retention
// window so toollog-*.jsonl doesn't grow unbounded across task starts.
func Clear(root string) error {
	mu.Lock()
	defer mu.Unlock()
	if err := archiveLocked(root); err != nil {
		return err
	}
	path := filepath.Join(root, ".forge", toollogFile)
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	pruneArchives(filepath.Join(root, ".forge"))
	return nil
}

// pruneArchives deletes toollog-*.jsonl archives older than the retention
// window (FORGE_LOG_RETENTION_DAYS, default 30; ≤0 disables). Best-effort,
// same rationale as checklog.Clear's pruneArchives — keeps toollog-*.jsonl
// bounded across task starts without racing Record (which writes only the
// active file).
func pruneArchives(dir string) {
	days := util.RetentionDays("FORGE_LOG_RETENTION_DAYS", 30)
	if days <= 0 {
		return
	}
	_, _ = util.PruneArchives(dir, "toollog", time.Now().AddDate(0, 0, -days))
}

// loadFromPath reads JSONL entries from a file.
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
	// Allow longer lines for large tool inputs.
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var call ToolCall
		if err := json.Unmarshal([]byte(line), &call); err != nil {
			continue // skip malformed lines
		}
		call.ID = ensureID(call) // backfill ID for legacy entries written without one
		calls = append(calls, call)
	}
	return calls, scanner.Err()
}

// TruncateInput truncates a string to maxToolInputLen characters (rune-safe).
func TruncateInput(s string) string {
	runes := []rune(s)
	if len(runes) <= maxToolInputLen {
		return s
	}
	return string(runes[:maxToolInputLen])
}

// EstimateTokens 粗估字符串 token 数（loop 成本代理，非精确账单）。
// 无 tiktoken 依赖：中文≈1字/1-2 token、英文≈4 char/token，折中用 rune/3。
// 用于 iteration breaker 与 trace 可见性——判断"loop 是否跑飞"，不用于计费，
// 精度够成本量级判断即可（1.5x 偏差不影响"该不该换策略"的决策）。
func EstimateTokens(s string) int {
	n := utf8.RuneCountInString(s)
	if n == 0 {
		return 0
	}
	return n/3 + 1
}

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

// TaskTokenBreaker 是 task 级 token 成本熔断（advisory）。聚合 task 全部 tool 调用的
// 估算 token，超阈值返回警示字符串（CLI 写 stderr / MCP 塞进 result），未超返回空。
// 这是 EstimateTokens/SumEstTokens 真正参与 loop 成本控制的接入点——让 token 计量不止于
// forge trace 可观测，而是 task gate 推进时的成本上限警示，对齐"loop 成本上限"卖点。
func TaskTokenBreaker(root, taskRef string) (warning string, total int) {
	calls, err := LoadForTaskAll(root, taskRef)
	if err != nil || len(calls) == 0 {
		return "", 0
	}
	total = SumEstTokens(calls)
	return tokenBreakerWarning(total), total
}
