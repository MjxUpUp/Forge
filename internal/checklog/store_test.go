package checklog

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
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
