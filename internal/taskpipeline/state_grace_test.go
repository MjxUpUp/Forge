package taskpipeline

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/MjxUpUp/Forge/internal/forgedata"
	"github.com/MjxUpUp/Forge/internal/forgedata/forgedatatest"
)

// TestMarkCompleteGrace_WritesEpochTimestamp dogfood 2.3: MarkCompleteGrace writes
// the current epoch timestamp at CompleteGracePath so file-sentinel can compare
// NOW - stamp < completeGraceWindow to allow the post-complete 'git commit' that
// would otherwise be quarantined as "no active task + source write". The file
// content must be a single integer (so bash 'tr -d [:space:]' produces a
// shell-comparable value) and the file must live under DataDir (project-level
// .forge/ is reserved for the fork-side / legacy path; DataDir is canonical post
// refactor-data-home, A6).
func TestMarkCompleteGrace_WritesEpochTimestamp(t *testing.T) {
	root, _ := forgedatatest.RealProject(t)
	const sid = "sess-grace"

	before := time.Now().Unix()
	if err := MarkCompleteGrace(root, sid); err != nil {
		t.Fatalf("MarkCompleteGrace: %v", err)
	}
	after := time.Now().Unix()

	path := CompleteGracePath(root, sid)
	if !strings.HasPrefix(path, forgedata.DataDirFor(root)) {
		t.Errorf("CompleteGracePath(%q, %q) = %q; want under DataDir %q (A6: project .forge out of reach for git diff)",
			root, sid, path, forgedata.DataDirFor(root))
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read grace file: %v", err)
	}
	stamp := strings.TrimSpace(string(data))
	if stamp == "" {
		t.Fatalf("grace file empty: %q", string(data))
	}
	// Integer-only: bash $((NOW - MTIME)) arithmetic, no quotes.
	var parsed int64
	for _, ch := range stamp {
		if ch < '0' || ch > '9' {
			t.Errorf("grace file must be integer-only; got non-digit in %q", stamp)
			break
		}
		parsed = parsed*10 + int64(ch-'0')
	}
	if parsed < before || parsed > after {
		t.Errorf("grace stamp %d not within [%d, %d] (file-sentinel would treat as expired or future-dated)", parsed, before, after)
	}
	// Per-session isolation: a different session ID must resolve to a different
	// path so concurrent sessions don't collide / share grace.
	if other := CompleteGracePath(root, "sess-other"); other == path {
		t.Errorf("per-session grace path collides: %q == %q (sid=other fails to isolate)", other, path)
	}
}

// TestMarkCompleteGrace_EmptySessionSilent: a session without an ID (defensive —
// callers always pass CurrentSessionID but the helper guards) should not write
// a file. The cost of writing a global "default" grace path that survives session
// changes is an attacker-style abuse vector (a stale grace stays in effect
// across legitimate fresh sessions). Fail-closed = no-op for empty sid.
func TestMarkCompleteGrace_EmptySessionSilent(t *testing.T) {
	root, _ := forgedatatest.RealProject(t)
	if err := MarkCompleteGrace(root, ""); err != nil {
		t.Errorf("empty sid must not error: %v", err)
	}
	// Sanity: no default-suffix grace file should exist (the "" branch returns
	// the un-suffixed completeGraceFile in CompleteGracePath; verify it was not
	// written).
	if _, err := os.Stat(CompleteGracePath(root, "")); err == nil {
		t.Errorf("MarkCompleteGrace(root, \"\") should not write a default grace file (silent no-op), but it exists")
	} else if !os.IsNotExist(err) {
		t.Errorf("stat: %v", err)
	}
}

// TestCompleteGracePath_SessionIDSanitization: per the dogfood claw-back
// pattern, session IDs are user-controlled environment variables; unsafe bytes
// would let a session ID escape into the filesystem layer. SanitizeSessionID
// (called inside CompleteGracePath) must result in a path that lives within
// DataDir (no "../" style escape).
func TestCompleteGracePath_SessionIDSanitization(t *testing.T) {
	root, _ := forgedatatest.RealProject(t)
	dataDir := forgedata.DataDirFor(root)
	for _, sid := range []string{
		"../../etc/passwd",
		"sess/with/slash",
		"sess\\with\\backslash",
	} {
		path := CompleteGracePath(root, sid)
		cleaned := filepath.Clean(path)
		if !strings.HasPrefix(cleaned, dataDir) {
			t.Errorf("sid=%q → path %q escapes DataDir %q (sanitization broken)", sid, cleaned, dataDir)
		}
	}
}
