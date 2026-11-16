package taskpipeline

import (
	"testing"
)

// TestActivityCheckStillSkipsOnLastGate ensures task-complete (last gate) still
// skips the activity check — we only relaxed the auto-predecessor rule, not the
// last-gate exemption.
func TestActivityCheckStillSkipsOnLastGate(t *testing.T) {
	dir := t.TempDir()
	runGit(t, dir, "init")
	runGit(t, dir, "config", "user.email", "test@test.com")
	runGit(t, dir, "config", "user.name", "Test")

	state := &TaskState{TaskRef: "last-gate", Branch: "feat/test"}
	state.RecordGateResult("task-implement", true, "")
	state.RecordGateResult("task-verify", true, "")
	state.MarkReviewPassed("", "") // 满足 review 硬前置以隔离 activity 逻辑（空基线=跳过快照检查）
	// No toollog activity at all — last gate must still pass.

	_, err := ExecuteTaskGate(dir, "task-complete", state)
	if err != nil {
		t.Fatalf("task-complete (last gate) should skip activity check: %v", err)
	}
}
