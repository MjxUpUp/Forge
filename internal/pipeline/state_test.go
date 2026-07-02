package pipeline

import (
	"sync"
	"testing"
	"time"
)

func TestStateSaveAndLoad(t *testing.T) {
	dir := t.TempDir()

	state := &State{
		PipelineVersion: "2.0",
		Mode:            "medium",
		StartedAt:       time.Now().Truncate(time.Second),
		History: []GateHistory{
			{Gate: "gate-1-prd", Passed: true, Attempt: 1, CompletedAt: time.Now().Truncate(time.Second)},
		},
	}

	if err := state.Save(dir); err != nil {
		t.Fatalf("Save() error: %v", err)
	}

	loaded, err := LoadState(dir)
	if err != nil {
		t.Fatalf("LoadState() error: %v", err)
	}

	if loaded.Mode != "medium" {
		t.Errorf("Mode = %s, want medium", loaded.Mode)
	}
	if len(loaded.History) != 1 {
		t.Errorf("History length = %d, want 1", len(loaded.History))
	}
	if !loaded.History[0].Passed {
		t.Error("History[0].Passed = false, want true")
	}
}

func TestStatePrerequisitesPassed(t *testing.T) {
	state := &State{
		History: []GateHistory{
			{Gate: "gate-1-prd", Passed: true},
			{Gate: "gate-2-design", Passed: false},
		},
	}

	// Dependency passed
	err := state.PrerequisitesPassed([]string{"gate-1-prd"})
	if err != nil {
		t.Errorf("PrerequisitesPassed([gate-1-prd]) error: %v", err)
	}

	// Dependency failed
	err = state.PrerequisitesPassed([]string{"gate-2-design"})
	if err == nil {
		t.Error("PrerequisitesPassed([gate-2-design]) should return error")
	}

	// Dependency not executed
	err = state.PrerequisitesPassed([]string{"gate-4-implement"})
	if err == nil {
		t.Error("PrerequisitesPassed([non-executed]) should return error")
	}
}

func TestStateOverrides(t *testing.T) {
	state := &State{}
	state.AddOverride("gate-2-design", "--force")

	err := state.PrerequisitesPassed([]string{"gate-2-design"})
	if err != nil {
		t.Errorf("PrerequisitesPassed with override should pass: %v", err)
	}
}

func TestStateMarkGate(t *testing.T) {
	state := &State{}

	state.MarkGateStarted("gate-1-prd")
	if state.CurrentGate != "gate-1-prd" {
		t.Errorf("CurrentGate = %s, want gate-1-prd", state.CurrentGate)
	}
	if state.StartedAt.IsZero() {
		t.Error("StartedAt should be set")
	}

	state.MarkGateComplete("gate-1-prd", true, 1500)
	if len(state.History) != 1 {
		t.Errorf("History length = %d, want 1", len(state.History))
	}
	if state.CurrentGate != "" {
		t.Errorf("CurrentGate = %s, want '' after completion", state.CurrentGate)
	}
	if state.History[0].Attempt != 1 {
		t.Errorf("Attempt = %d, want 1", state.History[0].Attempt)
	}
	if state.History[0].DurationMs != 1500 {
		t.Errorf("DurationMs = %d, want 1500", state.History[0].DurationMs)
	}
}

func TestStateAttemptCount(t *testing.T) {
	state := &State{}
	state.MarkGateComplete("gate-1", false, 100)
	state.MarkGateComplete("gate-1", false, 200)
	state.MarkGateComplete("gate-1", true, 300)

	count := state.AttemptCount("gate-1")
	if count != 3 {
		t.Errorf("AttemptCount = %d, want 3", count)
	}
}

func TestHasGateCompleted(t *testing.T) {
	state := &State{
		History: []GateHistory{
			{Gate: "gate-1-prd", Passed: false},
			{Gate: "gate-1-prd", Passed: true},
			{Gate: "gate-2-design", Passed: false},
		},
	}

	if !HasGateCompleted(state, "gate-1-prd") {
		t.Error("HasGateCompleted(gate-1-prd) = false, want true (latest run passed)")
	}
	if HasGateCompleted(state, "gate-2-design") {
		t.Error("HasGateCompleted(gate-2-design) = true, want false (failed)")
	}
	if HasGateCompleted(state, "gate-3-plan") {
		t.Error("HasGateCompleted(gate-3-plan) = true, want false (not executed)")
	}
}

func TestStateHistoryIsAppendOnly(t *testing.T) {
	state := &State{}
	state.MarkGateComplete("gate-1", false, 100)
	state.MarkGateComplete("gate-1", true, 200)

	if len(state.History) != 2 {
		t.Errorf("History should have 2 entries, got %d", len(state.History))
	}
	if state.History[0].Passed {
		t.Error("First attempt should be failed")
	}
	if !state.History[1].Passed {
		t.Error("Second attempt should be passed")
	}
}

