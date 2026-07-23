package taskpipeline

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/MjxUpUp/Forge/internal/checklog"
	"github.com/MjxUpUp/Forge/internal/docsconsistency"
	"github.com/MjxUpUp/Forge/internal/review"
	"github.com/MjxUpUp/Forge/internal/taskcontext"
	"github.com/MjxUpUp/Forge/internal/toolusage"

	"github.com/spf13/cobra"
)

func TestDefaultGates(t *testing.T) {
	gates := DefaultGates()
	if len(gates) != 3 {
		t.Fatalf("DefaultGates count = %d, want 3", len(gates))
	}

	// v0.17: reduced from 5 to 3 gates
	wantIDs := []string{"task-implement", "task-verify", "task-complete"}
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
	if got := state.NextGate(); got != "task-implement" {
		t.Errorf("NextGate() = %q, want task-implement", got)
	}

	// Pass first gate
	state.RecordGateResult("task-implement", true, "")
	if got := state.NextGate(); got != "task-verify" {
		t.Errorf("NextGate after implement = %q, want task-verify", got)
	}

	// Pass all gates
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
	state.RecordGateResult("task-implement", true, "")
	state.RecordGateResult("task-verify", false, "")

	if state.NextGate() != "task-verify" {
		t.Errorf("NextGate after fail = %q, want task-verify", state.NextGate())
	}
	if state.CurrentGate != "task-verify" {
		t.Errorf("CurrentGate = %q, want task-verify", state.CurrentGate)
	}
}

func TestRecordGateResultDedup(t *testing.T) {
	state := &TaskState{}

	// Pass gate once
	state.RecordGateResult("task-implement", true, "")
	if len(state.History) != 1 {
		t.Fatalf("History len after 1 pass = %d, want 1", len(state.History))
	}

	// Pass same gate again — should be deduplicated (no-op)
	state.RecordGateResult("task-implement", true, "")
	if len(state.History) != 1 {
		t.Errorf("History len after duplicate pass = %d, want 1 (should dedup)", len(state.History))
	}

	// Fail a passed gate — should record (not dedup for failures)
	state.RecordGateResult("task-implement", false, "")
	if len(state.History) != 2 {
		t.Errorf("History len after fail of passed gate = %d, want 2", len(state.History))
	}

	// Re-pass after failure — dedup still applies (gate was passed in entry 1)
	state.RecordGateResult("task-implement", true, "")
	if len(state.History) != 2 {
		t.Errorf("History len after re-pass = %d, want 2 (dedup: gate was already passed)", len(state.History))
	}
}

