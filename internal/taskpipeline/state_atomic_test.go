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

// completedAt builds a task state with IsComplete()==true and CompletedAt=completedAt, then saves it.
// All DefaultGates must be filled as passed — MarkComplete only sets CompletedAt / clears CurrentGate,
// IsComplete() looks at history; when the two disagree, PruneOldTasks follows IsComplete() (stricter, to avoid
// deleting tasks in abnormal states).
//
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

// TestPruneOldTasks: only deletes tasks that are IsComplete and whose CompletedAt is before cutoff; recently-completed and
// in-progress tasks are kept.
//
// TestPruneOldTasks：只删 IsComplete 且 CompletedAt 早于 cutoff 的任务；近期完成与
// 进行中的任务保留。
func TestPruneOldTasks(t *testing.T) {
	dir := t.TempDir()
	// complete + old (2020) → pruned.
	//
	// complete + 老（2020）→ 删
	saveCompletedAt(t, dir, "feat/old", time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC))
	// complete + recent → kept.
	//
	// complete + 近期 → 保留
	saveCompletedAt(t, dir, "feat/recent", time.Now())
	// in-progress (not complete) → kept.
	//
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

// TestPruneOldTasks_GatesDoneNeverCompletedZombie 钉住 2026-08-18 死锁修复引入的新滞留类
// 回收（review m2）：门禁全过但从未 `forge task complete`（CompletedAt==nil）。老化锚是
// 最后一道 gate 的通过时间——启动早但门禁刚过的长命任务不得被误杀。
//
// TestPruneOldTasks_GatesDoneNeverCompletedZombie pins pruning of the new straggler
// class from the 2026-08-18 deadlock fix (review m2): all gates passed but never
// `forge task complete`d (CompletedAt==nil). The aging anchor is the LAST gate's pass
// time — a long-lived task whose gates passed recently must not be culled for having
// started early.
func TestPruneOldTasks_GatesDoneNeverCompletedZombie(t *testing.T) {
	dir := t.TempDir()
	// 僵尸类：三门全过、无 CompletedAt、最后一道门在 2020 年通过 → 回收。
	//
	// Zombie class: all gates passed, no CompletedAt, last gate back in 2020 → pruned.
	oldGate := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	zombie := &TaskState{TaskRef: "feat/zombie", Branch: "feat/zombie", StartedAt: oldGate}
	for _, g := range DefaultGates() {
		zombie.RecordGateResult(g.ID, true, "")
	}
	// RecordGateResult 打的是当前时间——把 History 末条时间戳改老，模拟门禁早过。
	//
	// RecordGateResult stamps now — age the final History entry to simulate gates
	// passed long ago.
	h := zombie.History[len(zombie.History)-1]
	h.CompletedAt = oldGate
	zombie.History[len(zombie.History)-1] = h
	if err := SaveTaskState(dir, zombie); err != nil {
		t.Fatalf("SaveTaskState zombie: %v", err)
	}
	// 长命任务：启动于 2020、但最后一道门刚通过（now）→ 保留（StartedAt 锚会误杀）。
	//
	// Long-lived task: started in 2020 but last gate passed just now → kept (a
	// StartedAt anchor would cull it wrongly).
	freshGate := &TaskState{TaskRef: "feat/fresh-gate", Branch: "feat/fresh-gate", StartedAt: oldGate}
	for _, g := range DefaultGates() {
		freshGate.RecordGateResult(g.ID, true, "")
	}
	if err := SaveTaskState(dir, freshGate); err != nil {
		t.Fatalf("SaveTaskState fresh-gate: %v", err)
	}

	cutoff := time.Now().AddDate(0, 0, -30)
	removed, err := PruneOldTasks(dir, cutoff)
	if err != nil {
		t.Fatalf("PruneOldTasks: %v", err)
	}
	if removed != 1 {
		t.Fatalf("removed = %d, want 1 (only the aged zombie)", removed)
	}
	if _, err := LoadTaskState(dir, "feat/zombie"); err == nil {
		t.Error("aged gates-done-never-completed zombie should be pruned")
	}
	if _, err := LoadTaskState(dir, "feat/fresh-gate"); err != nil {
		t.Error("recently-gated long-lived task must be kept（老化锚=最后门禁时间，非 StartedAt）")
	}
}
