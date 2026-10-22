package cli

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/MjxUpUp/Forge/internal/taskpipeline"
)

// reviewGit runs a git command in dir and fails the test on error.
//
// reviewGit 在 dir 里运行 git 命令，出错即让测试失败。
func reviewGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return strings.TrimSpace(string(out))
}

// reviewCommit writes files and commits them in dir.
//
// reviewCommit 在 dir 写入文件并提交。
func reviewCommit(t *testing.T, dir, msg string, files map[string]string) {
	t.Helper()
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	reviewGit(t, dir, "add", "-A")
	reviewGit(t, dir, "commit", "-q", "-m", msg)
}

// TestReviewWindowFiles_ExcludesMergedBaseFiles: with a review baseline, test files that arrived only by merging another branch after the baseline are not in the review window (so test-diff does not disclose them); the task's own test file still is.
//
// TestReviewWindowFiles_ExcludesMergedBaseFiles：有审查基线时，基线之后只经合并别的
// 分支带入的测试文件不进审查窗口（test-diff 不再披露它们）；本任务自己的测试文件
// 仍在窗口里。
func TestReviewWindowFiles_ExcludesMergedBaseFiles(t *testing.T) {
	dir := t.TempDir()
	reviewGit(t, dir, "init", "-q")
	reviewGit(t, dir, "config", "user.email", "t@t.com")
	reviewGit(t, dir, "config", "user.name", "T")
	reviewCommit(t, dir, "init", map[string]string{"main.go": "package main\n"})
	reviewGit(t, dir, "checkout", "-q", "-b", "task")
	reviewGit(t, dir, "branch", "base")
	baseline := reviewGit(t, dir, "rev-parse", "HEAD")

	reviewCommit(t, dir, "task own test", map[string]string{"own_test.go": "package main\n"})
	reviewGit(t, dir, "checkout", "-q", "base")
	reviewCommit(t, dir, "base test", map[string]string{"base_test.go": "package main\n"})
	reviewGit(t, dir, "checkout", "-q", "task")
	reviewGit(t, dir, "merge", "-q", "--no-edit", "base")

	got := reviewTestTouchedFiles(dir, &taskpipeline.TaskState{TaskRef: "t"}, baseline)
	if !slices.Contains(got, "own_test.go") {
		t.Errorf("task's own test file must stay in the review window, got %v", got)
	}
	if slices.Contains(got, "base_test.go") {
		t.Errorf("test file merged in from base must not be in the review window, got %v", got)
	}
}
