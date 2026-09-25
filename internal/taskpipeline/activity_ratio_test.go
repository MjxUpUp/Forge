package taskpipeline

import (
	"testing"
	"time"

	"github.com/MjxUpUp/Forge/internal/toolusage"
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
// passes as long as the agent read at least once. The read/edit ratio is a
// scoring signal, not a gate (read-check WARN was sunk to Red Flags text).
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
	state.MarkReviewPassed("", "") // 满足 review 硬前置以隔离 activity 逻辑（空基线=跳过快照检查）
	// No toollog activity at all — last gate must still pass.

	_, err := ExecuteTaskGate(dir, "task-complete", state)
	if err != nil {
		t.Fatalf("task-complete (last gate) should skip activity check: %v", err)
	}
}

// TestActivityGraceRecoversTaskStartReadRace verifies the race-recovery path
// added for the ce9b2410 finding: when an agent fires Read concurrently with
// `forge task start`, the Read is logged under the PREVIOUS task's ref (the
// active ref hasn't switched yet) and/or with a timestamp just before the new
// task's StartedAt — so ReadEditCounts(newTaskRef, StartedAt) sees 0 reads and
// the gate falsely hard-fails. The grace window re-counts Reads across all
// tasks; with a nearby Read present, task-verify passes (the agent DID read
// before editing). The companion TestActivityBlocksVerifyWithoutAnyRead covers
// the grace==0 path (genuine edit-without-read still hard-fails).
func TestActivityGraceRecoversTaskStartReadRace(t *testing.T) {
	dir := t.TempDir()
	runGit(t, dir, "init")
	runGit(t, dir, "config", "user.email", "test@test.com")
	runGit(t, dir, "config", "user.name", "Test")

	// StartedAt anchored to a fixed moment so the race Read lands at
	// StartedAt-2s — inside the 60s grace window but BEFORE StartedAt (the
	// exact shape of the task-start/Read race).
	base := time.Now().Add(2 * time.Second)
	state := &TaskState{TaskRef: "race-task", Branch: "feat/test", StartedAt: base}
	state.RecordGateResult("task-implement", true, "")

	// The agent DID read, but that Read raced task start: logged under the
	// PREVIOUS task's ref, 2s before this task's StartedAt. Reads for THIS
	// task are zero; edits are present → would hard-fail without grace.
	records := []toolusage.ToolCall{
		{ToolName: "Read", TaskRef: "previous-task", Timestamp: base.Add(-2 * time.Second)},
		{ToolName: "Edit", TaskRef: "race-task", Timestamp: base.Add(1 * time.Second)},
		{ToolName: "Write", TaskRef: "race-task", Timestamp: base.Add(2 * time.Second)},
	}
	for _, r := range records {
		rr := r
		if err := toolusage.Record(dir, &rr); err != nil {
			t.Fatalf("Record: %v", err)
		}
	}

	res, err := ExecuteTaskGate(dir, "task-verify", state)
	if err != nil {
		t.Fatalf("task-verify should PASS via grace recovery (nearby cross-task Read means the agent did read before editing): %v", err)
	}
	if res == nil || !res.Passed {
		t.Fatalf("expected passed result, got %+v", res)
	}
}
