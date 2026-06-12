package taskpipeline

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
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
	state.RecordGateResult("task-understand", true, "")
	if got := state.NextGate(); got != "task-design" {
		t.Errorf("NextGate after understand = %q, want task-design", got)
	}

	// Pass all gates
	state.RecordGateResult("task-design", true, "")
	state.RecordGateResult("task-implement", true, "")
	state.RecordGateResult("task-verify", true, "")
	state.RecordGateResult("task-complete", true, "")
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
		state.RecordGateResult(g.ID, true, "")
	}
	if !state.IsComplete() {
		t.Error("all gates passed should be complete")
	}
}

func TestTaskStateFailedGate(t *testing.T) {
	state := &TaskState{}
	state.RecordGateResult("task-understand", true, "")
	state.RecordGateResult("task-design", false, "")

	if state.NextGate() != "task-design" {
		t.Errorf("NextGate after fail = %q, want task-design", state.NextGate())
	}
	if state.CurrentGate != "task-design" {
		t.Errorf("CurrentGate = %q, want task-design", state.CurrentGate)
	}
}

func TestRecordGateResultDedup(t *testing.T) {
	state := &TaskState{}

	// Pass gate once
	state.RecordGateResult("task-understand", true, "")
	if len(state.History) != 1 {
		t.Fatalf("History len after 1 pass = %d, want 1", len(state.History))
	}

	// Pass same gate again — should be deduplicated (no-op)
	state.RecordGateResult("task-understand", true, "")
	if len(state.History) != 1 {
		t.Errorf("History len after duplicate pass = %d, want 1 (should dedup)", len(state.History))
	}

	// Fail a passed gate — should record (not dedup for failures)
	state.RecordGateResult("task-understand", false, "")
	if len(state.History) != 2 {
		t.Errorf("History len after fail of passed gate = %d, want 2", len(state.History))
	}

	// Re-pass after failure — dedup still applies (gate was passed in entry 1)
	state.RecordGateResult("task-understand", true, "")
	if len(state.History) != 2 {
		t.Errorf("History len after re-pass = %d, want 2 (dedup: gate was already passed)", len(state.History))
	}
}