// TestGateFailures：iteration circuit breaker 的判据——同一 gate 的失败次数（passed 不计入）。
// 守护 breaker 不误触发：正常"修了再试"往返（fail→pass）不计为反复失败，只有连续 fail 才警示卡死。
func TestGateFailures(t *testing.T) {
	state := &State{}
	state.MarkGateComplete("gate-1", false, 100) // fail 1
	state.MarkGateComplete("gate-1", false, 200) // fail 2
	state.MarkGateComplete("gate-1", true, 300)  // pass（不计入失败）
	state.MarkGateComplete("gate-2", false, 400) // gate-2 fail 1

	if got := GateFailures(state, "gate-1"); got != 2 {
		t.Fatalf("GateFailures(gate-1)=%d want 2（2 fail + 1 pass，pass 不计入）", got)
	}
	if got := GateFailures(state, "gate-2"); got != 1 {
		t.Fatalf("GateFailures(gate-2)=%d want 1", got)
	}
	if got := GateFailures(state, "gate-3"); got != 0 {
		t.Fatalf("GateFailures(gate-3)=%d want 0（未执行）", got)
	}
}

// TestStateSave_ConcurrentAtomic guards the C1 fix: State.Save now uses
// util.AtomicWrite, so concurrent saves of state.json leave a complete, loadable
// file — never the torn JSON a truncating os.WriteFile produces mid-write.
func TestStateSave_ConcurrentAtomic(t *testing.T) {
	dir := t.TempDir()
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			s := &State{Mode: "medium", PipelineVersion: "2.0"}
			s.MarkGateStarted("gate-1")
			// A losing rename on Windows is an expected concurrent-loss; the
			// assertion is the final file is loadable (not corrupt).
			_ = s.Save(dir)
		}()
	}
	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("State.Save concurrent writes deadlocked")
	}
	loaded, err := LoadState(dir)
	if err != nil {
		t.Fatalf("final state.json not loadable (torn write?): %v", err)
	}
	if loaded.Mode != "medium" {
		t.Errorf("loaded Mode = %q, want medium", loaded.Mode)
	}
}

// TestAddOverride_Dedup guards the A5 fix: repeated --force on the same gate
// previously appended duplicate Override entries, so Overrides grew without
// bound — polluting state.json and inflating `forge status`. AddOverride now
// dedups by gate ID.
func TestAddOverride_Dedup(t *testing.T) {
	state := &State{}
	state.AddOverride("gate-1", "--force")
	state.AddOverride("gate-1", "--force")
	state.AddOverride("gate-1", "--force")

	if !state.IsOverridden("gate-1") {
		t.Fatal("IsOverridden(gate-1) = false, want true")
	}
	if len(state.Overrides) != 1 {
		t.Errorf("len(Overrides) = %d, want 1 (dedup by gate ID)", len(state.Overrides))
	}
}

// TestRemoveOverride guards the A5 fix: when a gate subsequently passes WITHOUT
// --force, its stale override is cleared. Other gates' overrides are preserved,
// and removing a never-overridden gate is a safe no-op.
func TestRemoveOverride(t *testing.T) {
	state := &State{}
	state.AddOverride("gate-1", "--force")
	state.AddOverride("gate-2", "--force")

	state.RemoveOverride("gate-1")

	if state.IsOverridden("gate-1") {
		t.Error("IsOverridden(gate-1) = true, want false after RemoveOverride")
	}
	if !state.IsOverridden("gate-2") {
		t.Error("IsOverridden(gate-2) = false, want true (other overrides preserved)")
	}
	if len(state.Overrides) != 1 {
		t.Errorf("len(Overrides) = %d, want 1", len(state.Overrides))
	}

	// RemoveOverride on a gate that was never overridden is a no-op.
	state.RemoveOverride("gate-3")
	if !state.IsOverridden("gate-2") {
		t.Error("IsOverridden(gate-2) = false after no-op RemoveOverride(gate-3)")
	}
}

// TestSaveStatus_AtomicAndReadable covers the status.go atomic-write path
// (no dedicated status_test.go exists; this satisfies the same-package test
// requirement and guards the gate status.json writer).
func TestSaveStatus_AtomicAndReadable(t *testing.T) {
	dir := t.TempDir()
	s := &GateStatus{Gate: "gate-1", Passed: true, Attempt: 1}
	if err := SaveStatus(dir, "gate-1", s); err != nil {
		t.Fatalf("SaveStatus: %v", err)
	}
	loaded, err := LoadStatus(dir, "gate-1")
	if err != nil {
		t.Fatalf("LoadStatus: %v", err)
	}
	if loaded.Gate != "gate-1" || !loaded.Passed {
		t.Errorf("loaded = %+v, want gate-1/passed", loaded)
	}
}
