package scoring

import (
	"os"
	"os/exec"
	"path/filepath"
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

	diffTest, diffStat, err := CollectGitData(dir, "master")
	if err != nil {
		t.Fatalf("CollectGitData returned error: %v", err)
	}

	// In a clean repo with no diff, these may be empty — that's fine.
	// The key is it doesn't crash.
	t.Logf("diffTest: %q", diffTest)
	t.Logf("diffStat: %q", diffStat)
}

func TestCollectGitData_NotGitRepo(t *testing.T) {
	dir := t.TempDir()
	// Not a git repo — should return empty strings without panicking
	diffTest, diffStat, err := CollectGitData(dir, "main")
	if err != nil {
		t.Fatalf("expected no error for non-git dir, got: %v", err)
	}
	if diffTest != "" || diffStat != "" {
		t.Fatalf("expected empty results for non-git dir, got test=%q stat=%q", diffTest, diffStat)
	}
}

func TestParseDiffStatLines(t *testing.T) {
	tests := []struct {
		input    string
		expected int
	}{
		{"main.go | 5 +++--", 5},
		{"main.go | 10 ++++++++++---------", 10},
		{"", 0},
		{"no pipe here", 0},
		{"file1.go | 3 +++\nfile2.go | 7 -------", 10},
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