func TestRecordGateResultDedupPrevents25x(t *testing.T) {
	// Simulate the exact DevWorkbench scenario: stop hook re-runs task-verify 25 times
	state := &TaskState{}
	state.RecordGateResult("task-understand", true, "")
	state.RecordGateResult("task-design", true, "")

	// Pass task-verify once (legitimate)
	state.RecordGateResult("task-verify", true, "")
	verifyCount := 0
	for _, r := range state.History {
		if r.Gate == "task-verify" && r.Passed {
			verifyCount++
		}
	}
	if verifyCount != 1 {
		t.Fatalf("task-verify count after 1 pass = %d, want 1", verifyCount)
	}

	// Stop hook re-runs task-verify 24 more times — should all be no-ops
	for i := 0; i < 24; i++ {
		state.RecordGateResult("task-verify", true, "")
	}

	verifyCount = 0
	for _, r := range state.History {
		if r.Gate == "task-verify" && r.Passed {
			verifyCount++
		}
	}
	if verifyCount != 1 {
		t.Errorf("task-verify count after 25 passes = %d, want 1 (dedup should prevent duplicates)", verifyCount)
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
	state.RecordGateResult("task-understand", true, "")

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
	state.RecordGateResult("task-understand", true, "")
	state.RecordGateResult("task-design", true, "")

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
	state.RecordGateResult("task-understand", true, "")
	state.RecordGateResult("task-design", true, "")
	state.RecordGateResult("task-implement", true, "")
	state.RecordGateResult("task-verify", true, "")
	state.RecordGateResult("task-complete", true, "")

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

func TestGateMinInterval(t *testing.T) {
	// Default should be 60s
	os.Unsetenv("FORGE_GATE_MIN_INTERVAL")
	if d := getGateMinInterval(); d != 60*time.Second {
		t.Errorf("default interval = %v, want 60s", d)
	}

	// Custom via env
	os.Setenv("FORGE_GATE_MIN_INTERVAL", "30s")
	defer os.Unsetenv("FORGE_GATE_MIN_INTERVAL")
	if d := getGateMinInterval(); d != 30*time.Second {
		t.Errorf("custom interval = %v, want 30s", d)
	}

	// Invalid env falls back to default
	os.Setenv("FORGE_GATE_MIN_INTERVAL", "not-a-duration")
	if d := getGateMinInterval(); d != 60*time.Second {
		t.Errorf("invalid env interval = %v, want 60s", d)
	}
}

func TestGateTimingRejectsRapidFire(t *testing.T) {
	dir := t.TempDir()
	runGit(t, dir, "init")
	runGit(t, dir, "config", "user.email", "test@test.com")
	runGit(t, dir, "config", "user.name", "Test")
	os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\n"), 0644)
	runGit(t, dir, "add", ".")
	runGit(t, dir, "commit", "-m", "initial")

	// Set very short interval for testing
	os.Setenv("FORGE_GATE_MIN_INTERVAL", "5s")
	defer os.Unsetenv("FORGE_GATE_MIN_INTERVAL")

	state := &TaskState{
		TaskRef: "test-rapid",
		Branch:  "feat/test",
	}

	// Pass first gate (understand) — no previous gate, so no timing check
	result, err := ExecuteTaskGate(dir, "task-understand", state)
	if err != nil {
		t.Fatalf("first gate should pass: %v", err)
	}
	state.RecordGateResult(result.GateID, result.Passed, "")

	// Try passing design immediately — should be rejected
	_, err = ExecuteTaskGate(dir, "task-design", state)
	if err == nil {
		t.Fatal("rapid-fire gate should be rejected")
	}
	if !strings.Contains(err.Error(), "too quickly") {
		t.Errorf("error message should mention timing: %v", err)
	}
}

func TestGateTimingAllowsAfterInterval(t *testing.T) {
	dir := t.TempDir()
	runGit(t, dir, "init")
	runGit(t, dir, "config", "user.email", "test@test.com")
	runGit(t, dir, "config", "user.name", "Test")
	os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\n"), 0644)
	runGit(t, dir, "add", ".")
	runGit(t, dir, "commit", "-m", "initial")

	// Set very short interval
	os.Setenv("FORGE_GATE_MIN_INTERVAL", "0s")
	defer os.Unsetenv("FORGE_GATE_MIN_INTERVAL")

	state := &TaskState{
		TaskRef: "test-delayed",
		Branch:  "feat/test",
	}

	// Pass first gate
	result, err := ExecuteTaskGate(dir, "task-understand", state)
	if err != nil {
		t.Fatalf("first gate should pass: %v", err)
	}
	state.RecordGateResult(result.GateID, result.Passed, "")

	// Wait for interval to pass
	time.Sleep(1100 * time.Millisecond)

	// Second gate should now be allowed
	_, err = ExecuteTaskGate(dir, "task-design", state)
	if err != nil {
		t.Fatalf("gate after interval should pass: %v", err)
	}
}

func TestGateTimingExemptsAutoGates(t *testing.T) {
	dir := t.TempDir()
	runGit(t, dir, "init")
	runGit(t, dir, "config", "user.email", "test@test.com")
	runGit(t, dir, "config", "user.name", "Test")
	os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\n"), 0644)
	runGit(t, dir, "add", ".")
	runGit(t, dir, "commit", "-m", "initial")

	// Long interval — auto gate should be exempt
	os.Setenv("FORGE_GATE_MIN_INTERVAL", "10m")
	defer os.Unsetenv("FORGE_GATE_MIN_INTERVAL")

	state := &TaskState{
		TaskRef: "test-auto",
		Branch:  "feat/test",
	}

	// Pass first two gates (non-auto)
	state.RecordGateResult("task-understand", true, "")
	state.RecordGateResult("task-design", true, "")

	// Auto gate (task-implement) should pass immediately despite long interval
	_, err := ExecuteTaskGate(dir, "task-implement", state)
	if err != nil {
		t.Fatalf("auto gate should be exempt from timing: %v", err)
	}
}
