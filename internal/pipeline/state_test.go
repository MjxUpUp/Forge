package pipeline

import (
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
