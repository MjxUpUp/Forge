package toolusage

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
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

// ToolCounts returns a map of tool_name -> invocation count.
func ToolCounts(calls []ToolCall) map[string]int {
	counts := make(map[string]int)
	for _, c := range calls {
		counts[c.ToolName]++
	}
	return counts
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

// SortedToolCounts returns tool counts sorted by count descending.
func SortedToolCounts(calls []ToolCall) []string {
	counts := ToolCounts(calls)

	type kv struct {
		tool  string
		count int
	}
	var entries []kv
	for k, v := range counts {
		entries = append(entries, kv{k, v})
	}
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].count != entries[j].count {
			return entries[i].count > entries[j].count
		}
		return entries[i].tool < entries[j].tool
	})

	var result []string
	for _, e := range entries {
		result = append(result, fmt.Sprintf("%s(%d)", e.tool, e.count))
	}
	return result
}

// Archive renames the current toollog to a timestamped backup.
func Archive(root string) error {
	path := filepath.Join(root, ".forge", toollogFile)
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return nil
	}
	archived := filepath.Join(root, ".forge",
		fmt.Sprintf("toollog-%s.jsonl", time.Now().Format("20060102150405")))
	return os.Rename(path, archived)
}

// Clear archives the current toollog and removes the active file.
func Clear(root string) error {
	if err := Archive(root); err != nil {
		return err
	}
	path := filepath.Join(root, ".forge", toollogFile)
	os.Remove(path)
	return nil
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
