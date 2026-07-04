package checklog

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/MjxUpUp/Forge/internal/forgedata/forgedatatest"
)

func TestRecordAndLoadAll(t *testing.T) {
	dir := t.TempDir()
	forgeDir := filepath.Join(dir, ".forge")
	os.MkdirAll(forgeDir, 0755)

	entry1 := &Entry{
		Check:  CheckAutoCompile,
		Passed: true,
		Checked: true,
		Detail: "All builds passed",
	}
	entry2 := &Entry{
		Check:  CheckAssertion,
		Passed: false,
		Checked: true,
		Detail: "t.Fatal removed",
	}

	if err := Record(dir, entry1); err != nil {
		t.Fatalf("Record entry1: %v", err)
	}
	if err := Record(dir, entry2); err != nil {
		t.Fatalf("Record entry2: %v", err)
	}

	entries, err := LoadAll(dir)
	if err != nil {
		t.Fatalf("LoadAll: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(entries))
	}
	if entries[0].Check != CheckAutoCompile {
		t.Errorf("entry[0].Check = %q, want %q", entries[0].Check, CheckAutoCompile)
	}
	if !entries[0].Passed {
		t.Errorf("entry[0].Passed = false, want true")
	}
	if entries[1].Check != CheckAssertion {
		t.Errorf("entry[1].Check = %q, want %q", entries[1].Check, CheckAssertion)
	}
	if entries[1].Passed {
		t.Errorf("entry[1].Passed = true, want false")
	}
	// RecordedAt should be set
	if entries[0].RecordedAt.IsZero() {
		t.Error("entry[0].RecordedAt is zero")
	}
}

func TestLoadAll_NoFile(t *testing.T) {
	dir := t.TempDir()
	entries, err := LoadAll(dir)
	if err != nil {
		t.Fatalf("LoadAll on missing file: %v", err)
	}
	if entries != nil {
		t.Fatalf("expected nil entries, got %v", entries)
	}
}

func TestLatestByCheck(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, ".forge"), 0755)

	// Record two entries for auto-compile: one fail, then one pass
	Record(dir, &Entry{Check: CheckAutoCompile, Passed: false, Detail: "failed"})
	time.Sleep(10 * time.Millisecond) // ensure ordering
	Record(dir, &Entry{Check: CheckAutoCompile, Passed: true, Detail: "passed"})
	Record(dir, &Entry{Check: CheckAssertion, Passed: true, Detail: "ok"})

	latest, err := LatestByCheck(dir)
	if err != nil {
		t.Fatalf("LatestByCheck: %v", err)
	}
	if len(latest) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(latest))
	}
	// Latest auto-compile should be the passing one
	if ac, ok := latest[CheckAutoCompile]; !ok {
		t.Fatal("auto-compile not in results")
	} else if !ac.Passed {
		t.Error("latest auto-compile should be passed")
	}
	if as, ok := latest[CheckAssertion]; !ok {
		t.Fatal("assertion-check not in results")
	} else if !as.Passed {
		t.Error("assertion-check should be passed")
	}
}

func TestClear(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, ".forge"), 0755)

	Record(dir, &Entry{Check: CheckAutoCompile, Passed: true, Detail: "ok"})

	// File should exist
	if _, err := os.Stat(filepath.Join(dir, ".forge", "checklog.jsonl")); os.IsNotExist(err) {
		t.Fatal("checklog.jsonl should exist after Record")
	}

	if err := Clear(dir); err != nil {
		t.Fatalf("Clear: %v", err)
	}

	// File should be gone
	if _, err := os.Stat(filepath.Join(dir, ".forge", "checklog.jsonl")); !os.IsNotExist(err) {
		t.Fatal("checklog.jsonl should be removed after Clear")
	}

	// Clear on nonexistent file should not error
	if err := Clear(dir); err != nil {
		t.Fatalf("Clear on nonexistent: %v", err)
	}
}

func TestArchive(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, ".forge"), 0755)

	Record(dir, &Entry{Check: CheckAutoCompile, Passed: true, Detail: "ok"})

	if err := Archive(dir); err != nil {
		t.Fatalf("Archive: %v", err)
	}

	// Original file should be gone
	if _, err := os.Stat(filepath.Join(dir, ".forge", "checklog.jsonl")); !os.IsNotExist(err) {
		t.Fatal("checklog.jsonl should not exist after Archive")
	}

	// Timestamped archive should exist
	entries, err := os.ReadDir(filepath.Join(dir, ".forge"))
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "checklog-") && strings.HasSuffix(e.Name(), ".jsonl") {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("no timestamped archive found in .forge/")
	}

	// Archive on nonexistent should be idempotent
	if err := Archive(dir); err != nil {
		t.Fatalf("Archive on nonexistent: %v", err)
	}
}

