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

// TestLatestByCheckForSessionSince_TaskScoping pins the M2 fix (multi-task-concurrency):
// an entry recorded BEFORE the task's StartedAt must not surface to a reader scoping
// "during this task" — otherwise a new task inherits the previous task's PASS.
//
// TestLatestByCheckForSessionSince_TaskScoping 钉住 M2 修正
// （multi-task-concurrency）：任务 StartedAt 之前记录的条目不得出现在「本任务期
// 间」的读方结果里——否则新任务继承上一任务的 PASS。
func TestLatestByCheckForSessionSince_TaskScoping(t *testing.T) {
	t.Setenv("FORGE_DATA_HOME", t.TempDir())
	root := t.TempDir()
	old := time.Now().Add(-time.Hour)
	fresh := time.Now()
	// 上一任务（一小时前）的 PASS + 本任务（此刻）尚无条目。直接写 JSONL——
	// Record 会把 RecordedAt 覆盖为 now，fixture 需要可控的历史时间戳。
	writeJSONL := func(e Entry) {
		line, err := json.Marshal(e)
		if err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(forgedata.DataDirFor(root), "checklog.jsonl")
		os.MkdirAll(filepath.Dir(path), 0o755)
		f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
		f.Write(append(line, '\n'))
	}
	writeJSONL(Entry{Check: CheckAutoCompile, Passed: true, Checked: true, SessionID: "s1", RecordedAt: old})

	unbounded, err := LatestByCheckForSession(root, "s1")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := unbounded[CheckAutoCompile]; !ok {
		t.Fatal("无界查询应看到历史 PASS（旧行为保留）")
	}
	bounded, err := LatestByCheckForSessionSince(root, "s1", old.Add(30*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := bounded[CheckAutoCompile]; ok {
		t.Fatal("M2 回归：任务下界前的上一任务 PASS 泄漏进本任务视图")
	}
	// 下界后的新条目正常可见。
	writeJSONL(Entry{Check: CheckAutoCompile, Passed: true, Checked: true, SessionID: "s1", RecordedAt: fresh})
	bounded, _ = LatestByCheckForSessionSince(root, "s1", old.Add(30*time.Minute))
	if e, ok := bounded[CheckAutoCompile]; !ok || !e.RecordedAt.Equal(fresh) {
		t.Fatal("下界后的本任务条目应可见")
	}
}
