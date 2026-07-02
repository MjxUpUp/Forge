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