func TestRecord_SetsRecordedAt(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, ".forge"), 0755)

	before := time.Now()
	Record(dir, &Entry{Check: CheckAutoCompile, Passed: true})
	after := time.Now()

	entries, _ := LoadAll(dir)
	if entries[0].RecordedAt.Before(before) || entries[0].RecordedAt.After(after) {
		t.Errorf("RecordedAt %v not between %v and %v", entries[0].RecordedAt, before, after)
	}
}

func TestLoadForTask(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, ".forge"), 0755)

	// Active checklog: two task refs interleaved.
	Record(dir, &Entry{Check: CheckAutoCompile, Passed: true, TaskRef: "feat/x", Detail: "active-auto"})
	Record(dir, &Entry{Check: CheckAssertion, Passed: false, TaskRef: "feat/y", Detail: "other-task"})
	Record(dir, &Entry{Check: CheckTaskVerify, Passed: true, TaskRef: "feat/x", Detail: "active-exp"})

	// Archived checklog (produced by Archive on a previous task start) — feat/x
	// history that LoadAll would miss. This is the gap LoadForTask closes.
	archivePath := filepath.Join(dir, ".forge", "checklog-20260101000000.jsonl")
	archived := []byte(`{"check":"auto-compile","passed":true,"checked":true,"task_ref":"feat/x","detail":"archived","recorded_at":"2026-01-01T00:00:00Z"}` + "\n")
	if err := os.WriteFile(archivePath, archived, 0644); err != nil {
		t.Fatal(err)
	}

	got, err := LoadForTask(dir, "feat/x")
	if err != nil {
		t.Fatalf("LoadForTask: %v", err)
	}
	// 2 active (auto-compile, security) + 1 archived.
	if len(got) != 3 {
		t.Fatalf("expected 3 entries for feat/x, got %d: %+v", len(got), got)
	}
	for _, e := range got {
		if e.TaskRef != "feat/x" {
			t.Errorf("entry TaskRef = %q, want feat/x", e.TaskRef)
		}
	}
	// Sorted ascending by RecordedAt — the archived entry (2026-01-01) is earliest.
	if got[0].Detail != "archived" {
		t.Errorf("first entry should be the archived (earliest ts), got Detail=%q", got[0].Detail)
	}
}

func TestLoadForTask_NoMatch(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, ".forge"), 0755)
	Record(dir, &Entry{Check: CheckAutoCompile, Passed: true, TaskRef: "feat/x"})

	got, err := LoadForTask(dir, "nonexistent-ref")
	if err != nil {
		t.Fatalf("LoadForTask no match: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("expected 0 entries for nonexistent ref, got %d", len(got))
	}
}

// TestRecordAndClear_ConcurrentNoDeadlock guards the C2 fix: checklog Clear and
// Archive now hold the same mutex as Record. The fix splits archiveLocked out of
// Archive so Clear can archive-then-remove under one lock WITHOUT re-entering
// the non-reentrant mutex (a re-entry would deadlock; the timeout surfaces it).
func TestRecordAndClear_ConcurrentNoDeadlock(t *testing.T) {
	dir := t.TempDir()
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			for j := 0; j < 5; j++ {
				_ = Record(dir, &Entry{Check: CheckAutoCompile, Passed: true, TaskRef: "t"})
			}
		}()
		go func() {
			defer wg.Done()
			_ = Clear(dir)
		}()
	}
	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Record/Clear deadlocked (Clear→Archive mutex re-entry?)")
	}
	if _, err := LoadAll(dir); err != nil {
		t.Fatalf("LoadAll after concurrent Record/Clear: %v", err)
	}
}

// TestArchive_NanosecondNaming guards the C3 fix: archive names carry nanosecond
// precision so two same-second rotations don't collide.
func TestArchive_NanosecondNaming(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, ".forge"), 0755)
	if err := Record(dir, &Entry{Check: CheckAutoCompile, Passed: true}); err != nil {
		t.Fatal(err)
	}
	if err := Archive(dir); err != nil {
		t.Fatalf("Archive: %v", err)
	}
	entries, err := os.ReadDir(filepath.Join(dir, ".forge"))
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		name := e.Name()
		if !strings.HasPrefix(name, "checklog-") || !strings.HasSuffix(name, ".jsonl") {
			continue
		}
		stamp := strings.TrimSuffix(strings.TrimPrefix(name, "checklog-"), ".jsonl")
		if !strings.Contains(stamp, ".") {
			t.Errorf("archive name %q lacks nanosecond precision (C3 regression)", name)
		}
		return
	}
	t.Fatal("no checklog-* archive produced by Archive")
}