func TestRecordGateResultDedupPrevents25x(t *testing.T) {
	state := &TaskState{}
	state.RecordGateResult("task-implement", true, "")

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
	state.RecordGateResult("task-implement", true, "")

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
	if loaded.History[0].Gate != "task-implement" {
		t.Errorf("History[0].Gate = %q, want task-implement", loaded.History[0].Gate)
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
	if state.CurrentGate != "task-implement" {
		t.Errorf("CurrentGate = %q, want task-implement", state.CurrentGate)
	}
	if state.Source != "explicit" {
		t.Errorf("Source = %q, want explicit", state.Source)
	}
}

func TestCompletedGates(t *testing.T) {
	state := &TaskState{}
	state.RecordGateResult("task-implement", true, "")
	state.RecordGateResult("task-verify", true, "")

	completed := state.CompletedGates()
	if len(completed) != 2 {
		t.Fatalf("CompletedGates count = %d, want 2", len(completed))
	}
	if completed[0] != "task-implement" || completed[1] != "task-verify" {
		t.Errorf("CompletedGates = %v, want [task-implement, task-verify]", completed)
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

func TestLastGateSkipsTiming(t *testing.T) {
	dir := t.TempDir()
	runGit(t, dir, "init")
	runGit(t, dir, "config", "user.email", "test@test.com")
	runGit(t, dir, "config", "user.name", "Test")
	os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\n"), 0644)
	runGit(t, dir, "add", ".")
	runGit(t, dir, "commit", "-m", "initial")

	// Set long interval — last gate should skip it entirely
	os.Setenv("FORGE_WORK_ACTIVITY", "disable")
	defer os.Unsetenv("FORGE_WORK_ACTIVITY")

	state := &TaskState{
		TaskRef: "test-last-gate",
		Branch:  "feat/test",
	}

	// Pass all gates up to task-verify
	state.RecordGateResult("task-implement", true, "")
	state.RecordGateResult("task-verify", true, "")
	state.MarkReviewPassed("", "") // 满足 review 硬前置以隔离 timing 逻辑

	// task-complete (last gate) should pass immediately despite 10m interval
	_, err := ExecuteTaskGate(dir, "task-complete", state)
	if err != nil {
		t.Fatalf("last gate should skip timing check: %v", err)
	}
}

// TestTaskCompleteRequiresReview 守卫 task-complete 的 review 硬前置——code-review-gate
// 未通过（ReviewPassed=false）时 task-complete 必须被拒。这是"提交前必审"task 路径的
// 强制点：防 agent 跳过子 agent 审查直接 complete。
func TestTaskCompleteRequiresReview(t *testing.T) {
	dir := t.TempDir()
	state := &TaskState{TaskRef: "review-gate", Branch: "feat/r"}
	state.RecordGateResult("task-implement", true, "")
	state.RecordGateResult("task-verify", true, "")

	// ReviewPassed 仍 false → task-complete 必须拒绝
	if _, err := ExecuteTaskGate(dir, "task-complete", state); err == nil {
		t.Fatal("task-complete 应因 ReviewPassed=false 被拒——硬前置失效（agent 可跳过审查直接 complete）")
	}

	// 标记通过后应放行
	state.MarkReviewPassed("", "")
	if _, err := ExecuteTaskGate(dir, "task-complete", state); err != nil {
		t.Fatalf("ReviewPassed=true 后 task-complete 应通过: %v", err)
	}
}

func TestIsLastGate(t *testing.T) {
	if !isLastGate("task-complete") {
		t.Error("task-complete should be the last gate")
	}
	if isLastGate("task-verify") {
		t.Error("task-verify should NOT be the last gate")
	}
	if isLastGate("task-implement") {
		t.Error("task-implement should NOT be the last gate")
	}
}

func TestIsPreviousGateAuto(t *testing.T) {
	state := &TaskState{}
	// task-implement is auto
	if isPreviousGateAuto(state) {
		t.Error("No gates passed — returns false (no previous gate to check)")
	}

	state.RecordGateResult("task-implement", true, "")
	if !isPreviousGateAuto(state) {
		t.Error("task-implement IS auto — should return true")
	}

	state.RecordGateResult("task-verify", true, "")
	if isPreviousGateAuto(state) {
		t.Error("task-verify is not auto")
	}

	state.RecordGateResult("task-complete", true, "")
	if isPreviousGateAuto(state) {
		t.Error("task-complete is not auto")
	}
}

func TestAutoGateSkipsTimingForNextGate(t *testing.T) {
	dir := t.TempDir()
	runGit(t, dir, "init")
	runGit(t, dir, "config", "user.email", "test@test.com")
	runGit(t, dir, "config", "user.name", "Test")
	os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\n"), 0644)
	runGit(t, dir, "add", ".")
	runGit(t, dir, "commit", "-m", "initial")

	// Set long interval — next gate after auto should skip it
	os.Setenv("FORGE_WORK_ACTIVITY", "disable")
	defer os.Unsetenv("FORGE_WORK_ACTIVITY")

	state := &TaskState{
		TaskRef: "test-auto-next",
		Branch:  "feat/test",
	}

	// Pass task-implement (auto)
	state.RecordGateResult("task-implement", true, "")

	// task-verify should pass immediately despite long interval
	// because previous gate (task-implement) is auto
	_, err := ExecuteTaskGate(dir, "task-verify", state)
	if err != nil {
		t.Fatalf("task-verify after auto task-implement should skip timing: %v", err)
	}
}

func TestActiveTaskState_BranchDetection(t *testing.T) {
	dir := t.TempDir()
	runGit(t, dir, "init")
	runGit(t, dir, "config", "user.email", "test@test.com")
	runGit(t, dir, "config", "user.name", "Test")
	os.MkdirAll(filepath.Join(dir, ".forge", "tasks"), 0755)

	// Create task with branch ref matching the branch name
	ctx := &taskcontext.Context{
		Source:     "explicit",
		TaskRef:    "feat/test-branch",
		Branch:     "feat/test-branch",
		Summary:    "test-branch",
		DetectedAt: time.Now(),
	}
	state := NewTaskState(ctx)
	SaveTaskState(dir, state)

	// Checkout matching branch
	runGit(t, dir, "checkout", "-b", "feat/test-branch")

	active, err := ActiveTaskState(dir, "")
	if err != nil {
		t.Fatalf("ActiveTaskState failed: %v", err)
	}
	if active == nil {
		t.Fatal("ActiveTaskState should detect task on matching feature branch")
	}
	if active.TaskRef != "feat/test-branch" {
		t.Errorf("TaskRef = %q, want feat/test-branch", active.TaskRef)
	}
}

func TestActiveTaskState_FallbackSingleIncompleteOnMaster(t *testing.T) {
	dir := t.TempDir()
	runGit(t, dir, "init")
	runGit(t, dir, "config", "user.email", "test@test.com")
	runGit(t, dir, "config", "user.name", "Test")
	os.MkdirAll(filepath.Join(dir, ".forge", "tasks"), 0755)

	// Stay on master (default branch)
	// Create a single incomplete task
	ctx := &taskcontext.Context{
		Source:     "explicit",
		TaskRef:    "fix/skill-sync",
		Branch:     "master",
		Summary:    "sync skills",
		DetectedAt: time.Now(),
	}
	state := NewTaskState(ctx)
	SaveTaskState(dir, state)

	// On master, branch detection returns empty — fallback should find the task
	active, err := ActiveTaskState(dir, "")
	if err != nil {
		t.Fatalf("ActiveTaskState failed: %v", err)
	}
	if active == nil {
		t.Fatal("ActiveTaskState fallback should find single incomplete task on master")
	}
	if active.TaskRef != "fix/skill-sync" {
		t.Errorf("TaskRef = %q, want fix/skill-sync", active.TaskRef)
	}
}

func TestActiveTaskState_FallbackAmbiguousMultipleIncomplete(t *testing.T) {
	dir := t.TempDir()
	runGit(t, dir, "init")
	runGit(t, dir, "config", "user.email", "test@test.com")
	runGit(t, dir, "config", "user.name", "Test")
	os.MkdirAll(filepath.Join(dir, ".forge", "tasks"), 0755)

	// Create two incomplete tasks
	ctx1 := &taskcontext.Context{
		Source: "explicit", TaskRef: "fix/one",
		Branch: "master", DetectedAt: time.Now(),
	}
	ctx2 := &taskcontext.Context{
		Source: "explicit", TaskRef: "fix/two",
		Branch: "master", DetectedAt: time.Now(),
	}
	SaveTaskState(dir, NewTaskState(ctx1))
	SaveTaskState(dir, NewTaskState(ctx2))

	// Ambiguous — should return nil
	active, err := ActiveTaskState(dir, "")
	if err != nil {
		t.Fatalf("ActiveTaskState failed: %v", err)
	}
	if active != nil {
		t.Error("ActiveTaskState should return nil with multiple incomplete tasks (ambiguous)")
	}
}

func TestActiveTaskState_FallbackIgnoresCompleted(t *testing.T) {
	dir := t.TempDir()
	runGit(t, dir, "init")
	runGit(t, dir, "config", "user.email", "test@test.com")
	runGit(t, dir, "config", "user.name", "Test")
	os.MkdirAll(filepath.Join(dir, ".forge", "tasks"), 0755)

	// Create one completed task
	ctx1 := &taskcontext.Context{
		Source: "explicit", TaskRef: "fix/done",
		Branch: "master", DetectedAt: time.Now(),
	}
	completed := NewTaskState(ctx1)
	for _, g := range DefaultGates() {
		completed.RecordGateResult(g.ID, true, "")
	}
	completed.MarkComplete()
	SaveTaskState(dir, completed)

	// Create one incomplete task
	ctx2 := &taskcontext.Context{
		Source: "explicit", TaskRef: "fix/active",
		Branch: "master", DetectedAt: time.Now(),
	}
	SaveTaskState(dir, NewTaskState(ctx2))

	// Should find the single incomplete task (ignoring completed ones)
	active, err := ActiveTaskState(dir, "")
	if err != nil {
		t.Fatalf("ActiveTaskState failed: %v", err)
	}
	if active == nil {
		t.Fatal("ActiveTaskState should find the single incomplete task (ignoring completed)")
	}
	if active.TaskRef != "fix/active" {
		t.Errorf("TaskRef = %q, want fix/active", active.TaskRef)
	}
}

func TestActiveTaskState_NoTasks(t *testing.T) {
	dir := t.TempDir()
	runGit(t, dir, "init")
	runGit(t, dir, "config", "user.email", "test@test.com")
	runGit(t, dir, "config", "user.name", "Test")
	os.MkdirAll(filepath.Join(dir, ".forge", "tasks"), 0755)

	// No tasks at all — should return nil
	active, err := ActiveTaskState(dir, "")
	if err != nil {
		t.Fatalf("ActiveTaskState failed: %v", err)
	}
	if active != nil {
		t.Error("ActiveTaskState should return nil with no tasks")
	}
}

func TestActiveTaskState_ExplicitRefFilePriority(t *testing.T) {
	dir := t.TempDir()
	runGit(t, dir, "init")
	runGit(t, dir, "config", "user.email", "test@test.com")
	runGit(t, dir, "config", "user.name", "Test")
	os.MkdirAll(filepath.Join(dir, ".forge", "tasks"), 0755)

	// Create multiple incomplete tasks (ambiguous for fallback)
	task1 := &TaskState{TaskRef: "feat/first", Branch: "main", StartedAt: time.Now()}
	task2 := &TaskState{TaskRef: "fix/second", Branch: "main", StartedAt: time.Now()}
	SaveTaskState(dir, task1)
	SaveTaskState(dir, task2)

	// Without explicit ref — fallback returns nil (ambiguous)
	active, _ := ActiveTaskState(dir, "")
	if active != nil {
		t.Fatal("expected nil with multiple incomplete tasks")
	}

	// Set explicit active ref — should find it despite ambiguity
	SetActiveTaskRef(dir, "", "fix/second")
	active, _ = ActiveTaskState(dir, "")
	if active == nil {
		t.Fatal("expected to find task via explicit ref file")
	}
	if active.TaskRef != "fix/second" {
		t.Errorf("TaskRef = %q, want %q", active.TaskRef, "fix/second")
	}

	// Stale ref (completed task) — falls through to branch/fallback
	ClearActiveTaskRef(dir, "")
}

func TestActiveTaskState_StaleRefFileFallsThrough(t *testing.T) {
	dir := t.TempDir()
	runGit(t, dir, "init")
	runGit(t, dir, "config", "user.email", "test@test.com")
	runGit(t, dir, "config", "user.name", "Test")
	os.MkdirAll(filepath.Join(dir, ".forge", "tasks"), 0755)

	// Create a completed task
	completed := &TaskState{TaskRef: "feat/done", Branch: "main", StartedAt: time.Now()}
	now := time.Now()
	completed.CompletedAt = &now
	SaveTaskState(dir, completed)

	// Point active-task-ref to the completed task
	SetActiveTaskRef(dir, "", "feat/done")

	// Should fall through (stale ref points to completed task)
	active, _ := ActiveTaskState(dir, "")
	if active != nil {
		t.Fatal("expected nil when explicit ref points to completed task")
	}
}

func TestSetActiveAndClearActiveTaskRef(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, ".forge"), 0755)

	// Set
	if err := SetActiveTaskRef(dir, "", "feat/test"); err != nil {
		t.Fatalf("SetActiveTaskRef failed: %v", err)
	}
	if got := ReadActiveTaskRef(dir, ""); got != "feat/test" {
		t.Errorf("ReadActiveTaskRef = %q, want %q", got, "feat/test")
	}

	// Clear
	if err := ClearActiveTaskRef(dir, ""); err != nil {
		t.Fatalf("ClearActiveTaskRef failed: %v", err)
	}
	if got := ReadActiveTaskRef(dir, ""); got != "" {
		t.Errorf("ReadActiveTaskRef after clear = %q, want empty", got)
	}

	// Clear non-existent — no error
	if err := ClearActiveTaskRef(dir, ""); err != nil {
		t.Fatalf("ClearActiveTaskRef on missing file should not error: %v", err)
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
	os.Setenv("FORGE_WORK_ACTIVITY", "disable")
	defer os.Unsetenv("FORGE_WORK_ACTIVITY")

	state := &TaskState{
		TaskRef: "test-auto",
		Branch:  "feat/test",
	}

	// Auto gate (task-implement) should pass immediately despite long interval
	_, err := ExecuteTaskGate(dir, "task-implement", state)
	if err != nil {
		t.Fatalf("auto gate should be exempt from timing: %v", err)
	}
}

// TestTestCoverageCheckScopedToVerifyGate guards the executor.go integration:
// the test-coverage check runs ONLY at task-verify, never at task-complete (the
// last gate). A task with an untested source change must still be able to reach
// TestTestCoverageCheckScopedToVerifyGate 钉死方案 A 兜底的 fudge factor 边界：task-complete
// 现在也跑 test-coverage 兜底（补 advisory 缺口），但单文件小改（total=1<3）即使零断言也
// advisory 放行——corrupt success 兜底只拦"大改（≥3）零断言"。bar.go 单文件无测试无断言
// → fudge factor 放行，task-complete 仍 PASS。
func TestTestCoverageCheckScopedToVerifyGate(t *testing.T) {
	dir := t.TempDir()
	runGit(t, dir, "init")
	runGit(t, dir, "config", "user.email", "t@t.com")
	runGit(t, dir, "config", "user.name", "T")
	runGit(t, dir, "commit", "--allow-empty", "-m", "master init")
	runGit(t, dir, "checkout", "-b", "feat/cov")
	// Source change with NO test — would fail task-verify.
	writeFile := func(name, body string) {
		full := filepath.Join(dir, name)
		os.MkdirAll(filepath.Dir(full), 0755)
		os.WriteFile(full, []byte(body), 0644)
	}
	writeFile("bar.go", "package main\n\nfunc Bar() int { return 7 }\n")
	runGit(t, dir, "add", "-A")
	runGit(t, dir, "commit", "-m", "add bar")

	// Seed a Read so the work-activity check does not pre-empt.
	state := &TaskState{TaskRef: "cov-scope", Branch: "feat/cov"}
	state.RecordGateResult("task-implement", true, "")
	state.RecordGateResult("task-verify", true, "")
	state.MarkReviewPassed("", "") // 满足 review 硬前置以隔离 coverage 逻辑
	base := time.Now().Add(2 * time.Second)
	rr := toolusage.ToolCall{ToolName: "Read", TaskRef: "cov-scope", Timestamp: base}
	if err := toolusage.Record(dir, &rr); err != nil {
		t.Fatalf("seed Read: %v", err)
	}

	// task-complete 兜底对单文件小改放行（fudge factor，total=1<3 不阻断）。
	// 大改零断言的阻断见 TestTaskCompleteTestCoverageHardGate_BlockedOnBigChangeNoAssertion。
	if _, err := ExecuteTaskGate(dir, "task-complete", state); err != nil {
		t.Fatalf("task-complete 对单文件小改应 advisory 放行（fudge factor），got: %v", err)
	}
}

// TestTaskCompleteTestCoverageHardGate_BlockedOnBigChangeNoAssertion 钉死方案 A 核心：
// task-complete 兜底对"大改零断言"硬阻断——改了 ≥3 个源文件既无配对测试也无任何断言
// = corrupt success 铁证（eval 证据：feat/eval-core 0/19、feat/m2 0/25 照过 complete 的漏洞）。
func TestTaskCompleteTestCoverageHardGate_BlockedOnBigChangeNoAssertion(t *testing.T) {
	dir := t.TempDir()
	initRepoWithMaster(t, dir)
	writeCommitSource(t, dir, map[string]string{
		"a.go": "package main\n\nfunc A() int { return 1 }\n",
		"b.go": "package main\n\nfunc B() int { return 2 }\n",
		"c.go": "package main\n\nfunc C() int { return 3 }\n",
	}, "add 3 sources no test")

	state := newVerifyState(t, dir, "bigchange-noassert")
	state.RecordGateResult("task-verify", true, "")
	state.MarkReviewPassed("", "") // 满足 review 硬前置以隔离 coverage 逻辑

	_, err := ExecuteTaskGate(dir, "task-complete", state)
	if err == nil {
		t.Fatal("task-complete 应因大改零断言被 BLOCKED——corrupt success 兜底失效（agent 可改 3+ 源文件不写测试不写断言照过 complete）")
	}
	if !IsGateBlocked(err) {
		t.Fatalf("应是 GateBlocked（HARD stop），got: %v", err)
	}
}

// TestTaskCompleteTestCoverageHardGate_SmallChangeAdvisoryPass 小改（≤2 文件）即使零断言
// 也 advisory 放行——fudge factor，对齐业界 Sonar <20 行豁免精神（用文件数代理行数）。
func TestTaskCompleteTestCoverageHardGate_SmallChangeAdvisoryPass(t *testing.T) {
	dir := t.TempDir()
	initRepoWithMaster(t, dir)
	writeCommitSource(t, dir, map[string]string{
		"a.go": "package main\n\nfunc A() int { return 1 }\n",
		"b.go": "package main\n\nfunc B() int { return 2 }\n",
	}, "add 2 sources no test")

	state := newVerifyState(t, dir, "smallchange-noassert")
	state.RecordGateResult("task-verify", true, "")
	state.MarkReviewPassed("", "")

	if _, err := ExecuteTaskGate(dir, "task-complete", state); err != nil {
		t.Fatalf("小改（2 文件）应 advisory 放行（fudge factor）, got: %v", err)
	}
}

// TestTaskCompleteTestCoverageHardGate_BigChangeWithAssertionsPass 大改（≥3）但有断言
// （测试在别处/重构场景）→ advisory 放行。断言信号复用 CollectAssertionDensity：一个被改动的
// 测试文件含断言即视为有验证痕迹，不硬拦。源文件分散在不同包（pkg1-3），断言测试在 pkg4——
// 避免同包 _test.go 包级 fallback 误让 missing 为空、ok=true（跳过 !ok 分支，
// 永不进入 testCoverageShouldBlock 判定——测试虽 PASS 但没走到 assertN 路径）。
func TestTaskCompleteTestCoverageHardGate_BigChangeWithAssertionsPass(t *testing.T) {
	dir := t.TempDir()
	initRepoWithMaster(t, dir)
	writeCommitSource(t, dir, map[string]string{
		"pkg1/a.go":      "package pkg1\n\nfunc A() int { return 1 }\n",
		"pkg2/b.go":      "package pkg2\n\nfunc B() int { return 2 }\n",
		"pkg3/c.go":      "package pkg3\n\nfunc C() int { return 3 }\n",
		"pkg4/x_test.go": "package pkg4\n\nimport \"testing\"\n\nfunc TestX(t *testing.T) { t.Fatal(\"x\") }\n",
	}, "add 3 sources in diff pkgs + assertion test elsewhere")

	state := newVerifyState(t, dir, "bigchange-withassert")
	state.RecordGateResult("task-verify", true, "")
	state.MarkReviewPassed("", "")

	if _, err := ExecuteTaskGate(dir, "task-complete", state); err != nil {
		t.Fatalf("大改有断言（测试在别处/重构）应 advisory 放行, got: %v", err)
	}
}

// TestTaskCompleteTestCoverageHardGate_EscapePasses escape 逃生舱 → task-complete 放行
// （CheckTestCoverage 内部返回 ok=true）。逃生降 evidence Weak 留痕，不阻断。
func TestTaskCompleteTestCoverageHardGate_EscapePasses(t *testing.T) {
	dir := t.TempDir()
	initRepoWithMaster(t, dir)
	writeCommitSource(t, dir, map[string]string{
		"a.go": "package main\n\nfunc A() int { return 1 }\n",
		"b.go": "package main\n\nfunc B() int { return 2 }\n",
		"c.go": "package main\n\nfunc C() int { return 3 }\n",
	}, "add 3 sources no test")

	state := newVerifyState(t, dir, "bigchange-escape")
	state.RecordGateResult("task-verify", true, "")
	state.MarkReviewPassed("", "")
	t.Setenv("FORGE_TEST_COVERAGE", "disable")

	if _, err := ExecuteTaskGate(dir, "task-complete", state); err != nil {
		t.Fatalf("escape（FORGE_TEST_COVERAGE=disable）应放行, got: %v", err)
	}
}

// TestExecuteTaskGate_TaskVerify_PersistsDesignPhases 钉死 BUG-1 接通点：task-verify
// gate 必须按改动文件推断 DesignPhases 并 SaveTaskState 持久化，下游
// （Conclusion/health/review 子 agent）才读得到。若有人删了接通块，下游行为测试只
// 隐式暴露、定位困难——这里直接断言"盘上 state.DesignPhases == [requirement, api]"。
func TestExecuteTaskGate_TaskVerify_PersistsDesignPhases(t *testing.T) {
	dir := t.TempDir()
	runGit(t, dir, "init")
	runGit(t, dir, "config", "user.email", "t@t.com")
	runGit(t, dir, "config", "user.name", "T")
	runGit(t, dir, "commit", "--allow-empty", "-m", "master init")
	runGit(t, dir, "checkout", "-b", "feat/phase")

	writeFile := func(name, body string) {
		full := filepath.Join(dir, name)
		os.MkdirAll(filepath.Dir(full), 0755)
		os.WriteFile(full, []byte(body), 0644)
	}
	// 触发 requirement + api 两个设计阶段。
	writeFile("docs/prd/feature.md", "# PRD\n## 验收\n- foo\n")
	writeFile("api/openapi/spec.yaml", "openapi: 3.0\n")
	// 测试自带本地 .gitignore（docs/）钉死盲区前提——不依赖开发机 ~/.gitignore_global
	// （CI/他人机器可能没配 docs/，那时 git 会正常跟踪 docs/prd，测的成 happy path
	// 而非兜底）。写本地 .gitignore 后 git add -A 跳过 docs/prd/feature.md，
	// taskChangedFiles(git diff) 漏掉它，scanDesignArtifacts 直接读文件系统兜底让
	// PhaseRequirement 仍能推出——验证 gitignore 盲区修复（不是绕过）。
	os.WriteFile(filepath.Join(dir, ".gitignore"), []byte("docs/\n"), 0644)
	runGit(t, dir, "add", "-A")
	runGit(t, dir, "commit", "-m", "add prd + openapi")
	// 前提断言：docs/prd/feature.md 确实被忽略——否则下面 DesignPhases==[requirement,api]
	// 测的是 git 看到了它（happy path）而非 scan 兜底。check-ignore -q 被忽略返 0。
	if err := exec.Command("git", "-C", dir, "check-ignore", "-q", "docs/prd/feature.md").Run(); err != nil {
		t.Fatalf("前提不成立:docs/prd/feature.md 未被忽略(%v)——.gitignore 没生效?将测 happy path", err)
	}

	state := &TaskState{TaskRef: "phase-persist", Branch: "feat/phase"}
	state.RecordGateResult("task-implement", true, "")
	state.RecordGateResult("task-verify", true, "")
	state.MarkReviewPassed("", "")
	base := time.Now().Add(2 * time.Second)
	if err := toolusage.Record(dir, &toolusage.ToolCall{ToolName: "Read", TaskRef: "phase-persist", Timestamp: base}); err != nil {
		t.Fatalf("seed Read: %v", err)
	}

	if _, err := ExecuteTaskGate(dir, "task-verify", state); err != nil {
		t.Fatalf("ExecuteTaskGate task-verify: %v", err)
	}

	// 从盘读回——验证接通块真的持久化了，不是只改内存 state。
	loaded, err := LoadTaskState(dir, "phase-persist")
	if err != nil {
		t.Fatalf("LoadTaskState: %v", err)
	}
	want := []DesignPhase{PhaseRequirement, PhaseAPI}
	if len(loaded.DesignPhases) != len(want) {
		t.Fatalf("DesignPhases=%v want %v（接通块未写盘？）", loaded.DesignPhases, want)
	}
	for i, p := range want {
		if loaded.DesignPhases[i] != p {
			t.Errorf("DesignPhases[%d]=%s want %s", i, loaded.DesignPhases[i], p)
		}
	}
}

// TestWorkActivityEscapeHatchAuditsToChecklog guards the A4 fix: the
// FORGE_WORK_ACTIVITY=disable escape hatch bypasses the read-before-edit check,
// but its use must be audited to checklog so `forge trace` can surface it. Here
// no Read is seeded — the hatch is what lets the gate pass, and it must leave a
// trail.
func TestWorkActivityEscapeHatchAuditsToChecklog(t *testing.T) {
	dir := t.TempDir()
	runGit(t, dir, "init")
	runGit(t, dir, "config", "user.email", "t@t.com")
	runGit(t, dir, "config", "user.name", "T")
	runGit(t, dir, "commit", "--allow-empty", "-m", "master init")
	runGit(t, dir, "checkout", "-b", "feat/hatch")

	t.Setenv("FORGE_WORK_ACTIVITY", "disable")

	// No seeded Read — without the hatch, the read-before-edit check would
	// refuse task-verify. With the hatch it passes AND records an audit entry.
	state := &TaskState{TaskRef: "hatch-wa", Branch: "feat/hatch"}
	state.RecordGateResult("task-implement", true, "")
	state.RecordGateResult("task-verify", true, "")

	if _, err := ExecuteTaskGate(dir, "task-verify", state); err != nil {
		t.Fatalf("task-verify should PASS with FORGE_WORK_ACTIVITY=disable: %v", err)
	}

	entries, err := checklog.LoadForTask(dir, "hatch-wa")
	if err != nil {
		t.Fatalf("LoadForTask: %v", err)
	}
	var found *checklog.Entry
	for i := range entries {
		if entries[i].Check == checklog.CheckEscapeHatch {
			found = &entries[i]
			break
		}
	}
	if found == nil {
		t.Fatal("escape-hatch checklog entry not recorded for FORGE_WORK_ACTIVITY=disable")
	}
	if !strings.Contains(found.Detail, "FORGE_WORK_ACTIVITY") {
		t.Errorf("escape-hatch detail = %q, want it to mention FORGE_WORK_ACTIVITY", found.Detail)
	}
}

// TestReadBeforeEditFailureIsBlocked guards 方案1's exit-code contract: editing
// without reading must hard-fail task-verify with the BLOCKED: prefix, not soft
// advisory prose — the BLOCKED marker makes the hard stop unambiguous. Asserts both
// IsGateBlocked and the recognizable reason phrase.
func TestReadBeforeEditFailureIsBlocked(t *testing.T) {
	dir := t.TempDir()
	runGit(t, dir, "init")
	runGit(t, dir, "config", "user.email", "t@t.com")
	runGit(t, dir, "config", "user.name", "T")
	runGit(t, dir, "commit", "--allow-empty", "-m", "master init")
	runGit(t, dir, "checkout", "-b", "feat/rbe-block")

	state := &TaskState{TaskRef: "rbe-block", Branch: "feat/rbe-block", StartedAt: time.Now()}
	state.RecordGateResult("task-implement", true, "")

	// Seed an Edit but NO Read — reads==0, edits>0 → read-before-edit hard fail.
	editTS := time.Now().Add(2 * time.Second)
	if err := toolusage.Record(dir, &toolusage.ToolCall{ToolName: "Edit", TaskRef: "rbe-block", Timestamp: editTS}); err != nil {
		t.Fatalf("seed Edit: %v", err)
	}

	_, err := ExecuteTaskGate(dir, "task-verify", state)
	if err == nil {
		t.Fatal("task-verify unexpectedly passed (want BLOCKED hard failure for edit-without-read)")
	}
	if !IsGateBlocked(err) {
		t.Errorf("read-before-edit failure = %q, want BLOCKED contract prefix", err.Error())
	}
	if !strings.Contains(err.Error(), "without reading any code") {
		t.Errorf("read-before-edit failure = %q, want the recognizable reason phrase", err.Error())
	}
}

// TestTaskComplete_DocsConsistencyAdvisory guards the task-complete docs-consistency
// advisory: README drift (反引号引用不存在的 forge 命令) must be recorded to checklog
// but must NOT block the gate (advisory, not blocking). This is the local-before-push
// counterpart to the CI guard A — drift surfaced at `forge task complete` time, not
// only after CI runs.
func TestTaskComplete_DocsConsistencyAdvisory(t *testing.T) {
	dir := t.TempDir()
	runGit(t, dir, "init")
	runGit(t, dir, "config", "user.email", "t@t.com")
	runGit(t, dir, "config", "user.name", "T")
	runGit(t, dir, "commit", "--allow-empty", "-m", "master init")
	runGit(t, dir, "checkout", "-b", "feat/docs")

	// README 引用了不存在的 forge 命令 → drift。
	if err := os.WriteFile(filepath.Join(dir, "README.md"),
		[]byte("# proj\n\n运行 `forge ghostpropose` 提案。\n"), 0644); err != nil {
		t.Fatalf("write README: %v", err)
	}
	runGit(t, dir, "add", "-A")
	runGit(t, dir, "commit", "-m", "readme drift")

	// taskpipeline 测试不能 import cli（循环），cli init 不跑、命令树回调未注册。
	// 手动注册一个 mock 命令树（forge→init；ghostpropose 不存在 → drift），测试后还原 nil
	// 避免污染同包其他测试。
	docsconsistency.RegisterCommandTree(func() *cobra.Command {
		root := &cobra.Command{Use: "forge"}
		root.AddCommand(&cobra.Command{Use: "init"})
		return root
	})
	defer docsconsistency.RegisterCommandTree(nil)

	state := &TaskState{TaskRef: "docs-drift", Branch: "feat/docs"}
	state.RecordGateResult("task-implement", true, "")
	state.RecordGateResult("task-verify", true, "")
	state.MarkReviewPassed("", "") // 满足 review 硬前置

	// docs drift 是 advisory——task-complete 必须仍 Passed（不阻塞）。
	result, err := ExecuteTaskGate(dir, "task-complete", state)
	if err != nil {
		t.Fatalf("docs drift must not block task-complete (advisory only): %v", err)
	}
	if !result.Passed {
		t.Error("task-complete should pass despite README drift (advisory, not blocking)")
	}

	// drift 信号必须记进 checklog（forge trace 可见），Passed=true 表 gate 仍通过。
	entries, err := checklog.LoadForTask(dir, "docs-drift")
	if err != nil {
		t.Fatalf("LoadForTask: %v", err)
	}
	var found *checklog.Entry
	for i := range entries {
		if entries[i].Check == CheckNameDocsConsistency {
			found = &entries[i]
			break
		}
	}
	if found == nil {
		t.Fatal("checklog 缺 docs-consistency advisory 条目（drift 信号未记录）")
	}
	if !found.Passed {
		t.Error("docs-consistency advisory 条目应 Passed=true（gate 通过，advisory 仅记录信号）")
	}
	if !strings.Contains(found.Detail, "ghostpropose") {
		t.Errorf("advisory detail 应含 drift 命令名，got %q", found.Detail)
	}
}

// TestGateAdvancementRecordsAgentClaim 守卫证据链 agent-claim 数据源：agent 推进
// task-verify / task-complete gate 时，ExecuteTaskGate 必须把该"声明"记进 checklog
// （Source=agent-claim，由 Record 的 SourceForCheck 兜底写入）。没有这两个记录点，
// EvidenceChain 的 agent-claim 桶恒为 0，"完成声明 vs deterministic 支撑"的对比失效——
// 本测试把数据源接入钉成可回归验证。
func TestGateAdvancementRecordsAgentClaim(t *testing.T) {
	setup := func(branch, taskRef string) string {
		dir := t.TempDir()
		runGit(t, dir, "init")
		runGit(t, dir, "config", "user.email", "t@t.com")
		runGit(t, dir, "config", "user.name", "T")
		runGit(t, dir, "commit", "--allow-empty", "-m", "master init")
		runGit(t, dir, "checkout", "-b", branch)
		return dir
	}
	findClaim := func(dir, taskRef string, want checklog.CheckName) *checklog.Entry {
		entries, err := checklog.LoadForTask(dir, taskRef)
		if err != nil {
			t.Fatalf("LoadForTask: %v", err)
		}
		for i := range entries {
			if entries[i].TaskRef == taskRef && entries[i].Check == want {
				return &entries[i]
			}
		}
		return nil
	}

	t.Run("task-verify records agent-claim", func(t *testing.T) {
		dir := setup("feat/claim-v", "claim-v")
		// 走真实 read-before-edit 路径（seed 一个 Read）而非 FORGE_WORK_ACTIVITY=disable
		// 逃避——确保 claim 记录点在真实 gate 流程末端被覆盖，防 future early-return 漏检。
		state := &TaskState{TaskRef: "claim-v", Branch: "feat/claim-v", StartedAt: time.Now()}
		state.RecordGateResult("task-implement", true, "")
		readTS := time.Now().Add(2 * time.Second)
		if err := toolusage.Record(dir, &toolusage.ToolCall{ToolName: "Read", TaskRef: "claim-v", Timestamp: readTS}); err != nil {
			t.Fatalf("seed Read: %v", err)
		}
		if _, err := ExecuteTaskGate(dir, "task-verify", state); err != nil {
			t.Fatalf("task-verify should pass: %v", err)
		}
		entry := findClaim(dir, "claim-v", checklog.CheckTaskVerify)
		if entry == nil {
			t.Fatal(`task-verify 未记录 CheckTaskVerify 声明（agent-claim 数据源断裂）`)
		}
		if entry.Source != checklog.EvidenceAgentClaim {
			t.Errorf(`CheckTaskVerify.Source = %s, want agent-claim`, entry.Source)
		}
	})

	t.Run("task-complete records agent-claim", func(t *testing.T) {
		dir := setup("feat/claim-c", "claim-c")
		state := &TaskState{TaskRef: "claim-c", Branch: "feat/claim-c"}
		state.RecordGateResult("task-implement", true, "")
		state.RecordGateResult("task-verify", true, "")
		state.MarkReviewPassed("", "") // 满足 review 硬前置
		if _, err := ExecuteTaskGate(dir, "task-complete", state); err != nil {
			t.Fatalf("task-complete should pass: %v", err)
		}
		entry := findClaim(dir, "claim-c", checklog.CheckTaskComplete)
		if entry == nil {
			t.Fatal(`task-complete 未记录 CheckTaskComplete 声明（agent-claim 数据源断裂）`)
		}
		if entry.Source != checklog.EvidenceAgentClaim {
			t.Errorf(`CheckTaskComplete.Source = %s, want agent-claim`, entry.Source)
		}
	})
}

// TestTaskComplete_DocsConsistencyNoDriftSilent guards the silent path: when README
// has no forge-command drift, no docs-consistency advisory entry is recorded (no
// noise). Advisory must fire ONLY on drift, not on every task-complete.
func TestTaskComplete_DocsConsistencyNoDriftSilent(t *testing.T) {
	dir := t.TempDir()
	runGit(t, dir, "init")
	runGit(t, dir, "config", "user.email", "t@t.com")
	runGit(t, dir, "config", "user.name", "T")
	runGit(t, dir, "commit", "--allow-empty", "-m", "master init")
	runGit(t, dir, "checkout", "-b", "feat/clean")
	// README 无 forge 命令引用 → 无 drift。
	if err := os.WriteFile(filepath.Join(dir, "README.md"),
		[]byte("# proj\n\nclean readme, no forge commands\n"), 0644); err != nil {
		t.Fatalf("write README: %v", err)
	}
	runGit(t, dir, "add", "-A")
	runGit(t, dir, "commit", "-m", "clean readme")

	docsconsistency.RegisterCommandTree(func() *cobra.Command {
		root := &cobra.Command{Use: "forge"}
		root.AddCommand(&cobra.Command{Use: "init"})
		return root
	})
	defer docsconsistency.RegisterCommandTree(nil)

	state := &TaskState{TaskRef: "docs-clean", Branch: "feat/clean"}
	state.RecordGateResult("task-implement", true, "")
	state.RecordGateResult("task-verify", true, "")
	state.MarkReviewPassed("", "")

	if _, err := ExecuteTaskGate(dir, "task-complete", state); err != nil {
		t.Fatalf("task-complete should pass: %v", err)
	}
	entries, _ := checklog.LoadForTask(dir, "docs-clean")
	for _, e := range entries {
		if e.Check == CheckNameDocsConsistency {
			t.Errorf("无 drift 时不应记录 docs-consistency advisory，但找到：%+v", e)
		}
	}
}

// --- review-snapshot 门禁测试（审查-修复-复审自动化）---
// review pass 绑定 (HEAD, SourceChangesSince(HEAD)) 快照；task-complete 重算比对，审查后改码 → 拒。

// initTaskGitRepo 建临时 git 仓库并首次提交（.gitkeep），返回 dir（HEAD=C0）。快照测试需真实 git
// 仓库——SourceChangesSince 走 git diff/show，mock 不了"commit 前后内容指纹一致"这类端到端断言。
func initTaskGitRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	runGit(t, dir, "init")
	runGit(t, dir, "config", "user.email", "test@test.com")
	runGit(t, dir, "config", "user.name", "Test")
	if err := os.WriteFile(filepath.Join(dir, ".gitkeep"), []byte{}, 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "add", ".")
	runGit(t, dir, "commit", "-m", "init")
	return dir
}

// commitAll 提交工作区全部变更（add -A + commit）。
func commitAll(t *testing.T, dir, msg string) {
	t.Helper()
	runGit(t, dir, "add", "-A")
	runGit(t, dir, "commit", "-m", msg)
}

// headShort 返回 HEAD 短 hash（作 review 快照基线）。
func headShort(t *testing.T, dir string) string {
	t.Helper()
	out, err := exec.Command("git", "-C", dir, "rev-parse", "--short", "HEAD").Output()
	if err != nil {
		t.Fatalf(`git rev-parse HEAD: %v`, err)
	}
	return strings.TrimSpace(string(out))
}

// writeSrc 写源码文件（含父目录创建）。
func writeSrc(t *testing.T, dir, rel, content string) {
	t.Helper()
	p := filepath.Join(dir, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// fullyGatedState 构造已过 implement+verify 的 state（只剩 task-complete）。
func fullyGatedState(ref string) *TaskState {
	s := &TaskState{TaskRef: ref, Branch: "feat/" + ref}
	s.RecordGateResult("task-implement", true, "")
	s.RecordGateResult("task-verify", true, "")
	return s
}

// TestTaskComplete_ReviewSnapshotRejectsPostReviewChange 审查快照闭环核心：review pass 绑定快照后，
// 改源码（未 commit）→ task-complete 必须拒、强制复审。这是"审查-修复-复审自动化"的强制点——
// 不再靠 agent 自律重审（feat/dashboard-global 事故：修完审查发现没复审就推进 complete，门禁没拦住）。
func TestTaskComplete_ReviewSnapshotRejectsPostReviewChange(t *testing.T) {
	dir := initTaskGitRepo(t)
	head := headShort(t, dir) // C0
	writeSrc(t, dir, `svc.go`, `package svc`)
	hash, _, err := review.SourceChangesSince(dir, head)
	if err != nil {
		t.Fatalf(`SourceChangesSince: %v`, err)
	}
	state := fullyGatedState(`snap-reject`)
	state.MarkReviewPassed(head, hash)

	// 审查后改码（工作区未 commit）
	writeSrc(t, dir, `svc.go`, "package svc\nfunc F() {}")

	_, err = ExecuteTaskGate(dir, "task-complete", state)
	if err == nil {
		t.Fatal(`审查后改了源码，task-complete 应拒绝强制复审，实际放行——快照闭环失效`)
	}
	if !strings.Contains(err.Error(), `审查通过后检测到源码变更`) {
		t.Fatalf(`拒绝原因应含"审查通过后检测到源码变更"，got: %v`, err)
	}
}

// TestTaskComplete_ReviewSnapshotPassWhenUnchanged 审查后不改码 → task-complete 过（快照一致）。
func TestTaskComplete_ReviewSnapshotPassWhenUnchanged(t *testing.T) {
	dir := initTaskGitRepo(t)
	head := headShort(t, dir)
	writeSrc(t, dir, `svc.go`, `package svc`)
	hash, _, _ := review.SourceChangesSince(dir, head)
	state := fullyGatedState(`snap-pass`)
	state.MarkReviewPassed(head, hash)

	if _, err := ExecuteTaskGate(dir, "task-complete", state); err != nil {
		t.Fatalf(`审查后未改码应过，got: %v`, err)
	}
}

// TestTaskComplete_ReviewSnapshotEmptyBaselineSkips 空基线（MarkReviewPassed("","")）→ 跳过快照检查，
// 仅留 ReviewPassed 硬前置语义（老 state 兼容 / commit-then-review 流审查时工作区干净 hash 空）。
func TestTaskComplete_ReviewSnapshotEmptyBaselineSkips(t *testing.T) {
	dir := initTaskGitRepo(t)
	writeSrc(t, dir, `svc.go`, `package svc`)
	state := fullyGatedState(`snap-empty`)
	state.MarkReviewPassed("", "")

	if _, err := ExecuteTaskGate(dir, "task-complete", state); err != nil {
		t.Fatalf(`空基线应跳过快照检查（保 ReviewPassed 硬前置语义），got: %v`, err)
	}
}

// TestTaskComplete_ReviewSnapshotUnreachableFailOpen 基线不可达（amend/rebase 改写历史致 git 对象消失）
// → fail-open 放行。amend 是正常工作流，强复审会死循环；对齐 review/stamp.go 的 fail-open 哲学
// （可达则严、不可达则松的非对称是设计本意）。且必须落 checklog 留痕——让 score/dashboard 照出
// "靠 fail-open 而非真复审通过"，不能只 stderr 一闪而过（审查反馈的可观测性兜底）。
func TestTaskComplete_ReviewSnapshotUnreachableFailOpen(t *testing.T) {
	dir := initTaskGitRepo(t)
	state := fullyGatedState(`snap-unreachable`)
	state.MarkReviewPassed("deadbeefnotacommit", `anyc0ntent`)

	if _, err := ExecuteTaskGate(dir, "task-complete", state); err != nil {
		t.Fatalf(`基线不可达应 fail-open 放行（amend 正常流），got: %v`, err)
	}
	// fail-open 必须落盘——断言 checklog 有 CheckNameReviewSnapshot 条目（防回归成"只 stderr 无痕迹"）。
	entries, _ := checklog.LoadForTask(dir, `snap-unreachable`)
	var found bool
	for _, e := range entries {
		if e.Check == CheckNameReviewSnapshot {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf(`fail-open 应落 checklog:%s 留痕，实际无——score/dashboard 照不出"靠 fail-open 而非真复审通过"`, CheckNameReviewSnapshot)
	}
}

// TestTaskComplete_ReviewSnapshotCommitReviewedContentPasses commit 审查的工作区内容后 → 过。
// 镜像 commit-then-review E2E 真实流（cli_test.go）：review 时工作区有 svc.go（untracked），
// agent commit 它（不改内容），SourceChangesSince(基线) 用【内容指纹】仍 == 记录 hash → 放行。
// 用 git diff 输出做指纹会在 untracked→tracked 切换时假阳性（review 包单测已证），这里在门禁层再钉一次。
func TestTaskComplete_ReviewSnapshotCommitReviewedContentPasses(t *testing.T) {
	dir := initTaskGitRepo(t)
	head := headShort(t, dir)                 // C0
	writeSrc(t, dir, `svc.go`, `package svc`) // untracked
	hash, _, _ := review.SourceChangesSince(dir, head)
	state := fullyGatedState(`snap-commit`)
	state.MarkReviewPassed(head, hash)

	commitAll(t, dir, "reviewed") // C1：commit 审查内容，工作区干净

	if _, err := ExecuteTaskGate(dir, "task-complete", state); err != nil {
		t.Fatalf(`commit 审查的工作区内容后应过（内容指纹一致），got: %v`, err)
	}
}
