package checklog

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/MjxUpUp/Forge/internal/forgedata"
)

// writeEntry appends a raw entry to the checklog file with an explicit
// RecordedAt, bypassing Record() (which stamps time.Now()) so filter tests are
// deterministic. Writes to the user-level DataDir (forgedata.DataDirFor), under
// an isolated FORGE_DATA_HOME so the real ~/.forge is never touched.
func writeEntry(t *testing.T, dir string, e Entry) {
	t.Helper()
	isolateDataHome(t)
	path := filepath.Join(forgedata.DataDirFor(dir), "checklog.jsonl")
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	data, _ := json.Marshal(e)
	if _, err := f.Write(append(data, '\n')); err != nil {
		t.Fatal(err)
	}
}

// TestLatestByCheckForSession_IsolatesBySession verifies the concurrency fix:
// session A's scoring reads only global + session-A check results, never
// session B's, and vice-versa. Legacy (empty session) returns all.
func TestLatestByCheckForSession_IsolatesBySession(t *testing.T) {
	dir := t.TempDir()
	base := time.Date(2026, 6, 13, 12, 0, 0, 0, time.UTC)
	// Global entry (empty session) — always visible.
	writeEntry(t, dir, Entry{Check: CheckAutoCompile, Passed: true, Checked: true, SessionID: "", RecordedAt: base})
	// Session A assertion passed.
	writeEntry(t, dir, Entry{Check: CheckAssertion, Passed: true, Checked: true, SessionID: "sess-A", RecordedAt: base})
	// Session B assertion failed, NEWER than A's.
	writeEntry(t, dir, Entry{Check: CheckAssertion, Passed: false, Checked: true, SessionID: "sess-B", RecordedAt: base.Add(time.Second)})

	// Session A: sees global + own; B's (newer) failure must NOT contaminate.
	a, err := LatestByCheckForSession(dir, "sess-A")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := a[CheckAutoCompile]; !ok {
		t.Error("global auto-compile should be visible to sess-A")
	}
	if e, ok := a[CheckAssertion]; !ok || !e.Passed {
		t.Errorf("sess-A assertion should be its own (passed=true), got ok=%v e=%v", ok, e)
	}

	// Session B: sees its own failure.
	b, err := LatestByCheckForSession(dir, "sess-B")
	if err != nil {
		t.Fatal(err)
	}
	if e, ok := b[CheckAssertion]; !ok || e.Passed {
		t.Errorf("sess-B assertion should be its own (passed=false), got ok=%v e=%v", ok, e)
	}

	// Legacy (empty session id): no filtering → newest wins (B's failure).
	all, err := LatestByCheckForSession(dir, "")
	if err != nil {
		t.Fatal(err)
	}
	if e, ok := all[CheckAssertion]; !ok || e.Passed {
		t.Errorf("legacy latest assertion should be B (newer, passed=false), got ok=%v e=%v", ok, e)
	}
}
