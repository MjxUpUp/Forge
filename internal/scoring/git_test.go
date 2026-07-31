package scoring

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestCollectGitData_InGitRepo(t *testing.T) {
	// Create a temp git repo with a commit
	dir := t.TempDir()
	runGit(t, dir, "init")
	runGit(t, dir, "config", "user.email", "test@test.com")
	runGit(t, dir, "config", "user.name", "Test")

	// Write a test file and commit
	testFile := filepath.Join(dir, "foo_test.go")
	if err := os.WriteFile(testFile, []byte("package foo\nfunc TestFoo() {}"), 0644); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "add", ".")
	runGit(t, dir, "commit", "-m", "initial")

	// Make a change
	if err := os.WriteFile(testFile, []byte("package foo\nfunc TestBar() {}"), 0644); err != nil {
		t.Fatal(err)
	}

	diffStat, err := CollectGitData(dir, "master", "")
	if err != nil {
		t.Fatalf("CollectGitData returned error: %v", err)
	}

	// In a clean repo with no diff, this may be empty — that's fine.
	// The key is it doesn't crash.
	t.Logf("diffStat: %q", diffStat)
}

func TestCollectGitData_NotGitRepo(t *testing.T) {
	dir := t.TempDir()
	// Not a git repo — both git diff commands fail, so a real error must surface
	// (the failure is observable; the caller degrades to empty diffStat / neutral scope).
	//
	// 非 git repo——两个 git diff 命令都失败，必须返回真实 error
	// （失败可观测；调用方降级为空 diffStat / scope 中性分）。
	diffStat, err := CollectGitData(dir, "main", "")
	if err == nil {
		t.Fatal("expected error for non-git dir (both git diff commands fail), got nil")
	}
	if diffStat != "" {
		t.Fatalf("expected empty result for non-git dir, got stat=%q", diffStat)
	}
}

// TestCollectGitData_UnreachableBaseCommit pins the both-fail error path with a git repo whose
// base commit does not exist: base..HEAD fails and `git diff HEAD`... actually succeeds in a
// valid repo. To force both commands to fail we use an unreachable base AND a bare-invalid root
// is not possible here — instead assert the partial-failure contract: unreachable base still
// returns working-tree data without error.
//
// TestCollectGitData_UnreachableBaseCommit 钉住部分失败契约：base commit 不可达时
// base..HEAD 失败但 `git diff HEAD` 仍可用——返回工作区数据且无 error（单失败可容忍）。
func TestCollectGitData_UnreachableBaseCommit(t *testing.T) {
	dir := t.TempDir()
	runGit(t, dir, "init")
	runGit(t, dir, "config", "user.email", "test@test.com")
	runGit(t, dir, "config", "user.name", "Test")

	if err := os.WriteFile(filepath.Join(dir, "a.go"), []byte("package a\n"), 0644); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "add", ".")
	runGit(t, dir, "commit", "-m", "first")

	// Uncommitted change → visible via the `git diff --numstat HEAD` slice even though
	// base..HEAD fails on the unreachable base.
	//
	// 未提交改动——即使 base..HEAD 因 base 不可达而失败，也能经 `git diff --numstat HEAD` 段看到。
	if err := os.WriteFile(filepath.Join(dir, "a.go"), []byte("package a\n\n// changed\n"), 0644); err != nil {
		t.Fatal(err)
	}

	diffStat, err := CollectGitData(dir, "master", "deadbeefdeadbeefdeadbeefdeadbeefdeadbeef")
	if err != nil {
		t.Fatalf("single-command failure must be tolerated, got: %v", err)
	}
	if !strings.Contains(diffStat, "a.go") {
		t.Errorf("working-tree change should still be collected when base is unreachable, got: %q", diffStat)
	}
}

// TestCollectGitData_UsesTaskBaseCommit guards the multi-task scope fix: when
// baseCommit is set, the diff must be scoped to baseCommit..HEAD (the task's own
// changes), NOT master..HEAD. Without this, a second task on the same feature
// branch inherits the first task's commits into its scope score.
func TestCollectGitData_UsesTaskBaseCommit(t *testing.T) {
	dir := t.TempDir()
	runGit(t, dir, "init")
	runGit(t, dir, "config", "user.email", "test@test.com")
	runGit(t, dir, "config", "user.name", "Test")

	// commit1: pre-task baseline (a.go)
	if err := os.WriteFile(filepath.Join(dir, "a.go"), []byte("package a\n"), 0644); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "add", ".")
	runGit(t, dir, "commit", "-m", "first")

	// Capture HEAD as the task's baseCommit (what task start would record).
	out, err := exec.Command("git", "-C", dir, "rev-parse", "--short", "HEAD").Output()
	if err != nil {
		t.Fatal(err)
	}
	baseCommit := strings.TrimSpace(string(out))

	// commit2: change made during the task (b.go)
	if err := os.WriteFile(filepath.Join(dir, "b.go"), []byte("package b\n"), 0644); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "add", ".")
	runGit(t, dir, "commit", "-m", "second")

	diffStat, err := CollectGitData(dir, "master", baseCommit)
	if err != nil {
		t.Fatalf("CollectGitData error: %v", err)
	}
	if !strings.Contains(diffStat, "b.go") {
		t.Errorf("diffStat should include task-time change b.go, got: %q", diffStat)
	}
	if strings.Contains(diffStat, "a.go") {
		t.Errorf("diffStat should NOT include pre-task baseline a.go when baseCommit is set (multi-task accumulation bug), got: %q", diffStat)
	}
}

func TestParseDiffStatLines(t *testing.T) {
	tests := []struct {
		input    string
		expected int
	}{
		{"3\t2\tmain.go", 5},
		{"10\t0\tmain.go", 10},
		{"", 0},
		{"no tab here", 0},
		{"1\t2\tfile1.go\n3\t4\tfile2.go", 10},
		// Binary files report "-\t-\t<path>" — no line counts, must be skipped.
		{"-\t-\timage.png", 0},
	}

	for _, tt := range tests {
		got := parseDiffStatLines(tt.input)
		if got != tt.expected {
			t.Errorf("parseDiffStatLines(%q) = %d, want %d", tt.input, got, tt.expected)
		}
	}
}

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %s: %v", args, string(out), err)
	}
}
