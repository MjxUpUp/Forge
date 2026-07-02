package taskpipeline

import (
	"sync"
	"testing"
	"time"
)

// TestSaveTaskState_ConcurrentAtomic guards the C1 fix: SaveTaskState now uses
// util.AtomicWrite (temp+rename), so many goroutines saving the SAME task ref
// leave a complete, loadable state file — never the torn write that plain
// os.WriteFile produces when it truncates before writing (which corrupts the
// JSON every .forge/ loader parses). Under -race this also exercises
// util.AtomicWrite's Windows rename-retry path.
func TestSaveTaskState_ConcurrentAtomic(t *testing.T) {
	dir := t.TempDir()

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			state := &TaskState{
				TaskRef: "feat/atomic",
				Branch:  "feat/atomic",
				Summary: "concurrent save",
			}
			state.CurrentGate = "task-implement"
			// A losing rename on Windows is an expected concurrent-loss, not
			// corruption — the assertion is the final file is loadable.
			_ = SaveTaskState(dir, state)
		}()
	}
	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("SaveTaskState concurrent writes deadlocked")
	}

	loaded, err := LoadTaskState(dir, "feat/atomic")
	if err != nil {
		t.Fatalf("final task state not loadable (torn write?): %v", err)
	}
	if loaded.TaskRef != "feat/atomic" {
		t.Errorf("loaded TaskRef = %q, want feat/atomic", loaded.TaskRef)
	}
}

// TestSetActiveTaskRef_AtomicAndReadable: the active-task-ref writer now uses
// util.AtomicWrite; a write followed by a read must round-trip the ref (a
// truncating os.WriteFile could leave it empty mid-write, breaking active-task
// detection).
func TestSetActiveTaskRef_AtomicAndReadable(t *testing.T) {
	dir := t.TempDir()
	if err := SetActiveTaskRef(dir, "sess-1", "feat/atomic"); err != nil {
		t.Fatalf("SetActiveTaskRef: %v", err)
	}
	if got := ReadActiveTaskRef(dir, "sess-1"); got != "feat/atomic" {
		t.Errorf("active-task-ref = %q, want feat/atomic", got)
	}
}
