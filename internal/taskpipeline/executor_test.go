package taskpipeline

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/Harness/forge/internal/taskcontext"
)

func TestDefaultGates(t *testing.T) {
	gates := DefaultGates()
	if len(gates) != 5 {
		t.Fatalf("DefaultGates count = %d, want 5", len(gates))
	}

	// Check order
	wantIDs := []string{"task-understand", "task-design", "task-implement", "task-verify", "task-complete"}
	for i, g := range gates {
		if g.ID != wantIDs[i] {
			t.Errorf("gates[%d].ID = %q, want %q", i, g.ID, wantIDs[i])
		}
	}
}

func TestGateByID(t *testing.T) {
	g := GateByID("task-verify")
	if g == nil {
		t.Fatal("GateByID(task-verify) returned nil")
	}
	if g.Name != "测试验证" {
		t.Errorf("Name = %q, want 测试验证", g.Name)
	}

	if GateByID("nonexistent") != nil {
		t.Error("GateByID(nonexistent) should return nil")
	}
}

func TestTaskStateNextGate(t *testing.T) {
	state := &TaskState{History: nil}
	if got := state.NextGate(); got != "task-understand" {
		t.Errorf("NextGate() = %q, want task-understand", got)
	}

	// Pass first gate
	state.RecordGateResult("task-understand", true)
	if got := state.NextGate(); got != "task-design" {
		t.Errorf("NextGate after understand = %q, want task-design", got)
	}

	// Pass all gates
	state.RecordGateResult("task-design", true)
	state.RecordGateResult("task-implement", true)
	state.RecordGateResult("task-verify", true)
	state.RecordGateResult("task-complete", true)
	if got := state.NextGate(); got != "" {
		t.Errorf("NextGate after all passed = %q, want empty", got)
	}
}

func TestTaskStateIsComplete(t *testing.T) {
	state := &TaskState{History: nil}
	if state.IsComplete() {
		t.Error("empty state should not be complete")
	}

	// Pass all gates
	for _, g := range DefaultGates() {
		state.RecordGateResult(g.ID, true)
	}
	if !state.IsComplete() {
		t.Error("all gates passed should be complete")
	}
}

func TestTaskStateFailedGate(t *testing.T) {
	state := &TaskState{}
	state.RecordGateResult("task-understand", true)
	state.RecordGateResult("task-design", false)

	if state.NextGate() != "task-design" {
		t.Errorf("NextGate after fail = %q, want task-design", state.NextGate())
	}
	if state.CurrentGate != "task-design" {
		t.Errorf("CurrentGate = %q, want task-design", state.CurrentGate)
	}
}

func TestSaveAndLoadTaskState(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, ".forge", "tasks"), 0755)

	ctx := &taskcontext.Context{
		Source:     "branch",
		TaskRef:    "PROJ-123",
		Branch:     "fix/PROJ-123-bug",
		Summary:    "bug",
		DetectedAt: time.Now(),
	}
	state := NewTaskState(ctx)
	state.RecordGateResult("task-understand", true)

	if err := SaveTaskState(dir, state); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	loaded, err := LoadTaskState(dir, "PROJ-123")
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	if loaded.TaskRef != "PROJ-123" {
		t.Errorf("TaskRef = %q, want PROJ-123", loaded.TaskRef)
	}
	if loaded.Branch != "fix/PROJ-123-bug" {
		t.Errorf("Branch = %q, want fix/PROJ-123-bug", loaded.Branch)
	}
	if len(loaded.History) != 1 {
		t.Fatalf("History len = %d, want 1", len(loaded.History))
	}
	if loaded.History[0].Gate != "task-understand" {
		t.Errorf("History[0].Gate = %q, want task-understand", loaded.History[0].Gate)
	}
}

func TestLoadMissingTask(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, ".forge", "tasks"), 0755)

	_, err := LoadTaskState(dir, "MISSING-999")
	if err == nil {
		t.Fatal("expected error for missing task")
	}
}

func TestNewTaskState(t *testing.T) {
	ctx := &taskcontext.Context{
		Source:     "explicit",
		TaskRef:    "my-task",
		Branch:     "feature/my-task",
		Summary:    "my-task",
		DetectedAt: time.Now(),
	}
	state := NewTaskState(ctx)

	if state.TaskRef != "my-task" {
		t.Errorf("TaskRef = %q, want my-task", state.TaskRef)
	}
	if state.CurrentGate != "task-understand" {
		t.Errorf("CurrentGate = %q, want task-understand", state.CurrentGate)
	}
	if state.Source != "explicit" {
		t.Errorf("Source = %q, want explicit", state.Source)
	}
}

func TestCompletedGates(t *testing.T) {
	state := &TaskState{}
	state.RecordGateResult("task-understand", true)
	state.RecordGateResult("task-design", true)

	completed := state.CompletedGates()
	if len(completed) != 2 {
		t.Fatalf("CompletedGates count = %d, want 2", len(completed))
	}
	if completed[0] != "task-understand" || completed[1] != "task-design" {
		t.Errorf("CompletedGates = %v, want [task-understand, task-design]", completed)
	}
}

