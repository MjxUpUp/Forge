package checklog

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"
)

var mu sync.Mutex

// filePath returns the checklog path relative to project root.
func filePath(root string) string {
	return filepath.Join(root, ".forge", "checklog.jsonl")
}

// Record appends a check log entry to .forge/checklog.jsonl.
// Sets RecordedAt to now. Thread-safe.
func Record(root string, entry *Entry) error {
	mu.Lock()
	defer mu.Unlock()

	entry.RecordedAt = time.Now()

	dir := filepath.Dir(filePath(root))
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	f, err := os.OpenFile(filePath(root), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
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

// LoadAll reads all check log entries from .forge/checklog.jsonl.
// Returns entries in chronological order. Returns nil if file does not exist.
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
	for scanner.Scan() {
		var e Entry
		if err := json.Unmarshal(scanner.Bytes(), &e); err != nil {
			continue // skip malformed lines
		}
		entries = append(entries, e)
	}
	return entries, scanner.Err()
}

// LatestByCheck returns the most recent entry for each check name.
func LatestByCheck(root string) (map[CheckName]*Entry, error) {
	entries, err := LoadAll(root)
	if err != nil {
		return nil, err
	}

	result := make(map[CheckName]*Entry)
	for i := range entries {
		e := &entries[i]
		if existing, ok := result[e.Check]; !ok || e.RecordedAt.After(existing.RecordedAt) {
			result[e.Check] = e
		}
	}
	return result, nil
}

// Clear removes the check log file. Called at task start.
func Clear(root string) error {
	err := os.Remove(filePath(root))
	if os.IsNotExist(err) {
		return nil
	}
	return err
}
