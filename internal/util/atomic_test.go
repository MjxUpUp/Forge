package util

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestAtomicWrite_WritesContent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")
	if err := AtomicWrite(path, []byte(`{"a":1}`), 0644); err != nil {
		t.Fatalf("AtomicWrite: %v", err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(got) != `{"a":1}` {
		t.Fatalf("content = %q, want {\"a\":1}", got)
	}
}

// TestAtomicWrite_OverwritesExisting verifies the Windows path: Go's os.Rename
// must atomically replace an existing target (MoveFileEx + MOVEFILE_REPLACE_EXISTING),
// not error on it. Without that guarantee atomic rotation is impossible on Windows.
func TestAtomicWrite_OverwritesExisting(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")
	if err := os.WriteFile(path, []byte("OLD"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := AtomicWrite(path, []byte("NEW"), 0644); err != nil {
		t.Fatalf("AtomicWrite overwrite: %v", err)
	}
	got, _ := os.ReadFile(path)
	if string(got) != "NEW" {
		t.Fatalf("after overwrite = %q, want NEW", got)
	}
}

func TestAtomicWrite_CreatesParentDir(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nested", "deep", "state.json")
	if err := AtomicWrite(path, []byte("x"), 0644); err != nil {
		t.Fatalf("AtomicWrite with missing parent: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("parent not created: %v", err)
	}
}

func TestAtomicWrite_NoStaleTemp(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")
	if err := AtomicWrite(path, []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if e.Name() != "state.json" {
			t.Errorf("stale temp file left behind: %s", e.Name())
		}
	}
}

// TestAtomicWrite_ConcurrentSamePath runs many goroutines writing the same path
// under -race. The final file must be a complete, parseable document from ONE
// writer — never a torn interleaving of two writes (exactly what plain
// os.WriteFile produces under concurrent truncation). This is the core C1
// regression guard: an interleaved write must not corrupt the state file every
// .forge/ loader JSON-parses.
func TestAtomicWrite_ConcurrentSamePath(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")

	type payload struct {
		Val int `json:"val"`
	}
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			data, _ := json.Marshal(payload{Val: i})
			// A losing rename on Windows (Access Denied vs a concurrent winner)
			// is an expected concurrent-loss, NOT corruption — AtomicWrite
			// returns the error and the target stays a complete file. The
			// assertion below is "never corrupt", not "every write wins".
			_ = AtomicWrite(path, data, 0644)
		}(i)
	}
	wg.Wait()

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("final ReadFile: %v", err)
	}
	var p payload
	if err := json.Unmarshal(got, &p); err != nil {
		t.Fatalf("final file is not a complete JSON document (torn write): %v\nraw=%q", err, got)
	}
}

func TestArchivedName_NanosecondStamp(t *testing.T) {
	now := time.Date(2026, 6, 15, 14, 30, 25, 123456789, time.UTC)
	name := ArchivedName(t.TempDir(), "toollog", now)
	base := filepath.Base(name)
	// stamp carries nanosecond precision so same-second archives don't collide.
	if !strings.HasPrefix(base, "toollog-20260615143025.123456789") {
		t.Fatalf("archive name = %q, want nanosecond-precision stamp", base)
	}
	if !strings.HasSuffix(base, ".jsonl") {
		t.Fatalf("archive name = %q, want .jsonl suffix", base)
	}
}

// TestArchivedName_NoCollisionSameInstant: two calls with the SAME now (same
// nanosecond — possible across processes or a tight loop) must NOT return the
// same path. The second gets a numeric suffix. Guards C3: the old
// second-precision stamp silently overwrote (POSIX) or errored (Windows).
func TestArchivedName_NoCollisionSameInstant(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 6, 15, 14, 30, 25, 0, time.UTC)
	first := ArchivedName(dir, "checklog", now)
	// Materialize the first so the stat-based tiebreaker engages on the second call.
	if err := os.WriteFile(first, []byte{}, 0644); err != nil {
		t.Fatal(err)
	}
	second := ArchivedName(dir, "checklog", now)
	if first == second {
		t.Fatalf("same-instant archives collided: both %s", first)
	}
}

