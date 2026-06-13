package checklog

import (
	"bufio"
	"encoding/json"
	"fmt"
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

// LatestByCheck returns the most recent entry for each check name (all sessions).
// Equivalent to LatestByCheckForSession with an empty session id (no filtering).
func LatestByCheck(root string) (map[CheckName]*Entry, error) {
	return LatestByCheckForSession(root, "")
}

// LatestByCheckForSession returns the most recent entry per check name, scoped
// to the given session.
//
// Filtering rules (prevents cross-session contamination of scoring when two
// Claude Code sessions run concurrently on a shared checkout):
//   - sessionID == "" (legacy / no session): no filtering — every entry counts.
//   - sessionID != "": entries whose SessionID is non-empty AND differs from
//     sessionID are excluded. Entries with empty SessionID (global/legacy) are
//     always kept so globally-applicable checks still register.
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

// Archive renames the existing checklog to a timestamped backup file.
// This preserves the audit trail across task starts.
// Returns nil if the checklog does not exist (idempotent).
func Archive(root string) error {
	src := filePath(root)
	if _, err := os.Stat(src); os.IsNotExist(err) {
		return nil
	}
	dst := filepath.Join(filepath.Dir(src),
		fmt.Sprintf("checklog-%s.jsonl", time.Now().Format("20060102150405")))
	return os.Rename(src, dst)
}

// Clear removes the check log file after archiving. Called at task start.
func Clear(root string) error {
	// Archive first to preserve audit trail across task boundaries.
	if err := Archive(root); err != nil {
		return err
	}
	err := os.Remove(filePath(root))
	if os.IsNotExist(err) {
		return nil
	}
	return err
}