// TestClear_PrunesOldArchives：Clear 在轮转后按 FORGE_LOG_RETENTION_DAYS 清超期归档，
// 保留近期归档与 active 清空语义。
func TestClear_PrunesOldArchives(t *testing.T) {
	t.Setenv("FORGE_LOG_RETENTION_DAYS", "30")
	dir := t.TempDir()
	forgeDir := filepath.Join(dir, ".forge")
	os.MkdirAll(forgeDir, 0755)
	// 老归档（2000 年，必然超 30 天）→ 删
	os.WriteFile(filepath.Join(forgeDir, "checklog-20000101000000.jsonl"), []byte("old"), 0644)
	// 新归档（今天时间戳）→ 保留
	today := time.Now().Format("20060102150405.000000000")
	os.WriteFile(filepath.Join(forgeDir, "checklog-"+today+".jsonl"), []byte("new"), 0644)
	// active
	Record(dir, &Entry{Check: CheckAutoCompile, Passed: true})

	if err := Clear(dir); err != nil {
		t.Fatalf("Clear: %v", err)
	}
	if _, err := os.Stat(filepath.Join(forgeDir, "checklog-20000101000000.jsonl")); !os.IsNotExist(err) {
		t.Error("old archive should be pruned after Clear")
	}
	if _, err := os.Stat(filepath.Join(forgeDir, "checklog-"+today+".jsonl")); err != nil {
		t.Error("recent archive should be kept")
	}
	if _, err := os.Stat(filepath.Join(forgeDir, "checklog.jsonl")); !os.IsNotExist(err) {
		t.Error("active should be removed after Clear")
	}
}

// TestClear_DisabledRetention：FORGE_LOG_RETENTION_DAYS=0 禁用清理，老归档保留。
func TestClear_DisabledRetention(t *testing.T) {
	t.Setenv("FORGE_LOG_RETENTION_DAYS", "0")
	dir := t.TempDir()
	forgeDir := filepath.Join(dir, ".forge")
	os.MkdirAll(forgeDir, 0755)
	os.WriteFile(filepath.Join(forgeDir, "checklog-20000101000000.jsonl"), []byte("old"), 0644)
	Record(dir, &Entry{Check: CheckAutoCompile, Passed: true})

	if err := Clear(dir); err != nil {
		t.Fatalf("Clear: %v", err)
	}
	if _, err := os.Stat(filepath.Join(forgeDir, "checklog-20000101000000.jsonl")); err != nil {
		t.Error("with retention disabled, old archive should be kept")
	}
}

// TestRecord_WritesToDataDir_GitProject guards the refactor-data-home migration:
// for a real git project, checklog must land in the user-level DataDir
// (~/.forge/projects/<key>/), NOT the legacy project-level <root>/.forge/.
// Non-git tmp-dir tests above exercise the fallback path; this one exercises
// the DataDir path through a real ProjectFor so the migration is actually
// covered (the fallback tests would pass even if DataDir resolution were dead).
func TestRecord_WritesToDataDir_GitProject(t *testing.T) {
	root, p := forgedatatest.RealProject(t)
	if err := Record(root, &Entry{Check: CheckAutoCompile, Passed: true, TaskRef: "t-data"}); err != nil {
		t.Fatalf("Record: %v", err)
	}
	// checklog must NOT be in the legacy ConfigDir.
	if _, err := os.Stat(filepath.Join(root, ".forge", "checklog.jsonl")); err == nil {
		t.Fatal("checklog should NOT be in legacy ConfigDir <root>/.forge/ for a git project")
	}
	// checklog must be in the DataDir.
	if _, err := os.Stat(p.ChecklogPath()); err != nil {
		t.Errorf("checklog should be in DataDir %s: %v", p.ChecklogPath(), err)
	}
	// LoadAll reads back from the DataDir.
	entries, err := LoadAll(root)
	if err != nil {
		t.Fatalf("LoadAll: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry from DataDir, got %d", len(entries))
	}
	if entries[0].TaskRef != "t-data" {
		t.Errorf("TaskRef = %q, want t-data", entries[0].TaskRef)
	}
}