// TestPruneArchives_DeletesOnlyOld 验证：active 文件（无 "-"）和新归档保留，只有早于
// cutoff 的归档被删；兼容纳秒精度与旧版秒精度两种文件名时间戳。
func TestPruneArchives_DeletesOnlyOld(t *testing.T) {
	dir := t.TempDir()
	// active（无 "-")：绝不能删
	os.WriteFile(filepath.Join(dir, "checklog.jsonl"), []byte("active"), 0644)
	// 新归档（纳秒精度）：保留
	os.WriteFile(filepath.Join(dir, "checklog-20260701120000.000000000.jsonl"), []byte("new"), 0644)
	// 老归档（纳秒精度）：删
	os.WriteFile(filepath.Join(dir, "checklog-20200101120000.000000000.jsonl"), []byte("old-ns"), 0644)
	// 老归档（旧版秒精度）：删
	os.WriteFile(filepath.Join(dir, "checklog-20200101000000.jsonl"), []byte("old-sec"), 0644)
	// 老归档（纳秒精度 + 同纳秒冲突后缀 -1）：删——覆盖 archiveTimestamp 的 "-{i}" 剥离分支
	os.WriteFile(filepath.Join(dir, "checklog-20200101120000.000000000-1.jsonl"), []byte("old-collide"), 0644)

	cutoff := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	removed, err := PruneArchives(dir, "checklog", cutoff)
	if err != nil {
		t.Fatalf("PruneArchives: %v", err)
	}
	if removed != 3 {
		t.Fatalf("removed = %d, want 3", removed)
	}
	if _, err := os.Stat(filepath.Join(dir, "checklog.jsonl")); err != nil {
		t.Errorf("active should be kept: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "checklog-20260701120000.000000000.jsonl")); err != nil {
		t.Errorf("recent archive should be kept: %v", err)
	}
	for _, name := range []string{"checklog-20200101120000.000000000.jsonl", "checklog-20200101000000.jsonl", "checklog-20200101120000.000000000-1.jsonl"} {
		if _, err := os.Stat(filepath.Join(dir, name)); !os.IsNotExist(err) {
			t.Errorf("old archive %s should be pruned", name)
		}
	}
}

// TestPruneArchives_FallbackMtime：文件名时间戳不可解析时 fallback 到 mtime。
func TestPruneArchives_FallbackMtime(t *testing.T) {
	dir := t.TempDir()
	// "garbage" 不是合法时间戳 → 触发 mtime fallback
	path := filepath.Join(dir, "toollog-garbage.jsonl")
	os.WriteFile(path, []byte("old"), 0644)
	oldTime := time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)
	if err := os.Chtimes(path, oldTime, oldTime); err != nil {
		t.Fatal(err)
	}
	removed, err := PruneArchives(dir, "toollog", time.Now())
	if err != nil {
		t.Fatalf("PruneArchives: %v", err)
	}
	if removed != 1 {
		t.Fatalf("removed = %d, want 1 (mtime fallback)", removed)
	}
}

func TestRetentionDays(t *testing.T) {
	t.Setenv("FORGE_LOG_RETENTION_DAYS", "")
	if d := RetentionDays("FORGE_LOG_RETENTION_DAYS", 30); d != 30 {
		t.Errorf("missing env: got %d, want 30", d)
	}
	t.Setenv("FORGE_LOG_RETENTION_DAYS", "14")
	if d := RetentionDays("FORGE_LOG_RETENTION_DAYS", 30); d != 14 {
		t.Errorf("valid env: got %d, want 14", d)
	}
	t.Setenv("FORGE_LOG_RETENTION_DAYS", "notanumber")
	if d := RetentionDays("FORGE_LOG_RETENTION_DAYS", 30); d != 30 {
		t.Errorf("invalid env: got %d, want 30", d)
	}
	t.Setenv("FORGE_LOG_RETENTION_DAYS", "0")
	if d := RetentionDays("FORGE_LOG_RETENTION_DAYS", 30); d != 0 {
		t.Errorf("zero env: got %d, want 0", d)
	}
	t.Setenv("FORGE_LOG_RETENTION_DAYS", "-1")
	if d := RetentionDays("FORGE_LOG_RETENTION_DAYS", 30); d != -1 {
		t.Errorf("negative env: got %d, want -1", d)
	}
}
