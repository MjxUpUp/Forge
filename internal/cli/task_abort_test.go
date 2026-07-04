package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MjxUpUp/Forge/internal/forgedata"
	"github.com/MjxUpUp/Forge/internal/taskcontext"
)

// taskStatePath returns the DataDir/tasks/<sanitized-ref>.json path for dir,
// mirroring how SaveTaskState names state files after the refactor-data-home
// migration (task state lives in the user-level DataDir, NOT <dir>/.forge/).
func taskStatePath(dir, taskRef string) string {
	return filepath.Join(forgedata.DataDirFor(dir), "tasks", taskcontext.SanitizeRef(taskRef)+".json")
}

// TestTaskAbort_ExplicitRef deletes the task state file and clears the
// active-task-ref. This is the escape hatch for ghost/stuck tasks — the gap
// that left the 2026-06-16 code-knowledge-base session unable to clean up a
// task that could never pass its gates.
func TestTaskAbort_ExplicitRef(t *testing.T) {
	// Pin the session id so forge writes the deterministic legacy global
	// active-task-ref file rather than a CLAUDE_CODE_SESSION_ID-scoped one
	// (which the ambient Claude Code env would otherwise inject).
	t.Setenv("CLAUDE_CODE_SESSION_ID", "")
	t.Setenv("FORGE_DATA_HOME", t.TempDir()) // isolate DataDir from real ~/.forge (refactor-data-home)
	tmpDir := t.TempDir()
	runGit(t, tmpDir, "init")
	runGit(t, tmpDir, "config", "user.email", "test@test.com")
	runGit(t, tmpDir, "config", "user.name", "Test")

	if stdout, _, code := runForge(t, tmpDir, "init", "--mode", "medium"); code != 0 {
		t.Fatalf("forge init failed: %s", stdout)
	}
	os.WriteFile(filepath.Join(tmpDir, "main.go"), []byte("package main\nfunc main() {}\n"), 0644)
	runGit(t, tmpDir, "add", ".")
	runGit(t, tmpDir, "commit", "-m", "initial")
	runGit(t, tmpDir, "checkout", "-b", "feature/test-abort")

	// Start a task with an explicit ref. It must create the state file AND set
	// the active-task-ref (the legacy global file, since no CLAUDE_CODE_SESSION_ID).
	if stdout, _, code := runForge(t, tmpDir, "task", "start", "--ref", "feature/test-abort", "--title", "abort probe"); code != 0 {
		t.Fatalf("forge task start failed: %s", stdout)
	}
	statePath := taskStatePath(tmpDir, "feature/test-abort")
	if _, err := os.Stat(statePath); err != nil {
		t.Fatalf("task state file not created at %s: %v", statePath, err)
	}
	activeRefPath := filepath.Join(forgedata.DataDirFor(tmpDir), "active-task-ref")
	if _, err := os.Stat(activeRefPath); err != nil {
		t.Fatalf("active-task-ref not created: %v", err)
	}

	// Abort by explicit ref.
	stdout, _, code := runForge(t, tmpDir, "task", "abort", "--ref", "feature/test-abort")
	if code != 0 {
		t.Fatalf("forge task abort exit %d, output: %s", code, stdout)
	}
	if !strings.Contains(stdout, "Task aborted") {
		t.Fatalf("abort output missing confirmation, got: %s", stdout)
	}

	// Core guarantee: the state file is gone.
	if _, err := os.Stat(statePath); !os.IsNotExist(err) {
		t.Fatalf("task state file still exists after abort (err=%v)", err)
	}
	// The active-task-ref pointing at the aborted task is cleared.
	if _, err := os.Stat(activeRefPath); !os.IsNotExist(err) {
		t.Fatalf("active-task-ref still exists after abort (err=%v)", err)
	}

	// Idempotent: re-aborting an already-gone task must not error (it may be a
	// stale dangling ref). This is what makes abort safe to call repeatedly.
	stdout, _, code = runForge(t, tmpDir, "task", "abort", "--ref", "feature/test-abort")
	if code != 0 {
		t.Fatalf("re-abort of missing task should be idempotent, got exit %d: %s", code, stdout)
	}
}

// TestTaskAbort_NoRefResolvesActiveTask verifies abort without --ref resolves to
// the session's active task — the common case for cleaning up a half-started task.
func TestTaskAbort_NoRefResolvesActiveTask(t *testing.T) {
	t.Setenv("CLAUDE_CODE_SESSION_ID", "")
	t.Setenv("FORGE_DATA_HOME", t.TempDir()) // isolate DataDir from real ~/.forge (refactor-data-home)
	tmpDir := t.TempDir()
	runGit(t, tmpDir, "init")
	runGit(t, tmpDir, "config", "user.email", "test@test.com")
	runGit(t, tmpDir, "config", "user.name", "Test")
	if stdout, _, code := runForge(t, tmpDir, "init", "--mode", "medium"); code != 0 {
		t.Fatalf("forge init failed: %s", stdout)
	}
	os.WriteFile(filepath.Join(tmpDir, "main.go"), []byte("package main\nfunc main() {}\n"), 0644)
	runGit(t, tmpDir, "add", ".")
	runGit(t, tmpDir, "commit", "-m", "initial")
	runGit(t, tmpDir, "checkout", "-b", "feature/active-probe")

	// Start with branch-derived ref (no explicit --ref).
	if stdout, _, code := runForge(t, tmpDir, "task", "start"); code != 0 {
		t.Fatalf("forge task start failed: %s", stdout)
	}
	statePath := taskStatePath(tmpDir, "feature/active-probe")
	if _, err := os.Stat(statePath); err != nil {
		t.Fatalf("task state file not created: %v", err)
	}

	// Abort with no --ref — must resolve the active task.
	stdout, _, code := runForge(t, tmpDir, "task", "abort")
	if code != 0 {
		t.Fatalf("forge task abort (no ref) exit %d, output: %s", code, stdout)
	}
	if _, err := os.Stat(statePath); !os.IsNotExist(err) {
		t.Fatalf("active task state file still exists after abort (err=%v)", err)
	}
}

// TestTaskAbort_NoTaskErrors verifies abort errors clearly when there is no task
// to identify, rather than silently no-op'ing or deleting something unexpected.
func TestTaskAbort_NoTaskErrors(t *testing.T) {
	tmpDir := t.TempDir()
	runGit(t, tmpDir, "init")
	runGit(t, tmpDir, "config", "user.email", "test@test.com")
	runGit(t, tmpDir, "config", "user.name", "Test")
	if stdout, _, code := runForge(t, tmpDir, "init", "--mode", "medium"); code != 0 {
		t.Fatalf("forge init failed: %s", stdout)
	}
	// On master with no active task and no --ref → must error.
	stdout, _, code := runForge(t, tmpDir, "task", "abort")
	if code == 0 {
		t.Fatalf("expected non-zero exit when no task to abort, got exit 0: %s", stdout)
	}
	if !strings.Contains(stdout, "no task to abort") {
		t.Fatalf("error output should guide the user, got: %s", stdout)
	}
}
