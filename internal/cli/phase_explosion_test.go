package cli

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/MjxUpUp/Forge/internal/taskpipeline"
)

func saveIncompleteTaskState(t *testing.T, dir, ref, sessionID string) {
	t.Helper()
	s := &taskpipeline.TaskState{
		TaskRef:   ref,
		SessionID: sessionID,
		Branch:    "feat/x",
		StartedAt: time.Now(),
	}
	if err := taskpipeline.SaveTaskState(dir, s); err != nil {
		t.Fatalf("save %s: %v", ref, err)
	}
}

// TestPhaseExplosionWarning_TriggersAtThree: 3 incomplete tasks in the same
// session must produce a warning when starting a new task.
func TestPhaseExplosionWarning_TriggersAtThree(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, ".forge", "tasks"), 0755)
	saveIncompleteTaskState(t, dir, "feat/a", "sess-1")
	saveIncompleteTaskState(t, dir, "feat/b", "sess-1")
	saveIncompleteTaskState(t, dir, "feat/c", "sess-1")

	if w := phaseExplosionWarning(dir, "sess-1", "feat/new"); w == "" {
		t.Fatal("expected phase explosion warning with 3 active tasks in same session")
	}
}

// TestPhaseExplosionWarning_SilentBelowThreshold: 2 tasks is below the threshold.
func TestPhaseExplosionWarning_SilentBelowThreshold(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, ".forge", "tasks"), 0755)
	saveIncompleteTaskState(t, dir, "feat/a", "sess-1")
	saveIncompleteTaskState(t, dir, "feat/b", "sess-1")

	if w := phaseExplosionWarning(dir, "sess-1", "feat/new"); w != "" {
		t.Fatalf("expected no warning with 2 tasks, got: %s", w)
	}
}

// TestPhaseExplosionWarning_IgnoresOtherSessions: tasks in a different session
// must not count toward this session's total.
func TestPhaseExplosionWarning_IgnoresOtherSessions(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, ".forge", "tasks"), 0755)
	saveIncompleteTaskState(t, dir, "feat/a", "sess-1")
	saveIncompleteTaskState(t, dir, "feat/b", "sess-1")
	saveIncompleteTaskState(t, dir, "feat/c", "sess-2") // different session

	if w := phaseExplosionWarning(dir, "sess-1", "feat/new"); w != "" {
		t.Fatalf("expected no warning — only 2 tasks in sess-1, got: %s", w)
	}
}

// TestPhaseExplosionWarning_IgnoresCompleted: completed tasks do not count.
func TestPhaseExplosionWarning_IgnoresCompleted(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, ".forge", "tasks"), 0755)
	saveIncompleteTaskState(t, dir, "feat/a", "sess-1")
	saveIncompleteTaskState(t, dir, "feat/b", "sess-1")
	completed := &taskpipeline.TaskState{TaskRef: "feat/c", SessionID: "sess-1", Branch: "feat/x", StartedAt: time.Now()}
	now := time.Now()
	completed.CompletedAt = &now
	if err := taskpipeline.SaveTaskState(dir, completed); err != nil {
		t.Fatalf("save completed: %v", err)
	}

	if w := phaseExplosionWarning(dir, "sess-1", "feat/new"); w != "" {
		t.Fatalf("expected no warning — completed task ignored, got: %s", w)
	}
}

// TestPhaseExplosionWarning_EmptySession: no session id → no warning (defensive).
func TestPhaseExplosionWarning_EmptySession(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, ".forge", "tasks"), 0755)
	if w := phaseExplosionWarning(dir, "", "feat/new"); w != "" {
		t.Fatalf("expected no warning for empty session id, got: %s", w)
	}
}
