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

// completedAt 构造一个 IsComplete()==true 且 CompletedAt=completedAt 的任务状态并存盘。
// 必须填齐 DefaultGates 全部 passed——MarkComplete 只设 CompletedAt/清 CurrentGate，
// IsComplete() 看 history，二者不一致时 PruneOldTasks 以 IsComplete() 为准（更严，避免
// 删异常状态的任务）。
func saveCompletedAt(t *testing.T, dir, ref string, completedAt time.Time) {
	t.Helper()
	s := &TaskState{TaskRef: ref, Branch: ref, Summary: ref}
	for _, g := range DefaultGates() {
		s.RecordGateResult(g.ID, true, "")
	}
	s.MarkComplete()
	s.CompletedAt = &completedAt
	if err := SaveTaskState(dir, s); err != nil {
		t.Fatalf("SaveTaskState %s: %v", ref, err)
	}
}

// TestPruneOldTasks：只删 IsComplete 且 CompletedAt 早于 cutoff 的任务；近期完成与
// 进行中的任务保留。
func TestPruneOldTasks(t *testing.T) {
	dir := t.TempDir()
	// complete + 老（2020）→ 删
	saveCompletedAt(t, dir, "feat/old", time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC))
	// complete + 近期 → 保留
	saveCompletedAt(t, dir, "feat/recent", time.Now())
	// in-progress（未 complete）→ 保留
	inprog := &TaskState{TaskRef: "feat/inprog", Branch: "feat/inprog", CurrentGate: "task-implement"}
	if err := SaveTaskState(dir, inprog); err != nil {
		t.Fatalf("SaveTaskState inprog: %v", err)
	}

	cutoff := time.Now().AddDate(0, 0, -30)
	removed, err := PruneOldTasks(dir, cutoff)
	if err != nil {
		t.Fatalf("PruneOldTasks: %v", err)
	}
	if removed != 1 {
		t.Fatalf("removed = %d, want 1 (only old completed)", removed)
	}
	if _, err := LoadTaskState(dir, "feat/old"); err == nil {
		t.Error("old completed task should be pruned")
	}
	if _, err := LoadTaskState(dir, "feat/recent"); err != nil {
		t.Error("recent completed task should be kept")
	}
	if _, err := LoadTaskState(dir, "feat/inprog"); err != nil {
		t.Error("in-progress task should be kept")
	}
}