func TestListTaskStates(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, ".forge", "tasks"), 0755)

	// Create two tasks
	ctx1 := &taskcontext.Context{Source: "explicit", TaskRef: "TASK-1", DetectedAt: time.Now()}
	ctx2 := &taskcontext.Context{Source: "branch", TaskRef: "TASK-2", Branch: "fix/TASK-2", DetectedAt: time.Now()}

	SaveTaskState(dir, NewTaskState(ctx1))
	SaveTaskState(dir, NewTaskState(ctx2))

	states, err := ListTaskStates(dir)
	if err != nil {
		t.Fatalf("ListTaskStates failed: %v", err)
	}
	if len(states) != 2 {
		t.Errorf("ListTaskStates count = %d, want 2", len(states))
	}
}

func TestMarkComplete(t *testing.T) {
	state := &TaskState{}
	state.RecordGateResult("task-understand", true)
	state.RecordGateResult("task-design", true)
	state.RecordGateResult("task-implement", true)
	state.RecordGateResult("task-verify", true)
	state.RecordGateResult("task-complete", true)

	if !state.IsComplete() {
		t.Error("should be complete")
	}

	state.MarkComplete()
	if state.CompletedAt == nil {
		t.Error("CompletedAt should be set")
	}
	if state.CurrentGate != "" {
		t.Errorf("CurrentGate = %q, want empty after complete", state.CurrentGate)
	}
}

// runGit is a test helper that runs a git command in dir.
func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s failed: %v\n%s", args[0], err, string(out))
	}
}

func TestHasCodeChanges_NonGitRepo(t *testing.T) {
	dir := t.TempDir()
	// Non-git repo should gracefully degrade
	if !hasCodeChanges(dir, nil) {
		t.Error("expected hasCodeChanges to return true in non-git directory")
	}
}

func TestHasCodeChanges_NoChanges(t *testing.T) {
	dir := t.TempDir()
	runGit(t, dir, "init")
	runGit(t, dir, "config", "user.email", "test@test.com")
	runGit(t, dir, "config", "user.name", "Test")

	os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\n"), 0644)
	runGit(t, dir, "add", ".")
	runGit(t, dir, "commit", "-m", "initial")

	state := &TaskState{Branch: "main"}
	if hasCodeChanges(dir, state) {
		t.Error("expected hasCodeChanges to return false with no changes")
	}
}

func TestHasCodeChanges_WithUncommittedChanges(t *testing.T) {
	dir := t.TempDir()
	runGit(t, dir, "init")
	runGit(t, dir, "config", "user.email", "test@test.com")
	runGit(t, dir, "config", "user.name", "Test")

	os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\n"), 0644)
	runGit(t, dir, "add", ".")
	runGit(t, dir, "commit", "-m", "initial")

	// Make uncommitted changes
	os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\nfunc main() {}\n"), 0644)

	state := &TaskState{Branch: "main"}
	if !hasCodeChanges(dir, state) {
		t.Error("expected hasCodeChanges to return true with uncommitted changes")
	}
}

func TestHasCodeChanges_FeatureBranchWithCommits(t *testing.T) {
	dir := t.TempDir()
	runGit(t, dir, "init")
	runGit(t, dir, "config", "user.email", "test@test.com")
	runGit(t, dir, "config", "user.name", "Test")

	os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\n"), 0644)
	runGit(t, dir, "add", ".")
	runGit(t, dir, "commit", "-m", "initial")

	// Create a feature branch with a new commit
	runGit(t, dir, "checkout", "-b", "feature/test")
	os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\nfunc main() { println(\"hi\") }\n"), 0644)
	runGit(t, dir, "add", ".")
	runGit(t, dir, "commit", "-m", "add feature")

	state := &TaskState{Branch: "feature/test"}
	if !hasCodeChanges(dir, state) {
		t.Error("expected hasCodeChanges to return true on feature branch with new commits")
	}
}

func TestSanitizeRefInStatePath(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, ".forge", "tasks"), 0755)

	ctx := &taskcontext.Context{
		Source:     "branch",
		TaskRef:    "feature/login",
		Branch:     "feature/login",
		DetectedAt: time.Now(),
	}
	state := NewTaskState(ctx)

	if err := SaveTaskState(dir, state); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	// File should be feature-login.json (slash replaced)
	expectedPath := filepath.Join(dir, ".forge", "tasks", "feature-login.json")
	if _, err := os.Stat(expectedPath); os.IsNotExist(err) {
		t.Errorf("expected file %s not found", expectedPath)
	}

	// Load with original ref
	loaded, err := LoadTaskState(dir, "feature/login")
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if loaded.TaskRef != "feature/login" {
		t.Errorf("TaskRef = %q, want feature/login", loaded.TaskRef)
	}
}
