package checklog

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/MjxUpUp/Forge/internal/forgedata"
	"github.com/MjxUpUp/Forge/internal/util"
)

var mu sync.Mutex

// filePath returns the active checklog path under the resolved runtime-state
// directory: the user-level DataDir (~/.forge/projects/<key>/) for git
// projects, falling back to the legacy project-level <root>/.forge/ for non-git
// forge projects so hooks can still record checks. See dataDir.
func filePath(root string) string {
	return filepath.Join(dataDir(root), "checklog.jsonl")
}

// dataDir resolves the runtime-state directory for checklog via the shared
// forgedata.DataDirFor (git-only Key → ~/.forge/projects/<key>/, fallback
// <root>/.forge/). See forgedata.DataDirFor for the load-bearing git-only-Key
// rationale (MkdirAll-stable resolution — Record must not flip paths mid-write).
func dataDir(root string) string { return forgedata.DataDirFor(root) }

// Record appends a check log entry to the DataDir checklog.jsonl (falls back to
// <root>/.forge/ for non-git projects — see dataDir). Sets RecordedAt to now.
// Thread-safe.
func Record(root string, entry *Entry) error {
	mu.Lock()
	defer mu.Unlock()

	entry.RecordedAt = time.Now()
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

// LoadAll reads all check log entries from the DataDir checklog.jsonl (falls
// back to <root>/.forge/ for non-git projects). Returns entries in chronological
// order. Returns nil if file does not exist.
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

// LoadForTask reads check entries from the active checklog AND all archived
// checklog-*.jsonl files, filtered by task reference, in chronological order.
// Used by `forge trace <ref>` to reconstruct a task's full event timeline.
// LoadAll only reads the active checklog.jsonl and misses archived history;
// this globs all checklog*.jsonl so trace covers tasks whose log was archived
// at the next task start. Entries whose TaskRef differs are excluded.
func LoadForTask(root, taskRef string) ([]Entry, error) {
	matches, err := filepath.Glob(filepath.Join(dataDir(root), "checklog*.jsonl"))
	if err != nil {
		return nil, err
	}
	var entries []Entry
	for _, path := range matches {
		f, err := os.Open(path)
		if err != nil {
			continue
		}
		scanner := bufio.NewScanner(f)
		for scanner.Scan() {
			var e Entry
			if err := json.Unmarshal(scanner.Bytes(), &e); err != nil {
				continue // skip malformed lines
			}
			if e.TaskRef == taskRef {
				entries = append(entries, e)
			}
		}
		f.Close()
	}
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].RecordedAt.Before(entries[j].RecordedAt)
	})
	return entries, nil
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

// archiveLocked renames the existing checklog to a timestamped backup WITHOUT
// taking the mutex; the caller must hold mu. See Archive for the deadlock
// rationale. Uses nanosecond-precision naming (util.ArchivedName) so same-second
// rotations don't collide.
func archiveLocked(root string) error {
	src := filePath(root)
	if _, err := os.Stat(src); os.IsNotExist(err) {
		return nil
	}
	dst := util.ArchivedName(filepath.Dir(src), "checklog", time.Now())
	return os.Rename(src, dst)
}

// Archive renames the existing checklog to a timestamped backup file, preserving
// the audit trail across task starts. Holds the same mutex as Record so a
// concurrent entry append and a rotation cannot interleave. Returns nil if the
// checklog does not exist (idempotent).
func Archive(root string) error {
	mu.Lock()
	defer mu.Unlock()
	return archiveLocked(root)
}

// Clear removes the check log file after archiving. Called at task start. Both
// the archive and the remove run under the mutex so no Record can append
// between them. After archiving+removing the active file, it best-effort prunes
// archives older than the retention window so checklog-*.jsonl doesn't grow
// unbounded across task starts.
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

// pruneArchives deletes checklog-*.jsonl archives older than the retention
// window (FORGE_LOG_RETENTION_DAYS, default 30; ≤0 disables). Best-effort:
// PruneArchives globs only checklog-*.jsonl (never the active file
// checklog.jsonl, which lacks the dash the glob requires), so it can't race a
// concurrent Record (which writes only the active file). Called under Clear's
// mutex purely to keep the rotation+prune atomic in intent; failures here
// don't affect Clear's main outcome.
func pruneArchives(dir string) {
	days := util.RetentionDays("FORGE_LOG_RETENTION_DAYS", 30)
	if days <= 0 {
		return
	}
	_, _ = util.PruneArchives(dir, "checklog", time.Now().AddDate(0, 0, -days))
}
