package taskpipeline

import (
	"testing"
	"time"

	"github.com/Harness/forge/internal/toolusage"
)

// TestActivityBlocksVerifyWithoutAnyRead verifies the gate fails when the agent
// edited code during the task but never read any — the pure edit-without-read
// failure mode. The check now runs on task-verify (previously dead code, skipped
// because the prior gate task-implement is auto).
func TestActivityBlocksVerifyWithoutAnyRead(t *testing.T) {
	dir := t.TempDir()
	runGit(t, dir, "init")
	runGit(t, dir, "config", "user.email", "test@test.com")
	runGit(t, dir, "config", "user.name", "Test")

	state := &TaskState{TaskRef: "no-read", Branch: "feat/test"}
	state.RecordGateResult("task-implement", true, "")

	// Toollog: edits only, no reads → pure edit-without-read.
	base := time.Now().Add(2 * time.Second)
	records := []toolusage.ToolCall{
		{ToolName: "Edit", TaskRef: "no-read", Timestamp: base},
		{ToolName: "Edit", TaskRef: "no-read", Timestamp: base.Add(time.Second)},
		{ToolName: "Write", TaskRef: "no-read", Timestamp: base.Add(2 * time.Second)},
	}
	for _, r := range records {
		rr := r
		if err := toolusage.Record(dir, &rr); err != nil {
			t.Fatalf("Record: %v", err)
		}
	}

	_, err := ExecuteTaskGate(dir, "task-verify", state)
	if err == nil {
		t.Fatal("task-verify should FAIL when edits exist but no reads (edit-without-read)")
	}
}

// TestActivityAllowsVerifyEditHeavyWithReads verifies edit-heavy work still
// passes as long as the agent read at least once. The read/edit ratio is an
// advisory WARN (read-check hook), not a gate.
func TestActivityAllowsVerifyEditHeavyWithReads(t *testing.T) {
	dir := t.TempDir()
	runGit(t, dir, "init")
	runGit(t, dir, "config", "user.email", "test@test.com")
	runGit(t, dir, "config", "user.name", "Test")

	state := &TaskState{TaskRef: "edit-heavy", Branch: "feat/test"}
	state.RecordGateResult("task-implement", true, "")

	// Toollog: 1 read, 3 edits — edit-heavy but the agent did read first.
	base := time.Now().Add(2 * time.Second)
	records := []toolusage.ToolCall{
		{ToolName: "Read", TaskRef: "edit-heavy", Timestamp: base},
		{ToolName: "Edit", TaskRef: "edit-heavy", Timestamp: base.Add(time.Second)},
		{ToolName: "Edit", TaskRef: "edit-heavy", Timestamp: base.Add(2 * time.Second)},
		{ToolName: "Write", TaskRef: "edit-heavy", Timestamp: base.Add(3 * time.Second)},
	}
	for _, r := range records {
		rr := r
		if err := toolusage.Record(dir, &rr); err != nil {
			t.Fatalf("Record: %v", err)
		}
	}

	_, err := ExecuteTaskGate(dir, "task-verify", state)
	if err != nil {
		t.Fatalf("task-verify should PASS when reads>=1 even if edits>reads: %v", err)
	}
}

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
	// No toollog activity at all — last gate must still pass.

	_, err := ExecuteTaskGate(dir, "task-complete", state)
	if err != nil {
		t.Fatalf("task-complete (last gate) should skip activity check: %v", err)
	}
}
